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

func TestProjectSnapshotWatchesOnlyDevSourceFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "app", "page.gsx"), []byte("<Page />"))
	writeTestFile(t, filepath.Join(dir, "app", "page.go"), []byte("package app"))
	writeTestFile(t, filepath.Join(dir, "public", "site.css"), []byte("body{}"))
	writeTestFile(t, filepath.Join(dir, "public", "app.js"), []byte("console.log('ok')"))
	writeTestFile(t, filepath.Join(dir, "README.md"), []byte("# ignored"))
	writeTestFile(t, filepath.Join(dir, "build", "bootstrap.js"), []byte("ignored"))

	snapshot, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, path := range []string{
		"app/page.gsx",
		"app/page.go",
		"public/site.css",
		"public/app.js",
	} {
		if _, ok := snapshot[path]; !ok {
			t.Fatalf("expected watched path %s in snapshot %#v", path, snapshot)
		}
	}
	for _, path := range []string{"README.md", "build/bootstrap.js"} {
		if _, ok := snapshot[path]; ok {
			t.Fatalf("did not expect ignored path %s in snapshot", path)
		}
	}
}

func TestShouldWatchProjectFile(t *testing.T) {
	for _, path := range []string{"page.gsx", "main.go", "style.CSS", "app.JS"} {
		if !shouldWatchProjectFile(path) {
			t.Fatalf("%s should be watched", path)
		}
	}
	for _, path := range []string{"README.md", "data.json", "image.png"} {
		if shouldWatchProjectFile(path) {
			t.Fatalf("%s should not be watched", path)
		}
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
