package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticRobotsHandlerServesPlainText(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()

	StaticRobotsHandler("User-agent: *\nAllow: /\n").ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Allow: /") {
		t.Fatalf("unexpected robots body %q", body)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected robots content type %q", got)
	}
}

func TestSitemapHandlerServesXMLAndErrors(t *testing.T) {
	okReq := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	okW := httptest.NewRecorder()

	SitemapHandler(func(r *http.Request) (string, error) {
		return `<urlset><url><loc>https://m31labs.dev/</loc></url></urlset>`, nil
	}).ServeHTTP(okW, okReq)

	if okW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", okW.Code)
	}
	if got := okW.Header().Get("Content-Type"); got != "application/xml; charset=utf-8" {
		t.Fatalf("unexpected sitemap content type %q", got)
	}

	errReq := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	errW := httptest.NewRecorder()
	SitemapHandler(func(r *http.Request) (string, error) {
		return "", errors.New("boom")
	}).ServeHTTP(errW, errReq)

	if errW.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", errW.Code)
	}
}
