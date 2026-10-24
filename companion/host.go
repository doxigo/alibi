package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
	// Only an agent can keep the system's clock on Alibi's location, and only Windows has one.
	takesCovers := layoutErr == nil && l.agent != nil
	if err := s.hello(takesCovers); err != nil {
		logger.Print(err)
		return 1
	}
	// stdin closing means the browser closed the port.
	go func() {
		readMessages(stdin, func(message []byte) {
			if takesCovers {
				if err := takeCover(message, l.agent.wanted); err != nil {
					logger.Print(err)
				}
			}
		})
		cancel()
	}()
	if err := follow(ctx, defaultWatch(newGeoJS().lookup, logger.Printf), agentWord(sys), s); err != nil {
		logger.Print(err)
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
	sent   []byte // the last place
	system *string
}

func (s *sender) write(v any) error {
	message, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := writeFrame(s.out, message); err != nil {
		return fmt.Errorf("the browser stopped reading: %w", err)
	}
	return nil
}

// hello tells Alibi what it's talking to: a companion that, on Windows, takes the All sites cover.
// Whereabouts says no hello, so Alibi knows to send it nothing.
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
	if err := writeFrame(s.out, message); err != nil {
		return fmt.Errorf("the browser stopped reading: %w", err)
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

// follow passes on the agent's word while the agent runs: on Windows it has already moved the
// system's clock, and its place names the zone the way browsers now read it, so Alibi has nothing
// to fake. While no agent runs, follow looks the place up itself.
func follow(ctx context.Context, cfg watchConfig, agent func() (agentState, bool), s *sender) error {
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
		own, stop := context.WithCancel(ctx)
		go func() { // hand back to the agent the moment it's there
			tick := time.NewTicker(cfg.relay)
			defer tick.Stop()
			for {
				select {
				case <-own.Done():
					return
				case <-tick.C:
					if _, ok := agent(); ok {
						stop()
						return
					}
				}
			}
		}()
		err := watch(own, cfg, func(place cover, err error) error {
			if err != nil {
				if own.Err() == nil {
					cfg.logf("couldn't look up the connection: %v", err)
				}
				return nil // offline, or GeoJS down: Alibi keeps the last place
			}
			return s.send(place)
		})
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

// Alibi's last word on its All sites cover, which the host keeps for the agent: wanted.json.
type wishState struct {
	Cover   wish      `json:"cover"`
	Updated time.Time `json:"updated"`
}

// takeCover keeps the All sites cover from a message of Alibi's for the agent. Other messages are
// ignored, and a cover that isn't well formed is refused.
func takeCover(message []byte, path string) error {
	var m struct {
		Cover *struct {
			Configuration string  `json:"configuration"`
			Timezone      *string `json:"timezone"`
		} `json:"cover"`
	}
	if json.Unmarshal(message, &m) != nil || m.Cover == nil {
		return nil
	}
	w := wish{Configuration: m.Cover.Configuration}
	if m.Cover.Timezone != nil {
		w.Timezone = *m.Cover.Timezone
	}
	if w.Configuration == "" || len(w.Configuration) > 64 || (w.Timezone != "" && (len(w.Timezone) > 64 || !zonePattern.MatchString(w.Timezone))) {
		return fmt.Errorf("refused a cover from Alibi: %q", message)
	}
	b, _ := json.Marshal(wishState{Cover: w, Updated: time.Now().UTC()})
	return writeFileAtomic(path, b, 0o644)
}

// readWish reads the All sites cover the host last kept, if any.
func readWish(path string) (*wish, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state wishState
	if err := json.Unmarshal(b, &state); err != nil || state.Cover.Configuration == "" {
		return nil, fmt.Errorf("%s isn't a cover", path)
	}
	return &state.Cover, nil
}

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
