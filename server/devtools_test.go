package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppServesDevtoolsLantern(t *testing.T) {
	app := New()
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, DevtoolsLanternPath, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected JS content type, got %q", got)
	}

	body := w.Body.String()
	// Lantern is read-only: it reflects the Scene3D debug registry the
	// runtime bundle already exposes. Pin the accessors it depends on.
	for _, want := range []string{
		"__gosx_scene3d_debug",
		"__gosx_scene3d_telemetry",
		"__gosx_scene3d_cull_telemetry",
		"__gosxScene3DWebGPUStats",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lantern asset missing %q", want)
		}
	}
}

func TestDevtoolsLanternServesWithoutRuntimeRoot(t *testing.T) {
	// The asset is embedded, so it must resolve even when no runtime root
	// or build manifest exists (bare `go run` development).
	app := New()
	app.SetRuntimeRoot("")
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, DevtoolsLanternPath, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDevtoolsLanternScriptTag(t *testing.T) {
	tag := DevtoolsLanternScriptTag()
	if !strings.Contains(tag, DevtoolsLanternPath) || !strings.Contains(tag, "defer") {
		t.Fatalf("unexpected script tag %q", tag)
	}
}
