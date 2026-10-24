package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
func (a agentConfig) settleExit(place cover) cover {
	settled, _, _ := a.settle(place, true, nil)
	return settled
}

func TestSettleReportsTheZoneAsBrowsersReadIt(t *testing.T) {
	f := atHome()
	a := agentConfig{sys: f, home: home, logf: t.Logf}
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
	if got := (agentConfig{sys: reverted, home: home, logf: t.Logf}).settleExit(exit("America/Detroit", "US")); got.Timezone != "America/Detroit" {
		t.Errorf("reverted: reported %s, want GeoJS's America/Detroit", got.Timezone)
	}
	denied := atHome()
	denied.failSet = true
	if got := (agentConfig{sys: denied, home: home, logf: t.Logf}).settleExit(exit("Europe/Oslo", "NO")); got.Timezone != "Europe/Oslo" {
		t.Errorf("denied: reported %s", got.Timezone)
	}
	if got := (agentConfig{sys: atHome(), home: home, logf: t.Logf}).settleExit(exit("Mars/Olympus", "MA")); got.Timezone != "Mars/Olympus" {
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
		sys:   f, home: home, state: state, dryRun: true, logf: t.Logf,
	})
	if len(f.calls) > 0 {
		t.Errorf("dry run changed %v", f.calls)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("dry run wrote its state")
	}
}

func TestHomeIsRecordedOnceAndRestored(t *testing.T) {
	f := atHome()
	path := filepath.Join(t.TempDir(), "home.json")
	if got, err := loadHome(f, path); err != nil || got != home {
		t.Fatalf("first: %v %v", got, err)
	}
	apply(f, setting{"W. Europe Standard Time", "NO"})
	if got, _ := loadHome(f, path); got != home {
		t.Errorf("after a change, home = %v, want the first one recorded", got)
	}
	got, restored, err := restoreHome(f, path)
	if err != nil || !restored || got != home || f.zoneNow != home.Zone || f.regionNow != home.Region {
		t.Errorf("restore: %v %v %v, Windows %s/%s", got, restored, err, f.zoneNow, f.regionNow)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("home.json still there after restoring")
	}
	if _, restored, err := restoreHome(f, path); restored || err != nil {
		t.Errorf("second restore: %v %v", restored, err)
	}
}
