package server

import (
	"m31labs.dev/gosx"
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

func TestISRTrailingSlashBypassesRenderOrigin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static", "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "static", "notes", "index.html"), []byte("anonymous snapshot"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{Routes: []isrRoute{{Path: "/notes", File: "notes/index.html"}}})
	for _, tc := range []struct{ name, method, header, value string }{
		{"cookie", "GET", "Cookie", "session=visitor"},
		{"authorization", "GET", "Authorization", "Bearer visitor"},
		{"head", "HEAD", "Cookie", "session=visitor"},
		{"revalidation", "GET", isrBypassHeader, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New()
			app.SetRuntimeRoot(root)
			app.EnableISR()
			calls := 0
			app.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; next.ServeHTTP(w, r) })
			})
			app.Page("GET /notes", func(ctx *Context) gosx.Node {
				ctx.NoStore()
				return gosx.Text("origin " + ctx.Request.URL.Query().Get("q"))
			})
			handler := app.Build()
			req := httptest.NewRequest(tc.method, "/notes/?q=kept", nil)
			req.Header.Set("Accept", "text/html")
			req.Header.Set(tc.header, tc.value)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 200 || w.Header().Get("X-GoSX-ISR") != "" || w.Header().Get("Location") != "" {
				t.Fatalf("bypass response status=%d headers=%v", w.Code, w.Header())
			}
			if tc.method == "GET" && !strings.Contains(w.Body.String(), "origin kept") {
				t.Fatalf("missing origin/query data: %q", w.Body.String())
			}
			if calls != 1 || req.URL.Path != "/notes/" {
				t.Fatalf("middleware calls=%d original path=%q", calls, req.URL.Path)
			}
			unknown := httptest.NewRequest("GET", "/notes/unknown/", nil)
			unknown.Header.Set(tc.header, tc.value)
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, unknown)
			if w.Code != 404 {
				t.Fatalf("unknown trailing path status=%d", w.Code)
			}
		})
	}
}
