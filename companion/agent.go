package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The agent keeps the system on the time zone of wherever the connection comes out. Every program
// then reads the VPN's zone natively: a page's very first script, its workers of every kind, and
// apps outside the browser. With Follow Alibi's location on in the tray, a place Alibi has for all
// sites moves it there instead. Off and Match my IP address in Alibi ask nothing of the system.
//
// On Windows it also sets the region ("Country or region") when, and only when, that changes the
// name browsers give the zone: the W. Europe zone reads as Europe/Oslo only with the region set to
// Norway, and as Europe/Berlin otherwise. When the connection comes out at home again, Windows goes
// back to exactly how it was before the agent first ran.

// zoneSystem is the system's time zone and region, as the agent reads and sets them.
type zoneSystem interface {
	zone() (string, error)       // the Windows time zone ID now, as tzutil /g prints it
	setZone(id string) error     // as tzutil /s does
	region() (string, error)     // the two-letter region now
	setRegion(code string) error //
	// The Windows zone for an IANA zone, and the IANA zone browsers read for a Windows zone in a region.
	windowsZone(iana string) (string, error)
	ianaZone(windowsID, region string) (string, error)
	// The IANA zone browsers read now.
	hostZone() (string, error)
	// Tells running programs the time zone changed, so they read it again.
	announce()
}

// setting is a Windows time zone ID and a two-letter region.
type setting struct {
	Zone   string `json:"zone"`
	Region string `json:"region"`
}

// tzutil marks a zone set with daylight saving switched off with this suffix.
func baseZone(id string) string { return strings.TrimSuffix(id, "_dstoff") }

// wanted is how the system should be set for a connection that comes out at place.
func wanted(sys zoneSystem, place cover, home setting) (setting, error) {
	zone, err := sys.windowsZone(place.Timezone)
	if err != nil {
		return setting{}, fmt.Errorf("no Windows time zone for %s: %w", place.Timezone, err)
	}
	name := func(region string) string {
		n, _ := sys.ianaZone(zone, region)
		return n
	}
	region := home.Region
	if place.Country != "" && name(home.Region) != name(place.Country) {
		region = place.Country
	}
	// Home's own zone and region: exactly what was there, its daylight saving choice included.
	if zone == baseZone(home.Zone) && region == home.Region {
		return home, nil
	}
	return setting{Zone: zone, Region: region}, nil
}

// apply sets the system to want, changing only what differs, and reports whether it changed anything.
func apply(sys zoneSystem, want setting) (bool, error) {
	zone, err := sys.zone()
	if err != nil {
		return false, err
	}
	region, err := sys.region()
	if err != nil {
		return false, err
	}
	changed := false
	// The region first: programs read the zone's name again when the zone changes.
	if want.Region != region {
		if err := sys.setRegion(want.Region); err != nil {
			return false, fmt.Errorf("couldn't set the region to %s: %w", want.Region, err)
		}
		changed = true
	}
	if want.Zone != zone {
		if err := sys.setZone(want.Zone); err != nil {
			return changed, fmt.Errorf("couldn't set the time zone to %s: %w", want.Zone, err)
		}
		changed = true
	}
	if changed {
		sys.announce()
	}
	return changed, nil
}

type agentConfig struct {
	watch watchConfig
	sys   zoneSystem
	// Why the system's clock can't be moved here at all (an older Windows); nil when it can. The agent
	// then only looks the connection up, for the tray and the host.
	unsupported error
	// Another user's agent keeps the system's clock, which every user shares: this one moves nothing
	// and records no home until it's handed the clock (takeClock).
	clockElsewhere bool
	// Home, what the agent last set the system to, if it holds it anywhere, and the country the
	// connection comes out in at home, "" for home's zone's (home.json, homeState).
	home   setting
	held   *setting
	homeIn string
	// Where home and what's held are kept as they change; "" keeps them in memory only.
	homeFile string
	state    string // where the agent writes its word for the host (agent.json)
	// Look and report what would change, without changing the system or writing the state.
	dryRun bool
	// Whether to move the system now (the tray can switch it off); nil means always.
	sync func() bool
	// Whether a place Alibi has for all sites moves the system there (the tray's Follow Alibi's
	// location); nil means never.
	followAlibi func() bool
	// Whether a VPN is up. Only then does the system follow where the connection comes out; without
	// one that place is only approximate, so it stays home, as on the Mac. nil means never.
	vpn func() bool
	// The country set as home in the tray, if one is; otherwise home's zone says.
	homeCountry func() string
	// Where this agent tells other users' agents what it does with the clock they share; nil for nowhere.
	notes clockNotes
	// Told after every look and every change of Alibi's cover.
	report func(agentReport)
	logf   func(format string, args ...any)
}

// agentReport is what the tray hears from the agent.
type agentReport struct {
	Place       cover   // the connection's place; empty before the first answer
	Err         error   // why the last look failed, if it did
	Wish        *wish   // the place Alibi's running browsers ask for; nil for none
	System      string  // the zone the agent holds the system's clock on, as browsers read it; "" for none
	Target      string  // the zone the system should read now, as browsers read it; "" when unknown
	Home        setting // home as it stands now
	HomeZone    string  // home's zone as browsers read it
	HomeCountry string  // where the connection comes out when it's home
	Elsewhere   bool    // another user's agent keeps the clock
	VPN         bool    // a VPN is up, so the system follows the connection
}

// runAgent keeps the system on the connection's place until ctx ends.
func runAgent(ctx context.Context, a agentConfig) error {
	return (&agent{agentConfig: a}).run(ctx)
}

// agent is a running agentConfig, with the last place it found and Alibi's last word on its cover,
// so it can put the system right the moment either changes, or something else moves it.
type agent struct {
	agentConfig
	mu      sync.Mutex
	found   cover // as GeoJS gave it
	have    bool
	lastErr error
	wish    *wish
	h       *homeState // home as it stands, from agentConfig's at first
	// finish has run: the agent has let go for good and moves nothing more, as the session ends or it quits.
	finished bool
}

// homeState is home.json: how the system is set when the agent holds it nowhere, and what the agent
// last set it to while it does. A setting that's neither was made by someone else (you, back from a
// trip, or Windows' own automatic time zone), and becomes home.
type homeState struct {
	setting
	Held *setting `json:"held,omitempty"`
	// What the agent is about to set: written before it calls tzutil, cleared once it has. An agent that
	// dies in between still knows the change, or the part of it that took, was its own.
	Pending *setting `json:"pending,omitempty"`
	// Where the connection comes out at home: home's zone's country when it was first recorded. A zone
	// you set yourself later doesn't change it. The tray's alerts go by it.
	Country string `json:"country,omitempty"`
	Version int    `json:"version"` // 0: written before held was, when home was recorded once and kept
}

func (g *agent) run(ctx context.Context) error {
	return watch(ctx, g.watch, func(place cover, err error) error {
		if err != nil && ctx.Err() == nil {
			g.logf("couldn't look up the connection: %v", err)
		}
		g.mu.Lock()
		if err == nil {
			g.found, g.have = place, true
		}
		g.lastErr = err
		// After a failed look too: a zone moved back while offline goes where it was.
		r := g.syncLocked()
		g.mu.Unlock()
		if g.report != nil && ctx.Err() == nil {
			g.report(r)
		}
		return nil
	})
}

// setWish takes the place Alibi's running browsers ask for, or nil for none, and moves the system's
// clock at once.
func (g *agent) setWish(w *wish) {
	g.mu.Lock()
	if g.wish != nil && w != nil && *g.wish == *w || g.wish == nil && w == nil {
		g.mu.Unlock()
		return
	}
	g.wish = w
	r := g.syncLocked()
	g.mu.Unlock()
	if g.report != nil {
		g.report(r)
	}
}

// reassert puts the system back where it should be, without looking anything up again. The tray
// calls it whenever Windows says the time zone changed; after the agent's own changes it finds
// nothing to do.
func (g *agent) reassert() {
	g.mu.Lock()
	r := g.syncLocked()
	g.mu.Unlock()
	if g.report != nil {
		g.report(r)
	}
}

// release lets go of the system (letGoLocked), under the same lock as every move, so it can't cross
// one already under way: when the tray stops following, and when the agent stops.
func (g *agent) release() {
	g.mu.Lock()
	if g.unsupported == nil && !g.clockElsewhere {
		g.letGoLocked()
	}
	r := g.reportLocked("", "")
	g.mu.Unlock()
	if g.report != nil {
		g.report(r)
	}
}

// finish lets go of the system for good: Windows goes back home (letGoLocked), and from then on
// nothing moves it. Putting it back broadcasts a time change, which the agent's own window hears and
// queues a reassert for; that one, and any other still on its way, finds the agent finished and does
// nothing. For the end of the session, and Quit.
func (g *agent) finish() {
	g.mu.Lock()
	if g.unsupported == nil && !g.clockElsewhere && !g.finished {
		g.letGoLocked()
	}
	g.finished = true
	r := g.reportLocked("", "")
	g.mu.Unlock()
	if g.report != nil {
		g.report(r)
	}
}

// takeClock hands the agent the system's clock, with home as it loaded then, and moves the system
// where it should be at once.
func (g *agent) takeClock(h homeState) {
	g.mu.Lock()
	g.clockElsewhere, g.h = false, &h
	r := g.syncLocked()
	g.mu.Unlock()
	if g.report != nil {
		g.report(r)
	}
}

// syncLocked moves the system where it should be now, writes the agent's word for the host, and
// reports. The caller holds g.mu.
func (g *agent) syncLocked() agentReport {
	place, system, target := g.settle(g.found, g.have, g.wish)
	if !g.have {
		place = cover{}
	}
	if !g.dryRun {
		// After every look and change, so the host can tell the agent still runs.
		if err := writeAgentState(g.state, agentState{Cover: place, System: system}); err != nil {
			g.logf("%v", err)
		}
	}
	r := g.reportLocked(system, target)
	r.Place = place
	return r
}

func (g *agent) reportLocked(system, target string) agentReport {
	r := agentReport{Err: g.lastErr, Wish: g.wish, System: system, Target: target, Elsewhere: g.clockElsewhere, VPN: g.vpnUp()}
	if g.unsupported != nil || g.clockElsewhere {
		return r
	}
	h := g.homeLocked()
	r.Home, r.HomeCountry = h.setting, g.homeCountryLocked()
	r.HomeZone, _ = g.sys.ianaZone(baseZone(h.Zone), h.Region)
	return r
}

// homeLocked is home as it stands.
func (g *agent) homeLocked() *homeState {
	if g.h == nil {
		g.h = &homeState{setting: g.home, Held: g.held, Country: g.homeIn, Version: 1}
		if g.h.Country == "" {
			g.h.Country = countryOf(g.sys, g.home)
		}
	}
	return g.h
}

func (g *agent) saveHomeLocked() {
	if g.dryRun {
		return
	}
	h := g.homeLocked()
	if g.homeFile != "" {
		if err := saveHome(g.homeFile, *h); err != nil {
			g.logf("couldn't save home: %v", err)
		}
	}
	if g.notes == nil {
		return
	}
	if note := noteFor(*h); len(note.Zones) == 0 {
		g.notes.clear()
	} else if err := g.notes.write(note); err != nil {
		g.logf("couldn't tell other users' agents about the clock: %v", err)
	}
}

// clockNote is what an agent tells other users' agents about the time zone they all share: the zones
// it holds Windows on or is about to set, and its own home zone. It's gone once the agent lets go, so a
// note still there when another agent gets the clock was left by one that stopped without letting go,
// and the zone it names is that agent's doing, not the next user's.
type clockNote struct {
	Zones []string `json:"zones"`
	Home  string   `json:"home"`
}

// clockNotes is where every user's agent on this computer leaves its note: each writes only its own.
type clockNotes interface {
	write(clockNote) error
	clear()
	others() []clockNote
}

// noteFor is the note for home as it stands: the zones the agent holds, or is about to set, that
// aren't home's.
func noteFor(h homeState) clockNote {
	note := clockNote{Home: h.Zone}
	for _, s := range []*setting{h.Held, h.Pending} {
		if s != nil && s.Zone != h.Zone && !slices.Contains(note.Zones, s.Zone) {
			note.Zones = append(note.Zones, s.Zone)
		}
	}
	return note
}

// leftBy finds the note of another user's agent that left the system on zone, if one did.
func leftBy(notes []clockNote, zone string) (clockNote, bool) {
	for _, n := range notes {
		if slices.Contains(n.Zones, zone) {
			return n, true
		}
	}
	return clockNote{}, false
}

// fileClockNotes keeps each agent's note as a file of its own, name, in a folder every user can read
// and add to (on Windows, in ProgramData).
type fileClockNotes struct{ dir, name string }

func (n fileClockNotes) write(note clockNote) error {
	b, _ := json.Marshal(note)
	return writeFileAtomic(filepath.Join(n.dir, n.name), b, 0o644)
}

func (n fileClockNotes) clear() { os.Remove(filepath.Join(n.dir, n.name)) }

func (n fileClockNotes) others() []clockNote {
	paths, _ := filepath.Glob(filepath.Join(n.dir, "*.json"))
	var notes []clockNote
	for _, path := range paths {
		if filepath.Base(path) == n.name {
			continue
		}
		var note clockNote
		if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &note) == nil {
			notes = append(notes, note)
		}
	}
	return notes
}

// noticeLocked takes a setting someone else made as home from now on. What the agent set itself is what
// it holds, so that doesn't count.
// It returns how the system is set now, and whether it could tell.
func (g *agent) noticeLocked() (setting, bool) {
	h := g.homeLocked()
	now, err := currentSetting(g.sys)
	if err != nil {
		return setting{}, false
	}
	home, held, theirs := adopt(h.setting, h.Held, h.Pending, now)
	if !theirs {
		// All of it the agent's own, or home's: at most, what it was about to set has taken.
		if h.Pending != nil || !sameSetting(held, h.Held) {
			h.Held, h.Pending = held, nil
			g.saveHomeLocked()
		}
		return now, true
	}
	g.logf("Windows was set to %s, region %s, by something else, so home is now %s, region %s", now.Zone, now.Region, home.Zone, home.Region)
	*h = homeState{setting: home, Held: held, Country: h.Country, Version: 1}
	g.saveHomeLocked()
	return now, true
}

// adopt takes what someone else changed as home: the zone, the region, or both, wherever it differs
// from what the agent left (what it held, or home while it held nothing) and from what it was about to
// set. The part they didn't touch keeps home's: a zone you set while the agent had the region on the
// VPN's country leaves home's region as it was. It returns the new home, what the agent still holds
// (nil when nothing is its own), and whether someone else changed anything at all.
func adopt(home setting, held, pending *setting, now setting) (setting, *setting, bool) {
	left := home
	if held != nil {
		left = *held
	}
	next, theirs := home, false
	if now.Zone != left.Zone && (pending == nil || now.Zone != pending.Zone) {
		next.Zone, theirs = now.Zone, true
	}
	if now.Region != left.Region && (pending == nil || now.Region != pending.Region) {
		next.Region, theirs = now.Region, true
	}
	if next == now {
		return next, nil, theirs
	}
	return next, &now, theirs
}

func sameSetting(a, b *setting) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// letGoLocked puts the system back home, once, if it's still where the agent left it, and from then on
// holds it nowhere: Windows is yours again. Moved since by someone else, it stays put, and that's home.
func (g *agent) letGoLocked() {
	g.noticeLocked()
	h := g.homeLocked()
	if h.Held == nil {
		return
	}
	if _, err := apply(g.sys, h.setting); err != nil {
		g.logf("couldn't put the time zone back: %v", err)
		return
	}
	g.logf("put Windows back on %s, region %s", h.Zone, h.Region)
	h.Held = nil
	g.saveHomeLocked()
}

// holdLocked records what the agent has just set the system to: nothing held, when that's home.
func (g *agent) holdLocked() {
	h := g.homeLocked()
	now, err := currentSetting(g.sys)
	if err != nil {
		return
	}
	held := &now
	if now == h.setting {
		held = nil
	}
	if sameSetting(held, h.Held) && h.Pending == nil {
		return
	}
	h.Held, h.Pending = held, nil
	g.saveHomeLocked()
}

// intendLocked writes down what the agent is about to set, before it sets it.
func (g *agent) intendLocked(want setting) {
	g.homeLocked().Pending = &want
	g.saveHomeLocked()
}

func (g *agent) vpnUp() bool { return g.vpn != nil && g.vpn() }

// homeCountryLocked is the country the connection comes out in when it's home: the one set in the
// tray's settings, if any, or home's own.
func (g *agent) homeCountryLocked() string {
	if g.homeCountry != nil {
		if c := g.homeCountry(); c != "" {
			return c
		}
	}
	return g.homeLocked().Country
}

// countryOf is the country of a setting's zone, as browsers name the zone.
func countryOf(sys zoneSystem, s setting) string {
	zone, _ := sys.ianaZone(baseZone(s.Zone), s.Region)
	return zoneCountries[zone]
}

// placeOf is the zone of the place Alibi has for all sites, when the agent follows it; "" otherwise.
// Off and "Match my IP address" name no place: Alibi doesn't mind where the system is.
func (a agentConfig) placeOf(w *wish) string {
	if w == nil || !w.namesPlace() {
		return ""
	}
	if a.followAlibi == nil || !a.followAlibi() {
		return ""
	}
	return w.Timezone
}

// settle moves the system's clock where it should be: to the place Alibi has for all sites, with
// Follow Alibi's location on, or otherwise to where the connection comes out; exactly home when that's
// the home country. It returns the connection's place for the host, its zone as browsers now read it
// when the system's on it; the zone it holds the system on, or ""; and the zone the system should read.
// Whenever the system isn't there, the place keeps GeoJS's zone, so Alibi fakes it rather than showing
// the real one. With sync off it puts the system back home once, then leaves it alone, but still says
// when it matches. The caller holds g.mu.
func (g *agent) settle(found cover, have bool, w *wish) (cover, string, string) {
	if g.unsupported != nil || g.clockElsewhere || g.finished {
		return found, "", ""
	}
	place := g.placeOf(w)
	syncing := g.sync == nil || g.sync()
	var now setting
	known := false
	if !g.dryRun {
		now, known = g.noticeLocked()
		if !syncing {
			g.letGoLocked()
		}
	}
	home := g.homeLocked().setting
	var want setting
	var err error
	var why string
	switch {
	case place == "" && !have:
		return found, "", "" // nothing to aim at before the first answer
	case place == "" && !g.vpnUp():
		// No VPN: the connection's place is only approximate (your own ISP can show up in another zone
		// of a big country), so the system stays home, exactly.
		want, why = home, "no VPN is up"
	case place == "":
		want, err = wanted(g.sys, found, home)
		why = fmt.Sprintf("connection comes out in %s (%s)", found.Timezone, found.Country)
	default:
		want, err = wanted(g.sys, cover{Timezone: place, Country: zoneCountries[place]}, home)
		why = "Alibi's location is " + place
	}
	if err != nil {
		g.logf("%v", err)
		return found, "", ""
	}
	expected, _ := g.sys.ianaZone(baseZone(want.Zone), want.Region)
	if g.dryRun {
		now, _ := currentSetting(g.sys)
		g.logf("%s; Windows is on %s, region %s; would set %s, region %s, which browsers read as %s", why, now.Zone, now.Region, want.Zone, want.Region, expected)
		return found, "", expected
	}
	changed := false
	if syncing {
		if !known || now != want {
			g.intendLocked(want)
		}
		changed, err = apply(g.sys, want)
		if changed || g.homeLocked().Pending != nil {
			g.holdLocked() // what it set, even if only part of it took
		}
		if err != nil {
			g.logf("%v", err)
			return found, "", expected
		}
	}
	reads, err := g.sys.hostZone()
	if err != nil || reads == "" || reads != expected {
		if syncing {
			// Something set it back (Windows' "Set time zone automatically", say), or reads oddly.
			g.logf("the system reads as %q, not %q, so Alibi keeps covering the zone", reads, expected)
		}
		return found, "", expected
	}
	if changed {
		g.logf("%s: set Windows to %s, region %s, which browsers read as %s", why, want.Zone, want.Region, reads)
	}
	if place == "" {
		found.Timezone = reads
	}
	if !syncing {
		return found, "", expected
	}
	return found, reads, expected
}

// worker runs jobs one at a time, off the caller's thread, until ctx ends, and none after. It counts
// itself in running, so once running.Wait returns, nothing it does can still land: a time change that
// arrived as the agent quit can't put the cover back after Quit has put Windows home.
func worker(ctx context.Context, running *sync.WaitGroup) func(func()) {
	jobs := make(chan func(), 16)
	running.Add(1)
	go func() {
		defer running.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-jobs:
				if ctx.Err() == nil {
					job()
				}
			}
		}
	}()
	return func(job func()) {
		select {
		case jobs <- job:
		case <-ctx.Done(): // stopping: nothing more to do
		}
	}
}

// currentSetting is how the system is set now.
func currentSetting(sys zoneSystem) (setting, error) {
	zone, err := sys.zone()
	if err != nil {
		return setting{}, err
	}
	region, err := sys.region()
	if err != nil {
		return setting{}, err
	}
	return setting{Zone: zone, Region: region}, nil
}

func writeAgentState(path string, state agentState) error {
	state.Updated = time.Now().UTC()
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o644)
}

// errNoHome: home.json is there, but doesn't say how the system is at home.
var errNoHome = errors.New("home.json doesn't say how the system is at home")

func readHome(path string) (homeState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return homeState{}, err
	}
	var h homeState
	if err := json.Unmarshal(b, &h); err != nil || h.Zone == "" || h.Region == "" {
		return homeState{}, errNoHome
	}
	return h, nil
}

func saveHome(path string, h homeState) error {
	h.Version = 1
	b, _ := json.MarshalIndent(h, "", "  ")
	return writeFileAtomic(path, append(b, '\n'), 0o644)
}

// loadHome is home as the agent starts. Unless the system is still where the agent left it, how it's
// set now is home: on the first run, and after a change made while the agent wasn't running. A zone
// that another user's agent left the system on (others, their notes) is no change of yours: home keeps
// its zone, or on the first run takes that agent's home zone, and the zone counts as held, so letting
// go puts it back.
func loadHome(sys zoneSystem, path string, others []clockNote) (homeState, error) {
	now, err := currentSetting(sys)
	if err != nil {
		return homeState{}, err
	}
	h, err := readHome(path)
	note, foreign := leftBy(others, now.Zone)
	switch {
	case foreign && err == nil && h.Version > 0:
		left := h.setting
		if h.Held != nil {
			left = *h.Held
		}
		h.Held = &setting{Zone: now.Zone, Region: left.Region}
		home, held, _ := adopt(h.setting, h.Held, h.Pending, now)
		h = homeState{setting: home, Held: held, Country: h.Country}
	case foreign && errors.Is(err, os.ErrNotExist) && note.Home != "":
		h = homeState{setting: setting{Zone: note.Home, Region: now.Region}, Held: &now}
	case err == nil && h.Version == 0:
		// Recorded once by an earlier agent, which held the system wherever it was from then on.
		h.Held = &now
		if now == h.setting {
			h.Held = nil
		}
	case err == nil:
		// What the agent left, or was about to set, is still its own; anything else changed while it
		// wasn't running, and is home now.
		home, held, _ := adopt(h.setting, h.Held, h.Pending, now)
		h = homeState{setting: home, Held: held, Country: h.Country}
	case !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errNoHome):
		return homeState{}, err
	default:
		h = homeState{setting: now}
	}
	if h.Country == "" {
		h.Country = countryOf(sys, h.setting)
	}
	h.Version = 1
	if h.Held != nil && *h.Held == h.setting {
		h.Held = nil
	}
	return h, saveHome(path, h)
}

// currentHome is home as the agent last recorded it, or how the system is set now if it hasn't yet.
// It changes nothing: only the agent records home.
func currentHome(sys zoneSystem, path string) (setting, error) {
	if h, err := readHome(path); err == nil {
		return h.setting, nil
	}
	return currentSetting(sys)
}

// restoreHome puts the system back home, if it's still where the agent left it, and forgets home.
func restoreHome(sys zoneSystem, path string) (setting, bool, error) {
	h, err := readHome(path)
	if errors.Is(err, os.ErrNotExist) {
		return setting{}, false, nil
	}
	if err != nil {
		return setting{}, false, err
	}
	now, err := currentSetting(sys)
	if err != nil {
		return h.setting, false, err
	}
	// What the agent left, or was about to set, goes back to home's; what someone else set since stays.
	// An earlier agent held the system for as long as it ran.
	home, held, _ := adopt(h.setting, h.Held, h.Pending, now)
	if h.Version == 0 {
		home, held = h.setting, &now
	}
	if held == nil || now == home {
		return home, false, os.Remove(path)
	}
	if _, err := apply(sys, home); err != nil {
		return home, false, err
	}
	return home, true, os.Remove(path)
}
