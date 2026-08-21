package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// navScriptOpenTag is the literal opening-tag fragment the framework-owned
// navigation runtime renders. Counting it in the response body distinguishes
// the runtime from any other data-gosx-navigation-* attribute.
const navScriptOpenTag = `<script data-gosx-navigation="true"`

func buildFileRoutedApp(t *testing.T, layout LayoutFunc) (*server.App, http.Handler) {
	t.Helper()
	router := NewRouter()
	router.SetLayout(layout)
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("p", gosx.Text("home"))
		},
	})

	rootHandler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	app := server.New()
	app.EnableNavigation()
	app.Mount("/", rootHandler)
	return app, app.Build()
}

func getBody(t *testing.T, handler http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestFileRoutedAppEnableNavigationInjectsScript covers issue #169: an app
// built with route.NewRouter + router.SetLayout + app.Mount("/", handler)
// bypasses App.decoratePageContext, so app.EnableNavigation() alone used to
// be a silent no-op. The navigation runtime must now reach the page head
// through app.EnableNavigation() with no manual AddHead call in the layout.
func TestFileRoutedAppEnableNavigationInjectsScript(t *testing.T) {
	_, handler := buildFileRoutedApp(t, func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocumentWithLanguage(ctx.Title("Test"), "en", ctx.Head(), body)
	})

	html := getBody(t, handler)
	if count := strings.Count(html, navScriptOpenTag); count != 1 {
		t.Fatalf("expected exactly one navigation script tag, got %d\n%s", count, html)
	}
}

// TestFileRoutedAppMountBeforeEnableNavigationStillInjectsScript covers PR
// #174 review N3: Mount used to read app.navigation eagerly, so calling
// app.Mount before app.EnableNavigation silently dropped the wiring. The
// wiring now resolves at Build() time (see server.App.registerMountRoutes),
// so app.EnableNavigation() may run before or after app.Mount() as long as
// both run before app.Build().
func TestFileRoutedAppMountBeforeEnableNavigationStillInjectsScript(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocumentWithLanguage(ctx.Title("Test"), "en", ctx.Head(), body)
	})
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			return gosx.El("p", gosx.Text("home"))
		},
	})
	rootHandler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	app := server.New()
	app.Mount("/", rootHandler) // Mount before EnableNavigation, deliberately.
	app.EnableNavigation()
	handler := app.Build()

	html := getBody(t, handler)
	if count := strings.Count(html, navScriptOpenTag); count != 1 {
		t.Fatalf("expected exactly one navigation script tag when Mount runs before EnableNavigation, got %d\n%s", count, html)
	}
}

func TestSharedCacheableFileRouteStripsNonceBakedByHandlerScripts(t *testing.T) {
	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocumentWithLanguage(ctx.Title("Shared route"), "en", ctx.Head(), body)
	})
	var issuedNonce string
	router.Add(Route{
		Pattern: "/",
		Handler: func(ctx *RouteContext) gosx.Node {
			issuedNonce = ctx.Nonce()
			ctx.CachePublic(time.Minute)
			ctx.AddHead(ctx.InlineScript("window.routeHeadScript = true"))
			return gosx.El("main",
				ctx.InlineScript("window.routeBodyScript = true"),
				ctx.JSONScript("route-json", map[string]string{"status": "shared"}),
			)
		},
	})

	rootHandler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}
	app := server.New()
	app.EnableSecurityPolicy(server.SecurityPolicy{
		ContentSecurityPolicy: "default-src 'self' 'unsafe-inline'; script-src 'nonce-{nonce}'",
	})
	app.Mount("/", rootHandler)

	res := httptest.NewRecorder()
	app.Build().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if issuedNonce == "" {
		t.Fatal("expected the route handler to observe a request nonce before CachePublic")
	}
	if got, want := res.Header().Get("Content-Security-Policy"), "default-src 'self' 'unsafe-inline'; script-src 'none'"; got != want {
		t.Fatalf("shared policy = %q, want %q", got, want)
	}
	body := res.Body.String()
	if strings.Contains(body, issuedNonce) || strings.Contains(body, "nonce=") {
		t.Fatalf("shared route retained request nonce %q: %s", issuedNonce, body)
	}
	for _, want := range []string{
		"window.routeHeadScript = true",
		"window.routeBodyScript = true",
		`id="route-json"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("shared route missing %q: %s", want, body)
		}
	}
}
