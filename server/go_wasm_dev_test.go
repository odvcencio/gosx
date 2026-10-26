package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

// goWASMDist is a build directory with one Go WASM module, "ui", that a test
// rebuilds the way the CLI does: a new hashed file and a new build.json.
type goWASMDist struct {
	t    *testing.T
	root string
}

func newGoWASMDist(t *testing.T) *goWASMDist { return &goWASMDist{t: t, root: t.TempDir()} }

func goWASMBody(tag string) []byte {
	return append([]byte("\x00asm\x01\x00\x00\x00"), bytes.Repeat([]byte(tag), 200)...)
}

// build writes the module and its manifest and returns the hashed file's path.
func (d *goWASMDist) build(body []byte, sidecars bool) string {
	d.t.Helper()
	hash := buildmanifest.ContentHash(body)
	file := "ui." + hash + ".wasm"
	path := filepath.Join(d.root, "assets", "go-wasm", file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		d.t.Fatal(err)
	}
	if sidecars {
		if err := writeTestBrotli(path+".br", body); err != nil {
			d.t.Fatal(err)
		}
	}
	data, _ := json.Marshal(buildmanifest.Manifest{GoWASM: map[string]buildmanifest.HashedAsset{"ui": {File: file, Hash: hash, Size: int64(len(body))}}})
	if err := os.WriteFile(filepath.Join(d.root, "build.json"), data, 0o644); err != nil {
		d.t.Fatal(err)
	}
	return path
}

func fetchGoWASM(t *testing.T, handler http.Handler, url string, header http.Header) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	for name, values := range header {
		req.Header[name] = values
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var reader io.Reader = w.Body
	if w.Header().Get("Content-Encoding") == "br" {
		reader = brotli.NewReader(reader)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return w, body
}

func TestDevServerServesTheCurrentGoWASMBuild(t *testing.T) {
	t.Setenv("GOSX_ENV", "development")
	t.Setenv("GOSX_DEV", "")
	dist := newGoWASMDist(t)
	old, rebuilt := goWASMBody("old build "), goWASMBody("new build ")
	path := dist.build(old, true)
	app := New()
	app.SetRuntimeRoot(dist.root)
	handler := app.Build()
	url := app.GoWASMURL("ui")
	br := http.Header{"Accept-Encoding": {"br"}}

	w, body := fetchGoWASM(t, handler, url, br)
	if w.Code != 200 || !bytes.Equal(body, old) {
		t.Fatalf("first fetch: %d, %d bytes", w.Code, len(body))
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("a dev build must be revalidated on reload; Cache-Control = %q", got)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("dev response has no validator")
	}
	if w, _ = fetchGoWASM(t, handler, url, http.Header{"If-None-Match": {etag}, "Accept-Encoding": {"br"}}); w.Code != http.StatusNotModified {
		t.Fatalf("unchanged build: %d, want 304", w.Code)
	}

	// Replace the file in place. Its compressed sidecar still holds the old build.
	if err := os.WriteFile(path, rebuilt, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	w, body = fetchGoWASM(t, handler, url, br)
	if !bytes.Equal(body, rebuilt) {
		t.Fatalf("in-place replacement not served: got %d bytes of the old build (Content-Encoding %q)", len(body), w.Header().Get("Content-Encoding"))
	}
	if w.Header().Get("ETag") == etag {
		t.Fatal("the validator did not change with the file")
	}
	if w, _ = fetchGoWASM(t, handler, url, http.Header{"If-None-Match": {etag}, "Accept-Encoding": {"br"}}); w.Code != 200 {
		t.Fatalf("reload with the old validator: %d, want 200", w.Code)
	}

	// A rebuild writes a new hashed file and build.json. An app that learned the
	// old URL at startup still asks for it, and gets the current build.
	dist.build(goWASMBody("third build "), false)
	if app.GoWASMURL("ui") == url {
		t.Fatal("manifest URL did not follow the rebuild")
	}
	w, body = fetchGoWASM(t, handler, url, br)
	if w.Code != 200 || !bytes.Equal(body, goWASMBody("third build ")) {
		t.Fatalf("stale URL after a rebuild: %d, %d bytes, not the current build", w.Code, len(body))
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("Cache-Control = %q", w.Header().Get("Cache-Control"))
	}
}

func TestProductionServerKeepsGoWASMImmutable(t *testing.T) {
	t.Setenv("GOSX_ENV", "production")
	t.Setenv("GOSX_DEV", "")
	dist := newGoWASMDist(t)
	body := goWASMBody("old build ")
	dist.build(body, true)
	app := New()
	app.SetRuntimeRoot(dist.root)
	handler := app.Build()
	url := app.GoWASMURL("ui")
	w, got := fetchGoWASM(t, handler, url, http.Header{"Accept-Encoding": {"br"}})
	if w.Code != 200 || !bytes.Equal(got, body) || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("production response: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	// An old hashed URL is not remapped: a hash names exactly one build.
	dist.build(goWASMBody("next build "), false)
	if w, _ = fetchGoWASM(t, handler, url, nil); w.Code != 200 {
		t.Fatalf("an old hashed file must still serve its own build, got %d", w.Code)
	}
	if err := os.Remove(filepath.Join(dist.root, "assets", "go-wasm", filepath.Base(url))); err != nil {
		t.Fatal(err)
	}
	if w, _ = fetchGoWASM(t, handler, url, nil); w.Code != http.StatusNotFound {
		t.Fatalf("a removed hashed file was remapped to another build: %d", w.Code)
	}
}
