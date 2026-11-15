//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
)

// The rooms tests run the examples/rooms js/wasm client in Chrome, served from
// one origin, against a room server on another.
//
// By default they start the room server themselves. Set
// GOSX_ROOMS_E2E_SERVER=wss://rooms.example.com/play/ws to test a deployed
// server instead; the page is then served from 127.0.0.1 (or localhost through
// the Windows relay) on GOSX_ROOMS_E2E_PAGE_PORT, default 47831, so the
// deployment can allowlist that origin. Live runs skip the steps that need
// control of the server (cutting sockets, killing it). GOSX_ROOMS_E2E_HOLD
// sets how long the two-browser test holds its room (default 70 s live, 15 s
// local).

const defaultRoomsPagePort = 47831

func roomsRemote() string { return os.Getenv("GOSX_ROOMS_E2E_SERVER") }

func roomsRunner() string {
	if e2eWindowsRelay() {
		return "nvidia"
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "runner"
}

// trackedListener records accepted connections so the test can cut them at
// the TCP level, the way a dropped network or a crashed server does.
type trackedListener struct {
	net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, c)
		l.mu.Unlock()
	}
	return c, err
}

// drop closes every accepted connection without a WebSocket close frame.
func (l *trackedListener) drop() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.conns)
	for _, c := range l.conns {
		_ = c.Close()
	}
	l.conns = nil
	return n
}

type roomsState struct {
	Status   string   `json:"status"`
	Reason   string   `json:"reason"`
	Local    bool     `json:"local"`
	Offline  bool     `json:"offline"`
	Seat     int      `json:"seat"`
	Code     string   `json:"code"`
	Phase    string   `json:"phase"`
	Tick     float64  `json:"tick"`
	AuthTick float64  `json:"authTick"`
	Lead     int      `json:"lead"`
	RTTMs    float64  `json:"rttMs"`
	Dials    float64  `json:"dials"`
	Seats    []string `json:"seats"`
}

func (s roomsState) count(kind string) int {
	n := 0
	for _, k := range s.Seats {
		if k == kind {
			n++
		}
	}
	return n
}

// roomsSite is the static client and, in local mode, the room server.
type roomsSite struct {
	pageURL string // origin A
	wsURL   string // the room server's WebSocket endpoint (origin B)
	server  *room.Server
	ln      *trackedListener
	hs      *http.Server
	killed  bool
}

func startRoomsSite(t *testing.T) *roomsSite {
	t.Helper()
	root := e2eRepoRoot(t)
	static := buildRoomsClient(t, root)
	site := &roomsSite{}

	// Origin A: the static client.
	files := http.FileServer(http.Dir(static))
	if remote := roomsRemote(); remote != "" {
		port := defaultRoomsPagePort
		if v, err := strconv.Atoi(os.Getenv("GOSX_ROOMS_E2E_PAGE_PORT")); err == nil && v > 0 {
			port = v
		}
		site.pageURL = serveOnPort(t, files, port)
		site.wsURL = remote
		return site
	}
	page := httptest.NewServer(files)
	t.Cleanup(page.Close)
	site.pageURL = page.URL
	if e2eWindowsRelay() {
		site.pageURL = e2eExposeToWindows(t, page.URL)
	}

	// Origin B: the room server, on its own tracked listener.
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	site.ln = &trackedListener{Listener: raw}
	serverURL := "http://" + raw.Addr().String()
	if e2eWindowsRelay() {
		serverURL = e2eExposeToWindows(t, serverURL)
	}
	srv, err := room.NewServer(room.Config{
		// Same names and seats as the client's modes; short countdowns keep the
		// test quick.
		Modes: []mode.Mode{
			roomtest.Mode(roomtest.Options{Name: "duel", Seats: 2, Props: 6, Countdown: 500 * time.Millisecond, Duration: 2 * time.Minute}),
			roomtest.Mode(roomtest.Options{Name: "party", Seats: 6, Props: 12, Countdown: 500 * time.Millisecond, Duration: 3 * time.Minute, LobbyWait: time.Second}),
		},
		AllowedOrigins: []string{site.pageURL},
		RequireOrigin:  true,
		Logf:           t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	site.server = srv
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", srv))
	site.hs = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = site.hs.Serve(site.ln) }()
	site.wsURL = "ws" + strings.TrimPrefix(serverURL, "http") + "/play/ws"
	t.Cleanup(func() {
		if !site.killed {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Close(ctx)
			_ = site.hs.Close()
		}
	})
	return site
}

// kill stops the local room server abruptly: no close frames.
func (s *roomsSite) kill() {
	s.killed = true
	_ = s.ln.Listener.Close()
	s.ln.drop()
	_ = s.hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = s.server.Close(ctx)
	cancel()
}

// roomsPage is one browser running the client.
type roomsPage struct {
	t *testing.T
	p *browserPage
}

// openRoomsPage opens the client in a new browser. With beside set, it opens
// a new window in beside's browser instead: the GPU runners hand every test
// the same running Chrome, and a second tab in one window would sit in the
// background, where Chrome stops animation frames and so the client's loop.
func openRoomsPage(t *testing.T, site *roomsSite, params url.Values, beside *roomsPage) *roomsPage {
	t.Helper()
	params.Set("server", site.wsURL)
	if params.Get("connectTimeout") == "" {
		params.Set("connectTimeout", "5s")
	}
	params.Set("reconnectTimeout", "3s")
	params.Set("stallTimeout", "2s")
	var p *browserPage
	if beside == nil {
		p = newBrowserPage(t, e2eChromePath(t), nil, 1280, 900, "", 5*time.Minute)
	} else {
		// A new window in the same browser. sessionStorage is per window, so
		// the two clients keep separate resume tokens.
		var id target.ID
		if err := chromedp.Run(beside.p.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			c := chromedp.FromContext(ctx)
			var err error
			id, err = target.CreateTarget("about:blank").WithNewWindow(true).Do(cdp.WithExecutor(ctx, c.Browser))
			return err
		})); err != nil {
			t.Fatalf("open a second window: %v", err)
		}
		ctx, cancel := chromedp.NewContext(beside.p.ctx, chromedp.WithTargetID(id))
		t.Cleanup(cancel)
		p = &browserPage{ctx: ctx}
		attachPageListeners(ctx, p)
		if err := chromedp.Run(ctx, network.Enable()); err != nil {
			t.Fatalf("open a second window: %v", err)
		}
	}
	if status := p.navigate(t, site.pageURL+"/?"+params.Encode()); status != http.StatusOK {
		t.Fatalf("page status %d", status)
	}
	return &roomsPage{t: t, p: p}
}

func (r *roomsPage) state() roomsState {
	r.t.Helper()
	var s roomsState
	if err := r.p.tryEval(`typeof gosxRoomsState === "function" ? gosxRoomsState() : null`, &s); err != nil {
		r.t.Fatalf("read client state: %v\n%s", err, r.p.Console())
	}
	return s
}

func (r *roomsPage) wait(what string, timeout time.Duration, ok func(roomsState) bool) roomsState {
	r.t.Helper()
	deadline := time.Now().Add(timeout)
	var s roomsState
	for time.Now().Before(deadline) {
		s = r.state()
		if ok(s) {
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.t.Fatalf("timed out after %v waiting for %s; last state %+v\nconsole:\n%s", timeout, what, s, r.p.Console())
	return s
}

func (r *roomsPage) noPageErrors() {
	r.t.Helper()
	if errs := r.p.PageErrors(); len(errs) != 0 {
		r.t.Fatalf("page errors: %v\nconsole:\n%s", errs, r.p.Console())
	}
}

// TestRoomsNetsyncCrossOriginBrowser plays at once (locally, then in a room
// against a bot), survives a forced disconnect by resuming its seat, and falls
// back to offline play when the server dies. Live runs stop after the first
// part.
func TestRoomsNetsyncCrossOriginBrowser(t *testing.T) {
	site := startRoomsSite(t)
	q := url.Values{"name": {"judge-" + roomsRunner()}}
	if roomsRemote() != "" {
		q.Set("private", "1") // keep concurrent runs and visitors out of this room
	}
	page := openRoomsPage(t, site, q, nil)

	// 1. Play starts at once: the first state already ticks a match.
	first := page.wait("the client to start", 60*time.Second, func(s roomsState) bool { return s.Status != "" && s.Tick > 0 })
	t.Logf("first state: %+v", first)

	// 2. The client hands over to the room and plays against a bot.
	online := page.wait("an online match", 30*time.Second, func(s roomsState) bool {
		return s.Status == "online" && !s.Local && s.Phase == "running" && s.AuthTick > 60
	})
	if online.Seat < 0 || len(online.Code) != 5 || len(online.Seats) != 2 || online.count("human") != 1 || online.count("bot") != 1 {
		t.Fatalf("online state %+v, want one person and one bot", online)
	}
	t.Logf("online via %s: rtt %.0f ms, lead %d, state %+v", site.wsURL, online.RTTMs, online.Lead, online)

	if site.server == nil {
		t.Logf("live server %s: RTT %.0f ms from %s; skipping the disconnect and kill steps", site.wsURL, online.RTTMs, roomsRunner())
		page.noPageErrors()
		return
	}
	r, ok := site.server.Room(online.Code)
	if !ok {
		t.Fatalf("server has no room %s", online.Code)
	}
	if info := r.Info(); info.Humans != 1 || info.Bots != 1 || !info.SeatList[online.Seat].Connected {
		t.Fatalf("server room %+v", info)
	}

	// 3. Cut the socket at the TCP level. The client redials with its token
	// and gets the same seat back.
	if n := site.ln.drop(); n == 0 {
		t.Fatal("no connection to drop")
	}
	resumed := page.wait("the seat to be resumed", 20*time.Second, func(s roomsState) bool {
		return s.Status == "online" && !s.Local && s.Dials > online.Dials && s.AuthTick > online.AuthTick+30
	})
	if resumed.Seat != online.Seat || resumed.Code != online.Code {
		t.Fatalf("resumed into %s seat %d, want %s seat %d", resumed.Code, resumed.Seat, online.Code, online.Seat)
	}
	if info := r.Info(); !info.SeatList[online.Seat].Connected || info.Humans != 1 {
		t.Fatalf("server room after resume %+v", info)
	}
	t.Logf("resumed: %+v", resumed)

	// 4. Kill the server: no close frames, the listener is gone.
	site.kill()
	offline := page.wait("offline play", 20*time.Second, func(s roomsState) bool { return s.Status == "offline" && s.Local && s.Offline })
	time.Sleep(time.Second)
	later := page.state()
	if later.Status != "offline" || later.Tick < offline.Tick+20 {
		t.Fatalf("offline play did not keep ticking: %+v then %+v", offline, later)
	}
	if later.Seat != resumed.Seat {
		t.Fatalf("offline continuation changed seat %d -> %d", resumed.Seat, later.Seat)
	}
	t.Logf("offline: %+v", later)
	page.noPageErrors()
}

// TestRoomsNetsyncTwoBrowsers puts two browsers in one six-seat room (two
// people, four bots) and holds it, checking that neither reconnects and both
// keep receiving the server's ticks. A live hold of 70 s crosses the 60 s read
// timeout that Traefik v3 applies by default.
func TestRoomsNetsyncTwoBrowsers(t *testing.T) {
	site := startRoomsSite(t)
	hold := 15 * time.Second
	if roomsRemote() != "" {
		hold = 70 * time.Second
	}
	if d, err := time.ParseDuration(os.Getenv("GOSX_ROOMS_E2E_HOLD")); err == nil && d > 0 {
		hold = d
	}
	runner := roomsRunner()
	a := openRoomsPage(t, site, url.Values{"mode": {"party"}, "private": {"1"}, "name": {"a-" + runner}}, nil)
	sa := a.wait("the first browser to join a room", 60*time.Second, func(s roomsState) bool { return s.Status == "online" && s.Code != "" })
	b := openRoomsPage(t, site, url.Values{"mode": {"party"}, "code": {sa.Code}, "name": {"b-" + runner}}, a)
	for name, page := range map[string]*roomsPage{"a": a, "b": b} {
		var visible string
		page.p.eval(t, `document.visibilityState`, &visible)
		if visible != "visible" {
			t.Fatalf("browser %s is %q: a hidden page gets no animation frames, so its client stops", name, visible)
		}
	}
	full := func(s roomsState) bool {
		return s.Status == "online" && !s.Local && s.Phase == "running" && s.Code == sa.Code &&
			s.count("human") == 2 && s.count("bot") == 4 && s.AuthTick > 30
	}
	sa = a.wait("the first browser in the full room", 60*time.Second, full)
	sb := b.wait("the second browser in the full room", 60*time.Second, full)
	if sa.Seat == sb.Seat {
		t.Fatalf("both browsers hold seat %d", sa.Seat)
	}
	t.Logf("room %s: a seat %d, b seat %d, seats %v; holding %v", sa.Code, sa.Seat, sb.Seat, sa.Seats, hold)

	type rtt struct{ min, max, sum, n float64 }
	var ra, rb rtt
	sample := func(r *rtt, s roomsState) {
		if s.RTTMs <= 0 {
			return
		}
		if r.n == 0 || s.RTTMs < r.min {
			r.min = s.RTTMs
		}
		r.max = max(r.max, s.RTTMs)
		r.sum += s.RTTMs
		r.n++
	}
	startA, startB := sa, sb
	end := time.Now().Add(hold)
	for time.Now().Before(end) {
		time.Sleep(time.Second)
		for _, c := range []struct {
			name  string
			page  *roomsPage
			start roomsState
			r     *rtt
		}{{"a", a, startA, &ra}, {"b", b, startB, &rb}} {
			s := c.page.state()
			if s.Status != "online" || s.Local || s.Dials != c.start.Dials || s.Code != c.start.Code || s.Seat != c.start.Seat {
				t.Fatalf("browser %s left the room during the hold: %+v (started %+v)", c.name, s, c.start)
			}
			if s.count("human") != 2 {
				t.Fatalf("browser %s sees %d people: %v", c.name, s.count("human"), s.Seats)
			}
			sample(c.r, s)
		}
	}
	endA, endB := a.state(), b.state()
	want := hold.Seconds() * 30 * 0.9 // 30 Hz, allow 10%
	for name, d := range map[string]float64{"a": endA.AuthTick - startA.AuthTick, "b": endB.AuthTick - startB.AuthTick} {
		if d < want {
			t.Fatalf("browser %s received %.0f server ticks in %v, want at least %.0f", name, d, hold, want)
		}
	}
	t.Logf("held %v via %s from %s: a rtt %.0f/%.0f/%.0f ms (min/avg/max), b rtt %.0f/%.0f/%.0f ms; ticks a %.0f b %.0f",
		hold, site.wsURL, runner, ra.min, ra.sum/max(ra.n, 1), ra.max, rb.min, rb.sum/max(rb.n, 1), rb.max,
		endA.AuthTick-startA.AuthTick, endB.AuthTick-startB.AuthTick)
	a.noPageErrors()
	b.noPageErrors()
}

// serveOnPort serves h on a fixed loopback port (localhost through the
// Windows relay) and returns the page origin.
func serveOnPort(t *testing.T, h http.Handler, port int) string {
	t.Helper()
	host := "127.0.0.1"
	if e2eWindowsRelay() {
		host = "0.0.0.0" // Windows Chrome reaches WSL on localhost:<port> through every interface
	}
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		t.Fatalf("listen on the page port %d: %v", port, err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	if e2eWindowsRelay() {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// buildRoomsClient builds examples/rooms/client for js/wasm into a fresh
// static directory with its page and wasm_exec.js.
func buildRoomsClient(t *testing.T, root string) string {
	t.Helper()
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "main.wasm"), "./examples/rooms/client")
	build.Dir = root
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the rooms client: %v\n%s", err, out)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	wasmExec := ""
	for _, rel := range []string{"lib/wasm/wasm_exec.js", "misc/wasm/wasm_exec.js"} {
		candidate := filepath.Join(strings.TrimSpace(string(goroot)), rel)
		if _, err := os.Stat(candidate); err == nil {
			wasmExec = candidate
			break
		}
	}
	if wasmExec == "" {
		t.Fatal("wasm_exec.js not found under GOROOT")
	}
	for src, dst := range map[string]string{
		wasmExec: "wasm_exec.js",
		filepath.Join(root, "examples", "rooms", "static", "index.html"): "index.html",
	} {
		in, err := os.Open(src)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dir, dst))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, in); err != nil {
			t.Fatal(err)
		}
		in.Close()
		out.Close()
	}
	return dir
}
