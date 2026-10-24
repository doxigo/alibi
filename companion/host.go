package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// host runs for as long as the browser keeps Alibi's port open. It says hello, reports where the
// connection appears to be and then each change, passes the All sites cover Alibi sends it on to the
// agent, and exits when the browser closes the port.
func host(origin string, stdin io.Reader, stdout, stderr io.Writer) int {
	logger := log.New(stderr, "alibi-companion: ", 0)
	if origin != extensionOrigin {
		logger.Printf("not started by Alibi (%s), so not answering", origin)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sys := defaultSystem()
	l, layoutErr := currentLayout(sys)
	s := &sender{out: stdout}
	// Covers go to an agent that keeps the system's clock on Alibi's location: only Windows has one, and
	// only while its tray follows Alibi. Otherwise Alibi would hold the port open for nothing. If the
	// tray's switches change, Alibi hears hello again, and opens or closes the port to suit.
	hasAgent := layoutErr == nil && l.agent != nil
	takes := func() bool { return hasAgent && loadSettings(l.agent.settings).followsAlibi() }
	var takesCovers atomic.Bool
	takesCovers.Store(takes())
	if err := s.hello(takesCovers.Load()); err != nil {
		logger.Print(err)
		return 1
	}
	word := newFollowWord(olderAlibiAfter)
	me := thisProcess()
	if hasAgent {
		defer forgetCover(l.agent.covers, me) // the agent stops following this browser's cover
		go func() {
			tick := time.NewTicker(settingsEvery)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
				if now := takes(); now != takesCovers.Load() {
					takesCovers.Store(now)
					if !now {
						forgetCover(l.agent.covers, me)
					}
					if err := s.hello(now); err != nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	var keepFailed atomic.Bool
	// stdin closing means the browser closed the port.
	go func() {
		readMessages(stdin, func(message []byte) {
			if on, ok := followingIn(message); ok {
				word.set(on)
			}
			if !takesCovers.Load() {
				return
			}
			w, err := parseCover(message)
			if err != nil {
				logger.Print(err)
			}
			if w == nil {
				return
			}
			if err := keepCover(l.agent.covers, me, *w); err != nil {
				// Alibi sends a cover only when it changes. Hanging up has it start the host again and resend.
				logger.Printf("couldn't keep Alibi's cover for the agent: %v", err)
				keepFailed.Store(true)
				cancel()
			}
		})
		cancel()
	}()
	if err := follow(ctx, defaultWatch(newGeoJS().lookup, logger.Printf), agentWord(sys), s, word); err != nil {
		logger.Print(err)
		return 1
	}
	if keepFailed.Load() {
		return 1
	}
	return 0
}

type watchConfig struct {
	lookup      func(context.Context) (cover, error)
	fingerprint func() string
	logf        func(format string, args ...any)
	// A fresh look this often, for exits that move without the network interfaces changing.
	poll time.Duration
	// How often to compare the network interfaces.
	watch time.Duration
	// Fresh looks after the interfaces change, while the new routes and DNS settle.
	settle []time.Duration
	// How often to read the agent's place while it runs.
	relay time.Duration
	// A look right away, whenever something sends on it (the tray, after a setting changes).
	nudge <-chan struct{}
}

func defaultWatch(lookup func(context.Context) (cover, error), logf func(string, ...any)) watchConfig {
	return watchConfig{
		lookup:      lookup,
		fingerprint: networkFingerprint,
		logf:        logf,
		poll:        30 * time.Second,
		watch:       2 * time.Second,
		settle:      []time.Duration{1 * time.Second, 3 * time.Second, 8 * time.Second, 20 * time.Second},
		relay:       time.Second,
	}
}

// sender writes messages to the browser, and nothing it has already sent.
type sender struct {
	out    io.Writer
	mu     sync.Mutex // one frame at a time: a hello can go out while places do
	sent   []byte     // the last place
	system *string
}

func (s *sender) frame(message []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFrame(s.out, message); err != nil {
		return fmt.Errorf("the browser stopped reading: %w", err)
	}
	return nil
}

func (s *sender) write(v any) error {
	message, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.frame(message)
}

// hello tells Alibi what it's talking to, and whether it takes the All sites cover now: on Windows, while
// the tray follows Alibi's location. Whereabouts before doxigo/whereabouts#1 says no hello at all.
func (s *sender) hello(takesCovers bool) error {
	accepts := []string{}
	if takesCovers {
		accepts = append(accepts, "cover")
	}
	return s.write(map[string]any{"hello": "alibi-companion", "version": version, "accepts": accepts})
}

func (s *sender) send(place cover) error {
	message, err := json.Marshal(place)
	if err != nil {
		return err
	}
	if bytes.Equal(message, s.sent) {
		return nil
	}
	if err := s.frame(message); err != nil {
		return err
	}
	s.sent = message
	return nil
}

// sendSystem tells Alibi the zone the agent holds the system's clock on, or that it holds none.
func (s *sender) sendSystem(zone string) error {
	if s.system != nil && *s.system == zone {
		return nil
	}
	var value any // null when there's none
	if zone != "" {
		value = zone
	}
	if err := s.write(map[string]any{"system": value}); err != nil {
		return err
	}
	s.system = &zone
	return nil
}

// How often the host reads the tray's switches again, to tell Alibi when it starts or stops taking covers.
var settingsEvery = 2 * time.Second

// An Alibi that hasn't said within this long whether it follows the IP is one from before it said so,
// which only ever opens the port while a cover follows the IP.
const olderAlibiAfter = 2 * time.Second

// followWord is what Alibi last said about following the IP ({"following": …}), which it says the
// moment the port opens. Until it does, the host looks nothing up.
type followWord struct {
	mu      sync.Mutex
	on      bool
	said    bool          // Alibi has said, rather than the host assuming
	changed chan struct{} // closed, and replaced, at every change
}

// newFollowWord waits for Alibi's word, and after assume without one, takes it that Alibi follows the IP.
func newFollowWord(assume time.Duration) *followWord {
	w := &followWord{changed: make(chan struct{})}
	time.AfterFunc(assume, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.said {
			w.changeLocked(true)
		}
	})
	return w
}

func (w *followWord) set(on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.said = true
	w.changeLocked(on)
}

func (w *followWord) changeLocked(on bool) {
	if on == w.on {
		return
	}
	w.on = on
	close(w.changed)
	w.changed = make(chan struct{})
}

// now is the word as it stands, and a channel that closes when it next changes.
func (w *followWord) now() (bool, <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.on, w.changed
}

// followingIn reads Alibi's word on following the IP from one of its messages, if it is one.
func followingIn(message []byte) (bool, bool) {
	var m struct {
		Following *bool `json:"following"`
	}
	if json.Unmarshal(message, &m) != nil || m.Following == nil {
		return false, false
	}
	return *m.Following, true
}

// follow passes on the agent's word while the agent runs: on Windows it has already moved the
// system's clock, and its place names the zone the way browsers now read it. While no agent runs,
// follow looks the place up itself, but only while Alibi says a cover follows the IP: otherwise there's
// nothing to ask GeoJS for.
func follow(ctx context.Context, cfg watchConfig, agent func() (agentState, bool), s *sender, word *followWord) error {
	for ctx.Err() == nil {
		if _, ok := agent(); ok {
			if err := relay(ctx, cfg.relay, agent, s); err != nil {
				return err
			}
			continue
		}
		if err := s.sendSystem(""); err != nil {
			return err
		}
		following, changed := word.now()
		own, stop := context.WithCancel(ctx)
		go func() { // hand back to the agent the moment it's there, and start or stop looking when Alibi says
			tick := time.NewTicker(cfg.relay)
			defer tick.Stop()
			for {
				select {
				case <-own.Done():
					return
				case <-changed:
					stop()
					return
				case <-tick.C:
					if _, ok := agent(); ok {
						stop()
						return
					}
				}
			}
		}()
		var err error
		if following {
			err = watch(own, cfg, func(place cover, err error) error {
				if err != nil {
					if own.Err() == nil {
						cfg.logf("couldn't look up the connection: %v", err)
					}
					return nil // offline, or GeoJS down: Alibi keeps the last place
				}
				return s.send(place)
			})
		} else {
			<-own.Done()
		}
		stop()
		if err != nil {
			return err
		}
	}
	return nil
}

// relay passes on the agent's place and system zone whenever they change, until the agent goes quiet.
func relay(ctx context.Context, every time.Duration, agent func() (agentState, bool), s *sender) error {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		state, ok := agent()
		if !ok {
			return nil
		}
		if state.Cover.Timezone != "" {
			if err := s.send(state.Cover); err != nil {
				return err
			}
		}
		if err := s.sendSystem(state.System); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// watch looks up where the connection appears to be, right away and then on a schedule, and hands
// every answer to found, which can stop it by returning an error.
func watch(ctx context.Context, cfg watchConfig, found func(cover, error) error) error {
	interfaces := cfg.fingerprint()
	ticker := time.NewTicker(cfg.watch)
	defer ticker.Stop()
	nextPoll := time.Now()
	var settling []time.Time
	for {
		due := nextPoll
		if len(settling) > 0 && settling[0].Before(due) {
			due = settling[0]
		}
		timer := time.NewTimer(time.Until(due))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-cfg.nudge:
			timer.Stop()
			nextPoll = time.Now()
		case <-ticker.C:
			timer.Stop()
			if now := cfg.fingerprint(); now != interfaces {
				interfaces = now
				changed := time.Now()
				settling = settling[:0]
				for _, after := range cfg.settle {
					settling = append(settling, changed.Add(after))
				}
			}
		case <-timer.C:
			now := time.Now()
			for len(settling) > 0 && !settling[0].After(now) {
				settling = settling[1:]
			}
			nextPoll = now.Add(cfg.poll)
			if err := found(cfg.lookup(ctx)); err != nil {
				return err
			}
		}
	}
}

// readMessages reads native messaging messages from the browser until it closes the port.
func readMessages(r io.Reader, got func([]byte)) {
	for {
		var size uint32
		if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
			return
		}
		if size > 64<<10 { // far beyond anything Alibi sends
			if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
				return
			}
			continue
		}
		message := make([]byte, size)
		if _, err := io.ReadFull(r, message); err != nil {
			return
		}
		got(message)
	}
}

// wish is Alibi's All sites cover, as far as the system's clock goes (systemCover in js/sites.js):
// its configuration, and the zone it's on, if any.
type wish struct {
	Configuration string `json:"configuration"`
	Timezone      string `json:"timezone"`
}

// namesPlace reports whether the cover is a place. Off and "Match my IP address" ask nothing of the
// system's clock.
func (w wish) namesPlace() bool {
	return w.Configuration != "browserDefault" && w.Configuration != "ipAddress" && w.Timezone != ""
}

// process is one running program, told apart from a later one that gets the same number.
type process struct {
	pid     int
	started int64 // when it started, in nanoseconds; 0 where the system needn't say
}

// heardCover is one host's word for the agent: the All sites cover its browser last sent, and when, with
// the host's own process, so the agent can tell whether it still runs. Each host keeps its own file in
// the covers folder, so two browsers or profiles never write over each other, and deletes it as it exits.
type heardCover struct {
	PID     int       `json:"pid"`
	Started int64     `json:"started"`
	Cover   wish      `json:"cover"`
	Updated time.Time `json:"updated"`
}

func coverPath(dir string, pid int) string {
	return filepath.Join(dir, fmt.Sprintf("cover-%d.json", pid))
}

// parseCover reads the All sites cover from one of Alibi's messages: nil for any other message, and an
// error for a cover that isn't well formed.
func parseCover(message []byte) (*wish, error) {
	var m struct {
		Cover *struct {
			Configuration string  `json:"configuration"`
			Timezone      *string `json:"timezone"`
		} `json:"cover"`
	}
	if json.Unmarshal(message, &m) != nil || m.Cover == nil {
		return nil, nil
	}
	w := wish{Configuration: m.Cover.Configuration}
	if m.Cover.Timezone != nil {
		w.Timezone = *m.Cover.Timezone
	}
	if w.Configuration == "" || len(w.Configuration) > 64 || (w.Timezone != "" && (len(w.Timezone) > 64 || !zonePattern.MatchString(w.Timezone))) {
		return nil, fmt.Errorf("refused a cover from Alibi: %q", message)
	}
	return &w, nil
}

// keepCover writes this host's word for the agent.
func keepCover(dir string, me process, w wish) error {
	b, _ := json.Marshal(heardCover{PID: me.pid, Started: me.started, Cover: w, Updated: time.Now().UTC()})
	return writeFileAtomic(coverPath(dir, me.pid), b, 0o644)
}

// forgetCover deletes this host's word as it exits.
func forgetCover(dir string, me process) {
	path := coverPath(dir, me.pid)
	retry(func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
}

// liveCovers reads the word of every host that still runs, and deletes what hosts that have gone left
// behind: a host the browser killed outright never got to delete its own.
func liveCovers(dir string) []heardCover {
	paths, _ := filepath.Glob(filepath.Join(dir, "cover-*.json"))
	var live []heardCover
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue // being replaced: there next time
		}
		var h heardCover
		if json.Unmarshal(b, &h) != nil || h.Cover.Configuration == "" {
			continue
		}
		if !(process{pid: h.PID, started: h.Started}).running() {
			os.Remove(path)
			continue
		}
		live = append(live, h)
	}
	return live
}

// pickWish is the cover the system's clock can follow, among those of the browsers running now: the
// newest that names a place. Off and "Match my IP address" never outvote another browser's place, and
// closing a browser hands back to the others. nil when none names a place.
func pickWish(heard []heardCover) *wish {
	var newest *heardCover
	for i := range heard {
		if h := &heard[i]; h.Cover.namesPlace() && (newest == nil || h.Updated.After(newest.Updated)) {
			newest = h
		}
	}
	if newest == nil {
		return nil
	}
	w := newest.Cover
	return &w
}

// readWish is the place the hosts of the running browsers ask for, if any (pickWish).
func readWish(dir string) *wish { return pickWish(liveCovers(dir)) }

// The agent's latest word, which the host relays: agent.json in the companion's folder.
type agentState struct {
	Cover cover `json:"cover"` // the connection's place, its zone as browsers read it when the system's on it
	// The zone the agent holds the system's clock on, as browsers read it; "" when it holds none.
	System  string    `json:"system,omitempty"`
	Updated time.Time `json:"updated"`
}

// An agent that hasn't written for this long has stopped (it writes after every look, every 30 s).
const agentQuiet = 75 * time.Second

func agentStatePath(dir string) string { return filepath.Join(dir, "agent.json") }

// agentWord reads the running agent's word. It reports false when no agent has written lately.
func agentWord(sys system) func() (agentState, bool) {
	l, err := currentLayout(sys)
	if err != nil || runtime.GOOS != "windows" {
		return func() (agentState, bool) { return agentState{}, false }
	}
	path := agentStatePath(l.dir)
	return func() (agentState, bool) {
		b, err := os.ReadFile(path)
		if err != nil {
			return agentState{}, false
		}
		var state agentState
		if json.Unmarshal(b, &state) != nil || time.Since(state.Updated) > agentQuiet {
			return agentState{}, false
		}
		return state, true
	}
}

// writeFrame sends one native messaging message: its length as a 32-bit number in the machine's
// byte order, then the JSON. One write, so a frame never goes out in pieces.
func writeFrame(w io.Writer, message []byte) error {
	frame := make([]byte, 4, 4+len(message))
	binary.NativeEndian.PutUint32(frame, uint32(len(message)))
	_, err := w.Write(append(frame, message...))
	return err
}

// networkFingerprint changes whenever an interface comes up, goes down or changes address,
// as it does when a VPN connects, disconnects or reconnects.
func networkFingerprint() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var lines []string
	for _, i := range interfaces {
		if i.Flags&net.FlagUp == 0 {
			continue
		}
		lines = append(lines, i.Name)
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			lines = append(lines, i.Name+" "+addr.String())
		}
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
