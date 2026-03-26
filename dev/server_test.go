package dev

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerProxyInjectsReloadScriptIntoHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><head><title>Docs</title></head><body><main>hello</main></body></html>")
	}))
	defer upstream.Close()

	srv := &Server{
		Dir:         t.TempDir(),
		BuildDir:    t.TempDir(),
		ProxyTarget: upstream.URL,
	}
	srv.SetProxyTarget(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-gosx-dev-reload") {
		t.Fatalf("expected reload script in body, got %q", body)
	}
	if !strings.Contains(body, "/gosx/dev/events") {
		t.Fatalf("expected reload event stream in body, got %q", body)
	}
}

func TestServerProxySkipsReloadScriptForNavigationFetches(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body><main>hello</main></body></html>")
	}))
	defer upstream.Close()

	srv := &Server{
		Dir:         t.TempDir(),
		BuildDir:    t.TempDir(),
		ProxyTarget: upstream.URL,
	}
	srv.SetProxyTarget(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/docs", nil)
	req.Header.Set("X-GoSX-Navigation", "1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "data-gosx-dev-reload") {
		t.Fatalf("did not expect reload script in navigation response: %q", rec.Body.String())
	}
}

func TestServerServesBuildAssets(t *testing.T) {
	buildDir := t.TempDir()
	writeTestFile(t, filepath.Join(buildDir, "gosx-runtime.wasm"), []byte("wasm"))
	writeTestFile(t, filepath.Join(buildDir, "bootstrap.js"), []byte("bootstrap"))
	writeTestFile(t, filepath.Join(buildDir, "islands", "Counter.json"), []byte(`{"name":"Counter"}`))
	writeTestFile(t, filepath.Join(buildDir, "css", "page.css"), []byte("body{}"))

	srv := &Server{
		Dir:      t.TempDir(),
		BuildDir: buildDir,
	}

	cases := []struct {
		path string
		want string
	}{
		{path: "/gosx/runtime.wasm", want: "wasm"},
		{path: "/gosx/bootstrap.js", want: "bootstrap"},
		{path: "/gosx/islands/Counter.json", want: `{"name":"Counter"}`},
		{path: "/gosx/css/page.css", want: "body{}"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://gosx.test"+tc.path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.path, rec.Code)
		}
		if got := rec.Body.String(); got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.path, tc.want, got)
		}
		if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
			t.Fatalf("%s: expected no-cache headers, got %q", tc.path, cache)
		}
	}
}

func TestSnapshotChangedDetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTestFile(t, path, []byte("<Page />"))

	before, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot before delete: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove watched file: %v", err)
	}
	after, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot after delete: %v", err)
	}
	if !snapshotChanged(before, after) {
		t.Fatal("expected deleted file to change snapshot")
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
