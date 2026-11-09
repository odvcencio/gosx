//go:build e2e

package e2e

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
)

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

// TestRoomsNetsyncCrossOriginBrowser runs the examples/rooms js/wasm client in
// Chrome, served from one origin, against a room server on another. It plays
// at once (locally, then in the room against a bot), survives a forced
// disconnect by resuming its seat, and falls back to offline play when the
// server dies.
func TestRoomsNetsyncCrossOriginBrowser(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	static := buildRoomsClient(t, root)

	// Origin A: the static client.
	page := httptest.NewServer(http.FileServer(http.Dir(static)))
	defer page.Close()
	pageURL := page.URL
	if e2eWindowsRelay() {
		pageURL = e2eExposeToWindows(t, page.URL)
	}

	// Origin B: the room server, on its own listener.
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &trackedListener{Listener: raw}
	serverURL := "http://" + raw.Addr().String()
	if e2eWindowsRelay() {
		serverURL = e2eExposeToWindows(t, serverURL)
	}
	srv, err := room.NewServer(room.Config{
		// Same name and seats as the client's "duel" mode; a short countdown
		// keeps the test quick.
		Modes:          []mode.Mode{roomtest.Mode(roomtest.Options{Name: "duel", Seats: 2, Props: 6, Countdown: 500 * time.Millisecond, Duration: 2 * time.Minute})},
		AllowedOrigins: []string{pageURL},
		RequireOrigin:  true,
		Logf:           t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", srv))
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	killed := false
	defer func() {
		if !killed {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Close(ctx)
			_ = hs.Close()
		}
	}()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/play/ws"
	q := url.Values{"server": {wsURL}, "name": {"judge"}, "connectTimeout": {"5s"}, "reconnectTimeout": {"3s"}, "stallTimeout": {"2s"}}
	p := newBrowserPage(t, chrome, nil, 1280, 900, "", 3*time.Minute)
	if status := p.navigate(t, pageURL+"/?"+q.Encode()); status != http.StatusOK {
		t.Fatalf("page status %d", status)
	}
	state := func() roomsState {
		var s roomsState
		if err := p.tryEval(`typeof gosxRoomsState === "function" ? gosxRoomsState() : null`, &s); err != nil {
			t.Fatalf("read client state: %v\n%s", err, p.Console())
		}
		return s
	}
	waitState := func(what string, timeout time.Duration, ok func(roomsState) bool) roomsState {
		t.Helper()
		deadline := time.Now().Add(timeout)
		var s roomsState
		for time.Now().Before(deadline) {
			s = state()
			if ok(s) {
				return s
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("timed out after %v waiting for %s; last state %+v\nconsole:\n%s", timeout, what, s, p.Console())
		return s
	}

	// 1. Play starts at once: the first state already ticks a match.
	first := waitState("the client to start", 60*time.Second, func(s roomsState) bool { return s.Status != "" && s.Tick > 0 })
	t.Logf("first state: %+v", first)

	// 2. The client hands over to the room and plays against a bot.
	online := waitState("an online match", 30*time.Second, func(s roomsState) bool {
		return s.Status == "online" && !s.Local && s.Phase == "running" && s.AuthTick > 60
	})
	if online.Seat < 0 || len(online.Code) != 5 || len(online.Seats) != 2 {
		t.Fatalf("online state %+v", online)
	}
	r, ok := srv.Room(online.Code)
	if !ok {
		t.Fatalf("server has no room %s", online.Code)
	}
	info := r.Info()
	if info.Humans != 1 || info.Bots != 1 || info.SeatList[online.Seat].Name != "judge" || !info.SeatList[online.Seat].Connected {
		t.Fatalf("server room %+v", info)
	}
	t.Logf("online: %+v", online)

	// 3. Cut the socket at the TCP level. The client redials with its token
	// and gets the same seat back.
	if n := ln.drop(); n == 0 {
		t.Fatal("no connection to drop")
	}
	resumed := waitState("the seat to be resumed", 20*time.Second, func(s roomsState) bool {
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
	killed = true
	_ = ln.Listener.Close()
	ln.drop()
	_ = hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Close(ctx)
	cancel()
	offline := waitState("offline play", 20*time.Second, func(s roomsState) bool { return s.Status == "offline" && s.Local && s.Offline })
	time.Sleep(time.Second)
	later := state()
	if later.Status != "offline" || later.Tick < offline.Tick+20 {
		t.Fatalf("offline play did not keep ticking: %+v then %+v", offline, later)
	}
	if later.Seat != resumed.Seat {
		t.Fatalf("offline continuation changed seat %d -> %d", resumed.Seat, later.Seat)
	}
	t.Logf("offline: %+v", later)
	if errs := p.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v\nconsole:\n%s", errs, p.Console())
	}
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
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, dst), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
