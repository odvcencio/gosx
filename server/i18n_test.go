package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestI18nMiddlewareStripsLocalePrefixForRouting(t *testing.T) {
	app := New()
	app.UseI18n(I18nConfig{
		Locales:       []string{"en", "fr"},
		DefaultLocale: "en",
	})
	app.Page("GET /about", func(ctx *Context) gosx.Node {
		return gosx.Text(RequestLocale(ctx.Request) + " " + ctx.Request.URL.Path)
	})

	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fr/about", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "fr /about") {
		t.Fatalf("expected localized routed path, got %q", w.Body.String())
	}
	if got := w.Header().Get("Content-Language"); got != "fr" {
		t.Fatalf("expected Content-Language fr, got %q", got)
	}
}

func TestI18nMiddlewareFallsBackToAcceptLanguage(t *testing.T) {
	app := New()
	app.UseI18n(I18nConfig{
		Locales:       []string{"en", "fr"},
		DefaultLocale: "en",
	})
	app.Page("GET /about", func(ctx *Context) gosx.Node {
		return gosx.Text(RequestLocale(ctx.Request))
	})

	req := httptest.NewRequest(http.MethodGet, "/about", nil)
	req.Header.Set("Accept-Language", "fr-CA, en;q=0.8")
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "fr") {
		t.Fatalf("expected accept-language locale, got %q", w.Body.String())
	}
	if got := w.Header().Values("Vary"); len(got) == 0 {
		t.Fatal("expected Vary header")
	}
}

func TestLocalePathHonorsDefaultPrefixPolicy(t *testing.T) {
	cfg := I18nConfig{Locales: []string{"en", "fr"}, DefaultLocale: "en"}
	if got := LocalePath("en", "/about", cfg); got != "/about" {
		t.Fatalf("expected default locale path without prefix, got %q", got)
	}
	if got := LocalePath("fr", "/about", cfg); got != "/fr/about" {
		t.Fatalf("expected non-default locale path with prefix, got %q", got)
	}
	cfg.PrefixDefault = true
	if got := LocalePath("en", "/", cfg); got != "/en" {
		t.Fatalf("expected default locale root prefix, got %q", got)
	}
}
