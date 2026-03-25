package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestAppBasic(t *testing.T) {
	app := New()
	app.Route("/", func(r *http.Request) gosx.Node {
		return gosx.Text("home")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "home") {
		t.Fatalf("expected 'home' in body, got %q", w.Body.String())
	}
}

func TestAppWithLayout(t *testing.T) {
	app := New()
	app.SetLayout(func(title string, body gosx.Node) gosx.Node {
		return gosx.El("html", gosx.El("body", body))
	})
	app.Route("/page", func(r *http.Request) gosx.Node {
		return gosx.Text("content")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/page", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "content") {
		t.Fatalf("expected 'content' in body, got %q", body)
	}
	if !strings.Contains(body, "<html>") {
		t.Fatalf("expected '<html>' in body, got %q", body)
	}
}

func TestHTMLDocument(t *testing.T) {
	doc := HTMLDocument("Test Page", gosx.Text(""), gosx.Text("hello"))
	html := gosx.RenderHTML(doc)

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Fatal("missing doctype")
	}
	if !strings.Contains(html, "<title>Test Page</title>") {
		t.Fatalf("missing title, got %q", html)
	}
	if !strings.Contains(html, "hello") {
		t.Fatal("missing body content")
	}
}

func TestAppMultipleRoutes(t *testing.T) {
	app := New()
	app.Route("/foo", func(r *http.Request) gosx.Node {
		return gosx.Text("foo-page")
	})
	app.Route("/bar", func(r *http.Request) gosx.Node {
		return gosx.Text("bar-page")
	})

	handler := app.Build()

	req := httptest.NewRequest("GET", "/foo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "foo-page") {
		t.Fatalf("expected 'foo-page', got %q", w.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/bar", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), "bar-page") {
		t.Fatalf("expected 'bar-page', got %q", w2.Body.String())
	}
}

func TestAppSetsRequestIDAndSecurityHeaders(t *testing.T) {
	app := New()
	app.Route("/", func(r *http.Request) gosx.Node {
		id := RequestID(r)
		return gosx.Text("request:" + id)
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	requestID := w.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected request id header")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected nosniff header, got %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("unexpected referrer policy: %q", got)
	}
	if !strings.Contains(w.Body.String(), requestID) {
		t.Fatalf("expected handler to see request id %q, got %q", requestID, w.Body.String())
	}
}

func TestAppHealthAndReadinessEndpoints(t *testing.T) {
	app := New()
	handler := app.Build()

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, w.Code)
		}
		if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("%s: expected json content type, got %q", path, got)
		}
		if body := strings.TrimSpace(w.Body.String()); body != `{"ok":true}` {
			t.Fatalf("%s: unexpected body %q", path, body)
		}
	}
}

func TestAppRecoversFromPanics(t *testing.T) {
	app := New()
	app.Route("/panic", func(r *http.Request) gosx.Node {
		panic("boom")
	})

	handler := app.Build()
	req := httptest.NewRequest("GET", "/panic", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("unexpected recovery body %q", w.Body.String())
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request id header on recovered response")
	}
}
