package dev

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelenaOnlyChangeHotSwapsWithoutReloadOrRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "materials", "lava.sel")
	restarts := 0
	var asked []string
	s := &Server{
		Dir:      dir,
		OnChange: func() error { restarts++; return nil },
		PreflightChange: func([]string) error {
			t.Fatal("a .sel-only change must not run the Go/GSX preflight")
			return nil
		},
		SelenaChange: func(paths []string) ([]SelenaUpdate, error) {
			asked = paths
			return []SelenaUpdate{{Source: "materials/lava.sel", Materials: []map[string]any{{
				"material": "Lava", "customFragment": "void main() {}", "shaderLayout": map[string]any{"material": "Lava"},
			}}}}, nil
		},
	}
	events := captureEvents(t, s)
	s.emitChange([]string{path})
	got := drainEvents(events)
	if _, ok := got["reload"]; ok {
		t.Fatalf("a .sel change must not reload the page; got %v", keys(got))
	}
	if restarts != 0 {
		t.Fatalf("a .sel change restarted the app %d times", restarts)
	}
	if len(asked) != 1 || asked[0] != path {
		t.Fatalf("SelenaChange got %v", asked)
	}
	ev, ok := got["selena"]
	if !ok {
		t.Fatalf("expected a selena event; got %v", keys(got))
	}
	var payload SelenaUpdate
	if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Source != "materials/lava.sel" || len(payload.Materials) != 1 || payload.Materials[0]["material"] != "Lava" {
		t.Fatalf("selena payload = %+v", payload)
	}

	// The app still embeds the old artifact: the next page load rebuilds it
	// first, and only once.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Sec-Fetch-Dest", "document")
	s.refreshStaleApp(req)
	s.refreshStaleApp(req)
	if restarts != 1 {
		t.Fatalf("page load after a hot swap restarted %d times, want 1", restarts)
	}
	asset := httptest.NewRequest("GET", "/gosx/bootstrap.js", nil)
	asset.Header.Set("Sec-Fetch-Dest", "script")
	s.mu.Lock()
	s.appStale = true
	s.mu.Unlock()
	s.refreshStaleApp(asset)
	if restarts != 1 {
		t.Fatal("a script request must not rebuild the app")
	}
}

func TestSelenaCompileErrorKeepsThePage(t *testing.T) {
	s := &Server{
		Dir:      t.TempDir(),
		OnChange: func() error { t.Fatal("no restart on a Selena error"); return nil },
		SelenaChange: func([]string) ([]SelenaUpdate, error) {
			return nil, errors.New("selena lava.sel: SEL2001 at 3:16: unknown name")
		},
	}
	events := captureEvents(t, s)
	s.emitChange([]string{filepath.Join(s.Dir, "lava.sel")})
	got := drainEvents(events)
	if _, ok := got["reload"]; ok {
		t.Fatal("a Selena compile error must not reload")
	}
	if ev, ok := got["build-error"]; !ok || !strings.Contains(ev.Data, "SEL2001") {
		t.Fatalf("expected a build-error with the diagnostic; got %v", got)
	}
}

func TestMixedSelenaAndGoChangeStillRestarts(t *testing.T) {
	restarts := 0
	s := &Server{
		Dir:          t.TempDir(),
		OnChange:     func() error { restarts++; return nil },
		SelenaChange: func([]string) ([]SelenaUpdate, error) { t.Fatal("mixed change must not hot-swap"); return nil, nil },
	}
	events := captureEvents(t, s)
	s.emitChange([]string{filepath.Join(s.Dir, "lava.sel"), filepath.Join(s.Dir, "main.go")})
	if _, ok := drainEvents(events)["reload"]; !ok || restarts != 1 {
		t.Fatalf("mixed change: restarts=%d", restarts)
	}
}

func TestSelenaSwapScriptInjectedOnlyWithHook(t *testing.T) {
	page := "<html><head></head><body></body></html>"
	without := (&Server{}).injectDevScripts(page)
	if strings.Contains(without, "data-gosx-dev-selena") {
		t.Fatal("swap script injected without a SelenaChange hook")
	}
	with := (&Server{SelenaChange: func([]string) ([]SelenaUpdate, error) { return nil, nil }}).injectDevScripts(page)
	for _, want := range []string{
		`data-gosx-dev-selena`,
		`addEventListener("selena"`,
		`window.__gosxDevEvents`, // shares the reload script's EventSource
		`kind:3`,                 // SetMaterial command
		`object.shaderBackend!=="selena"`,
	} {
		if !strings.Contains(with, want) {
			t.Fatalf("swap script lacks %q", want)
		}
	}
	if strings.Index(with, "data-gosx-dev-reload") > strings.Index(with, "data-gosx-dev-selena") {
		t.Fatal("the swap script must follow the reload script that opens the EventSource")
	}
	if !shouldWatchProjectFile("materials/lava.sel") || shouldWatchProjectFile("materials/lava.sel.json") {
		t.Fatal("the watcher must see .sel sources and ignore their artifacts")
	}
}
