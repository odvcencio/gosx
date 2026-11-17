package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCounterRendersCompiledIsland(t *testing.T) {
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
