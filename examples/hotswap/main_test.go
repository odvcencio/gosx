package main

import (
	"fmt"
	"m31labs.dev/gosx/buildmanifest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// TestCounterIslandCompiles keeps counter.gsx, the file the README tells you to
// edit, compiling to an island program, and checks the page mounts it.
func TestCounterIslandCompiles(t *testing.T) {
	prog, data, err := compileIsland("counter.gsx", "Counter")
	if err != nil {
		t.Fatal(err)
	}
	if prog.Name != "Counter" || len(data) == 0 {
		t.Fatalf("unexpected island: name=%q bytes=%d", prog.Name, len(data))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	app := server.New()
	app.Route("/", func(_ *http.Request) gosx.Node { return counterPage(prog, "", 0) })
	app.Build().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-gosx-island="Counter"`) {
		t.Fatalf("page does not mount the island: status %d\n%s", rec.Code, rec.Body)
	}
}

func TestStandaloneSetup(t *testing.T) {
	t.Setenv("GOSX_DEV", "")
	root := t.TempDir()
	sourceDir := filepath.Join(root, "examples", "demo")
	if _, err := newApp(sourceDir); err == nil || !strings.Contains(err.Error(), "this example must be run from a checkout of the repository") {
		t.Fatalf("missing checkout: %v", err)
	}
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := newApp(sourceDir); err == nil || !strings.Contains(err.Error(), filepath.Join(root, "build", "gosx-runtime.wasm")) {
		t.Fatalf("missing runtime: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("counter.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{filepath.Join(sourceDir, "counter.gsx"): src, filepath.Join(root, "build", "gosx-runtime.wasm"): []byte("runtime"), filepath.Join(root, "build", "wasm_exec.js"): []byte("exec")} {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	app, err := newApp(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	check := func(data string) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		body := rec.Body.String()
		if !strings.Contains(body, buildmanifest.ContentHash([]byte(data))) || !strings.Contains(body, fmt.Sprintf(`"size":%d`, len(data))) {
			t.Fatalf("runtime metadata missing: %s", body)
		}
	}
	check("runtime")
	if err := os.WriteFile(filepath.Join(root, "build", "gosx-runtime.wasm"), []byte("new runtime"), 0644); err != nil {
		t.Fatal(err)
	}
	check("new runtime")
}
