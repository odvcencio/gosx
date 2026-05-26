package surface

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestLoadWasmExecFromGoroot_Lib verifies the resolver finds wasm_exec.js
// under the modern lib/wasm/ path (Go 1.21+).
func TestLoadWasmExecFromGoroot_Lib(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib", "wasm")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("// fake wasm_exec for testing\n")
	if err := os.WriteFile(filepath.Join(libDir, "wasm_exec.js"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	data, reason := loadWasmExecFromGoroot(root)
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
	if string(data) != string(body) {
		t.Errorf("body mismatch: got %q", data)
	}
}

// TestLoadWasmExecFromGoroot_Misc verifies the resolver still finds the
// legacy misc/wasm/ path (Go 1.19–1.25).
func TestLoadWasmExecFromGoroot_Misc(t *testing.T) {
	root := t.TempDir()
	miscDir := filepath.Join(root, "misc", "wasm")
	if err := os.MkdirAll(miscDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("// misc/wasm path\n")
	if err := os.WriteFile(filepath.Join(miscDir, "wasm_exec.js"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	data, reason := loadWasmExecFromGoroot(root)
	if reason != "" {
		t.Fatalf("reason = %q, want empty", reason)
	}
	if string(data) != string(body) {
		t.Errorf("body mismatch: got %q", data)
	}
}

// TestLoadWasmExecFromGoroot_Missing returns an explanatory reason when
// neither candidate exists.
func TestLoadWasmExecFromGoroot_Missing(t *testing.T) {
	root := t.TempDir()
	data, reason := loadWasmExecFromGoroot(root)
	if data != nil {
		t.Fatalf("data = %v, want nil", data)
	}
	if reason == "" {
		t.Fatal("reason empty, want explanation")
	}
}

// TestLoadWasmExecFromGoroot_EmptyRoot rejects an empty goroot up-front
// rather than reading the filesystem.
func TestLoadWasmExecFromGoroot_EmptyRoot(t *testing.T) {
	data, reason := loadWasmExecFromGoroot("")
	if data != nil {
		t.Fatalf("data = %v, want nil", data)
	}
	if !strings.Contains(reason, "empty") {
		t.Errorf("reason = %q, want mention of empty GOROOT", reason)
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
