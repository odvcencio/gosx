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
