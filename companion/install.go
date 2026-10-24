package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// system is what install, uninstall and status touch, so tests can point them at a scratch folder,
// a fake registry and a fake time zone.
type system struct {
	getenv   func(string) string
	self     func() (string, error) // the running program
	registry registry               // Windows only
	zones    zoneSystem             // Windows only
	agent    agentController        // Windows only
	// Turns the console program into the agent's windowed one (guiSubsystem).
	makeAgent func([]byte) ([]byte, error)
	autoTZ    autoTimeZoneControl // Windows only
	// Asks a yes-or-no question on the console; nil when there's no one to ask.
	ask func(question string) bool
}

// autoTimeZoneControl is Windows' "Set time zone automatically".
type autoTimeZoneControl interface {
	on() bool
	set(on bool) error // asks for an administrator's approval
}

func defaultSystem() system {
	return system{
		getenv: os.Getenv,
		self: func() (string, error) {
			exe, err := os.Executable()
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(exe)
		},
		registry:  userRegistry(),
		zones:     systemZones(),
		agent:     agentControl(),
		makeAgent: guiSubsystem,
		autoTZ:    autoTimeZoneSwitch(),
		ask:       consoleAsk(),
	}
}

// registry is the slice of the Windows registry install needs: string values under HKCU.
// The name "" is a key's default value.
type registry interface {
	value(key, name string) (value string, found bool, err error)
	setValue(key, name, value string) error
	setNumber(key, name string, value uint32) error
	deleteValue(key, name string) error
	deleteKey(key string) error
}

// agentController starts and stops the time zone agent.
type agentController interface {
	running() bool
	start(exe string) error
	stop() error // asks it to quit and waits; it leaves the system where it is
}

// layout is where the companion lives on this system and where each browser looks for it.
type layout struct {
	dir      string // the companion's folder
	exe      string // the installed program
	manifest string // Windows: the one host manifest the registry keys point at; elsewhere, none
	owner    string // what one of our registrations points at: the manifest on Windows, the program elsewhere
	targets  []target
	agent    *agentFiles // Windows only: the time zone agent
}

// Where Windows starts programs at sign-in, for this user only, and the agent's entry there.
const (
	runKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
	runName = "Alibi Companion"
)

// agentFiles is where the time zone agent keeps itself and what it knows.
type agentFiles struct {
	exe      string // the windowed copy that Windows starts at sign-in
	home     string // home.json: how the system was before the agent first changed it
	state    string // agent.json: its latest place, for the host
	log      string // agent.log
	settings string // settings.json: what the tray lets you choose
	wanted   string // wanted.json: Alibi's All sites cover, as the host last heard it
}

func newAgentFiles(dir string) *agentFiles {
	return &agentFiles{
		exe:      filepath.Join(dir, "alibi-companion-agent.exe"),
		home:     filepath.Join(dir, "home.json"),
		state:    agentStatePath(dir),
		log:      filepath.Join(dir, "agent.log"),
		settings: filepath.Join(dir, "settings.json"),
		wanted:   filepath.Join(dir, "wanted.json"),
	}
}

// target is one browser's registration of the host.
type target interface {
	browser() string
	// found reports whether the browser is there to register with.
	found() bool
	// owner is what the registration points at now, or "" if there's none.
	owner() (string, error)
	register() error
	unregister() error
}

// hostManifest is the native messaging manifest that lets Alibi, and only Alibi, start exe.
func hostManifest(exe string) []byte {
	b, _ := json.MarshalIndent(struct {
		Name           string   `json:"name"`
		Description    string   `json:"description"`
		Path           string   `json:"path"`
		Type           string   `json:"type"`
		AllowedOrigins []string `json:"allowed_origins"`
	}{hostName, "Alibi Companion: tells Alibi when your VPN exit changes", exe, "stdio", []string{extensionOrigin}}, "", "  ")
	return append(b, '\n')
}

// programIn reads the program a host manifest starts.
func programIn(manifest string) (string, error) {
	b, err := os.ReadFile(manifest)
	if err != nil {
		return "", err
	}
	var m struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(b, &m); err != nil || m.Path == "" {
		return "", fmt.Errorf("%s isn't a host manifest", manifest)
	}
	return m.Path, nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// install puts the companion in place and registers it. On Windows it also sets up the tray app,
// which starts now and at every sign-in; with timezone, it keeps Windows on the VPN's time zone.
func install(sys system, out io.Writer, force, timezone bool) error {
	l, err := currentLayout(sys)
	if err != nil {
		return err
	}
	var targets []target
	for _, t := range l.targets {
		if t.found() {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return errors.New("found no supported browser for this user. Open Chrome, Chromium, Brave, Edge or Vivaldi once, then run install again")
	}
	// Another program may answer to the same name (Whereabouts, one day). Don't take its place unasked.
	if !force {
		for _, t := range targets {
			if owner, _ := t.owner(); owner != "" && !samePath(owner, l.owner) {
				return fmt.Errorf("%s already starts another program for %s (%s). Run install --force to replace it", t.browser(), hostName, owner)
			}
		}
	}

	// A running agent holds its program open; it starts again from the new copy below.
	if l.agent != nil {
		if err := sys.agent.stop(); err != nil {
			return err
		}
	}
	if err := placeProgram(sys, l); err != nil {
		return fmt.Errorf("couldn't copy the companion to %s: %w", l.dir, err)
	}
	if l.manifest != "" {
		if err := writeFileAtomic(l.manifest, hostManifest(l.exe), 0o644); err != nil {
			return err
		}
	}
	var registered []string
	for _, t := range targets {
		if err := t.register(); err != nil {
			return fmt.Errorf("couldn't register with %s: %w", t.browser(), err)
		}
		registered = append(registered, t.browser())
	}
	fmt.Fprintf(out, "Installed Alibi Companion %s in %s\n", version, l.dir)
	fmt.Fprintf(out, "Registered with %s\n", strings.Join(registered, ", "))

	if l.agent != nil {
		if err := setUpAgent(sys, l, out, timezone); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "\nIn Alibi, choose \"Match my IP address\". Your browser starts the companion by itself from now on.")
	fmt.Fprintln(out, "If the browser is already open, Alibi finds the companion within a minute.")
	return nil
}

// setUpAgent puts the tray app in place, records how the time zone is set now so uninstalling can
// put it back, lists the app in Settings > Apps, and starts it now and at every sign-in.
func setUpAgent(sys system, l layout, out io.Writer, timezone bool) error {
	program, err := os.ReadFile(l.exe)
	if err != nil {
		return err
	}
	windowed, err := sys.makeAgent(program)
	if err != nil {
		return fmt.Errorf("couldn't make the tray app: %w", err)
	}
	if err := replaceProgram(l.dir, l.agent.exe, windowed); err != nil {
		return fmt.Errorf("couldn't copy the tray app: %w", err)
	}
	home, err := loadHome(sys.zones, l.agent.home)
	if err != nil {
		return fmt.Errorf("couldn't record how the time zone is set now: %w", err)
	}
	// A first install follows Alibi; an update keeps whatever you chose in the tray since. Only
	// --no-timezone turns following off.
	_, err = os.Stat(l.agent.settings)
	first := errors.Is(err, os.ErrNotExist)
	s := loadSettings(l.agent.settings)
	if first || !timezone {
		s.Sync = timezone
	}
	timezone = s.Sync
	if err := saveSettings(l.agent.settings, s); err != nil {
		return err
	}
	if !timezone {
		if _, err := apply(sys.zones, home); err != nil {
			return fmt.Errorf("couldn't put the time zone back: %w", err)
		}
	}
	if err := registerApp(sys, l); err != nil {
		return fmt.Errorf("couldn't list Alibi Companion in Settings > Apps: %w", err)
	}
	if err := sys.registry.setValue(runKey, runName, `"`+l.agent.exe+`" agent`); err != nil {
		return fmt.Errorf("couldn't have Windows start Alibi Companion at sign-in: %w", err)
	}

	fmt.Fprintln(out, "\nAlibi Companion now runs in the tray, and starts every time you sign in.")
	if timezone {
		fmt.Fprintln(out, "Windows follows Alibi's location: the city or zone you choose for all sites, or your VPN's.")
		fmt.Fprintf(out, "Quitting or uninstalling puts it back to %s, region %s.\n", home.Zone, home.Region)
		// Windows' own "Set time zone automatically" would move it back to where you really are.
		if sys.autoTZ != nil && sys.autoTZ.on() {
			const question = "\nWindows' \"Set time zone automatically\" is on, and it would move the zone back.\n" +
				"Turn it off now? Windows will ask for your approval. [Y/n] "
			switch {
			case sys.ask == nil || !sys.ask(question):
				fmt.Fprintln(out, autoTimeZoneWarning)
			case sys.autoTZ.set(false) != nil:
				fmt.Fprintln(out, "Windows' automatic time zone is still on."+autoTimeZoneWarning)
			default:
				os.WriteFile(filepath.Join(l.dir, autoTimeZoneMarker), nil, 0o644)
				fmt.Fprintln(out, "Turned off Windows' automatic time zone. Uninstalling offers to turn it back on.")
			}
		}
	} else {
		fmt.Fprintf(out, "Windows doesn't follow Alibi: it stays on %s, region %s. Turn it on from the tray.\n", home.Zone, home.Region)
	}
	if err := sys.agent.start(l.agent.exe); err != nil {
		return fmt.Errorf("couldn't start Alibi Companion: %w", err)
	}
	return nil
}

const autoTimeZoneWarning = "\nWindows' \"Set time zone automatically\" is on, and it can move the zone back.\n" +
	"Turn it off from the tray, or in Settings > Time & language > Date & time."

// Left in the companion's folder once the companion has turned off Windows' automatic time zone,
// so uninstalling can offer to turn it back on.
const autoTimeZoneMarker = "autotz-was-on"

// The app's entry in Settings > Apps > Installed apps, for this user.
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AlibiCompanion`

func registerApp(sys system, l layout) error {
	size := uint32(0)
	for _, f := range []string{l.exe, l.agent.exe} {
		if info, err := os.Stat(f); err == nil {
			size += uint32(info.Size() / 1024)
		}
	}
	for _, v := range [][2]string{
		{"DisplayName", "Alibi Companion"},
		{"DisplayVersion", version},
		{"Publisher", "Alibi"},
		{"Comments", "Keeps Windows on your VPN's time zone and tells Alibi where you are"},
		{"DisplayIcon", `"` + l.agent.exe + `",0`},
		{"InstallLocation", l.dir},
		{"InstallDate", time.Now().Format("20060102")},
		{"UninstallString", `"` + l.exe + `" uninstall`},
		{"QuietUninstallString", `"` + l.exe + `" uninstall --quiet`},
		{"URLInfoAbout", "https://github.com/doxigo/alibi"},
	} {
		if err := sys.registry.setValue(uninstallKey, v[0], v[1]); err != nil {
			return err
		}
	}
	for name, value := range map[string]uint32{"NoModify": 1, "NoRepair": 1, "EstimatedSize": size} {
		if err := sys.registry.setNumber(uninstallKey, name, value); err != nil {
			return err
		}
	}
	return nil
}

// removeAgent stops the tray app, puts the time zone back how it was before the first install, and
// removes the app from sign-in and from Settings > Apps.
func removeAgent(sys system, l layout, out io.Writer, quiet bool) error {
	if err := sys.agent.stop(); err != nil {
		return err
	}
	home, restored, err := restoreHome(sys.zones, l.agent.home)
	if err != nil {
		return fmt.Errorf("couldn't put the time zone back: %w", err)
	}
	if restored {
		fmt.Fprintf(out, "Put Windows back on %s, region %s\n", home.Zone, home.Region)
	}
	marker := filepath.Join(l.dir, autoTimeZoneMarker)
	if _, err := os.Stat(marker); err == nil {
		const question = "Alibi Companion turned off Windows' automatic time zone. Turn it back on? Windows will ask for your approval. [Y/n] "
		if !quiet && sys.ask != nil && sys.autoTZ != nil && sys.ask(question) {
			if err := sys.autoTZ.set(true); err != nil {
				fmt.Fprintln(out, "Windows' automatic time zone stays off. Turn it on in Settings > Time & language > Date & time.")
			} else {
				fmt.Fprintln(out, "Turned Windows' automatic time zone back on")
			}
		}
	}
	for _, v := range []string{runName} {
		if err := sys.registry.deleteValue(runKey, v); err != nil {
			return err
		}
	}
	if err := sys.registry.deleteKey(uninstallKey); err != nil {
		return err
	}
	for _, f := range []string{l.agent.state, l.agent.log, l.agent.settings, l.agent.wanted, marker} {
		os.Remove(f)
	}
	if err := os.Remove(l.agent.exe); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Rename(l.agent.exe, oldName(l.agent.exe))
	}
	return nil
}

func uninstall(sys system, out io.Writer, quiet bool) error {
	l, err := currentLayout(sys)
	if err != nil {
		return err
	}
	if l.agent != nil {
		if err := removeAgent(sys, l, out, quiet); err != nil {
			return err
		}
	}
	for _, t := range l.targets {
		owner, _ := t.owner()
		switch {
		case owner == "":
		case !samePath(owner, l.owner):
			fmt.Fprintf(out, "Left %s alone: its %s starts another program (%s)\n", t.browser(), hostName, owner)
		default:
			if err := t.unregister(); err != nil {
				return fmt.Errorf("couldn't unregister from %s: %w", t.browser(), err)
			}
			fmt.Fprintf(out, "Unregistered from %s\n", t.browser())
		}
	}
	if l.manifest != "" {
		if err := os.Remove(l.manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	removeOld(l.dir)
	if err := os.Remove(l.exe); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Windows won't delete a program that's running, here or in a browser. It will rename one.
		os.Rename(l.exe, oldName(l.exe))
	}
	os.Remove(l.dir) // only if it's empty now
	if _, err := os.Stat(l.dir); err == nil {
		fmt.Fprintf(out, "\nYour browser is still running the companion, so its folder can't be deleted yet. Close the browser, then delete %s\n", l.dir)
		return nil
	}
	fmt.Fprintln(out, "Removed Alibi Companion")
	return nil
}

func status(sys system, out io.Writer, lookup func(context.Context) (cover, error)) error {
	fmt.Fprintf(out, "Alibi Companion %s\n", version)
	l, err := currentLayout(sys)
	if err != nil {
		return err
	}
	if _, err := os.Stat(l.exe); err == nil {
		fmt.Fprintf(out, "Installed: %s\n\n", l.exe)
	} else {
		fmt.Fprint(out, "Not installed. Run: alibi-companion install\n\n")
	}
	for _, t := range l.targets {
		state := "not registered"
		owner, err := t.owner()
		switch {
		case err != nil:
			state = fmt.Sprintf("unreadable registration: %v", err)
		case owner != "" && samePath(owner, l.owner):
			state = "registered"
		case owner != "":
			state = "starts another program: " + owner
		case !t.found():
			state = "not installed"
		}
		fmt.Fprintf(out, "  %-20s %s\n", t.browser(), state)
	}
	if l.agent != nil {
		agentStatus(sys, l, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	place, err := lookup(ctx)
	if err != nil {
		fmt.Fprintf(out, "\nCouldn't ask GeoJS where the connection appears to be: %v\n", err)
		return nil
	}
	fmt.Fprintf(out, "\nYour connection appears to be in %s (%s", place.Timezone, place.Locale)
	if place.Lat != nil {
		fmt.Fprintf(out, ", %.4f, %.4f", *place.Lat, *place.Lon)
	}
	fmt.Fprintln(out, ")")
	return nil
}

func agentStatus(sys system, l layout, out io.Writer) {
	_, atSignIn, _ := sys.registry.value(runKey, runName)
	running := sys.agent.running()
	switch {
	case running:
		fmt.Fprintln(out, "\nTray app: running")
	case atSignIn:
		fmt.Fprintln(out, "\nTray app: not running now (it starts at sign-in)")
	default:
		fmt.Fprintln(out, "\nTray app: not set up")
	}
	sync := loadSettings(l.agent.settings).Sync && (running || atSignIn)
	if sync {
		fmt.Fprintln(out, "Follows Alibi's location: on")
	} else {
		fmt.Fprintln(out, "Follows Alibi's location: off")
	}
	if zone, err := sys.zones.zone(); err == nil {
		region, _ := sys.zones.region()
		reads, _ := sys.zones.hostZone()
		fmt.Fprintf(out, "  Windows now:  %s, region %s, which browsers read as %s\n", zone, region, reads)
	}
	if b, err := os.ReadFile(l.agent.home); err == nil {
		var home setting
		if json.Unmarshal(b, &home) == nil {
			fmt.Fprintf(out, "  Before sync:  %s, region %s\n", home.Zone, home.Region)
		}
	}
	if sync && sys.autoTZ != nil && sys.autoTZ.on() {
		fmt.Fprintln(out, autoTimeZoneWarning)
	}
}

// placeProgram copies the running program to l.exe, unless it's already running from there.
func placeProgram(sys system, l layout) error {
	self, err := sys.self()
	if err != nil {
		return err
	}
	if samePath(self, l.exe) {
		return nil
	}
	program, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return replaceProgram(l.dir, l.exe, program)
}

// replaceProgram writes program to exe, even while the old one runs.
func replaceProgram(dir, exe string, program []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	removeOld(dir)
	next := exe + ".new"
	if err := os.WriteFile(next, program, 0o755); err != nil {
		return err
	}
	// A browser may be running the old copy. Windows won't replace a running program, but it will
	// rename it out of the way. Elsewhere the rename below replaces it in one step.
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(exe); err == nil {
			if err := os.Rename(exe, oldName(exe)); err != nil {
				os.Remove(next)
				return err
			}
		}
	}
	if err := os.Rename(next, exe); err != nil {
		os.Remove(next)
		return err
	}
	removeOld(dir) // the copy set aside goes now, unless something still runs it
	return nil
}

func oldName(exe string) string {
	return fmt.Sprintf("%s.%d.old", exe, time.Now().UnixNano())
}

// removeOld deletes copies set aside by an earlier update or uninstall, once nothing runs them.
func removeOld(dir string) {
	old, _ := filepath.Glob(filepath.Join(dir, "*.old"))
	for _, f := range old {
		os.Remove(f)
	}
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	next := path + ".new"
	if err := os.WriteFile(next, data, mode); err != nil {
		return err
	}
	if err := os.Rename(next, path); err != nil {
		os.Remove(next)
		return err
	}
	return nil
}
