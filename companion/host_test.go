package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }

var (
	tokyo = cover{Timezone: "Asia/Tokyo", Locale: "ja-JP", Lat: ptr(35.6895), Lon: ptr(139.6917)}
	oslo  = cover{Timezone: "Europe/Oslo", Locale: "nb-NO", Lat: ptr(59.9139), Lon: ptr(10.7522)}
)

// readFrame reads one native messaging message the way the browser does.
func readFrame(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		t.Fatalf("reading frame length: %v", err)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		t.Fatalf("reading frame body: %v", err)
	}
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatalf("frame isn't JSON: %v (%q)", err, body)
	}
	return message
}

// readPlace reads frames until one is a place, skipping the host's other messages.
func readPlace(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	for {
		if message := readFrame(t, r); message["timezone"] != nil {
			return message
		}
	}
}

func TestWriteFrame(t *testing.T) {
	var buf bytes.Buffer
	message, _ := json.Marshal(tokyo)
	if err := writeFrame(&buf, message); err != nil {
		t.Fatal(err)
	}
	want := `{"timezone":"Asia/Tokyo","locale":"ja-JP","lat":35.6895,"lon":139.6917}`
	if got := buf.String()[4:]; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	if size := binary.NativeEndian.Uint32(buf.Bytes()); int(size) != len(want) {
		t.Errorf("length prefix = %d, want %d", size, len(want))
	}
}

// Coordinates GeoJS doesn't have stay out of the message; Alibi clears the old ones.
func TestCoverWithoutCoordinates(t *testing.T) {
	message, _ := json.Marshal(cover{Timezone: "Europe/Oslo", Locale: "nb-NO"})
	if want := `{"timezone":"Europe/Oslo","locale":"nb-NO"}`; string(message) != want {
		t.Errorf("got %s, want %s", message, want)
	}
}

// sequence answers each lookup with the next place, then keeps repeating the last one.
func sequence(places ...cover) (func(context.Context) (cover, error), *atomic.Int32) {
	var calls atomic.Int32
	return func(context.Context) (cover, error) {
		i := int(calls.Add(1)) - 1
		return places[min(i, len(places)-1)], nil
	}, &calls
}

// served is a follow() running in the background: done closes when it returns, and err is what it returned.
type served struct {
	done chan struct{}
	err  error
}

func noAgent() (agentState, bool) { return agentState{}, false }

// following is Alibi's word on following the IP, as if it had just said it.
func following(on bool) *followWord {
	w := newFollowWord(time.Hour)
	w.set(on)
	return w
}

// startServe runs follow with no agent, the way the host runs on a computer without time zone sync.
func startServe(t *testing.T, cfg watchConfig) (io.Reader, context.CancelFunc, *served) {
	return startFollow(t, cfg, noAgent)
}

func startFollow(t *testing.T, cfg watchConfig, agent func() (agentState, bool)) (io.Reader, context.CancelFunc, *served) {
	return startFollowing(t, cfg, agent, following(true))
}

// startFollowing is startFollow with Alibi's word on following the IP as the test sets it.
func startFollowing(t *testing.T, cfg watchConfig, agent func() (agentState, bool), word *followWord) (io.Reader, context.CancelFunc, *served) {
	t.Helper()
	if cfg.relay == 0 {
		cfg.relay = 5 * time.Millisecond
	}
	r, w := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{done: make(chan struct{})}
	go func() {
		s.err = follow(ctx, cfg, agent, &sender{out: w}, word)
		w.CloseWithError(io.EOF)
		close(s.done)
	}()
	t.Cleanup(func() {
		cancel()
		go io.Copy(io.Discard, r)
		<-s.done
	})
	return r, cancel, s
}

func TestServeSendsTheFirstPlaceThenOnlyChanges(t *testing.T) {
	lookup, calls := sequence(tokyo, tokyo, tokyo, oslo)
	r, _, _ := startServe(t, watchConfig{
		lookup: lookup, fingerprint: func() string { return "same" }, logf: t.Logf,
		poll: 5 * time.Millisecond, watch: time.Hour,
	})
	if got := readPlace(t, r)["timezone"]; got != "Asia/Tokyo" {
		t.Fatalf("first message = %v, want Asia/Tokyo", got)
	}
	if got := readPlace(t, r)["timezone"]; got != "Europe/Oslo" {
		t.Fatalf("second message = %v, want Europe/Oslo (the repeated Tokyo answers must not be sent)", got)
	}
	if n := calls.Load(); n < 4 {
		t.Errorf("only %d lookups before the change arrived", n)
	}
}

func TestServeLooksAgainWhenTheNetworkChanges(t *testing.T) {
	lookup, _ := sequence(tokyo, oslo)
	var network atomic.Value
	network.Store("wifi")
	r, _, _ := startServe(t, watchConfig{
		lookup: lookup, fingerprint: func() string { return network.Load().(string) }, logf: t.Logf,
		poll: time.Hour, watch: 5 * time.Millisecond, settle: []time.Duration{10 * time.Millisecond},
	})
	readPlace(t, r)
	network.Store("wifi + vpn")
	got := make(chan any, 1)
	go func() { got <- readPlace(t, r)["timezone"] }()
	select {
	case tz := <-got:
		if tz != "Europe/Oslo" {
			t.Errorf("after the change = %v, want Europe/Oslo", tz)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no lookup after the network changed (the next poll is an hour away)")
	}
}

func TestServeKeepsQuietWhenTheLookupFails(t *testing.T) {
	var calls atomic.Int32
	lookup := func(context.Context) (cover, error) {
		if calls.Add(1) < 3 {
			return cover{}, errors.New("offline")
		}
		return tokyo, nil
	}
	r, _, _ := startServe(t, watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: 5 * time.Millisecond, watch: time.Hour})
	if got := readPlace(t, r)["timezone"]; got != "Asia/Tokyo" {
		t.Errorf("got %v, want Asia/Tokyo once back online", got)
	}
}

func TestServeStopsWhenTheBrowserStopsReading(t *testing.T) {
	lookup, _ := sequence(tokyo)
	cfg := watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: time.Hour, watch: time.Hour, relay: time.Hour}
	if err := follow(context.Background(), cfg, noAgent, &sender{out: failingWriter{}}, following(true)); err == nil {
		t.Fatal("follow kept going after a failed write")
	}
}

// While no cover in Alibi follows the IP, the host asks GeoJS nothing: picking a city on Linux, or
// after Quit on Windows, sends no request. Once Alibi says one follows, it looks, and stops again when
// Alibi says none does.
func TestFollowLooksNothingUpWhileAlibiDoesntFollow(t *testing.T) {
	lookup, calls := sequence(tokyo)
	word := following(false)
	r, _, _ := startFollowing(t, watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf,
		poll: 5 * time.Millisecond, watch: time.Hour}, noAgent, word)
	if got := readFrame(t, r); got["system"] != nil || len(got) != 1 {
		t.Fatalf("first message %v, want {system: null}", got)
	}
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d lookups while nothing follows the IP", n)
	}
	word.set(true)
	if got := readPlace(t, r)["timezone"]; got != "Asia/Tokyo" {
		t.Fatalf("got %v once Alibi follows the IP", got)
	}
	word.set(false)
	time.Sleep(20 * time.Millisecond)
	before := calls.Load()
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != before {
		t.Errorf("%d more lookups after Alibi stopped following", n-before)
	}
}

// An Alibi from before it said {following} only ever opens the port while a cover follows the IP: with
// no word at all, the host takes it that one does.
func TestFollowWordAssumesAnOlderAlibiFollows(t *testing.T) {
	word := newFollowWord(10 * time.Millisecond)
	on, changed := word.now()
	if on {
		t.Fatal("following before Alibi said, or the wait was up")
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("never took it that an older Alibi follows the IP")
	}
	if on, _ := word.now(); !on {
		t.Error("not following after the wait")
	}
	said := newFollowWord(10 * time.Millisecond)
	said.set(false)
	time.Sleep(50 * time.Millisecond)
	if on, _ := said.now(); on {
		t.Error("an Alibi that said it doesn't follow the IP was taken to follow it")
	}
	for message, want := range map[string]int{`{"following":true}`: 1, `{"following":false}`: 0, `{"cover":{}}`: -1, `{"following":"yes"}`: -1, `nope`: -1} {
		on, ok := followingIn([]byte(message))
		if got := map[bool]int{true: 1, false: 0}[on]; !ok && want != -1 || ok && got != want {
			t.Errorf("%s: %v, %v", message, on, ok)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestServeEndsWithItsContext(t *testing.T) {
	lookup, _ := sequence(tokyo)
	r, cancel, s := startServe(t, watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: time.Hour, watch: time.Hour})
	readPlace(t, r) // now it's waiting for the next poll, an hour away
	cancel()
	select {
	case <-s.done:
		if s.err != nil {
			t.Errorf("follow returned %v, want nil", s.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("follow didn't return after its context ended")
	}
}

// fakeAgent is a time zone agent the test switches on and off, and moves.
type fakeAgent struct {
	mu    sync.Mutex
	on    bool
	state agentState
}

func (a *fakeAgent) set(on bool, place cover, system string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.on, a.state = on, agentState{Cover: place, System: system}
}

func (a *fakeAgent) read() (agentState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state, a.on
}

// While the agent runs, the host passes on its place (the zone as browsers read it) and the zone it
// holds the system on, and asks GeoJS nothing. When the agent stops, the host says the system's held
// on nothing, looks up the place itself, and hands back when the agent returns.
func TestFollowRelaysTheAgent(t *testing.T) {
	var lookups atomic.Int32
	lookup := func(context.Context) (cover, error) {
		lookups.Add(1)
		return cover{Timezone: "Asia/Tehran", Locale: "fa-IR"}, nil
	}
	agent := &fakeAgent{}
	agent.set(true, cover{Timezone: "Europe/Oslo", Locale: "nb-NO"}, "Europe/Oslo")
	r, _, _ := startFollow(t, watchConfig{lookup: lookup, fingerprint: func() string { return "" }, logf: t.Logf, poll: time.Hour, watch: time.Hour}, agent.read)

	if got := readFrame(t, r)["timezone"]; got != "Europe/Oslo" {
		t.Fatalf("first = %v, want the agent's Europe/Oslo", got)
	}
	if got := readFrame(t, r); got["system"] != "Europe/Oslo" {
		t.Fatalf("then %v, want the system held on Europe/Oslo", got)
	}
	// Alibi's Tokyo: the clock moves, the connection doesn't.
	agent.set(true, cover{Timezone: "Europe/Oslo", Locale: "nb-NO"}, "Asia/Tokyo")
	if got := readFrame(t, r); got["system"] != "Asia/Tokyo" {
		t.Fatalf("after Alibi's Tokyo: %v", got)
	}
	agent.set(true, cover{Timezone: "Asia/Dushanbe", Locale: "tg-TJ"}, "Asia/Tokyo")
	if got := readFrame(t, r)["timezone"]; got != "Asia/Dushanbe" {
		t.Fatalf("after the agent moved = %v, want Asia/Dushanbe", got)
	}
	if n := lookups.Load(); n != 0 {
		t.Errorf("asked GeoJS %d times while the agent ran", n)
	}
	agent.set(false, cover{}, "")
	if got := readFrame(t, r); !hasNull(got, "system") {
		t.Fatalf("with the agent gone: %v, want the system held on nothing", got)
	}
	if got := readPlace(t, r)["timezone"]; got != "Asia/Tehran" {
		t.Fatalf("with the agent gone = %v, want its own lookup's Asia/Tehran", got)
	}
	agent.set(true, cover{Timezone: "Asia/Tokyo", Locale: "ja-JP"}, "Asia/Tokyo")
	if got := readPlace(t, r)["timezone"]; got != "Asia/Tokyo" {
		t.Fatalf("with the agent back = %v, want Asia/Tokyo", got)
	}
}

func hasNull(m map[string]any, key string) bool {
	v, ok := m[key]
	return ok && v == nil
}

// The agent's state file counts only while it's fresh, and only on Windows, where there's an agent.
func TestAgentWord(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the agent runs on Windows only")
	}
	sys := scratchSystem(t)
	l, _ := currentLayout(sys)
	read := agentWord(sys)
	if _, ok := read(); ok {
		t.Fatal("an agent with no state file counts as running")
	}
	b, _ := json.Marshal(agentState{Cover: oslo, System: "Europe/Oslo", Updated: time.Now()})
	writeFileAtomic(agentStatePath(l.dir), b, 0o644)
	if state, ok := read(); !ok || state.Cover.Timezone != "Europe/Oslo" || state.System != "Europe/Oslo" {
		t.Errorf("fresh state: %+v %v", state, ok)
	}
	b, _ = json.Marshal(agentState{Cover: oslo, Updated: time.Now().Add(-2 * agentQuiet)})
	writeFileAtomic(agentStatePath(l.dir), b, 0o644)
	if _, ok := read(); ok {
		t.Error("a stale state file counts as a running agent")
	}
}

// scratchSystem is the real system's layout, moved into a scratch folder.
func scratchSystem(t *testing.T) system {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{"LOCALAPPDATA": dir, "HOME": dir}
	return system{getenv: func(k string) string { return env[k] }}
}

// Alibi's messages come in native messaging frames; anything over 64 KB is skipped, not read.
func TestReadMessages(t *testing.T) {
	var in bytes.Buffer
	for _, m := range []string{`{"cover":{"configuration":"tokyo","timezone":"Asia/Tokyo"}}`, strings.Repeat("x", 70<<10), `{"b":2}`} {
		writeFrame(&in, []byte(m))
	}
	var got []string
	readMessages(&in, func(m []byte) { got = append(got, string(m)) })
	if len(got) != 2 || !strings.Contains(got[0], "Asia/Tokyo") || got[1] != `{"b":2}` {
		t.Errorf("got %q", got)
	}
}

func TestParseCover(t *testing.T) {
	for _, c := range []struct {
		message string
		want    *wish // nil: not a cover
		err     bool
	}{
		{`{"cover":{"configuration":"tokyo","timezone":"Asia/Tokyo"}}`, &wish{"tokyo", "Asia/Tokyo"}, false},
		{`{"cover":{"configuration":"browserDefault","timezone":null}}`, &wish{"browserDefault", ""}, false},
		{`{"cover":{"configuration":"ipAddress","timezone":"America/Argentina/Buenos_Aires"}}`, &wish{"ipAddress", "America/Argentina/Buenos_Aires"}, false},
		{`{"something":"else"}`, nil, false},
		{`{"following":true}`, nil, false},
		{`not json`, nil, false},
		{`{"cover":{"configuration":"tokyo","timezone":"../../etc"}}`, nil, true},
		{`{"cover":{"configuration":"","timezone":"Asia/Tokyo"}}`, nil, true},
	} {
		got, err := parseCover([]byte(c.message))
		if (err != nil) != c.err || (c.want == nil) != (got == nil) || (c.want != nil && *got != *c.want) {
			t.Errorf("%s: got %+v, err %v", c.message, got, err)
		}
	}
}

// A process that has exited, with the number it had.
func exitedProcess(t *testing.T) process {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return process{pid: cmd.ProcessState.Pid(), started: 1}
}

func TestProcessRunning(t *testing.T) {
	me := thisProcess()
	if !me.running() {
		t.Error("this process doesn't count as running")
	}
	if p := exitedProcess(t); p.running() {
		t.Errorf("process %d still counts as running after it exited", p.pid)
	}
	if runtime.GOOS == "windows" && (process{pid: me.pid, started: me.started + 1}).running() {
		t.Error("a later process with the same number counts as this one")
	}
}

// Each host keeps its browser's cover in a file of its own. The agent reads only those of hosts that
// still run, and deletes what the others left.
func TestCoversLastOnlyAsLongAsTheirHost(t *testing.T) {
	dir := t.TempDir()
	me := thisProcess()
	if err := keepCover(dir, me, wish{"tokyo", "Asia/Tokyo"}); err != nil {
		t.Fatal(err)
	}
	gone := exitedProcess(t)
	if err := keepCover(dir, gone, wish{"oslo", "Europe/Oslo"}); err != nil {
		t.Fatal(err)
	}
	if got := readWish(dir); got == nil || *got != (wish{"tokyo", "Asia/Tokyo"}) {
		t.Errorf("read %+v, want this process's Tokyo", got)
	}
	if _, err := os.Stat(coverPath(dir, gone.pid)); !os.IsNotExist(err) {
		t.Error("the cover of a host that has gone is still there")
	}
	// A new word replaces the old one in the same file, even while the agent has it open to read.
	f, err := os.Open(coverPath(dir, me.pid))
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(50 * time.Millisecond); f.Close() }()
	if err := keepCover(dir, me, wish{"seoul", "Asia/Seoul"}); err != nil {
		t.Fatalf("while the file was open: %v", err)
	}
	if got := readWish(dir); got == nil || got.Timezone != "Asia/Seoul" {
		t.Errorf("read %+v after the change", got)
	}
	forgetCover(dir, me)
	if got := readWish(dir); got != nil {
		t.Errorf("read %+v after the host left", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) > 0 {
		t.Errorf("left behind %v", left)
	}
}

// With Alibi in more than one browser or profile, the newest place wins. Off and Match my IP address
// never outvote a place in another browser.
func TestPickWish(t *testing.T) {
	at := func(s int) time.Time { return time.Unix(1_761_217_200+int64(s), 0) }
	tokyoCover, seoul := wish{"tokyo", "Asia/Tokyo"}, wish{"seoul", "Asia/Seoul"}
	off, byIP := wish{Configuration: "browserDefault"}, wish{"ipAddress", "Europe/Oslo"}
	for _, c := range []struct {
		name  string
		heard []heardCover
		want  *wish
	}{
		{"none", nil, nil},
		{"one place", []heardCover{{Cover: tokyoCover, Updated: at(0)}}, &tokyoCover},
		{"Off later", []heardCover{{Cover: tokyoCover, Updated: at(0)}, {Cover: off, Updated: at(5)}}, &tokyoCover},
		{"Match my IP later", []heardCover{{Cover: byIP, Updated: at(9)}, {Cover: tokyoCover, Updated: at(0)}}, &tokyoCover},
		{"newest place", []heardCover{{Cover: seoul, Updated: at(7)}, {Cover: tokyoCover, Updated: at(3)}}, &seoul},
		{"no place", []heardCover{{Cover: off, Updated: at(1)}, {Cover: byIP, Updated: at(2)}}, nil},
		{"a zone of its own", []heardCover{{Cover: wish{Configuration: "custom"}, Updated: at(1)}}, nil},
	} {
		got := pickWish(c.heard)
		if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The whole host, as a browser starts it: origin argument, a stand-in GeoJS, stdin held open.
func TestHostAnswersAlibiAndExitsWhenThePortCloses(t *testing.T) {
	geo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "alibi-companion/") {
			t.Errorf("User-Agent = %q", ua)
		}
		io.WriteString(w, `{"timezone":"Europe/Oslo","country_code":"NO","latitude":"59.9139","longitude":"10.7522"}`)
	}))
	defer geo.Close()
	t.Setenv("ALIBI_COMPANION_GEOJS_URL", geo.URL)
	// A scratch user folder, so a real time zone agent on this computer isn't relayed instead.
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	var stderr syncBuffer
	exit := make(chan int, 1)
	go func() { exit <- run([]string{extensionOrigin, "--parent-window=0"}, stdinR, stdoutW, &stderr) }()

	// First, hello: on Windows it takes the All sites cover, elsewhere nothing.
	hello := readFrame(t, stdoutR)
	accepts, _ := hello["accepts"].([]any)
	takes := len(accepts) == 1 && accepts[0] == "cover"
	if hello["hello"] != "alibi-companion" || takes != (runtime.GOOS == "windows") {
		t.Errorf("hello %v", hello)
	}
	writeFrame(stdinW, []byte(`{"following":true}`)) // Alibi says so the moment the port opens
	message := readPlace(t, stdoutR)
	want := map[string]any{"timezone": "Europe/Oslo", "locale": "nb-NO", "lat": 59.9139, "lon": 10.7522}
	for k, v := range want {
		if message[k] != v {
			t.Errorf("%s = %v, want %v", k, message[k], v)
		}
	}
	// Alibi switches to Tokyo: on Windows the host keeps that for the agent.
	go io.Copy(io.Discard, stdoutR)
	frame := []byte(`{"cover":{"configuration":"tokyo","timezone":"Asia/Tokyo"}}`)
	writeFrame(stdinW, frame)
	covers := filepath.Join(local, "Programs", "Alibi Companion", "covers")
	if runtime.GOOS == "windows" {
		var got *wish
		for range 50 {
			if got = readWish(covers); got != nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if got == nil || *got != (wish{"tokyo", "Asia/Tokyo"}) {
			t.Errorf("kept %+v", got)
		}
	}
	stdinW.Close() // the browser closes the port
	select {
	case code := <-exit:
		if code != 0 {
			t.Errorf("exit code %d, stderr: %s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host still running after the browser closed the port")
	}
	// Its cover goes with it: the agent stops following a browser that's closed, or an Alibi removed.
	if left, _ := filepath.Glob(filepath.Join(covers, "*")); len(left) > 0 {
		t.Errorf("the host left %v behind", left)
	}
}

func TestHostRefusesOtherExtensions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"chrome-extension://abcdefghijklmnopabcdefghijklmnop/"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("wrote %q to stdout, where only frames for Alibi belong", stdout.String())
	}
}

func TestCommandLine(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"version"}, 0, version},
		{[]string{"help"}, 0, "Usage:"},
		{[]string{"frobnicate"}, 2, "Unknown command"},
		{[]string{"uninstall", "--force"}, 2, "Unknown option"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(c.args, strings.NewReader(""), &stdout, &stderr)
		if code != c.code || !strings.Contains(stdout.String()+stderr.String(), c.out) {
			t.Errorf("%v: exit %d, output %q; want exit %d with %q", c.args, code, stdout.String()+stderr.String(), c.code, c.out)
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
