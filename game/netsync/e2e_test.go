package netsync_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/game/netsync"
	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/room/wire"
)

// TestWebSocketEndToEnd runs a real room server and a native netsync client
// over WebSockets in real time.
func TestWebSocketEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time test")
	}
	m := roomtest.Mode(roomtest.Options{Seats: 2, Countdown: -1})
	srv, err := room.NewServer(room.Config{Modes: []mode.Mode{m}, Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", srv))
	hs := httptest.NewServer(mux)
	defer hs.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Close(ctx)
	}()

	c, err := netsync.New(netsync.Config{
		URL:   "ws" + strings.TrimPrefix(hs.URL, "http") + "/play/ws",
		Mode:  m,
		Input: func(_ uint64, dst []byte) []byte { return append(dst, roomtest.Right) },
		Join:  netsync.Join{Name: "e2e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deadline := time.Now().Add(5 * time.Second)
	last := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		now := time.Now()
		c.Frame(now.Sub(last))
		last = now
		if st := c.Stats(); st.AuthTick > 30 && st.RTT > 0 {
			break
		}
	}
	st := c.Stats()
	if c.Status() != netsync.StatusOnline || c.Phase() != wire.PhaseRunning || st.AuthTick <= 30 {
		t.Fatalf("status %v phase %v stats %+v", c.Status(), c.Phase(), st)
	}
	if st.Lead < 0 || st.Keyframes == 0 || st.InputFrames == 0 {
		t.Fatalf("stats %+v", st)
	}
	r, ok := srv.Room(c.Room().Code)
	if !ok {
		t.Fatal("room missing")
	}
	info := r.Info()
	if info.SeatList[c.Seat()].Name != "e2e" || !info.SeatList[c.Seat()].Connected {
		t.Fatalf("seat %+v", info.SeatList[c.Seat()])
	}
	t.Logf("e2e: %+v", st)
}

// TestJSWasmImportsAvoidNetHTTP checks the js/wasm build of netsync. Its own
// imports must never reach net/http or the hub server. Today game and sim
// still import hub (C1's import diet removes that), so a transitive
// net/http that only arrives through them is reported, not failed.
func TestJSWasmImportsAvoidNetHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	list := func(args ...string) []string {
		cmd := exec.Command(gobin, append([]string{"list"}, args...)...)
		cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go list %v: %v\n%s", args, err, out)
		}
		return strings.Fields(string(out))
	}
	direct := list("-f", `{{join .Imports " "}}`, "m31labs.dev/gosx/game/netsync")
	for _, imp := range direct {
		switch imp {
		case "net/http", "m31labs.dev/gosx/hub", "m31labs.dev/gosx/room", "github.com/gorilla/websocket", "m31labs.dev/gosx":
			t.Fatalf("netsync imports %s directly in its js/wasm build", imp)
		}
	}
	has := func(pkgs []string, want string) bool {
		for _, p := range pkgs {
			if p == want {
				return true
			}
		}
		return false
	}
	if deps := list("-deps", "m31labs.dev/gosx/game/netsync"); has(deps, "net/http") {
		viaGame := has(list("-deps", "m31labs.dev/gosx/game"), "net/http")
		viaSim := has(list("-deps", "m31labs.dev/gosx/sim"), "net/http")
		if !viaGame && !viaSim {
			t.Fatal("net/http reaches netsync's js/wasm build through a path other than game or sim")
		}
		t.Logf("C1 pending: net/http still reaches netsync through game=%v sim=%v", viaGame, viaSim)
	}
}
