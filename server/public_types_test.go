package server

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The public file handler names a file's type from GoSX's fixed table, so a
// host whose own table disagrees (or has no entry, as on macOS) does not
// change what a page receives.
func TestPublicFilesHaveFixedTypes(t *testing.T) {
	want := map[string]string{
		".ktx2": "image/ktx2",
		".gltf": "model/gltf+json",
		".glb":  "model/gltf-binary",
	}
	public := t.TempDir()
	for ext, fixed := range want {
		if err := os.WriteFile(filepath.Join(public, "file"+ext), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		// The host table cannot drop an entry, so put the fixed type back
		// when the test ends.
		t.Cleanup(func() { _ = mime.AddExtensionType(ext, fixed) })
		if err := mime.AddExtensionType(ext, "application/x-other"); err != nil {
			t.Fatal(err)
		}
	}

	app := New()
	app.SetPublicDir(public)
	handler := app.Build()
	for ext, fixed := range want {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/file"+ext, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", ext, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != fixed {
			t.Errorf("%s: Content-Type = %q, want %q", ext, got, fixed)
		}
	}
}
