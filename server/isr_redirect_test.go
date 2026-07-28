package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Prerendered export HTML carries relative links (../routing) that only
// resolve correctly when the page URL ends in a trailing slash. The origin
// must canonicalize ISR pages to trailing-slash URLs.
func TestAppEnableISRRedirectsPrerenderedPageToTrailingSlash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static", "docs", "getting-started"), 0755); err != nil {
		t.Fatal(err)
	}
	html := `<!DOCTYPE html><html><body><a href="../routing">Routing</a></body></html>`
	if err := os.WriteFile(filepath.Join(root, "static", "docs", "getting-started", "index.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Routes: []isrRoute{
			{Path: "/docs/getting-started", File: "docs/getting-started/index.html"},
		},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/docs/getting-started?x=1", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect to trailing-slash URL, got %d body=%q", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/docs/getting-started/?x=1" {
		t.Fatalf("expected redirect to trailing-slash URL preserving query, got %q", loc)
	}

	req = httptest.NewRequest(http.MethodGet, "/docs/getting-started/", nil)
	req.Header.Set("Accept", "text/html")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected trailing-slash URL to serve artifact with 200, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Routing") {
		t.Fatalf("expected artifact body, got %q", body)
	}
	if got := w.Header().Get("X-GoSX-ISR"); got != "HIT" {
		t.Fatalf("expected ISR hit header, got %q", got)
	}
}

func TestAppEnableISRRootPageNeedsNoRedirect(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "static", "index.html"), []byte("<!DOCTYPE html><html><body>static home</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{
		Routes: []isrRoute{{Path: "/", File: "index.html"}},
	})

	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected root to serve without redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no redirect for root page, got Location %q", loc)
	}
}
