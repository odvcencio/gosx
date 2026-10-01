package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/session"
)

func cacheTestSession(t *testing.T, manager *session.Manager) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session.Current(r).Set("name", "Ada")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session", nil))
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("expected session cookie")
	}
	return w.Result().Cookies()[0]
}

func TestSessionPagesSeparateAnonymousAndPrivateHTML(t *testing.T) {
	m := session.MustNew("page-cache-session-secret", session.Options{})
	cookie := cacheTestSession(t, m)
	app := New()
	app.Use(m.Middleware)
	app.Use(m.Protect)
	app.AddHeadDecorator(func(ctx *Context) (gosx.Node, bool) {
		token := m.Token(ctx.Request)
		return gosx.El("meta", gosx.Attrs(gosx.Attr("name", "csrf-token"), gosx.Attr("content", token))), token != ""
	})
	app.Page("GET /cache/{policy}", func(ctx *Context) gosx.Node {
		switch ctx.Request.PathValue("policy") {
		case "public":
			ctx.CachePublic(time.Minute)
		case "private":
			ctx.CachePrivate(time.Minute)
		case "no-store":
			ctx.NoStore()
		case "header":
			ctx.Header().Set("Cache-Control", "private, max-age=20")
		case "header-etag":
			ctx.Header().Set("Cache-Control", "private, max-age=20")
			ctx.SetETag("private-page")
		case "write":
			session.Current(ctx.Request).Set("name", "Ada")
		}
		return gosx.El("main", gosx.Text(session.Current(ctx.Request).String("name")))
	})
	h := app.Build()
	for _, tc := range []struct {
		path, cache string
	}{
		{"/default", "public, max-age=0, must-revalidate"},
		{"/public", "public, max-age=60"},
		{"/private", "private, max-age=60"},
		{"/no-store", "no-store"},
		{"/header", "private, max-age=20"},
		{"/header-etag", "private, max-age=20"},
		{"/write", "private, no-store"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/cache"+tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := w.Header().Get("Cache-Control"); got != tc.cache {
				t.Fatalf("Cache-Control = %q, want %q", got, tc.cache)
			}
			if tc.path != "/write" && (len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), `name="csrf-token"`)) {
				t.Fatal("anonymous HTML carries session state")
			}
			if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Cookie") {
				t.Fatal("anonymous HTML does not vary by Cookie")
			}
			r.AddCookie(cookie)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("personalized Cache-Control = %q", got)
			}
			if !strings.Contains(w.Body.String(), `name="csrf-token"`) || (tc.path != "/header-etag" && w.Header().Get("ETag") != "") {
				t.Fatalf("personalized headers or token incorrect: %v", w.Header())
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/cache/default", nil)
	r.Header.Set("Authorization", "Bearer example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Authorization response became public")
	}
}

func TestAnonymousSessionPageKeepsNonceSecurityPolicy(t *testing.T) {
	m := session.MustNew("page-cache-session-secret", session.Options{})
	app := New()
	app.Use(m.Middleware)
	app.EnableNavigation()
	app.EnableSecurityPolicy(SecurityPolicy{ContentSecurityPolicy: "script-src 'nonce-{nonce}'"})
	app.Page("GET /", func(ctx *Context) gosx.Node { return gosx.Text("anonymous") })
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(w.Result().Cookies()) != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("nonce response headers = %v", w.Header())
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-") || !strings.Contains(w.Body.String(), `nonce="`) {
		t.Fatal("default caching broke nonce-protected navigation")
	}
}

func TestSessionVisitorsBypassISR(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "static", "index.html"), []byte("anonymous artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	writeISRManifest(t, root, isrManifest{Routes: []isrRoute{{Path: "/", File: "index.html", RevalidateSeconds: 60}}})
	m := session.MustNew("page-cache-session-secret", session.Options{})
	cookie := cacheTestSession(t, m)
	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	app.Use(m.Middleware)
	app.Page("GET /", func(ctx *Context) gosx.Node {
		return gosx.Text("origin token: " + session.Token(ctx.Request))
	})
	h := app.Build()
	for _, tc := range []struct {
		name   string
		cookie *http.Cookie
		auth   string
		isr    string
	}{
		{"anonymous", nil, "", "HIT"},
		{"session", cookie, "", ""},
		{"invalid session", &http.Cookie{Name: "gosx_session", Value: "invalid"}, "", ""},
		{"other cookie", &http.Cookie{Name: "identity", Value: "example"}, "", ""},
		{"authorization", nil, "Bearer example", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Accept", "text/html")
			r.Header.Set("Authorization", tc.auth)
			if tc.cookie != nil {
				r.AddCookie(tc.cookie)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := w.Header().Get("X-GoSX-ISR"); got != tc.isr {
				t.Fatalf("ISR = %q, want %q", got, tc.isr)
			}
			if tc.isr == "HIT" && w.Header().Get("Cache-Control") != "public, max-age=0, stale-while-revalidate=60" {
				t.Fatal("anonymous ISR policy changed")
			}
			if tc.cookie == cookie && !strings.Contains(w.Body.String(), "origin token: ") {
				t.Fatal("session visitor received anonymous artifact")
			}
		})
	}
}

func TestISRDoesNotStoreSessionCreatingResponse(t *testing.T) {
	root := t.TempDir()
	writeISRManifest(t, root, isrManifest{Routes: []isrRoute{{Path: "/", File: "index.html"}}})
	m := session.MustNew("page-cache-session-secret", session.Options{})
	app := New()
	app.SetRuntimeRoot(root)
	app.EnableISR()
	app.Use(m.Middleware)
	app.Page("GET /", func(ctx *Context) gosx.Node {
		session.Current(ctx.Request).Set("name", "Ada")
		return gosx.Text("private token: " + session.Token(ctx.Request))
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, r)
	if w.Header().Get("X-GoSX-ISR") != "" || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("private response reached ISR: %v", w.Header())
	}
	if _, err := app.ISRStore().StatArtifact(filepath.Join(root, "static"), "/", "index.html"); err == nil {
		t.Fatal("ISR stored personalized HTML")
	}
}
