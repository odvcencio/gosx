package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

func TestRuntimeManifestDirectAssetPathUsesDistAssetsForAppRoot(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "dist", "assets", "runtime", "bootstrap-feature-scene3d-webgpu.hash.js")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("mkdir asset dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("webgpu"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	got, ok := runtimeManifestDirectAssetPath(root, "assets/runtime/bootstrap-feature-scene3d-webgpu.hash.js")
	if !ok {
		t.Fatal("expected dist runtime asset to resolve")
	}
	if got != assetPath {
		t.Fatalf("asset path = %q, want %q", got, assetPath)
	}
}

func TestRuntimeManifestDirectAssetPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if got, ok := runtimeManifestDirectAssetPath(root, "assets/../secret.js"); ok || got != "" {
		t.Fatalf("traversal path resolved to %q", got)
	}
}

// TestAppServesBootstrapFeatureTextlayoutChunk proves the lazily-fetched
// text-layout feature chunk resolves through the build manifest, matches the
// controllers chunk's content type, and negotiates brotli/gzip sidecars the
// same way every other feature chunk does.
func TestAppServesBootstrapFeatureTextlayoutChunk(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(assetsDir, "bootstrap-feature-textlayout.7777.js")
	body := []byte("window.__gosx_text_layout_engine = {};")
	if err := os.WriteFile(rawPath, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeTestGzip(rawPath+".gz", body); err != nil {
		t.Fatal(err)
	}
	if err := writeTestBrotli(rawPath+".br", body); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureTextlayout: buildmanifest.HashedAsset{
				File: "bootstrap-feature-textlayout.7777.js",
				Hash: "7777",
				Size: int64(len(body)),
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-textlayout.js?v=7777", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected JS content type, got %q", got)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty body")
	}

	// The client falls back to the literal, unversioned path when it has no
	// hash to append. That path must also resolve, not 404.
	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-textlayout.js", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for unversioned literal path, got %d", w.Code)
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("unexpected unversioned body %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-textlayout.js?v=7777", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for brotli negotiation, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("expected brotli content encoding, got %q", got)
	}
	decoded, err := io.ReadAll(brotli.NewReader(w.Body))
	if err != nil {
		t.Fatalf("read brotli body: %v", err)
	}
	if string(decoded) != string(body) {
		t.Fatalf("unexpected brotli body %q", decoded)
	}

	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-textlayout.js?v=7777", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for gzip negotiation, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip content encoding, got %q", got)
	}
}

// TestAppServesBootstrapFeatureScene3DWebGLChunk proves the lazily-fetched
// WebGL PBR renderer chunk resolves through the build manifest and
// negotiates brotli/gzip sidecars the same way its WebGPU sibling does.
// Without this plumbing, ensureWebGLFeatureLoaded gets a 404 and every
// WebGL Scene3D page silently downgrades to the legacy vertex-color
// renderer instead of PBR.
func TestAppServesBootstrapFeatureScene3DWebGLChunk(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(assetsDir, "bootstrap-feature-scene3d-webgl.8888.js")
	body := []byte("window.__gosx_scene3d_webgl_pbr = {};")
	if err := os.WriteFile(rawPath, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeTestGzip(rawPath+".gz", body); err != nil {
		t.Fatal(err)
	}
	if err := writeTestBrotli(rawPath+".br", body); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureScene3DWebGL: buildmanifest.HashedAsset{
				File: "bootstrap-feature-scene3d-webgl.8888.js",
				Hash: "8888",
				Size: int64(len(body)),
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-webgl.js?v=8888", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected JS content type, got %q", got)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty body")
	}

	// ensureWebGLFeatureLoaded falls back to the literal, unversioned path
	// when it has no content hash to append. That path must also resolve.
	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-webgl.js", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for unversioned literal path, got %d", w.Code)
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("unexpected unversioned body %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-webgl.js?v=8888", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for brotli negotiation, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("expected brotli content encoding, got %q", got)
	}
	decoded, err := io.ReadAll(brotli.NewReader(w.Body))
	if err != nil {
		t.Fatalf("read brotli body: %v", err)
	}
	if string(decoded) != string(body) {
		t.Fatalf("unexpected brotli body %q", decoded)
	}

	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-webgl.js?v=8888", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for gzip negotiation, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip content encoding, got %q", got)
	}
}

// TestAppServesBootstrapFeatureScene3DInstanceStreamChunk is a regression
// test: bootstrap-feature-scene3d-instance-stream.js (the opt-in binary
// instance-transform chunk, see client/runtime/scene3d/instance-stream.ts)
// used to be absent from every stage of the build/serve pipeline, so a page
// that called window.__gosx_scene3d_instance_stream_bridge, or the
// build.json manifest entry a matching client fetch relies on, 404ed. This
// pins the same manifest-driven path cmd/gosx/build.go now writes into
// build.json actually resolves to a servable file.
func TestAppServesBootstrapFeatureScene3DInstanceStreamChunk(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(assetsDir, "bootstrap-feature-scene3d-instance-stream.9999.js")
	body := []byte("window.__gosx_scene3d_instance_stream_bridge = {};")
	if err := os.WriteFile(rawPath, body, 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureScene3DInstanceStream: buildmanifest.HashedAsset{
				File: "bootstrap-feature-scene3d-instance-stream.9999.js",
				Hash: "9999",
				Size: int64(len(body)),
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-instance-stream.js?v=9999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected JS content type, got %q", got)
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("unexpected body %q", got)
	}

	// serveRuntimeAsset also tries runtimeCompatBuiltPath (the
	// manifest-driven production lookup) unconditionally, not just when a
	// ?v= query string is present; confirm the unversioned literal path
	// resolves through that path too instead of 404ing.
	req = httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-scene3d-instance-stream.js", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatalf("expected the unversioned literal path to resolve, got 404")
	}
}

// TestAppServesBootstrapFeatureScene3DInstanceStreamChunkCacheControlMatchesCommandChunk
// is a regression test for the caching half of the instance-stream chunk's
// wiring: before island/island.go advertised a versioned URL for this chunk
// (SetBootstrapFeatureScene3DInstanceStreamPath, the versionCompatRuntimePath
// switch, and the data-gosx-scene3d-instance-stream-url attribute), a page
// could only reach the chunk through a hand-written, UNVERSIONED <script>
// tag. serveRuntimeAsset still resolves that plain path through
// runtimeCompatSourcePath's dev-source fallback with a "no-cache,
// no-store, must-revalidate" header when a dev source tree is present (the
// gosx repo's own example apps; see the client/js fallback in
// runtimeCompatSourcePath) -- so serving it was never the missing piece.
// What was missing was any URL that carried a content hash at all: with no
// ?v= query, a request could never reach the "public, max-age=31536000,
// immutable" branch a real production deploy relies on to cache the chunk
// forever. This test pins that the instance-stream chunk now gets exactly
// the same plain-vs-versioned Cache-Control split the already-shipped
// command chunk gets, from the same server code path.
func TestAppServesBootstrapFeatureScene3DInstanceStreamChunkCacheControlMatchesCommandChunk(t *testing.T) {
	root := t.TempDir()

	// A dev source tree, the same shape runtimeCompatSourcePath's "bootstrap"
	// prefix fallback reads (root/client/js/<name>). This is what lets a
	// PLAIN (no ?v=) request resolve as "under active development, do not
	// cache" instead of falling through to the unconditional
	// runtimeCompatBuiltPath branch, which cannot tell a stale plain URL
	// from a fresh one and would cache either for a year.
	clientJSDir := filepath.Join(root, "client", "js")
	if err := os.MkdirAll(clientJSDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandDevBody := []byte("window.__gosx_scene3d_command_bridge = { dev: true };")
	if err := os.WriteFile(filepath.Join(clientJSDir, "bootstrap-feature-scene3d-command.js"), commandDevBody, 0644); err != nil {
		t.Fatal(err)
	}
	instanceStreamDevBody := []byte("window.__gosx_scene3d_instance_stream_apply = function() {};")
	if err := os.WriteFile(filepath.Join(clientJSDir, "bootstrap-feature-scene3d-instance-stream.js"), instanceStreamDevBody, 0644); err != nil {
		t.Fatal(err)
	}

	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	commandBuiltBody := []byte("window.__gosx_scene3d_command_bridge = { built: true };")
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap-feature-scene3d-command.7777.js"), commandBuiltBody, 0644); err != nil {
		t.Fatal(err)
	}
	instanceStreamBuiltBody := []byte("window.__gosx_scene3d_instance_stream_apply = function() { return true; };")
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap-feature-scene3d-instance-stream.8888.js"), instanceStreamBuiltBody, 0644); err != nil {
		t.Fatal(err)
	}

	manifest := buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapFeatureScene3DCommand: buildmanifest.HashedAsset{
				File: "bootstrap-feature-scene3d-command.7777.js",
				Hash: "7777",
				Size: int64(len(commandBuiltBody)),
			},
			BootstrapFeatureScene3DInstanceStream: buildmanifest.HashedAsset{
				File: "bootstrap-feature-scene3d-instance-stream.8888.js",
				Hash: "8888",
				Size: int64(len(instanceStreamBuiltBody)),
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()

	fetch := func(target string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code, w.Header().Get("Cache-Control")
	}

	commandVersionedCode, commandVersionedCache := fetch("/gosx/bootstrap-feature-scene3d-command.js?v=7777")
	commandPlainCode, commandPlainCache := fetch("/gosx/bootstrap-feature-scene3d-command.js")
	instanceStreamVersionedCode, instanceStreamVersionedCache := fetch("/gosx/bootstrap-feature-scene3d-instance-stream.js?v=8888")
	instanceStreamPlainCode, instanceStreamPlainCache := fetch("/gosx/bootstrap-feature-scene3d-instance-stream.js")

	for _, c := range []struct {
		name string
		code int
	}{
		{"command versioned", commandVersionedCode},
		{"command plain", commandPlainCode},
		{"instance-stream versioned", instanceStreamVersionedCode},
		{"instance-stream plain", instanceStreamPlainCode},
	} {
		if c.code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", c.name, c.code)
		}
	}

	const immutable = "public, max-age=31536000, immutable"
	const devCache = "no-cache, no-store, must-revalidate"

	if commandVersionedCache != immutable {
		t.Fatalf("command chunk ?v= Cache-Control = %q, want %q (test setup assumption broken)", commandVersionedCache, immutable)
	}
	if commandPlainCache != devCache {
		t.Fatalf("command chunk plain Cache-Control = %q, want %q (test setup assumption broken)", commandPlainCache, devCache)
	}

	if instanceStreamVersionedCache != commandVersionedCache {
		t.Fatalf("instance-stream chunk ?v= Cache-Control = %q, want to match command chunk's %q", instanceStreamVersionedCache, commandVersionedCache)
	}
	if instanceStreamPlainCache != commandPlainCache {
		t.Fatalf("instance-stream chunk plain Cache-Control = %q, want to match command chunk's %q -- a plain request must not be cached as immutable", instanceStreamPlainCache, commandPlainCache)
	}
	if instanceStreamVersionedCache == instanceStreamPlainCache {
		t.Fatalf("instance-stream chunk: versioned and plain requests both got %q; a ?v= request must be immutable and a plain request must not", instanceStreamVersionedCache)
	}
}
