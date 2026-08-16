package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestServePublicCachePolicy pins the two-tier cache policy for public files.
// A version query marks the URL as content-addressed by the app, so the
// response for that exact URL never changes and immutable caching is safe; an
// unversioned URL may be overwritten in place, so every view revalidates.
// Before this split every public file — fonts included — revalidated on every
// repeat view.
func TestServePublicCachePolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "site.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{publicDir: dir}

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		if !app.servePublic(rec, httptest.NewRequest(http.MethodGet, target, nil)) {
			t.Fatalf("servePublic did not serve %s", target)
		}
		return rec
	}

	unversioned := get("/site.css")
	if got := unversioned.Header().Get("Cache-Control"); got != "public, max-age=0, must-revalidate" {
		t.Errorf("unversioned Cache-Control = %q, want revalidation", got)
	}

	versioned := get("/site.css?v=1712345678-6")
	if got := versioned.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("versioned Cache-Control = %q, want a year immutable", got)
	}
}
