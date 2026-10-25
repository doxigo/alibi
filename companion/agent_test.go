package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeZones is Windows' time zone and region, with zone names as Windows' ICU gives them.
type fakeZones struct {
	zoneNow, regionNow string
	calls              []string
	readsAs            string // when set, the zone browsers read, as if something set Windows back
	failSet            bool
}

var windowsZoneOf = map[string]string{
	"Europe/Oslo": "W. Europe Standard Time", "Europe/Berlin": "W. Europe Standard Time",
	"America/Detroit": "Eastern Standard Time", "America/New_York": "Eastern Standard Time",
	"Asia/Dushanbe": "West Asia Standard Time", "Asia/Tokyo": "Tokyo Standard Time", "Asia/Tehran": "Iran Standard Time",
	"America/Los_Angeles": "Pacific Standard Time", "America/Chicago": "Central Standard Time",
}

func (f *fakeZones) zone() (string, error)   { return f.zoneNow, nil }
func (f *fakeZones) region() (string, error) { return f.regionNow, nil }
func (f *fakeZones) setZone(id string) error {
	if f.failSet {
		return errors.New("access denied")
	}
	f.calls = append(f.calls, "zone "+id)
	f.zoneNow = id
	return nil
}
func (f *fakeZones) setRegion(code string) error {
	f.calls = append(f.calls, "region "+code)
	f.regionNow = code
	return nil
}
func (f *fakeZones) windowsZone(iana string) (string, error) {
	if z, ok := windowsZoneOf[iana]; ok {
		return z, nil
	}
	return "", errors.New("unknown zone")
}

// What the probe of Windows' own ICU showed: some zones' names depend on the region.
func (f *fakeZones) ianaZone(windowsID, region string) (string, error) {
	switch windowsID {
	case "W. Europe Standard Time":
		if region == "NO" {
			return "Europe/Oslo", nil
		}
		return "Europe/Berlin", nil
	case "West Asia Standard Time":
		if region == "TJ" {
			return "Asia/Dushanbe", nil
		}
		return "Asia/Tashkent", nil
	case "Eastern Standard Time":
		return "America/New_York", nil
	case "Pacific Standard Time":
		return "America/Los_Angeles", nil
	case "Central Standard Time":
		return "America/Chicago", nil
	case "Tokyo Standard Time":
		return "Asia/Tokyo", nil
	case "Iran Standard Time":
		return "Asia/Tehran", nil
	}
	return "", errors.New("unknown Windows zone")
}
func (f *fakeZones) hostZone() (string, error) {
	if f.readsAs != "" {
		return f.readsAs, nil
	}
	return f.ianaZone(baseZone(f.zoneNow), f.regionNow)
}
func (f *fakeZones) announce() { f.calls = append(f.calls, "announce") }

// Home is Iran's zone with the region set to the US, as on the computer this was built on.
func atHome() *fakeZones { return &fakeZones{zoneNow: "Iran Standard Time", regionNow: "US"} }

var home = setting{Zone: "Iran Standard Time", Region: "US"}

func exit(zone, country string) cover {
	return cover{Timezone: zone, Locale: countryLocales[country], Country: country, Lat: ptr(1), Lon: ptr(2)}
}

func TestWanted(t *testing.T) {
	for _, c := range []struct {
		name  string
		place cover
		home  setting
		want  setting
	}{
		// The W. Europe zone reads as Oslo only with the region on Norway, so the region moves.
		{"Norway", exit("Europe/Oslo", "NO"), home, setting{"W. Europe Standard Time", "NO"}},
		// It reads as Berlin with the home region already, so the region stays home's.
		{"Germany", exit("Europe/Berlin", "DE"), home, setting{"W. Europe Standard Time", "US"}},
		{"Japan", exit("Asia/Tokyo", "JP"), home, setting{"Tokyo Standard Time", "US"}},
		{"Tajikistan", exit("Asia/Dushanbe", "TJ"), home, setting{"West Asia Standard Time", "TJ"}},
		{"Detroit", exit("America/Detroit", "US"), home, setting{"Eastern Standard Time", "US"}},
		// Back home: exactly as it was.
		{"home", exit("Asia/Tehran", "IR"), home, home},
		{"home without daylight saving", exit("Asia/Tehran", "IR"), setting{"Iran Standard Time_dstoff", "US"}, setting{"Iran Standard Time_dstoff", "US"}},
		{"no country", cover{Timezone: "Europe/Oslo"}, home, setting{"W. Europe Standard Time", "US"}},
	} {
		got, err := wanted(atHome(), c.place, c.home)
		if err != nil || got != c.want {
			t.Errorf("%s: %v (err %v), want %v", c.name, got, err, c.want)
		}
	}
	if _, err := wanted(atHome(), exit("Mars/Olympus", "MA"), home); err == nil {
		t.Error("an unknown zone got a Windows zone")
	}
}

func TestApplyChangesOnlyWhatDiffers(t *testing.T) {
	f := atHome()
	if changed, err := apply(f, setting{"W. Europe Standard Time", "NO"}); !changed || err != nil {
		t.Fatalf("changed %v, err %v", changed, err)
	}
	// The region first, so programs that read the zone again get its new name.
	if want := []string{"region NO", "zone W. Europe Standard Time", "announce"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls %v, want %v", f.calls, want)
	}
	f.calls = nil
	if changed, _ := apply(f, setting{"W. Europe Standard Time", "NO"}); changed || len(f.calls) > 0 {
		t.Errorf("changed an already-right system: %v", f.calls)
	}
	if apply(f, setting{"Tokyo Standard Time", "NO"}); !slices.Equal(f.calls, []string{"zone Tokyo Standard Time", "announce"}) {
		t.Errorf("calls %v", f.calls)
	}
}

// settleExit settles on a connection's place before Alibi has said anything: following the exit.
func (g *agent) settleExit(place cover) cover {
	settled, _, _ := g.settle(place, true, nil)
	return settled
}

func TestSettleReportsTheZoneAsBrowsersReadIt(t *testing.T) {
	f := atHome()
	a := &agent{agentConfig: agentConfig{vpn: vpnOn, sys: f, home: home, logf: t.Logf}}
	if got := a.settleExit(exit("Europe/Oslo", "NO")); got.Timezone != "Europe/Oslo" || f.zoneNow != "W. Europe Standard Time" || f.regionNow != "NO" {
		t.Errorf("Norway: reported %s, Windows %s/%s", got.Timezone, f.zoneNow, f.regionNow)
	}
	// GeoJS says Detroit; Windows, and so the browser, says New York. Alibi must hear New York to fake nothing.
	if got := a.settleExit(exit("America/Detroit", "US")); got.Timezone != "America/New_York" {
		t.Errorf("Detroit: reported %s, want America/New_York", got.Timezone)
	}
	if got := a.settleExit(exit("Asia/Tehran", "IR")); got.Timezone != "Asia/Tehran" || f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Errorf("home: reported %s, Windows %s/%s", got.Timezone, f.zoneNow, f.regionNow)
	}
}

// If Windows doesn't end up reading as the exit, the place keeps GeoJS's zone: Alibi then fakes it,
// rather than taking the real zone for the cover.
func TestSettleKeepsGeoJSZoneWhenWindowsDoesNotFollow(t *testing.T) {
	reverted := atHome()
	reverted.readsAs = "Asia/Tehran" // "Set time zone automatically" put it back
	if got := (&agent{agentConfig: agentConfig{vpn: vpnOn, sys: reverted, home: home, logf: t.Logf}}).settleExit(exit("America/Detroit", "US")); got.Timezone != "America/Detroit" {
		t.Errorf("reverted: reported %s, want GeoJS's America/Detroit", got.Timezone)
	}
	denied := atHome()
	denied.failSet = true
	if got := (&agent{agentConfig: agentConfig{vpn: vpnOn, sys: denied, home: home, logf: t.Logf}}).settleExit(exit("Europe/Oslo", "NO")); got.Timezone != "Europe/Oslo" {
		t.Errorf("denied: reported %s", got.Timezone)
	}
	if got := (&agent{agentConfig: agentConfig{vpn: vpnOn, sys: atHome(), home: home, logf: t.Logf}}).settleExit(exit("Mars/Olympus", "MA")); got.Timezone != "Mars/Olympus" {
		t.Errorf("unknown zone: reported %s", got.Timezone)
	}
}

func TestRunAgentMovesWindowsAndWritesItsPlace(t *testing.T) {
	f := atHome()
	state := filepath.Join(t.TempDir(), "agent.json")
	lookup, _ := sequence(exit("Asia/Dushanbe", "TJ"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := runAgent(ctx, agentConfig{
		watch: watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: 5 * time.Millisecond, watch: time.Hour},
		vpn:   vpnOn,
		sys:   f, home: home, state: state, logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.zoneNow != "West Asia Standard Time" || f.regionNow != "TJ" {
		t.Errorf("Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	if n := slices.Index(f.calls, "announce"); n < 0 || slices.Index(f.calls[n+1:], "announce") >= 0 {
		t.Errorf("announced %v: want once, not on every look", f.calls)
	}
	var got agentState
	b, _ := os.ReadFile(state)
	if json.Unmarshal(b, &got) != nil || got.Cover.Timezone != "Asia/Dushanbe" || got.System != "Asia/Dushanbe" || time.Since(got.Updated) > time.Second {
		t.Errorf("state %s", b)
	}
}

// newAgent is an agent on a fake Windows at home, its connection in Tajikistan once it looks.
func newAgent(t *testing.T) (*agent, *fakeZones, *[]agentReport) {
	f := atHome()
	var reports []agentReport
	lookup, _ := sequence(exit("Asia/Dushanbe", "TJ"))
	g := &agent{agentConfig: agentConfig{
		watch: watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: time.Hour, watch: time.Hour},
		vpn:   vpnOn,
		sys:   f, home: home, state: filepath.Join(t.TempDir(), "agent.json"), logf: t.Logf,
		report:      func(r agentReport) { reports = append(reports, r) },
		followAlibi: func() bool { return true },
	}}
	return g, f, &reports
}

func readState(t *testing.T, path string) agentState {
	t.Helper()
	var state agentState
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &state)
	return state
}

// With Follow Alibi's location on, every place chosen in Alibi for all sites moves the system's clock,
// at once, before any look at the network.
func TestAlibisLocationMovesWindows(t *testing.T) {
	g, f, reports := newAgent(t)
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if f.zoneNow != "Tokyo Standard Time" || f.regionNow != "US" {
		t.Errorf("Tokyo: Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	if s := readState(t, g.state); s.System != "Asia/Tokyo" || s.Cover.Timezone != "" {
		t.Errorf("Tokyo: state %+v (no connection place yet)", s)
	}
	if r := (*reports)[len(*reports)-1]; r.System != "Asia/Tokyo" || r.Wish.Timezone != "Asia/Tokyo" {
		t.Errorf("Tokyo: report %+v", r)
	}
	// Norway's zone reads as Oslo only with the region on Norway.
	g.setWish(&wish{Configuration: "oslo", Timezone: "Europe/Oslo"})
	if f.zoneNow != "W. Europe Standard Time" || f.regionNow != "NO" || readState(t, g.state).System != "Europe/Oslo" {
		t.Errorf("Oslo: Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	// The same word again changes nothing.
	f.calls = nil
	g.setWish(&wish{Configuration: "oslo", Timezone: "Europe/Oslo"})
	if len(f.calls) > 0 {
		t.Errorf("the same cover again changed %v", f.calls)
	}
	// Off asks nothing of the system: it follows the connection, as it does without Alibi.
	g.found, g.have = exit("Asia/Dushanbe", "TJ"), true
	g.setWish(&wish{Configuration: "browserDefault"})
	if f.zoneNow != "West Asia Standard Time" || f.regionNow != "TJ" || readState(t, g.state).System != "Asia/Dushanbe" {
		t.Errorf("off: Windows on %s/%s, not the connection's zone", f.zoneNow, f.regionNow)
	}
}

// Off before the first answer about the connection: nothing to aim at yet, so Windows stays put.
func TestAlibiOffBeforeTheFirstLookChangesNothing(t *testing.T) {
	g, f, _ := newAgent(t)
	g.setWish(&wish{Configuration: "browserDefault"})
	if len(f.calls) > 0 {
		t.Errorf("Off moved Windows before the connection was known: %v", f.calls)
	}
}

// Follow Alibi's location is off by default: a place for all sites stays in the browser, and Windows
// keeps following the connection.
func TestAlibisLocationNeedsFollowAlibi(t *testing.T) {
	if defaultSettings().FollowAlibi || loadSettings(filepath.Join(t.TempDir(), "settings.json")).FollowAlibi {
		t.Error("Follow Alibi's location is on by default")
	}
	g, f, _ := newAgent(t)
	g.followAlibi = func() bool { return false }
	g.found, g.have = exit("Asia/Dushanbe", "TJ"), true
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if f.zoneNow != "West Asia Standard Time" || f.regionNow != "TJ" {
		t.Errorf("Windows on %s/%s, want the connection's zone", f.zoneNow, f.regionNow)
	}
}

// "Match my IP address" follows the connection: nothing until the first answer, then the exit.
func TestMatchMyIPFollowsTheConnection(t *testing.T) {
	g, f, _ := newAgent(t)
	g.setWish(&wish{Configuration: "ipAddress", Timezone: "Asia/Tokyo"}) // the zone is the IP's last one: ignored
	if len(f.calls) > 0 {
		t.Fatalf("moved before knowing where the connection comes out: %v", f.calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	g.run(ctx)
	cancel()
	if f.zoneNow != "West Asia Standard Time" || f.regionNow != "TJ" {
		t.Errorf("Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	s := readState(t, g.state)
	if s.Cover.Timezone != "Asia/Dushanbe" || s.System != "Asia/Dushanbe" {
		t.Errorf("state %+v", s)
	}
	// A city instead: the clock goes there, and the connection's place still reaches IP covers on sites.
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if s := readState(t, g.state); f.zoneNow != "Tokyo Standard Time" || s.System != "Asia/Tokyo" || s.Cover.Country != "" || s.Cover.Timezone != "Asia/Dushanbe" {
		t.Errorf("Tokyo after the IP: Windows %s, state %+v", f.zoneNow, s)
	}
}

// With following switched off in the tray, Alibi's location changes nothing, and nothing's held.
func TestFollowingOffLeavesWindowsAlone(t *testing.T) {
	g, f, _ := newAgent(t)
	g.sync = func() bool { return false }
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if len(f.calls) > 0 || readState(t, g.state).System != "" {
		t.Errorf("changed %v, state %+v", f.calls, readState(t, g.state))
	}
}

// When something else moves the zone back (Windows' automatic time zone), reassert puts it where the
// connection is at once, from the last place found, without a new look.
func TestReassertPutsTheZoneBack(t *testing.T) {
	f := atHome()
	state := filepath.Join(t.TempDir(), "agent.json")
	lookup, calls := sequence(exit("Asia/Dushanbe", "TJ"))
	g := &agent{agentConfig: agentConfig{
		watch: watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: time.Hour, watch: time.Hour},
		vpn:   vpnOn,
		sys:   f, home: home, state: state, logf: t.Logf,
	}}
	g.reassert() // nothing found yet: nothing to do
	if len(f.calls) > 0 {
		t.Fatalf("changed the system before finding a place: %v", f.calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	g.run(ctx) // one look, then the next is an hour away
	cancel()
	if f.zoneNow != "West Asia Standard Time" || calls.Load() != 1 {
		t.Fatalf("Windows on %s after %d looks", f.zoneNow, calls.Load())
	}
	f.zoneNow, f.calls = "Iran Standard Time", nil // Windows' own automatic time zone moves it home
	g.reassert()
	if f.zoneNow != "West Asia Standard Time" || calls.Load() != 1 {
		t.Errorf("after reassert Windows on %s, %d looks", f.zoneNow, calls.Load())
	}
	f.calls = nil
	g.reassert() // its own change announces itself too: nothing to do then
	if len(f.calls) > 0 {
		t.Errorf("reassert on a right system changed %v", f.calls)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	f := atHome()
	state := filepath.Join(t.TempDir(), "agent.json")
	lookup, _ := sequence(exit("Europe/Oslo", "NO"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	runAgent(ctx, agentConfig{
		watch: watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: 5 * time.Millisecond, watch: time.Hour},
		vpn:   vpnOn,
		sys:   f, home: home, state: state, dryRun: true, logf: t.Logf,
	})
	if len(f.calls) > 0 {
		t.Errorf("dry run changed %v", f.calls)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("dry run wrote its state")
	}
}

// The worker runs what it's given in order, and nothing once the agent has stopped: running.Wait
// returning means no move can still land.
func TestWorkerStopsWithTheAgent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var running sync.WaitGroup
	later := worker(ctx, &running)
	done := make(chan int, 3)
	for i := range 3 {
		later(func() { done <- i })
	}
	for i := range 3 {
		if got := <-done; got != i {
			t.Fatalf("job %d ran as %d", i, got)
		}
	}
	cancel()
	running.Wait()
	ran := false
	later(func() { ran = true }) // must neither run nor block
	time.Sleep(20 * time.Millisecond)
	if ran {
		t.Error("a job ran after the agent stopped")
	}
}

// On a Windows too old for time zone sync, the agent moves nothing and records no home, but still
// reports where the connection comes out.
func TestAnUnsupportedWindowsIsLeftAlone(t *testing.T) {
	g, f, reports := newAgent(t)
	g.unsupported = errors.New("too old")
	g.homeFile = filepath.Join(t.TempDir(), "home.json")
	g.found, g.have = exit("Asia/Dushanbe", "TJ"), true
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	g.reassert()
	if r := (*reports)[len(*reports)-1]; r.Place.Country != "TJ" || r.System != "" || r.Home != (setting{}) {
		t.Errorf("report %+v", r)
	}
	g.release()
	if len(f.calls) > 0 {
		t.Errorf("changed %v", f.calls)
	}
	if _, err := os.Stat(g.homeFile); !os.IsNotExist(err) {
		t.Error("recorded a home")
	}
}

// While another user's agent keeps the clock, this one moves nothing and records no home: how Windows
// is set is that user's doing. Handed the clock, it records home as Windows is then, and follows.
func TestAnAgentWaitsForTheClock(t *testing.T) {
	g, f, reports := newAgent(t)
	g.clockElsewhere = true
	g.homeFile = filepath.Join(t.TempDir(), "home.json")
	f.zoneNow = "Tokyo Standard Time" // the other user's agent has Windows on Tokyo
	g.found, g.have = exit("Asia/Dushanbe", "TJ"), true
	g.reassert()
	g.release() // signing out
	if len(f.calls) > 0 {
		t.Errorf("changed %v while another agent keeps the clock", f.calls)
	}
	if _, err := os.Stat(g.homeFile); !os.IsNotExist(err) {
		t.Error("recorded the other user's setting as home")
	}
	if r := (*reports)[len(*reports)-1]; !r.Elsewhere || r.Home != (setting{}) {
		t.Errorf("report %+v", r)
	}
	f.zoneNow = home.Zone // the other user signed out, and their agent put Windows back
	h, err := loadHome(f, g.homeFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	g.takeClock(h)
	if f.zoneNow != tajik.Zone || f.regionNow != tajik.Region {
		t.Errorf("with the clock, Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	if hf := readHomeFile(t, g.homeFile); hf.setting != home || hf.Held == nil || *hf.Held != tajik {
		t.Errorf("home.json %+v", hf)
	}
}

// newHomeAgent is newAgent keeping home.json, with its connection already known to be in Tajikistan.
func newHomeAgent(t *testing.T) (*agent, *fakeZones) {
	g, f, _ := newAgent(t)
	g.homeFile = filepath.Join(t.TempDir(), "home.json")
	g.found, g.have = exit("Asia/Dushanbe", "TJ"), true
	return g, f
}

func readHomeFile(t *testing.T, path string) homeState {
	t.Helper()
	h, err := readHome(path)
	if err != nil {
		t.Fatalf("home.json: %v", err)
	}
	return h
}

var tajik = setting{"West Asia Standard Time", "TJ"}

// What the agent sets is what it holds: its own change, and the time zone change Windows announces
// for it, never become home.
func TestTheAgentsOwnChangeIsNotHome(t *testing.T) {
	g, f := newHomeAgent(t)
	g.reassert()
	g.reassert() // as WM_TIMECHANGE does, after the agent's own change
	if f.zoneNow != tajik.Zone || f.regionNow != tajik.Region {
		t.Fatalf("Windows on %s/%s", f.zoneNow, f.regionNow)
	}
	h := readHomeFile(t, g.homeFile)
	if h.setting != home || h.Held == nil || *h.Held != tajik {
		t.Errorf("home.json %+v, want home %v holding %v", h, home, tajik)
	}
}

func vpnOn() bool { return true }

// With no VPN up, the system stays home, exactly, whatever GeoJS says: your own ISP can show up in
// another zone of a big country. With one up, an exit in another zone moves it, in your own country too.
func TestTheClockMovesOnlyWhileAVPNIsUp(t *testing.T) {
	g, f, _ := newAgent(t)
	f.zoneNow, f.regionNow = "Eastern Standard Time", "US"
	g.home = setting{"Eastern Standard Time", "US"}
	up := false
	g.vpn = func() bool { return up }
	g.found, g.have = exit("America/Chicago", "US"), true // your own ISP, placed in Chicago
	g.reassert()
	if len(f.calls) > 0 {
		t.Errorf("with no VPN, moved Windows: %v", f.calls)
	}
	g.found = exit("Asia/Dushanbe", "TJ")
	g.reassert()
	if len(f.calls) > 0 {
		t.Errorf("with no VPN, moved Windows to an exit abroad: %v", f.calls)
	}
	up = true
	g.found = exit("America/Los_Angeles", "US")
	g.reassert()
	if f.zoneNow != "Pacific Standard Time" {
		t.Errorf("with a VPN to Los Angeles, Windows on %s", f.zoneNow)
	}
	up = false // the VPN drops
	g.reassert()
	if f.zoneNow != "Eastern Standard Time" || f.regionNow != "US" {
		t.Errorf("VPN down: Windows on %s/%s, want home", f.zoneNow, f.regionNow)
	}
	// A place in Alibi, with Follow Alibi's location on, needs no VPN.
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if f.zoneNow != "Tokyo Standard Time" {
		t.Errorf("Alibi's place: Windows on %s", f.zoneNow)
	}
}

// While Windows' own automatic time zone is on, the agent leaves the zone alone, as Whereabouts does,
// instead of putting it back every time. A zone it sets becomes home.
func TestAutomaticTimeZoneIsLeftAlone(t *testing.T) {
	g, f := newHomeAgent(t)
	auto := true
	g.autoZone = func() bool { return auto }
	g.reassert()
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if len(f.calls) > 0 {
		t.Fatalf("changed %v with automatic time zone on", f.calls)
	}
	f.zoneNow = "Arabian Standard Time" // where Windows' location service puts you
	g.reassert()
	if f.zoneNow != "Arabian Standard Time" || len(f.calls) > 0 {
		t.Errorf("put the zone back: %v", f.calls)
	}
	if h := readHomeFile(t, g.homeFile); h.Zone != "Arabian Standard Time" {
		t.Errorf("home.json %+v, want the automatic zone as home", h)
	}
	if r := g.reportLocked("", ""); !r.AutoZone {
		t.Error("the tray isn't told why the agent waits")
	}
	auto = false // switched off: the agent follows again, here Alibi's place
	g.reassert()
	if f.zoneNow != "Tokyo Standard Time" {
		t.Errorf("after switching it off, Windows on %s", f.zoneNow)
	}
}

// What counts as a VPN's adapter: PPP (IKEv2, SSTP, L2TP), a tunnel, a software adapter such as
// WireGuard's, or one that says it's a VPN's. Wi-Fi, Ethernet and Hyper-V's switches don't.
func TestIsVPNAdapter(t *testing.T) {
	for _, c := range []struct {
		ifType uint32
		names  []string
		want   bool
	}{
		{23, []string{"Office", "WAN Miniport (IKEv2)"}, true},
		{131, nil, true},
		{53, []string{"wg0", "WireGuard Tunnel"}, true},
		{6, []string{"Ethernet 3", "TAP-Windows Adapter V9"}, true},
		{6, []string{"OpenVPN Data Channel Offload for Surfshark", "OpenVPN Data Channel Offload"}, true},
		{71, []string{"Wi-Fi", "Intel(R) Wi-Fi 7 BE200 320MHz"}, false},
		{6, []string{"Ethernet", "Intel(R) Ethernet Controller I226-V"}, false},
		{6, []string{"vEthernet (Default Switch)", "Hyper-V Virtual Ethernet Adapter"}, false},
	} {
		if got := isVPNAdapter(c.ifType, c.names...); got != c.want {
			t.Errorf("%d %v: %v, want %v", c.ifType, c.names, got, c.want)
		}
	}
}

// You set a zone yourself while the connection comes out at home: that's home now, and it stays.
func TestAZoneYouSetAtHomeStays(t *testing.T) {
	g, f := newHomeAgent(t)
	g.found = exit("Asia/Tehran", "IR") // at home, no VPN
	g.vpn = func() bool { return false }
	g.reassert()
	if len(f.calls) > 0 {
		t.Fatalf("at home, changed %v", f.calls)
	}
	f.zoneNow = "Arabian Standard Time" // back from a trip, say: you set it in Settings
	g.reassert()
	if f.zoneNow != "Arabian Standard Time" {
		t.Errorf("your zone was put back to %s", f.zoneNow)
	}
	if h := readHomeFile(t, g.homeFile); h.Zone != "Arabian Standard Time" || h.Region != "US" || h.Held != nil {
		t.Errorf("home.json %+v, want your zone as home", h)
	}
}

// You set a zone yourself while on a VPN: that's home now, and the VPN's zone goes back on.
func TestAZoneYouSetOnAVPNBecomesHome(t *testing.T) {
	g, f := newHomeAgent(t)
	g.reassert() // on the VPN's zone
	f.zoneNow = "Arabian Standard Time"
	g.reassert()
	if f.zoneNow != tajik.Zone {
		t.Errorf("Windows on %s, want the VPN's zone back", f.zoneNow)
	}
	h := readHomeFile(t, g.homeFile)
	if h.Zone != "Arabian Standard Time" || h.Held == nil || *h.Held != tajik {
		t.Errorf("home.json %+v", h)
	}
	// Uninstalling, or quitting, now puts Windows back on the new home.
	if got, restored, err := restoreHome(f, g.homeFile); err != nil || !restored || got.Zone != "Arabian Standard Time" || f.zoneNow != "Arabian Standard Time" {
		t.Errorf("restore: %v %v %v, Windows on %s", got, restored, err, f.zoneNow)
	}
}

// You change only the time zone while the agent has the region on the VPN's country: home takes your
// zone and keeps its own region, and letting go puts that region back.
func TestAZoneYouSetKeepsHomesRegion(t *testing.T) {
	if got, held, _ := adopt(home, &tajik, nil, setting{"Arabian Standard Time", "TJ"}); got != (setting{"Arabian Standard Time", "US"}) || held == nil || *held != (setting{"Arabian Standard Time", "TJ"}) {
		t.Errorf("zone changed: home %v, held %v", got, held)
	}
	if got, held, _ := adopt(home, &tajik, nil, setting{tajik.Zone, "DE"}); got != (setting{home.Zone, "DE"}) || held == nil {
		t.Errorf("region changed: home %v, held %v", got, held)
	}
	if got, held, _ := adopt(home, &tajik, nil, setting{"Arabian Standard Time", "DE"}); got != (setting{"Arabian Standard Time", "DE"}) || held != nil {
		t.Errorf("both changed: home %v, held %v", got, held)
	}
	if got, held, _ := adopt(home, nil, nil, setting{"Arabian Standard Time", "US"}); got != (setting{"Arabian Standard Time", "US"}) || held != nil {
		t.Errorf("nothing held: home %v, held %v", got, held)
	}
	g, f := newHomeAgent(t)
	g.reassert() // the VPN's zone and region: West Asia, TJ
	f.zoneNow = "Arabian Standard Time"
	g.release()
	if f.zoneNow != "Arabian Standard Time" || f.regionNow != "US" {
		t.Errorf("after letting go, Windows on %s/%s, want your zone and home's region", f.zoneNow, f.regionNow)
	}
	// The same, while the agent wasn't running.
	path := filepath.Join(t.TempDir(), "home.json")
	saveHome(path, homeState{setting: home, Held: &tajik})
	f.zoneNow, f.regionNow = "Arabian Standard Time", "TJ"
	if h, _ := loadHome(f, path, nil); h.setting != (setting{"Arabian Standard Time", "US"}) || h.Held == nil {
		t.Errorf("loaded %+v", h)
	}
}

// pendingZones writes down, at each tzutil call, what home.json said the agent was about to set.
type pendingZones struct {
	*fakeZones
	path string
	seen []*setting
}

func (p *pendingZones) setZone(id string) error {
	h, _ := readHome(p.path)
	p.seen = append(p.seen, h.Pending)
	return p.fakeZones.setZone(id)
}

// The agent writes down what it's about to set before tzutil runs. An agent that died right after,
// or halfway (the region set, not yet the zone), still knows it was its own doing next time it starts,
// and home stays home.
func TestAChangeCutShortIsStillTheAgents(t *testing.T) {
	g, f := newHomeAgent(t)
	p := &pendingZones{fakeZones: f, path: g.homeFile}
	g.sys = p
	g.reassert()
	if len(p.seen) != 1 || p.seen[0] == nil || *p.seen[0] != tajik {
		t.Errorf("home.json said it was about to set %v at tzutil", p.seen)
	}
	if h := readHomeFile(t, g.homeFile); h.Pending != nil || h.Held == nil || *h.Held != tajik {
		t.Errorf("after the change, home.json %+v", h)
	}
	for _, c := range []struct {
		name string
		now  setting
	}{
		{"died after tzutil", tajik},
		{"died halfway", setting{home.Zone, tajik.Region}},
		{"died before tzutil", home},
	} {
		path := filepath.Join(t.TempDir(), "home.json")
		saveHome(path, homeState{setting: home, Pending: &tajik})
		f := atHome()
		f.zoneNow, f.regionNow = c.now.Zone, c.now.Region
		h, err := loadHome(f, path, nil)
		if err != nil || h.setting != home {
			t.Errorf("%s: home %+v (err %v), want %v kept", c.name, h, err, home)
		}
		if got, restored, _ := restoreHome(f, path); got != home || restored != (c.now != home) || f.zoneNow != home.Zone || f.regionNow != home.Region {
			t.Errorf("%s: uninstall restored %v %v, Windows %s/%s", c.name, got, restored, f.zoneNow, f.regionNow)
		}
	}
}

// While it holds the clock away from home, the agent leaves a note for other users' agents, and takes
// it away once it lets go.
func TestTheAgentLeavesANoteWhileItHoldsTheClock(t *testing.T) {
	dir := t.TempDir()
	mine, theirs := fileClockNotes{dir, "me.json"}, fileClockNotes{dir, "them.json"}
	g, _ := newHomeAgent(t)
	g.notes = mine
	g.reassert() // on the VPN's zone
	notes := theirs.others()
	if len(notes) != 1 || !slices.Equal(notes[0].Zones, []string{tajik.Zone}) || notes[0].Home != home.Zone {
		t.Errorf("other agents read %+v", notes)
	}
	if len(mine.others()) != 0 {
		t.Error("an agent reads its own note as another's")
	}
	g.finish()
	if notes := theirs.others(); len(notes) != 0 {
		t.Errorf("note still there after letting go: %+v", notes)
	}
}

// Another user's agent that stopped without letting go left Windows on its VPN's zone. The agent that
// takes the clock next doesn't take that zone as its user's home, and lets go of it like its own.
func TestAZoneAnotherAgentLeftIsNotHome(t *testing.T) {
	left := []clockNote{{Zones: []string{"Tokyo Standard Time"}, Home: home.Zone}}
	f := atHome()
	f.zoneNow = "Tokyo Standard Time"
	first := filepath.Join(t.TempDir(), "home.json")
	if h, err := loadHome(f, first, left); err != nil || h.setting != home || h.Held == nil || h.Held.Zone != "Tokyo Standard Time" {
		t.Errorf("first run: %+v (err %v), want the other agent's home, the zone held", h, err)
	}
	ran := filepath.Join(t.TempDir(), "home.json")
	saveHome(ran, homeState{setting: setting{"Arabian Standard Time", "US"}})
	if h, _ := loadHome(f, ran, left); h.Zone != "Arabian Standard Time" || h.Held == nil {
		t.Errorf("run before: %+v, want its own home kept", h)
	}
	if _, restored, _ := restoreHome(f, ran); !restored || f.zoneNow != "Arabian Standard Time" {
		t.Errorf("letting go: restored %v, Windows on %s", restored, f.zoneNow)
	}
	// Before then, what install and status call home is the other agent's too.
	f.zoneNow = "Tokyo Standard Time"
	if h, _ := currentHome(f, filepath.Join(t.TempDir(), "home.json"), left); h.Zone != home.Zone {
		t.Errorf("current home %+v, want the other agent's home", h)
	}
	// Without such a note, a zone found at start is a change of yours, as before.
	f.zoneNow = "Tokyo Standard Time"
	none := filepath.Join(t.TempDir(), "home.json")
	saveHome(none, homeState{setting: home})
	if h, _ := loadHome(f, none, nil); h.Zone != "Tokyo Standard Time" || h.Held != nil {
		t.Errorf("no note: %+v", h)
	}
}

// Switching following off puts Windows back home once, then leaves it alone: a zone you set after
// that isn't undone, and becomes home.
func TestFollowingOffLetsGoOnce(t *testing.T) {
	g, f := newHomeAgent(t)
	g.reassert()
	g.sync = func() bool { return false }
	g.release()
	if f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Fatalf("Windows on %s/%s, want home", f.zoneNow, f.regionNow)
	}
	f.zoneNow = "Arabian Standard Time"
	g.reassert()
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	if f.zoneNow != "Arabian Standard Time" {
		t.Errorf("with following off, Windows was moved to %s", f.zoneNow)
	}
	if h := readHomeFile(t, g.homeFile); h.Zone != "Arabian Standard Time" || h.Held != nil {
		t.Errorf("home.json %+v", h)
	}
}

// At the end of the session the agent puts Windows home, and nothing moves it after that: not the
// reassert its own time change broadcast queues, nor a look or a cover still on the way.
func TestFinishedAgentMovesNothing(t *testing.T) {
	g, f := newHomeAgent(t)
	g.reassert() // on the VPN's zone
	g.finish()
	if f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Fatalf("Windows on %s/%s, want home", f.zoneNow, f.regionNow)
	}
	f.calls = nil
	g.reassert() // WM_TIMECHANGE from putting it back
	g.setWish(&wish{Configuration: "tokyo", Timezone: "Asia/Tokyo"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	g.run(ctx)
	cancel()
	if len(f.calls) > 0 {
		t.Errorf("a finished agent changed %v", f.calls)
	}
}

// An agent that starts with following off, still holding what it set before, lets go of it once.
func TestAnAgentStartingWithFollowingOffLetsGo(t *testing.T) {
	g, f := newHomeAgent(t)
	f.zoneNow, f.regionNow = tajik.Zone, tajik.Region
	g.held = &tajik
	g.sync = func() bool { return false }
	g.reassert()
	if f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Errorf("Windows on %s/%s, want home", f.zoneNow, f.regionNow)
	}
}

// Letting go leaves a zone someone else set since the agent's change alone: that's home now.
func TestLettingGoLeavesSomeoneElsesZone(t *testing.T) {
	g, f := newHomeAgent(t)
	g.reassert()
	f.zoneNow = "Arabian Standard Time" // Windows' automatic time zone, say
	g.release()
	if f.zoneNow != "Arabian Standard Time" {
		t.Errorf("Windows on %s", f.zoneNow)
	}
	if h := readHomeFile(t, g.homeFile); h.Zone != "Arabian Standard Time" || h.Held != nil {
		t.Errorf("home.json %+v", h)
	}
}

// As the agent starts: home stays recorded while Windows is still where the agent left it; otherwise
// how Windows is set now is home.
func TestLoadHome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.json")
	f := atHome()
	if h, err := loadHome(f, path, nil); err != nil || h.setting != home || h.Held != nil {
		t.Fatalf("first run: %+v %v", h, err)
	}
	saveHome(path, homeState{setting: home, Held: &tajik})
	f.zoneNow, f.regionNow = tajik.Zone, tajik.Region // still where the agent left it
	if h, _ := loadHome(f, path, nil); h.setting != home || h.Held == nil || *h.Held != tajik {
		t.Errorf("still held: %+v", h)
	}
	f.zoneNow = "Arabian Standard Time" // changed while the agent wasn't running; the region is still its
	if h, _ := loadHome(f, path, nil); h.Zone != "Arabian Standard Time" || h.Region != "US" || h.Held == nil || h.Held.Region != "TJ" {
		t.Errorf("changed meanwhile: %+v", h)
	}
	// An earlier agent recorded home once and held Windows from then on.
	os.WriteFile(path, []byte(`{"zone":"Iran Standard Time","region":"US"}`), 0o644)
	f.zoneNow, f.regionNow = tajik.Zone, tajik.Region
	if h, _ := loadHome(f, path, nil); h.setting != home || h.Held == nil || *h.Held != tajik || h.Version != 1 {
		t.Errorf("earlier home.json: %+v", h)
	}
}

// Uninstalling puts Windows back home only if it's still where the agent left it.
func TestRestoreHome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.json")
	f := atHome()
	f.zoneNow, f.regionNow = tajik.Zone, tajik.Region
	saveHome(path, homeState{setting: home, Held: &tajik})
	if got, restored, err := restoreHome(f, path); err != nil || !restored || got != home || f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Errorf("held: %v %v %v, Windows %s/%s", got, restored, err, f.zoneNow, f.regionNow)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("home.json still there after restoring")
	}
	if _, restored, err := restoreHome(f, path); restored || err != nil {
		t.Errorf("second restore: %v %v", restored, err)
	}
	f.zoneNow = "Arabian Standard Time" // yours since
	saveHome(path, homeState{setting: home, Held: &tajik})
	if _, restored, _ := restoreHome(f, path); restored || f.zoneNow != "Arabian Standard Time" {
		t.Errorf("undid a zone set since: restored %v, Windows on %s", restored, f.zoneNow)
	}
}
