package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// A production build.json that lists renderer-probe must serve the hashed
// chunk at the unhashed public URL; an unlisted chunk is a 404.
func TestProductionServesRendererProbeChunk(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets", "runtime")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	body := []byte("window.__gosx_renderer_probe_chunk = 1;")
	if err := os.WriteFile(filepath.Join(assetsDir, "bootstrap-feature-renderer-probe.ab12.js"), body, 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		Features: map[string]buildmanifest.HashedAsset{
			"renderer-probe": {File: "bootstrap-feature-renderer-probe.ab12.js", Hash: "ab12", Size: int64(len(body))},
		},
	}}
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
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gosx/bootstrap-feature-renderer-probe.js", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "__gosx_renderer_probe_chunk") {
		t.Fatalf("GET renderer-probe = %d %q, want the hashed chunk", w.Code, w.Body.String())
	}
}
