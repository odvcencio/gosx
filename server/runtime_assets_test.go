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
