package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/session"
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

func TestI18nManagedCapabilityUsesLocaleRewriteInBothMiddlewareOrders(t *testing.T) {
	for _, tc := range []struct {
		name      string
		i18nFirst bool
	}{
		{name: "i18n-outer", i18nFirst: true},
		{name: "protect-outer", i18nFirst: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newI18nManagedHandler(t, tc.i18nFirst)
			get := httptest.NewRequest(http.MethodGet, "/fr/token", nil)
			getRec := httptest.NewRecorder()
			handler.ServeHTTP(getRec, get)
			if getRec.Code != http.StatusOK {
				t.Fatalf("token GET status=%d body=%s", getRec.Code, getRec.Body.String())
			}
			token := renderedBodyText(getRec.Body.String())
			if token == "" {
				t.Fatal("token GET returned an empty token")
			}
			cookies := getRec.Result().Cookies()
			if len(cookies) == 0 {
				t.Fatal("token GET did not set a session cookie")
			}

			values := url.Values{"csrf_token": {token}, "name": {"Ada + locale"}}
			urlReq := httptest.NewRequest(http.MethodPost, "/fr/gosx/action/locale", strings.NewReader(values.Encode()))
			urlReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			urlReq.AddCookie(cookies[0])
			urlRec := httptest.NewRecorder()
			handler.ServeHTTP(urlRec, urlReq)
			if urlRec.Code != http.StatusOK {
				t.Fatalf("URL-encoded locale action status=%d body=%s", urlRec.Code, urlRec.Body.String())
			}

			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if err := writer.WriteField("csrf_token", token); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("name", "multipart locale"); err != nil {
				t.Fatal(err)
			}
			part, err := writer.CreateFormFile("avatar", "locale.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, "locale upload"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			multipartReq := httptest.NewRequest(http.MethodPost, "/fr/gosx/action/locale", &body)
			multipartReq.Header.Set("Content-Type", writer.FormDataContentType())
			multipartReq.AddCookie(cookies[0])
			multipartRec := httptest.NewRecorder()
			handler.ServeHTTP(multipartRec, multipartReq)
			if multipartRec.Code != http.StatusOK {
				t.Fatalf("multipart locale action status=%d body=%s", multipartRec.Code, multipartRec.Body.String())
			}
		})
	}
}

func renderedBodyText(document string) string {
	start := strings.Index(document, "<body")
	if start < 0 {
		return ""
	}
	start = strings.IndexByte(document[start:], '>')
	if start < 0 {
		return ""
	}
	start += strings.Index(document, "<body") + 1
	end := strings.Index(document[start:], "<!--gosx-stream-tail-->")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(document[start : start+end])
}

func newI18nManagedHandler(t *testing.T, i18nFirst bool) http.Handler {
	t.Helper()
	manager, err := session.New("i18n-managed-test-secret", session.Options{CookieName: "i18n_managed", AllowInsecure: true})
	if err != nil {
		t.Fatal(err)
	}
	actions := action.NewRouter()
	if err := actions.RegisterManagedPOST("locale", action.Config{Limits: action.Limits{MaxFiles: 1, MaxFileBytes: 128}}, func(ctx *action.Context) (action.Result, error) {
		if ctx.Request.URL.Path != "/gosx/action/locale" {
			t.Fatalf("action saw unrewritten path %q", ctx.Request.URL.Path)
		}
		if ctx.Request.MultipartForm != nil {
			t.Fatal("multipart body was parsed before the managed action")
		}
		if ctx.Form.Value("name") == "" {
			t.Fatal("action did not receive form value")
		}
		if ctx.Form.Value("name") == "multipart locale" {
			upload, err := ctx.Form.File("avatar")
			if err != nil {
				return action.Result{}, err
			}
			reader, err := upload.Open()
			if err != nil {
				return action.Result{}, err
			}
			defer reader.Close()
			if _, err := io.ReadAll(reader); err != nil {
				return action.Result{}, err
			}
		}
		return action.Result{OK: true}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.Page("GET /token", func(ctx *Context) gosx.Node {
		return gosx.Text(session.Token(ctx.Request))
	})
	if i18nFirst {
		app.UseI18n(I18nConfig{Locales: []string{"en", "fr"}, DefaultLocale: "en", PrefixDefault: true})
		app.Use(func(next http.Handler) http.Handler { return manager.Middleware(manager.Protect(next)) })
	} else {
		app.Use(func(next http.Handler) http.Handler { return manager.Middleware(manager.Protect(next)) })
		app.UseI18n(I18nConfig{Locales: []string{"en", "fr"}, DefaultLocale: "en", PrefixDefault: true})
	}
	app.Mount("/", actions)
	return app.Build()
}
