package dev

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// islandGSX is a self-contained island component that compiles cleanly via
// gosx.Compile -> ir.LowerIsland -> program.EncodeJSON. It mirrors the fixture
// used by cmd/gosx dev_test.go so the dev hot-swap path is exercised against a
// real island program rather than a stub.
const islandGSX = `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`

// captureEvents registers an in-process SSE client channel on the server and
// returns it. broadcast() fans out to every channel in s.clients, so reading
// this channel observes exactly what a connected browser would receive.
func captureEvents(t *testing.T, s *Server) chan sseEvent {
	t.Helper()
	ch := make(chan sseEvent, 16)
	s.mu.Lock()
	if s.clients == nil {
		s.clients = make(map[chan sseEvent]struct{})
	}
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

// drainEvents collects every event currently buffered on ch without blocking
// past a short settle window.
func drainEvents(ch chan sseEvent) map[string]sseEvent {
	got := make(map[string]sseEvent)
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			got[ev.Name] = ev
		case <-deadline:
			return got
		default:
			// nothing buffered right now; allow a brief settle.
			select {
			case ev := <-ch:
				got[ev.Name] = ev
			case <-time.After(20 * time.Millisecond):
				return got
			}
		}
	}
}

func TestHotSwapIslandChangeEmitsProgramNotReload(t *testing.T) {
	dir := t.TempDir()
	gsxPath := filepath.Join(dir, "counter.gsx")
	writeTestFile(t, gsxPath, []byte(islandGSX))

	s := &Server{Dir: dir}
	events := captureEvents(t, s)

	s.emitChange([]string{gsxPath})

	got := drainEvents(events)

	if _, ok := got["reload"]; ok {
		t.Fatalf("island .gsx change must not emit reload; got events %v", keys(got))
	}
	prog, ok := got["program"]
	if !ok {
		t.Fatalf("island .gsx change must emit a program event; got events %v", keys(got))
	}

	var payload struct {
		Component string `json:"component"`
		Format    string `json:"format"`
		Program   string `json:"program"`
	}
	if err := json.Unmarshal([]byte(prog.Data), &payload); err != nil {
		t.Fatalf("program payload not JSON: %v (%s)", err, prog.Data)
	}
	if payload.Component != "Counter" {
		t.Fatalf("expected component %q, got %q", "Counter", payload.Component)
	}
	if payload.Format != "json" {
		t.Fatalf("expected format json, got %q", payload.Format)
	}
	if payload.Program == "" {
		t.Fatal("expected non-empty island bytecode in program payload")
	}
	// The payload program must itself be the JSON wire form carrying the
	// island name, proving fresh bytecode (not an empty/placeholder blob).
	if !json.Valid([]byte(payload.Program)) {
		t.Fatalf("program payload is not valid island JSON: %s", payload.Program)
	}
	var island struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(payload.Program), &island); err != nil || island.Name != "Counter" {
		t.Fatalf("expected island program for Counter, got name=%q err=%v", island.Name, err)
	}
}

// TestHotSwapProgramEventReachesSSEClient is the end-to-end headless check the
// Track B verify-reality calls for: stand the dev server up, connect to
// /gosx/dev/events, trigger an island change, and assert a "program" SSE frame
// with fresh bytecode arrives over the wire (and no "reload").
func TestHotSwapProgramEventReachesSSEClient(t *testing.T) {
	dir := t.TempDir()
	gsxPath := filepath.Join(dir, "counter.gsx")
	writeTestFile(t, gsxPath, []byte(islandGSX))

	s := &Server{Dir: dir}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/gosx/dev/events", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	// Drain the initial "connected" frame so the client is registered before
	// we trigger the change.
	readSSEFrame(t, reader)

	s.emitChange([]string{gsxPath})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		name, data := readSSEFrame(t, reader)
		switch name {
		case "program":
			var payload struct {
				Component string `json:"component"`
				Program   string `json:"program"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				t.Fatalf("program frame not JSON: %v (%s)", err, data)
			}
			if payload.Component != "Counter" {
				t.Fatalf("expected component Counter, got %q", payload.Component)
			}
			var island struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(payload.Program), &island); err != nil {
				t.Fatalf("program frame did not carry valid island bytecode: %v (%s)", err, payload.Program)
			}
			if island.Name != "Counter" {
				t.Fatalf("program frame missing fresh Counter bytecode, got name=%q", island.Name)
			}
			return
		case "reload":
			t.Fatalf("island change must not emit reload over SSE")
		case "heartbeat", "":
			continue
		}
	}
	t.Fatal("timed out waiting for program SSE frame")
}

// readSSEFrame reads one event:/data: frame from an SSE stream and returns the
// event name and data payload. It tolerates heartbeat comments and blank lines.
func readSSEFrame(t *testing.T, reader *bufio.Reader) (name, data string) {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if name != "" || data != "" {
				return name, data
			}
		}
	}
}

func TestHotSwapNonIslandChangeEmitsReload(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "main.go")
	writeTestFile(t, goPath, []byte("package main\n\nfunc main() {}\n"))

	s := &Server{Dir: dir}
	events := captureEvents(t, s)

	s.emitChange([]string{goPath})

	got := drainEvents(events)

	if _, ok := got["program"]; ok {
		t.Fatalf("non-island change must not emit a program event; got events %v", keys(got))
	}
	if _, ok := got["reload"]; !ok {
		t.Fatalf("non-island change must emit reload; got events %v", keys(got))
	}
}

// TestInjectedReloadScriptRoutesProgramEvents locks the client-routing
// contract: the injected dev snippet must listen for "program" events and
// drive window.__gosx_reload_program, fanning out across window.__gosx.islands
// by component, while still falling back to a full reload when the hot-swap
// export is absent (older runtimes).
func TestInjectedReloadScriptRoutesProgramEvents(t *testing.T) {
	html := injectReloadScript("<html><head></head><body></body></html>")

	for _, want := range []string{
		`addEventListener("program"`,
		`window.__gosx_reload_program`,
		`window.__gosx.islands`,
		`entry.component!==payload.component`,
		`window.location.reload()`, // fallback when the export is missing
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("injected reload script missing %q\ngot: %s", want, html)
		}
	}
}

func keys(m map[string]sseEvent) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
