package main

import (
	"fmt"
	"m31labs.dev/gosx/buildmanifest"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCounterRendersCompiledIsland(t *testing.T) {
	t.Setenv("GOSX_DEV", "1")
	_, thisFile, _, _ := runtime.Caller(0)
	app, err := newApp(filepath.Dir(thisFile))
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d\n%s", path, rec.Code, rec.Body)
		}
		return rec
	}

	page := get("/?count=5").Body.String()
	if !strings.Contains(page, `data-gosx-island="Counter"`) {
		t.Errorf("page does not mount the Counter island:\n%s", page)
	}
	if !strings.Contains(page, `<noscript>`) || !strings.Contains(page, `href="/?count=6"`) {
		t.Errorf("page lacks the no-JavaScript links for count 5:\n%s", page)
	}

	if !strings.Contains(get("/gosx/islands/Counter.json").Body.String(), `"Counter"`) {
		t.Errorf("compiled program does not name the Counter island")
	}
}

func TestCounterServesConfiguredRuntime(t *testing.T) {
	t.Setenv("GOSX_DEV", "")
	root := t.TempDir()
	example := filepath.Join(root, "examples", "counter")
	for _, dir := range []string{example, filepath.Join(root, "build")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	source, err := os.ReadFile("counter.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(example, "counter.gsx"):             source,
		filepath.Join(root, "build", "gosx-runtime.wasm"): []byte("runtime"),
		filepath.Join(root, "build", "wasm_exec.js"):      []byte("exec"),
	} {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	app, err := newApp(example)
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(page.Body.String(), `"path":"/gosx/runtime.wasm`) || !strings.Contains(page.Body.String(), `"hash":`) {
		t.Fatalf("page lacks the runtime URL or hash: %s", page.Body.String())
	}
	wasm := httptest.NewRecorder()
	handler.ServeHTTP(wasm, httptest.NewRequest(http.MethodGet, "/gosx/runtime.wasm", nil))
	if wasm.Code != http.StatusOK || wasm.Body.String() != "runtime" {
		t.Fatalf("runtime: status %d, body %q", wasm.Code, wasm.Body.String())
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
