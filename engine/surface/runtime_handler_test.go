package surface

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRuntimeHandlerServesRuntimeJS verifies that the runtime handler returns
// 200 with the JS content-type for /gosx/surface/runtime.js.
func TestRuntimeHandlerServesRuntimeJS(t *testing.T) {
	h := RuntimeHandler()
	req := httptest.NewRequest("GET", "/gosx/surface/runtime.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", ct)
	}
	if w.Body.Len() == 0 {
		t.Errorf("empty body")
	}
}

// TestRuntimeHandlerServesWasmExecJS verifies the wasm_exec.js loader is
// served from GOROOT or TinyGo at startup.
func TestRuntimeHandlerServesWasmExecJS(t *testing.T) {
	h := RuntimeHandler()
	req := httptest.NewRequest("GET", "/gosx/surface/wasm_exec.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// Either a 200 (resolver found a file) or 503 (no source available) is
	// acceptable in unit tests; 503 must include an explanatory body.
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 200 or 503", w.Code)
	}
	if w.Code == http.StatusOK {
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
			t.Errorf("Content-Type = %q, want application/javascript", ct)
		}
		if w.Body.Len() == 0 {
			t.Errorf("empty body")
		}
	}
	if w.Code == http.StatusServiceUnavailable {
		if w.Body.Len() == 0 {
			t.Errorf("503 must include an explanatory body")
		}
	}
}

// TestRuntimeHandlerUnknown404 verifies unknown paths return 404.
func TestRuntimeHandlerUnknown404(t *testing.T) {
	h := RuntimeHandler()
	req := httptest.NewRequest("GET", "/gosx/surface/nonexistent.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
