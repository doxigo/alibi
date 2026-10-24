package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	sets    []bool
}

func (f *fakeAutoTZ) on() bool { return f.enabled }
func (f *fakeAutoTZ) set(on bool) error {
	f.sets = append(f.sets, on)
	f.enabled = on
	return nil
}

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

// Install sets up the tray app: its program, a record of home, settings, sign-in, Settings > Apps,
// and a start now, with time zone sync on.
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
	var recorded setting
	b, _ := os.ReadFile(w.l.agent.home)
	if json.Unmarshal(b, &recorded) != nil || recorded != home {
		t.Errorf("home.json %s, want %v", b, home)
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

	// An update stops the running app before replacing it, and keeps the first home and the settings.
	w.zones.zoneNow, w.zones.regionNow = "W. Europe Standard Time", "NO" // where the agent moved Windows
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
	b, _ = os.ReadFile(w.l.agent.home)
	if json.Unmarshal(b, &recorded) != nil || recorded != home {
		t.Errorf("after the update, home.json %s, want the first home %v", b, home)
	}
	if loadSettings(w.l.agent.settings).Sound {
		t.Error("the update reset the settings")
	}
}

// Without time zone sync the tray app still runs; Windows goes back, and home stays recorded for later.
func TestWindowsInstallWithoutTimeZoneSync(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	install(w.sys, &bytes.Buffer{}, false, true)
	w.zones.zoneNow, w.zones.regionNow = "W. Europe Standard Time", "NO"
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
	if w.zones.zoneNow != home.Zone || w.zones.regionNow != home.Region {
		t.Errorf("Windows left on %s/%s", w.zones.zoneNow, w.zones.regionNow)
	}
	if _, err := os.Stat(w.l.agent.home); err != nil {
		t.Error("home.json gone: turning sync on again later would lose the real home")
	}
	if !strings.Contains(out.String(), "Windows doesn't follow your VPN: it stays on Iran Standard Time, region US") {
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

// With Windows' automatic time zone on, install offers to turn it off, and remembers that it did.
func TestWindowsInstallOffersToTurnOffAutomaticTimeZone(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.autoTZ.enabled = true
	w.answering(true)
	var out bytes.Buffer
	if err := install(w.sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	if len(w.asked) != 1 || len(w.autoTZ.sets) != 1 || w.autoTZ.sets[0] != false {
		t.Fatalf("asked %q, set %v", w.asked, w.autoTZ.sets)
	}
	if _, err := os.Stat(filepath.Join(w.l.dir, autoTimeZoneMarker)); err != nil {
		t.Error("didn't remember turning it off")
	}

	// Asked nothing (no console), it only warns.
	quiet := windowsSystem(t, fakeDownload(t, "program 1"))
	quiet.autoTZ.enabled = true
	out.Reset()
	install(quiet.sys, &out, false, true)
	if len(quiet.autoTZ.sets) != 0 || !strings.Contains(out.String(), "Set time zone automatically") {
		t.Errorf("set %v; output %s", quiet.autoTZ.sets, out.String())
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
	w.zones.zoneNow, w.zones.regionNow = "West Asia Standard Time", "TJ" // where the agent moved Windows
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
	// It turned the automatic time zone off at install, so it offers to turn it back on.
	if len(w.autoTZ.sets) != 2 || !w.autoTZ.sets[1] {
		t.Errorf("automatic time zone set %v, want off then on again", w.autoTZ.sets)
	}
	if _, err := os.Stat(w.l.dir); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(w.l.dir)
		t.Errorf("%s still there: %v", w.l.dir, entries)
	}
	for _, want := range []string{"Put Windows back on Iran Standard Time, region US", "Left Microsoft Edge alone", "Turned Windows' automatic time zone back on", "Removed Alibi Companion"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// A quiet uninstall asks nothing.
func TestWindowsQuietUninstall(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	w.autoTZ.enabled = true
	w.answering(true)
	install(w.sys, &bytes.Buffer{}, false, true)
	w.asked = nil
	if err := uninstall(w.sys, &bytes.Buffer{}, true); err != nil {
		t.Fatal(err)
	}
	if len(w.asked) != 0 {
		t.Errorf("asked %q", w.asked)
	}
}

func TestWindowsStatus(t *testing.T) {
	w := windowsSystem(t, fakeDownload(t, "program 1"))
	var before bytes.Buffer
	status(w.sys, &before, fakeLookup)
	for _, want := range []string{"Not installed", "not registered", "Tray app: not set up", "Follows Alibi's location: off"} {
		if !strings.Contains(before.String(), want) {
			t.Errorf("before install lacks %q:\n%s", want, before.String())
		}
	}
	install(w.sys, &bytes.Buffer{}, false, true)
	w.zones.zoneNow, w.zones.regionNow = "W. Europe Standard Time", "NO"
	var after bytes.Buffer
	status(w.sys, &after, fakeLookup)
	for _, want := range []string{
		"Tray app: running",
		"Follows Alibi's location: on",
		"Windows now:  W. Europe Standard Time, region NO, which browsers read as Europe/Oslo",
		"Before sync:  Iran Standard Time, region US",
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
