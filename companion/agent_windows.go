package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	winreg "golang.org/x/sys/windows/registry"
)

var (
	// Windows' own ICU, the library browsers use to name zones too (Windows 10 1903 and later).
	icu      = windows.NewLazySystemDLL("icu.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")

	ucalWindowsZone   = icu.NewProc("ucal_getWindowsTimeZoneID")
	ucalIANAZone      = icu.NewProc("ucal_getTimeZoneIDForWindowsID")
	ucalHostZone      = icu.NewProc("ucal_getHostTimeZone")
	getUserGeoName    = kernel32.NewProc("GetUserDefaultGeoName")
	setUserGeoName    = kernel32.NewProc("SetUserGeoName")
	sendMessageTimout = user32.NewProc("SendMessageTimeoutW")
)

// syncSupported says whether this Windows has every function time zone sync calls. LazyProc.Call
// panics on one that's missing (ucal_getHostTimeZone is newer than the rest of ICU), so this is asked
// before anything reads or moves the zone, and only once: the answer can't change while it runs.
var syncSupported = sync.OnceValue(func() error {
	return allFound(ucalWindowsZone, ucalIANAZone, ucalHostZone, getUserGeoName, setUserGeoName)
})

func allFound(procs ...*windows.LazyProc) error {
	for _, p := range procs {
		if err := p.Find(); err != nil {
			return fmt.Errorf("time zone sync isn't available on this Windows: %w", err) // names what's missing
		}
	}
	return nil
}

const (
	// One agent per user, in that user's session.
	agentMutexName = `Local\AlibiCompanionAgent`
	agentQuitName  = `Local\AlibiCompanionAgentQuit`
	// Windows has one time zone for everyone signed in: one agent at a time keeps it, whoever's it is.
	clockMutexName = `Global\AlibiCompanionClock`
)

// clockLock is the machine's time zone, claimed by one agent at a time. It's held for as long as its
// handle stays open, and every session sees it.
type clockLock struct {
	name   string
	handle windows.Handle
}

// claim takes the clock, unless another agent has it: ERROR_ALREADY_EXISTS, or ERROR_ACCESS_DENIED when
// that's another user's.
func (c *clockLock) claim() bool {
	if c.handle != 0 {
		return true
	}
	name, _ := windows.UTF16PtrFromString(c.name)
	h, err := windows.CreateMutex(nil, false, name)
	if err == nil {
		c.handle = h
		return true
	}
	if h != 0 {
		windows.CloseHandle(h)
	}
	return false
}

func (c *clockLock) close() {
	if c.handle != 0 {
		windows.CloseHandle(c.handle)
		c.handle = 0
	}
}

// windowsZones is the real system. tzutil sets the zone (it switches on the "Change the time zone"
// privilege that standard users hold), and SetUserGeoName the region; neither needs admin rights.
type windowsZones struct{}

// tzutil, from System32 rather than whatever PATH finds, with no console window flashing up.
func tzutil(args ...string) *exec.Cmd {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		dir = filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	cmd := exec.Command(filepath.Join(dir, "tzutil.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

func (windowsZones) zone() (string, error) {
	out, err := tzutil("/g").Output()
	if err != nil {
		return "", fmt.Errorf("tzutil /g: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (windowsZones) setZone(id string) error {
	if out, err := tzutil("/s", id).CombinedOutput(); err != nil {
		return fmt.Errorf("tzutil /s %q: %v %s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (windowsZones) region() (string, error) {
	if err := syncSupported(); err != nil {
		return "", err
	}
	buf := make([]uint16, 16)
	n, _, err := getUserGeoName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", fmt.Errorf("GetUserDefaultGeoName: %w", err)
	}
	return windows.UTF16ToString(buf), nil
}

func (windowsZones) setRegion(code string) error {
	if err := syncSupported(); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(code)
	if err != nil {
		return err
	}
	if ok, _, err := setUserGeoName.Call(uintptr(unsafe.Pointer(name))); ok == 0 {
		return fmt.Errorf("SetUserGeoName: %w", err)
	}
	return nil
}

// An ICU UErrorCode above zero is a failure.
func icuFailed(name string, status int32, n uintptr) error {
	if err := icu.Load(); err != nil {
		return fmt.Errorf("Windows' ICU isn't available (Windows 10 1903 or later has it): %w", err)
	}
	if status > 0 {
		return fmt.Errorf("%s failed (ICU error %d)", name, status)
	}
	if n == 0 {
		return fmt.Errorf("%s has no answer", name)
	}
	return nil
}

func (windowsZones) windowsZone(iana string) (string, error) {
	if err := syncSupported(); err != nil {
		return "", err
	}
	in, out := utf16(iana), make([]uint16, 128)
	if len(in) == 0 {
		return "", errors.New("no zone")
	}
	var status int32
	n, _, _ := ucalWindowsZone.Call(uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)), uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	if err := icuFailed("ucal_getWindowsTimeZoneID", status, n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(out[:n]), nil
}

func (windowsZones) ianaZone(windowsID, region string) (string, error) {
	if err := syncSupported(); err != nil {
		return "", err
	}
	in, out := utf16(windowsID), make([]uint16, 128)
	if len(in) == 0 {
		return "", errors.New("no zone")
	}
	var status int32
	var n uintptr
	if region == "" {
		n, _, _ = ucalIANAZone.Call(uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)), 0, uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	} else {
		code := append([]byte(region), 0)
		n, _, _ = ucalIANAZone.Call(uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)), uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	}
	if err := icuFailed("ucal_getTimeZoneIDForWindowsID", status, n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(out[:n]), nil
}

func (windowsZones) hostZone() (string, error) {
	if err := syncSupported(); err != nil {
		return "", err
	}
	out := make([]uint16, 128)
	var status int32
	n, _, _ := ucalHostZone.Call(uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	if err := icuFailed("ucal_getHostTimeZone", status, n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(out[:n]), nil
}

// announce broadcasts WM_TIMECHANGE. Browsers name their zone again when they get it, with the
// region as it is now, and pass the change into pages that are already open.
func (windowsZones) announce() {
	const hwndBroadcast, wmTimeChange, smtoAbortIfHung = 0xffff, 0x001e, 0x0002
	var result uintptr
	sendMessageTimout.Call(hwndBroadcast, wmTimeChange, 0, 0, smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&result)))
}

func utf16(s string) []uint16 {
	u, err := windows.UTF16FromString(s)
	if err != nil || len(u) < 2 {
		return nil
	}
	return u[:len(u)-1] // ICU takes the length, without the terminator
}

// autoTimeZone reports whether Windows' "Set time zone automatically" is on. It can move the zone
// back to where Windows thinks the computer is.
func autoTimeZone() bool {
	k, err := winreg.OpenKey(winreg.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\tzautoupdate`, winreg.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	start, _, err := k.GetIntegerValue("Start")
	return err == nil && start == 3 // 3 while the setting is on, 4 while it's off
}

// windowsAgent starts, stops and finds the agent through its named mutex and quit event.
type windowsAgent struct{}

func agentControl() agentController { return windowsAgent{} }
func systemZones() zoneSystem       { return windowsZones{} }

func (windowsAgent) running() bool {
	name, _ := windows.UTF16PtrFromString(agentMutexName)
	m, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err != nil {
		return false
	}
	windows.CloseHandle(m)
	return true
}

func (windowsAgent) start(exe string) error {
	cmd := exec.Command(exe, "agent")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// stop asks a running agent to quit, and waits until it has. The system stays where it put it.
func (a windowsAgent) stop() error {
	name, _ := windows.UTF16PtrFromString(agentQuitName)
	e, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return nil // not running
	}
	windows.SetEvent(e)
	windows.CloseHandle(e)
	for range 100 {
		if !a.running() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the time zone agent didn't stop")
}

// quitEvent is the event install, update and uninstall set to stop the agent, reset for this one. An
// earlier stop() may still hold it, perhaps still set: x/sys hands that back as ERROR_ALREADY_EXISTS,
// with a good handle, which is no failure.
func quitEvent(name string) (windows.Handle, error) {
	n, _ := windows.UTF16PtrFromString(name)
	quit, err := windows.CreateEvent(nil, 1, 0, n)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return 0, err
	}
	windows.ResetEvent(quit)
	return quit, nil
}

// agentCommand is `alibi-companion agent`: the hidden program Windows starts at sign-in.
// With --dry-run it looks once and prints what it would set, changing nothing.
func agentCommand(sys system, options []string, stderr io.Writer) error {
	l, err := currentLayout(sys)
	if err != nil {
		return err
	}
	dryRun := len(options) == 1 && options[0] == "--dry-run"
	if !dryRun && len(options) > 0 {
		return fmt.Errorf("unknown option %s", options[0])
	}
	if dryRun {
		logger := log.New(stderr, "", 0)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		place, err := newGeoJS().lookup(ctx)
		if err != nil {
			return fmt.Errorf("couldn't look up the connection: %w", err)
		}
		home, err := currentHome(sys.zones, l.agent.home)
		if err != nil {
			return err
		}
		w := readWish(l.agent.covers) // the place Alibi's running browsers ask for, if any
		follows := loadSettings(l.agent.settings).FollowAlibi
		dry := &agent{agentConfig: agentConfig{sys: sys.zones, home: home, dryRun: true, logf: logger.Printf, followAlibi: func() bool { return follows }}}
		dry.settle(place, true, w)
		return nil
	}

	mutexName, _ := windows.UTF16PtrFromString(agentMutexName)
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(mutex)
		return nil // one is running already
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(mutex)
	quit, err := quitEvent(agentQuitName)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(quit)

	// The window, and so the tray, belong to this thread for as long as it runs.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// In UTC: the program's own idea of local time is fixed when it starts, and it moves the zone.
	logger := log.New(openLog(l.agent.log), "", log.LstdFlags|log.LUTC)
	// On an older Windows the tray still shows where you are and relays it to Alibi; it moves nothing.
	// Nor does it while another user's agent keeps the clock, and it records no home until it has it:
	// how Windows is set then is that user's doing.
	unsupported := syncSupported()
	clock := &clockLock{name: clockMutexName}
	defer clock.close()
	elsewhere := unsupported == nil && !clock.claim()
	var h homeState
	switch {
	case unsupported != nil:
		logger.Printf("started; %v", unsupported)
	case elsewhere:
		logger.Printf("started; another user's Alibi Companion keeps Windows' time zone, so this one waits")
	default:
		if h, err = loadHome(sys.zones, l.agent.home); err != nil {
			logger.Printf("couldn't record how Windows is set now: %v", err)
			return err
		}
	}
	homeZone, _ := sys.zones.ianaZone(baseZone(h.Zone), h.Region)
	if unsupported == nil && !elsewhere {
		logger.Printf("started; home is %s, region %s (%s)", h.Zone, h.Region, homeZone)
	}

	tray := &trayApp{
		l: l, sys: sys, unsupported: unsupported, homeZone: homeZone, settingsPath: l.agent.settings,
		settings: loadSettings(l.agent.settings), nudge: make(chan struct{}, 1), logf: logger.Printf,
	}
	if err := newTray(tray); err != nil {
		return fmt.Errorf("couldn't show the tray icon: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { // install, update or uninstall asks it to go
		windows.WaitForSingleObject(quit, windows.INFINITE)
		cancel()
		tray.close()
	}()

	geo := newGeoJS()
	checker := newLeakChecker(geo)
	recheck := make(chan struct{}, 1)
	var running sync.WaitGroup
	running.Add(2)
	go func() { // where IPv6 and DNS come out: at the start, whenever the connection moves, and every 5 minutes
		defer running.Done()
		tick := time.NewTicker(5 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-recheck:
			case <-tick.C:
			}
			look, done := context.WithTimeout(ctx, 30*time.Second)
			tray.setLeaks(checker.check(look))
			done()
		}
	}()
	watch := defaultWatch(geo.lookup, logger.Printf)
	watch.nudge = tray.nudge
	follower := &agent{agentConfig: agentConfig{
		watch: watch, sys: sys.zones, unsupported: unsupported, clockElsewhere: elsewhere,
		home: h.setting, held: h.Held, homeIn: h.Country, homeFile: l.agent.home, state: l.agent.state, logf: logger.Printf,
		sync: tray.syncOn, followAlibi: tray.followAlibiOn, homeCountry: tray.homeCountrySet,
		report: func(r agentReport) {
			if tray.report(r) {
				select {
				case recheck <- struct{}{}:
				default:
				}
			}
		},
	}}
	tray.later = worker(ctx, &running)
	tray.onTimeChange = follower.reassert
	tray.onSyncChange = follower.reassert
	tray.onRelease = follower.release
	// Signing out or shutting down: Windows goes back home, so the sign-in screen and whoever signs in
	// next don't run on this user's location.
	tray.onSessionEnd = follower.finish
	if elsewhere {
		running.Add(1)
		go func() { // takes the clock once the other user's agent lets go of it
			defer running.Done()
			tick := time.NewTicker(10 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				if !clock.claim() {
					continue
				}
				h, err := loadHome(sys.zones, l.agent.home)
				if err != nil {
					logger.Printf("couldn't record how Windows is set now: %v", err)
					clock.close()
					continue
				}
				logger.Printf("now keeps Windows' time zone; home is %s, region %s", h.Zone, h.Region)
				follower.takeClock(h)
				return
			}
		}()
	}
	// The place Alibi's running browsers ask for, as their hosts keep it: Windows follows it within a
	// second of a change, and lets go within a second of the last of those browsers closing.
	os.Remove(filepath.Join(l.dir, "wanted.json")) // where an earlier build kept one cover for every browser
	follower.setWish(readWish(l.agent.covers))
	running.Add(1)
	go func() {
		defer running.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				follower.setWish(readWish(l.agent.covers))
			}
		}
	}()
	go func() {
		defer running.Done()
		if err := follower.run(ctx); err != nil {
			logger.Printf("%v", err)
		}
	}()

	tray.loop()
	cancel()
	running.Wait()
	os.Remove(l.agent.state) // the host stops relaying at once, rather than after agentQuiet
	// Quit from the tray: Windows goes back home until the next sign-in, unless something else has
	// moved it since the agent did.
	if tray.quitByUser {
		follower.finish()
	}
	logger.Printf("stopped")
	return nil
}

// openLog appends to the agent's log, starting it over once it passes 1 MB.
func openLog(path string) io.Writer {
	if info, err := os.Stat(path); err == nil && info.Size() > 1<<20 {
		os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return io.Discard
	}
	return f
}
