package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	home  setting
	state string // where the agent writes its word for the host (agent.json)
	// Look and report what would change, without changing the system or writing the state.
	dryRun bool
	// Whether to move the system now (the tray can switch it off); nil means always.
	sync func() bool
	// Whether a place Alibi has for all sites moves the system there (the tray's Follow Alibi's
	// location); nil means never.
	followAlibi func() bool
	// Told after every look and every change of Alibi's cover.
	report func(agentReport)
	logf   func(format string, args ...any)
}

// agentReport is what the tray hears from the agent.
type agentReport struct {
	Place  cover  // the connection's place; empty before the first answer
	Err    error  // why the last look failed, if it did
	Wish   *wish  // Alibi's All sites cover; nil until Alibi says
	System string // the zone the agent holds the system's clock on, as browsers read it; "" for none
	Target string // the zone the system should read now, as browsers read it; "" when unknown
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

// setWish takes Alibi's All sites cover, and moves the system's clock to it at once.
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
	return agentReport{Place: place, Err: g.lastErr, Wish: g.wish, System: system, Target: target}
}

// placeOf is the zone of the place Alibi has for all sites, when the agent follows it; "" otherwise.
// Off and "Match my IP address" name no place: Alibi doesn't mind where the system is.
func (a agentConfig) placeOf(w *wish) string {
	if w == nil || w.Configuration == "browserDefault" || w.Configuration == "ipAddress" || w.Timezone == "" {
		return ""
	}
	if a.followAlibi == nil || !a.followAlibi() {
		return ""
	}
	return w.Timezone
}

// settle moves the system's clock where it should be: to the place Alibi has for all sites, with
// Follow Alibi's location on, or otherwise to where the connection comes out. It returns the
// connection's place for the host, its zone as browsers now read it when the system's on it; the zone
// it holds the system on, or ""; and the zone the system should read. Whenever the system isn't there,
// the place keeps GeoJS's zone, so Alibi fakes it rather than showing the real one. With sync off it
// leaves the system alone, but still says when it matches.
func (a agentConfig) settle(found cover, have bool, w *wish) (cover, string, string) {
	place := a.placeOf(w)
	var want setting
	var err error
	var why string
	switch {
	case place == "" && !have:
		return found, "", "" // nothing to aim at before the first answer
	case place == "":
		want, err = wanted(a.sys, found, a.home)
		why = fmt.Sprintf("connection comes out in %s (%s)", found.Timezone, found.Country)
	default:
		want, err = wanted(a.sys, cover{Timezone: place, Country: zoneCountries[place]}, a.home)
		why = "Alibi's location is " + place
	}
	if err != nil {
		a.logf("%v", err)
		return found, "", ""
	}
	expected, _ := a.sys.ianaZone(baseZone(want.Zone), want.Region)
	if a.dryRun {
		now := setting{}
		now.Zone, _ = a.sys.zone()
		now.Region, _ = a.sys.region()
		a.logf("%s; Windows is on %s, region %s; would set %s, region %s, which browsers read as %s", why, now.Zone, now.Region, want.Zone, want.Region, expected)
		return found, "", expected
	}
	syncing := a.sync == nil || a.sync()
	changed := false
	if syncing {
		if changed, err = apply(a.sys, want); err != nil {
			a.logf("%v", err)
			return found, "", expected
		}
	}
	reads, err := a.sys.hostZone()
	if err != nil || reads == "" || reads != expected {
		if syncing {
			// Something set it back (Windows' "Set time zone automatically", say), or reads oddly.
			a.logf("the system reads as %q, not %q, so Alibi keeps covering the zone", reads, expected)
		}
		return found, "", expected
	}
	if changed {
		a.logf("%s: set Windows to %s, region %s, which browsers read as %s", why, want.Zone, want.Region, reads)
	}
	if place == "" {
		found.Timezone = reads
	}
	if !syncing {
		return found, "", expected
	}
	return found, reads, expected
}

func writeAgentState(path string, state agentState) error {
	state.Updated = time.Now().UTC()
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o644)
}

// loadHome reads how the system was before the agent first changed it, saving that first if needed.
func loadHome(sys zoneSystem, path string) (setting, error) {
	if b, err := os.ReadFile(path); err == nil {
		var home setting
		if json.Unmarshal(b, &home) == nil && home.Zone != "" && home.Region != "" {
			return home, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return setting{}, err
	}
	home := setting{}
	var err error
	if home.Zone, err = sys.zone(); err != nil {
		return setting{}, err
	}
	if home.Region, err = sys.region(); err != nil {
		return setting{}, err
	}
	b, _ := json.MarshalIndent(home, "", "  ")
	return home, writeFileAtomic(path, append(b, '\n'), 0o644)
}

// restoreHome puts the system back how it was before the agent, and forgets it.
func restoreHome(sys zoneSystem, path string) (setting, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return setting{}, false, nil
	}
	if err != nil {
		return setting{}, false, err
	}
	var home setting
	if err := json.Unmarshal(b, &home); err != nil || home.Zone == "" || home.Region == "" {
		return setting{}, false, fmt.Errorf("%s doesn't say how the system was", path)
	}
	if _, err := apply(sys, home); err != nil {
		return home, false, err
	}
	return home, true, os.Remove(path)
}
