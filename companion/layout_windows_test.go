package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeRegistry stands in for HKCU, as key|name -> value. Registry keys and names aren't case-sensitive.
type fakeRegistry map[string]string

func regKey(key, name string) string { return strings.ToLower(key) + "|" + strings.ToLower(name) }

func (r fakeRegistry) value(key, name string) (string, bool, error) {
	v, ok := r[regKey(key, name)]
	return v, ok, nil
}
func (r fakeRegistry) setValue(key, name, value string) error {
	r[regKey(key, name)] = value
	return nil
}
func (r fakeRegistry) setNumber(key, name string, value uint32) error {
	r[regKey(key, name)] = strconv.FormatUint(uint64(value), 10)
	return nil
}
func (r fakeRegistry) deleteValue(key, name string) error {
	delete(r, regKey(key, name))
	return nil
}
func (r fakeRegistry) deleteKey(key string) error {
	for k := range r {
		if strings.HasPrefix(k, strings.ToLower(key)+"|") {
			delete(r, k)
		}
	}
	return nil
}

// fakeAgentControl records what install and uninstall ask of the tray app.
type fakeAgentControl struct {
	on      bool
	started []string
	stops   int
}

func (a *fakeAgentControl) running() bool { return a.on }
func (a *fakeAgentControl) start(exe string) error {
	a.started = append(a.started, exe)
	a.on = true
	return nil
}
func (a *fakeAgentControl) stop() error {
	a.stops++
	a.on = false
	return nil
}

// fakeAutoTZ is Windows' "Set time zone automatically".
type fakeAutoTZ struct {
	enabled bool
	opened  int // times Settings > Date & time was opened
}

func (f *fakeAutoTZ) on() bool      { return f.enabled }
func (f *fakeAutoTZ) openSettings() { f.opened++ }

type windowsTest struct {
	sys    system
	reg    fakeRegistry
	zones  *fakeZones
	agent  *fakeAgentControl
	autoTZ *fakeAutoTZ
	asked  []string
	l      layout
}

func windowsSystem(t *testing.T, self string) *windowsTest {
	t.Helper()
	base := t.TempDir()
	w := &windowsTest{reg: fakeRegistry{}, zones: atHome(), agent: &fakeAgentControl{}, autoTZ: &fakeAutoTZ{}}
	w.sys = system{
		getenv:    func(k string) string { return map[string]string{"LOCALAPPDATA": base}[k] },
		self:      func() (string, error) { return self, nil },
		registry:  w.reg,
		zones:     w.zones,
		agent:     w.agent,
		autoTZ:    w.autoTZ,
		makeAgent: func(p []byte) ([]byte, error) { return append([]byte("windowed "), p...), nil },
	}
	l, err := currentLayout(w.sys)
	if err != nil {
		t.Fatal(err)
	}
	w.l = l
	return w
}

// agentMoved sets Windows the way a running agent does, and records it in home.json as the agent
// would: home is how Windows was set, and the agent holds it on to.
func (w *windowsTest) agentMoved(t *testing.T, to setting) {
	t.Helper()
	before := setting{w.zones.zoneNow, w.zones.regionNow}
	if err := saveHome(w.l.agent.home, homeState{setting: before, Held: &to}); err != nil {
		t.Fatal(err)
	}
	w.zones.zoneNow, w.zones.regionNow = to.Zone, to.Region
}

// answering makes the console answer every question with yes or no, and records them.
func (w *windowsTest) answering(yes bool) {
	w.sys.ask = func(q string) bool {
		w.asked = append(w.asked, q)
		return yes
	}
}

const chromeKey = `Software\Google\Chrome\NativeMessagingHosts\` + hostName

func TestWindowsLayout(t *testing.T) {
	l := windowsSystem(t, "").l
	if filepath.Base(filepath.Dir(l.dir)) != "Programs" || filepath.Base(l.dir) != "Alibi Companion" || filepath.Base(l.exe) != "alibi-companion.exe" {
		t.Errorf("installs to %s", l.exe)
	}
	if l.agent == nil || filepath.Dir(l.agent.exe) != l.dir || filepath.Dir(l.agent.settings) != l.dir {
		t.Errorf("agent at %v", l.agent)
	}
}

func TestWindowsInstall(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	var out bytes.Buffer
	if err := install(w.sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, w.l.exe, "program 1")
	checkManifest(t, w.l.manifest, w.l.exe)
	for _, b := range windowsBrowsers {
		key := `Software\` + b.key + `\NativeMessagingHosts\` + hostName
		if v, _, _ := w.reg.value(key, ""); v != w.l.manifest {
			t.Errorf("%s = %q, want %q", key, v, w.l.manifest)
		}
	}
	if !strings.Contains(out.String(), "Registered with Google Chrome, Microsoft Edge, Brave, Chromium") {
		t.Errorf("output: %s", out.String())
	}
}

// Install sets up the tray app: its program, settings, sign-in, Settings > Apps, and a start now,
// with time zone sync on. It leaves the time zone, and home, to the agent.
func TestWindowsInstallSetsUpTheTrayApp(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	var out bytes.Buffer
	if err := install(w.sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, w.l.agent.exe, "windowed program 1")
	if run, _, _ := w.reg.value(runKey, runName); run != `"`+w.l.agent.exe+`" agent` {
		t.Errorf("sign-in entry %q", run)
	}
	for name, want := range map[string]string{
		"DisplayName":     "Alibi Companion",
		"DisplayVersion":  version,
		"UninstallString": `"` + w.l.exe + `" uninstall`,
		"DisplayIcon":     `"` + w.l.agent.exe + `",0`,
		"InstallLocation": w.l.dir,
		"NoModify":        "1",
	} {
		if got, _, _ := w.reg.value(uninstallKey, name); got != want {
			t.Errorf("Settings > Apps %s = %q, want %q", name, got, want)
		}
	}
	if len(w.agent.started) != 1 || w.agent.started[0] != w.l.agent.exe {
		t.Errorf("started %v", w.agent.started)
	}
	if _, err := os.Stat(w.l.agent.home); !os.IsNotExist(err) {
		t.Error("install recorded home: only the agent holding the clock does that")
	}
	if !loadSettings(w.l.agent.settings).Sync {
		t.Error("time zone sync off after a plain install")
	}
	if len(w.zones.calls) > 0 {
		t.Errorf("install itself changed the time zone: %v (that's the agent's job)", w.zones.calls)
	}
	for _, want := range []string{"runs in the tray", "Windows follows your VPN's time zone", "Iran Standard Time, region US"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	// An update stops the running app before replacing it, and keeps home and the settings.
	w.agentMoved(t, setting{"W. Europe Standard Time", "NO"})
	s := loadSettings(w.l.agent.settings)
	s.Sound = false
	saveSettings(w.l.agent.settings, s)
	if err := install(w.sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	// Every install asks a running app to stop first (the first had none to stop).
	if w.agent.stops != 2 || len(w.agent.started) != 2 || !w.agent.on {
		t.Errorf("update: %d stops, %d starts, running %v", w.agent.stops, len(w.agent.started), w.agent.on)
	}
	if h, err := readHome(w.l.agent.home); err != nil || h.setting != home || h.Held == nil {
		t.Errorf("after the update, home.json %+v %v, want home %v, still held", h, err, home)
	}
	if loadSettings(w.l.agent.settings).Sound {
		t.Error("the update reset the settings")
	}
}

// Without time zone sync the tray app still runs. Install itself changes nothing: the agent puts
// Windows back home as it starts (TestAnAgentStartingWithFollowingOffLetsGo), and home stays recorded.
func TestWindowsInstallWithoutTimeZoneSync(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	install(w.sys, &bytes.Buffer{}, false, true)
	w.agentMoved(t, setting{"W. Europe Standard Time", "NO"})
	w.zones.calls = nil
	var out bytes.Buffer
	if err := install(w.sys, &out, false, false); err != nil {
		t.Fatal(err)
	}
	if loadSettings(w.l.agent.settings).Sync {
		t.Error("time zone sync still on")
	}
	if !w.agent.on {
		t.Error("the tray app isn't running")
	}
	if len(w.zones.calls) > 0 {
		t.Errorf("install changed the time zone: %v", w.zones.calls)
	}
	if _, err := os.Stat(w.l.agent.home); err != nil {
		t.Error("home.json gone: turning sync on again later would lose the real home")
	}
	if !strings.Contains(out.String(), "Windows doesn't follow your VPN: it goes back to Iran Standard Time, region US") {
		t.Errorf("output: %s", out.String())
	}
}

// An update keeps the choice made in the tray: following switched off there stays off.
func TestWindowsUpdateKeepsFollowingOff(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	install(w.sys, &bytes.Buffer{}, false, true)
	s := loadSettings(w.l.agent.settings)
	s.Sync = false // switched off in the tray
	saveSettings(w.l.agent.settings, s)
	var out bytes.Buffer
	if err := install(w.sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	if loadSettings(w.l.agent.settings).Sync {
		t.Error("an update turned following back on")
	}
	if !strings.Contains(out.String(), "Windows doesn't follow your VPN") {
		t.Errorf("output: %s", out.String())
	}
}

// With Windows' automatic time zone on, install offers to open Date & time settings, where you
// switch it off. It never changes the setting itself.
func TestWindowsInstallOffersToTurnOffAutomaticTimeZone(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.autoTZ.enabled = true
	w.answering(true)
	var out bytes.Buffer
	if err := install(w.sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	if len(w.asked) != 1 || w.autoTZ.opened != 1 || !w.autoTZ.enabled {
		t.Fatalf("asked %q, opened Settings %d times, automatic time zone on %v", w.asked, w.autoTZ.opened, w.autoTZ.enabled)
	}
	if entries, _ := os.ReadDir(w.l.dir); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() == "autotz-was-on" }) {
		t.Error("left a marker: there's nothing to undo")
	}

	// Asked nothing (no console), it only warns.
	quiet := windowsSystem(t, fakeDownload(t, "program 1"))
	quiet.autoTZ.enabled = true
	out.Reset()
	install(quiet.sys, &out, false, true)
	if quiet.autoTZ.opened != 0 || !strings.Contains(out.String(), "Set time zone automatically") {
		t.Errorf("opened Settings %d times; output %s", quiet.autoTZ.opened, out.String())
	}
}

func TestWindowsReinstallReplacesTheProgram(t *testing.T) {
	self := fakeDownload(t, "program 1")
	w := windowsSystem(t, self)
	if err := install(w.sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(self, []byte("program 2"), 0o755)
	if err := install(w.sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, w.l.exe, "program 2")
	checkProgram(t, w.l.agent.exe, "windowed program 2")

	// Run again from the installed copy: nothing to copy, and the registrations stay.
	w.sys.self = func() (string, error) { return strings.ToUpper(w.l.exe), nil }
	if err := install(w.sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, w.l.exe, "program 2")
}

func TestWindowsInstallLeavesAnotherProgramAlone(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.reg.setValue(chromeKey, "", `C:\Program Files\Whereabouts\host.json`)
	err := install(w.sys, &bytes.Buffer{}, false, true)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("install took over another program's registration (err %v)", err)
	}
	if _, err := os.Stat(w.l.exe); err == nil {
		t.Error("copied the program even though install stopped")
	}
	if len(w.agent.started) > 0 {
		t.Error("started the tray app even though install stopped")
	}
	if err := install(w.sys, &bytes.Buffer{}, true, true); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := w.reg.value(chromeKey, ""); v != w.l.manifest {
		t.Errorf("--force left %s = %q", chromeKey, v)
	}
}

func TestWindowsUninstall(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.autoTZ.enabled = true
	w.answering(true)
	if err := install(w.sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	w.agentMoved(t, setting{"West Asia Standard Time", "TJ"})
	w.autoTZ.enabled = false // you switched it off in Settings
	edge := `Software\Microsoft\Edge\NativeMessagingHosts\` + hostName
	w.reg.setValue(edge, "", `C:\Other\host.json`) // someone else's since
	var out bytes.Buffer
	if err := uninstall(w.sys, &out, false); err != nil {
		t.Fatal(err)
	}
	if len(w.reg) != 1 || w.reg[regKey(edge, "")] != `C:\Other\host.json` {
		t.Errorf("registry after uninstall: %v, want only the other program's Edge key", w.reg)
	}
	if w.agent.on || w.zones.zoneNow != home.Zone || w.zones.regionNow != home.Region {
		t.Errorf("app running %v, Windows on %s/%s", w.agent.on, w.zones.zoneNow, w.zones.regionNow)
	}
	// With the automatic time zone off, it reminds you, and offers Settings again.
	if w.autoTZ.opened != 2 {
		t.Errorf("opened Settings %d times, want at install and at uninstall", w.autoTZ.opened)
	}
	if _, err := os.Stat(w.l.dir); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(w.l.dir)
		t.Errorf("%s still there: %v", w.l.dir, entries)
	}
	for _, want := range []string{"Put Windows back on Iran Standard Time, region US", "Left Microsoft Edge alone", "switch it back on in Settings", "Removed Alibi Companion"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// A quiet uninstall asks nothing, but still reminds you about the automatic time zone.
func TestWindowsQuietUninstall(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.autoTZ.enabled = true
	w.answering(true)
	install(w.sys, &bytes.Buffer{}, false, true)
	w.asked = nil
	w.autoTZ.enabled = false
	var out bytes.Buffer
	if err := uninstall(w.sys, &out, true); err != nil {
		t.Fatal(err)
	}
	if len(w.asked) != 0 {
		t.Errorf("asked %q", w.asked)
	}
	if !strings.Contains(out.String(), "switch it back on in Settings") {
		t.Errorf("output: %s", out.String())
	}
}

func TestWindowsStatus(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	var before bytes.Buffer
	status(w.sys, &before, fakeLookup)
	for _, want := range []string{"Not installed", "not registered", "Tray app: not set up", "Keeps Windows on your VPN's time zone: off", "Follows Alibi's location: off"} {
		if !strings.Contains(before.String(), want) {
			t.Errorf("before install lacks %q:\n%s", want, before.String())
		}
	}
	install(w.sys, &bytes.Buffer{}, false, true)
	w.agentMoved(t, setting{"W. Europe Standard Time", "NO"})
	var after bytes.Buffer
	status(w.sys, &after, fakeLookup)
	for _, want := range []string{
		"Tray app: running",
		"Keeps Windows on your VPN's time zone: on",
		"Follows Alibi's location: off",
		"Windows now:  W. Europe Standard Time, region NO, which browsers read as Europe/Oslo",
		"Home:         Iran Standard Time, region US",
		"Asia/Tokyo (ja-JP, 35.6895, 139.6917)",
	} {
		if !strings.Contains(after.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, after.String())
		}
	}
	if strings.Count(after.String(), " registered\n") != len(windowsBrowsers) {
		t.Errorf("after install: %s", after.String())
	}
}
