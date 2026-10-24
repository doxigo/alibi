package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the home clock, in any zone, on any Windows
	"unsafe"

	"golang.org/x/sys/windows"
)

// The tray: where the connection comes out, as a country code on Alibi's stamp, with everything
// else a right-click away. It's red, and says why, when the connection isn't where it should be.

const (
	wmTray   = wmApp + 1 // the icon's clicks
	wmUpdate = wmApp + 2 // the agent has news
	trayID   = 1
)

const (
	cmdTitle = 1 + iota
	cmdSync
	cmdAlerts
	cmdSound
	cmdEdge
	cmdStartup
	cmdAutoTZ
	cmdCheckPage
	cmdFolder
	cmdQuit
)

const checkPage = "https://doxigo.github.io/alibi/"

type trayApp struct {
	l            layout
	sys          system
	home         setting
	homeZone     string // home's IANA zone, for its clock and its country
	settingsPath string
	nudge        chan struct{}
	logf         func(string, ...any)
	onTimeChange func() // when Windows says the time zone changed
	onSyncChange func() // when the tray switches following Alibi's location on or off

	mu       sync.Mutex
	settings settings
	place    cover // the last place found
	looked   bool
	lookErr  error // the last look's failure, if it failed
	leaks    leakReport
	reads    string // the zone browsers read now
	wish     *wish  // Alibi's All sites cover, once Alibi has said
	system   string // the zone the agent holds Windows on, or ""
	target   string // the zone Windows should read now

	// Only the window's thread touches these.
	hwnd, edge     uintptr
	icon           uintptr
	iconKey        string
	taskbarCreated uint32
	shown          verdict
	shownProblems  string
	quitByUser     bool
}

func newTray(t *trayApp) error {
	procSetDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
	instance, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	appIcon, _, _ := procLoadIcon.Call(instance, 1) // the program's own icon (rsrc), if it has one

	class := wndClassEx{WndProc: windows.NewCallback(t.wndProc), Instance: instance, Icon: appIcon, ClassName: utf16Ptr("AlibiCompanionTray")}
	class.Size = uint32(unsafe.Sizeof(class))
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	t.hwnd, _, _ = procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class.ClassName)), uintptr(unsafe.Pointer(utf16Ptr("Alibi Companion"))),
		wsPopup, 0, 0, 0, 0, 0, 0, instance, 0)
	if t.hwnd == 0 {
		return errors.New("CreateWindowEx failed")
	}
	// The red line: a thin, click-through bar along the top of the screen.
	red, _, _ := procCreateSolidBrush.Call(0x002a1ec4) // airmail red, as 0x00BBGGRR
	edgeClass := wndClassEx{WndProc: procDefWindowProc.Addr(), Instance: instance, Background: red, ClassName: utf16Ptr("AlibiCompanionEdge")}
	edgeClass.Size = uint32(unsafe.Sizeof(edgeClass))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&edgeClass)))
	t.edge, _, _ = procCreateWindowEx.Call(wsExTopmost|wsExToolWindow|wsExLayered|wsExTransparent|wsExNoActivate,
		uintptr(unsafe.Pointer(edgeClass.ClassName)), 0, wsPopup, 0, 0, 0, 0, 0, 0, instance, 0)
	if t.edge != 0 {
		procSetLayeredWindowAttrs.Call(t.edge, 0, 235, lwaAlpha)
	}

	t.taskbarCreated = registerMessage("TaskbarCreated") // Explorer restarted: the icon must be added again
	t.addIcon()
	procSetTimer.Call(t.hwnd, 1, 30_000, 0) // keeps the home clock in the tooltip current
	return nil
}

func registerMessage(name string) uint32 {
	r, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(utf16Ptr(name))))
	return uint32(r)
}

// loop runs the window until it closes, on the thread that made it.
func (t *trayApp) loop() {
	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// close ends the loop from any goroutine.
func (t *trayApp) close() { procPostMessage.Call(t.hwnd, wmClose, 0, 0) }

func (t *trayApp) wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch m := uint32(message); m {
	case wmTray:
		switch uint32(lParam & 0xffff) {
		case wmContextMenu, ninSelect, ninKeySelect, ninBalloonUserClick:
			t.showMenu(hwnd)
		}
		return 0
	case wmUpdate, wmTimer:
		t.refresh()
		return 0
	case wmTimeChange:
		// Something changed the time zone. If it wasn't the agent (Windows' own automatic time zone,
		// say), the agent puts it back now rather than at its next look.
		if t.onTimeChange != nil {
			go t.onTimeChange()
		}
		return 0
	case wmDpiChanged:
		t.iconKey = ""
		t.refresh()
		return 0
	case wmQueryEndSession:
		return 1
	case wmEndSession:
		if wParam != 0 { // signing out or shutting down: go, leaving the time zone as it is
			t.removeIcon()
			procPostQuitMessage.Call(0)
		}
		return 0
	case wmDestroy:
		t.removeIcon()
		procPostQuitMessage.Call(0)
		return 0
	default:
		if m == t.taskbarCreated && m != 0 {
			t.addIcon()
			return 0
		}
	}
	r, _, _ := procDefWindowProc.Call(hwnd, message, wParam, lParam)
	return r
}

func (t *trayApp) data(flags uint32) notifyIconData {
	n := notifyIconData{Wnd: t.hwnd, ID: trayID, Flags: flags, CallbackMessage: wmTray, Icon: t.icon}
	n.Size = uint32(unsafe.Sizeof(n))
	copyUTF16(n.Tip[:], t.tooltip())
	return n
}

func (t *trayApp) addIcon() {
	t.iconKey = ""
	t.updateIcon()
	n := t.data(nifMessage | nifIcon | nifTip | nifShowTip)
	procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&n)))
	n.Version = notifyIconVersion4
	procShellNotifyIcon.Call(nimSetVersion, uintptr(unsafe.Pointer(&n)))
}

func (t *trayApp) removeIcon() {
	n := t.data(0)
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&n)))
	t.showEdge(false)
}

// snapshot is what the agent last said, read once under the lock.
type snapshot struct {
	settings settings
	place    cover
	looked   bool
	lookErr  error
	leaks    leakReport
	reads    string
	wish     *wish
	system   string
	target   string
}

func (t *trayApp) snapshot() snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return snapshot{t.settings, t.place, t.looked, t.lookErr, t.leaks, t.reads, t.wish, t.system, t.target}
}

func (t *trayApp) judgeNow(s snapshot) (verdict, []string) {
	if !s.looked {
		return unknown, nil
	}
	return judge(s.place, s.leaks, homeCountry(s.settings, t.homeZone), s.settings)
}

// updateIcon redraws the icon when its country or colour changed.
func (t *trayApp) updateIcon() bool {
	s := t.snapshot()
	v, _ := t.judgeNow(s)
	code := s.place.Country
	if code == "" {
		code = "—"
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	size, _, _ := procGetSystemMetricsForDpi.Call(smCxSmIcon, dpi)
	if size == 0 {
		size = 16
	}
	key := fmt.Sprint(code, v, size)
	if key == t.iconKey {
		return false
	}
	icon, err := trayIcon(code, v, int(size))
	if err != nil {
		t.logf("couldn't draw the tray icon: %v", err)
		return false
	}
	if t.icon != 0 {
		procDestroyIcon.Call(t.icon)
	}
	t.icon, t.iconKey = icon, key
	return true
}

// refresh brings the icon, tooltip, alerts and red line up to date.
func (t *trayApp) refresh() {
	t.updateIcon()
	n := t.data(nifIcon | nifTip | nifShowTip)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))

	s := t.snapshot()
	v, problems := t.judgeNow(s)
	said := strings.Join(problems, " ")
	alerting := s.settings.Alerts
	switch {
	case v == wrong && alerting && (t.shown != wrong || said != t.shownProblems):
		t.notify("Not where you meant to be", said, niifWarning)
		if s.settings.Sound {
			procMessageBeep.Call(mbIconWarning)
		}
	case v == fine && t.shown == wrong && alerting:
		t.notify("Back where you meant to be", "Your connection comes out in "+placeName(s.place)+".", niifInfo)
	}
	t.showEdge(v == wrong && alerting && s.settings.EdgeLine)
	t.shown, t.shownProblems = v, said
}

func (t *trayApp) notify(title, text string, kind uint32) {
	n := t.data(nifInfo)
	copyUTF16(n.InfoTitle[:], title)
	copyUTF16(n.Info[:], text)
	n.InfoFlags = kind | niifNoSound | niifQuiet // the alert's own sound, if any, is played separately
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (t *trayApp) showEdge(on bool) {
	if t.edge == 0 {
		return
	}
	if !on {
		procShowWindow.Call(t.edge, swHide)
		return
	}
	dpi, _, _ := procGetDpiForSystem.Call()
	width, _, _ := procGetSystemMetrics.Call(smCxScreen)
	height := max(3, int(dpi)*4/96)
	procSetWindowPos.Call(t.edge, hwndTopmost, 0, 0, width, uintptr(height), swpNoActivate)
	procShowWindow.Call(t.edge, swShowNoActivate)
}

func placeName(p cover) string {
	switch {
	case p.City != "" && p.CountryName != "":
		return p.City + ", " + p.CountryName
	case p.CountryName != "":
		return p.CountryName
	}
	return p.Timezone
}

// homeClock is the time at home, with home's city: "21:18 in Tehran".
func (t *trayApp) homeClock() string {
	loc, err := time.LoadLocation(t.homeZone)
	if err != nil || t.homeZone == "" {
		return ""
	}
	return time.Now().In(loc).Format("15:04") + " in " + strings.ReplaceAll(t.homeZone[strings.LastIndex(t.homeZone, "/")+1:], "_", " ")
}

func (t *trayApp) tooltip() string {
	s := t.snapshot()
	parts := []string{"Alibi Companion"}
	if s.looked {
		parts = append(parts, placeName(s.place))
	}
	if clock := t.homeClock(); clock != "" {
		parts = append(parts, "home "+clock)
	}
	return strings.Join(parts, " · ")
}

// statusLines are the menu's read-only lines about time zone, IPv6 and DNS.
func (t *trayApp) statusLines(s snapshot) []string {
	var lines []string
	_, problems := t.judgeNow(s)
	for _, p := range problems {
		if !strings.HasPrefix(p, "IPv6") && !strings.HasPrefix(p, "DNS") {
			lines = append(lines, "⚠  "+p)
		}
	}
	following := s.wish == nil || s.wish.Configuration == "ipAddress"
	switch {
	case s.reads == "":
	case s.system != "" && s.system == s.reads:
		switch {
		case following:
			lines = append(lines, "✓  Windows on your VPN's time zone: "+s.reads)
		case s.wish.Timezone == "":
			lines = append(lines, "✓  Windows on your own time zone: "+s.reads+" (Alibi is off)")
		default:
			lines = append(lines, "✓  Windows on Alibi's location: "+s.reads)
		}
	case !s.settings.Sync:
		lines = append(lines, "Windows reads "+s.reads+" (it doesn't follow Alibi now)")
	case s.target != "":
		lines = append(lines, "⚠  Windows reads "+s.reads+", not "+s.target)
	default:
		lines = append(lines, "Windows reads "+s.reads)
	}
	// A clock in one country, a connection in another: sites can compare the two.
	if !following && s.wish.Timezone != "" && s.looked {
		if clock := zoneCountries[s.wish.Timezone]; clock != "" && clock != s.place.Country {
			lines = append(lines, "⚠  Windows says "+countryName(clock)+"; your connection comes out in "+countryName(s.place.Country))
		}
	}
	home := homeCountry(s.settings, t.homeZone)
	switch {
	case s.leaks.IPv6 == "":
		lines = append(lines, "IPv6: checking…")
	case s.leaks.IPv6 == "none":
		lines = append(lines, "✓  IPv6: none, so nothing to leak")
	case s.leaks.IPv6At.Country == s.place.Country:
		lines = append(lines, "✓  IPv6 comes out with the VPN")
	default:
		lines = append(lines, "✗  IPv6 comes out in "+describe(s.leaks.IPv6At))
	}
	switch {
	case s.leaks.Checked.IsZero():
		lines = append(lines, "DNS: checking…")
	case s.leaks.DNS.IP == "":
		lines = append(lines, "DNS: couldn't check")
	case home != "" && s.leaks.DNS.Country == home && s.place.Country != home:
		lines = append(lines, "✗  DNS goes through "+describe(s.leaks.DNS))
	default:
		lines = append(lines, "✓  DNS goes through "+describe(s.leaks.DNS))
	}
	return lines
}

func (t *trayApp) showMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	add := func(flags uintptr, id int, text string) {
		procAppendMenu.Call(menu, flags, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	check := func(on bool) uintptr {
		if on {
			return mfChecked
		}
		return 0
	}
	separator := func() { procAppendMenu.Call(menu, mfSeparator, 0, 0) }
	s := t.snapshot()

	switch {
	case s.looked:
		add(mfGrayed, cmdTitle, placeName(s.place))
	case s.lookErr != nil:
		add(mfGrayed, cmdTitle, "Can't reach GeoJS to see where you are")
	default:
		add(mfGrayed, cmdTitle, "Looking up where you are…")
	}
	procSetMenuDefaultItem.Call(menu, cmdTitle, 0)
	if detail := strings.Trim(s.place.Org+" · "+s.place.IP, " ·"); s.looked && detail != "" {
		add(mfGrayed, 0, detail)
	}
	if s.looked && s.lookErr != nil {
		add(mfGrayed, 0, "Offline? The last look didn't get an answer")
	}
	if clock := t.homeClock(); clock != "" {
		add(mfGrayed, 0, "At home: "+clock)
	}
	separator()
	for _, line := range t.statusLines(s) {
		add(mfGrayed, 0, line)
	}
	separator()
	alerts := s.settings.Alerts
	sub := func() uintptr {
		if alerts {
			return 0
		}
		return mfGrayed
	}
	add(check(s.settings.Sync), cmdSync, "Keep Windows on Alibi's location")
	add(check(alerts), cmdAlerts, "Alert me when something's wrong")
	add(check(s.settings.Sound)|sub(), cmdSound, "      with a sound")
	add(check(s.settings.EdgeLine)|sub(), cmdEdge, "      with a red line along the screen")
	_, startsAtSignIn, _ := t.sys.registry.value(runKey, runName)
	add(check(startsAtSignIn), cmdStartup, "Start when I sign in")
	separator()
	if autoTimeZone() {
		add(0, cmdAutoTZ, "Turn off Windows' automatic time zone…")
	}
	add(0, cmdCheckPage, "Open the check page")
	add(0, cmdFolder, "Open the companion's folder")
	separator()
	add(0, cmdQuit, "Quit until I sign in again")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd) // so the menu closes when you click elsewhere
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify, uintptr(pt.X), uintptr(pt.Y), hwnd, 0)
	procPostMessage.Call(hwnd, 0, 0, 0)
	t.command(int(cmd))
}

func (t *trayApp) toggle(change func(*settings)) settings {
	t.mu.Lock()
	change(&t.settings)
	s := t.settings
	t.mu.Unlock()
	if err := saveSettings(t.settingsPath, s); err != nil {
		t.logf("couldn't save the settings: %v", err)
	}
	return s
}

func (t *trayApp) command(cmd int) {
	switch cmd {
	case cmdSync:
		s := t.toggle(func(s *settings) { s.Sync = !s.Sync })
		go func() {
			if !s.Sync { // off: Windows goes back to how it was
				if _, err := apply(t.sys.zones, t.home); err != nil {
					t.logf("couldn't put the time zone back: %v", err)
				} else {
					t.logf("following Alibi off: put Windows back on %s, region %s", t.home.Zone, t.home.Region)
				}
			}
			if t.onSyncChange != nil {
				t.onSyncChange()
			}
		}()
	case cmdAlerts:
		t.toggle(func(s *settings) { s.Alerts = !s.Alerts })
	case cmdSound:
		t.toggle(func(s *settings) { s.Sound = !s.Sound })
	case cmdEdge:
		t.toggle(func(s *settings) { s.EdgeLine = !s.EdgeLine })
	case cmdStartup:
		if _, on, _ := t.sys.registry.value(runKey, runName); on {
			t.sys.registry.deleteValue(runKey, runName)
		} else {
			t.sys.registry.setValue(runKey, runName, `"`+t.l.agent.exe+`" agent`)
		}
	case cmdAutoTZ:
		go func() {
			if err := turnOffAutoTimeZone(t.hwnd); err != nil {
				t.logf("%v", err)
				return
			}
			os.WriteFile(filepath.Join(t.l.dir, autoTimeZoneMarker), nil, 0o644)
			t.logf("turned off Windows' automatic time zone")
			t.lookNow()
		}()
	case cmdCheckPage:
		shellOpen(checkPage)
	case cmdFolder:
		shellOpen(t.l.dir)
	case cmdQuit:
		t.quitByUser = true
		procDestroyWindow.Call(t.hwnd)
		return
	default:
		return
	}
	t.refresh()
}

func (t *trayApp) lookNow() {
	select {
	case t.nudge <- struct{}{}:
	default:
	}
}

// report takes the agent's news. It says whether the connection moved, so the leak checks know to
// look again.
func (t *trayApp) report(r agentReport) bool {
	reads, _ := t.sys.zones.hostZone()
	t.mu.Lock()
	moved := false
	if r.Place.Country != "" {
		moved = !t.looked || r.Place.IP != t.place.IP || r.Place.Country != t.place.Country
		t.place, t.looked = r.Place, true
	}
	t.lookErr, t.reads, t.wish, t.system, t.target = r.Err, reads, r.Wish, r.System, r.Target
	t.mu.Unlock()
	procPostMessage.Call(t.hwnd, wmUpdate, 0, 0)
	return moved
}

func countryName(code string) string {
	if name := countryNames[code]; name != "" {
		return name
	}
	return code
}

func (t *trayApp) setLeaks(r leakReport) {
	t.mu.Lock()
	t.leaks = r
	t.mu.Unlock()
	procPostMessage.Call(t.hwnd, wmUpdate, 0, 0)
}

func (t *trayApp) syncOn() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settings.Sync
}
