package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func serveRedirect(t *testing.T, pattern, destination, target string) *httptest.ResponseRecorder {
	t.Helper()
	app := New()
	app.Redirect(pattern, destination, http.StatusMovedPermanently)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", target, nil))
	return w
}

func TestAppRedirectRefusesProtocolRelativeSubstitution(t *testing.T) {
	for _, tc := range []struct{ name, target string }{
		{"encoded slash", "/blog/%2Fevil.com"},
		{"encoded backslash", "/blog/%5Cevil.com"},
		{"encoded slash backslash", "/blog/%2F%5Cevil.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serveRedirect(t, "GET /blog/{slug}", "/{slug}", tc.target)
			loc := w.Header().Get("Location")
			if w.Code == http.StatusMovedPermanently && (strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`)) {
				t.Fatalf("open redirect to %q", loc)
			}
			// Escaping keeps the value inside one path segment, so the
			// redirect stays on this host.
			if loc != "" && !strings.HasPrefix(loc, "/%") {
				t.Fatalf("unexpected location %q", loc)
			}
		})
	}
}

func TestRedirectLeavesSite(t *testing.T) {
	for _, tc := range []struct {
		written, expanded string
		want              bool
	}{
		{"/{path...}", "//evil.com", true},
		{"/{p}", `/\evil.com`, true},
		{"/{p}", "/%5Cevil.com", true},
		{"/{p}", "/%2fevil.com", true},
		{"/{p}", "/ok", false},
		{"//cdn.example.com/{p}", "//cdn.example.com/x", false},
		{"https://a.example/{p}", "https://a.example/x", false},
	} {
		if got := redirectLeavesSite(tc.written, tc.expanded); got != tc.want {
			t.Errorf("redirectLeavesSite(%q, %q) = %t, want %t", tc.written, tc.expanded, got, tc.want)
		}
	}
}

func TestAppRedirectSubstitutionCases(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, destination, target, want string
	}{
		{"normal slug", "GET /blog/{slug}", "/posts/{slug}", "/blog/hello-world", "/posts/hello-world"},
		{"slug with space and question mark", "GET /blog/{slug}", "/posts/{slug}", "/blog/a%20b%3Fc", "/posts/a%20b%3Fc"},
		{"slug cannot inject query", "GET /blog/{slug}", "/posts/{slug}", "/blog/x%3Fnext=evil.com", "/posts/x%3Fnext=evil.com"},
		{"slug in query", "GET /blog/{slug}", "/search?q={slug}", "/blog/a%26b%3Dc", "/search?q=a%26b%3Dc"},
		{"multi-segment wildcard", "GET /legacy/{path...}", "/docs/{path...}", "/legacy/a/b", "/docs/a/b"},
		{"wildcard written without dots", "GET /legacy/{path...}", "/docs/{path}", "/legacy/a/b%20c", "/docs/a/b%20c"},
		{"wildcard escapes inside segments", "GET /legacy/{path...}", "/docs/{path...}", "/legacy/a%3Fb/c", "/docs/a%3Fb/c"},
		{"absolute destination", "GET /blog/{slug}", "https://example.com/p/{slug}", "/blog/x", "https://example.com/p/x"},
		{"absolute destination host stays", "GET /blog/{slug}", "https://example.com/p/{slug}", "/blog/%2F%2Fevil.com", "https://example.com/p/%2F%2Fevil.com"},
		{"protocol-relative written by app", "GET /blog/{slug}", "//cdn.example.com/{slug}", "/blog/x", "//cdn.example.com/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serveRedirect(t, tc.pattern, tc.destination, tc.target)
			if w.Code != http.StatusMovedPermanently {
				t.Fatalf("status %d, want 301", w.Code)
			}
			if got := w.Header().Get("Location"); got != tc.want {
				t.Fatalf("Location %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAppRedirectWildcardLeadingSlashReturns400(t *testing.T) {
	// Go's mux collapses "//" in request paths, but a wildcard can still
	// capture a leading slash through an encoded separator.
	app := New()
	app.Redirect("GET /go/{path...}", "/{path...}", http.StatusMovedPermanently)
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/go/%2Fevil.com", nil))
	if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
		t.Fatalf("status %d Location %q, want 400 and no Location", w.Code, w.Header().Get("Location"))
	}
}

func TestAppRewriteEscapesSubstitutedValues(t *testing.T) {
	app := New()
	app.Rewrite("GET /view/{slug}", "/docs/{slug}")
	app.Page("GET /docs/{slug}", func(ctx *Context) gosx.Node {
		return gosx.Text("slug=" + ctx.Request.PathValue("slug"))
	})
	app.Page("GET /admin", func(ctx *Context) gosx.Node { return gosx.Text("admin") })
	handler := app.Build()

	// An encoded query separator stays inside the segment.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/view/a%3Fb", nil))
	if body := w.Body.String(); !strings.Contains(body, "slug=a?b") {
		t.Fatalf("rewrite lost the encoded value: %d %q", w.Code, body)
	}

	// An encoded slash cannot turn the rewrite target into another path or
	// an off-site Location.
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/view/%2F%2Fevil.com", nil))
	if loc := w.Header().Get("Location"); strings.HasPrefix(loc, "//") || strings.Contains(loc, "evil.com") && !strings.HasPrefix(loc, "/") {
		t.Fatalf("rewrite produced Location %q", loc)
	}

	if got := expandPatternValues(requestWithPathValue("slug", "../admin"), "/docs/{slug}"); got != "/docs/..%2Fadmin" {
		t.Fatalf("expand = %q", got)
	}
}

func requestWithPathValue(name, value string) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	req.SetPathValue(name, value)
	return req
}
