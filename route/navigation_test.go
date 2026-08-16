package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// navScriptOpenTag is the literal opening-tag fragment NavigationScript
// renders. Counting it in the response body distinguishes "the runtime
// script is present" from any other data-gosx-navigation-* attribute a
// future feature might add.
const navScriptOpenTag = `<script ` + server.NavigationScriptAttr + `="true"`

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

// TestFileRoutedAppManualNavigationScriptNotDuplicated covers the existing
// workaround (ctx.AddHead(server.NavigationScript()) in SetLayout) alongside
// app.EnableNavigation(): the automatic injection must defer to the
// already-present script instead of adding a second one.
func TestFileRoutedAppManualNavigationScriptNotDuplicated(t *testing.T) {
	_, handler := buildFileRoutedApp(t, func(ctx *RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.NavigationScript())
		return server.HTMLDocumentWithLanguage(ctx.Title("Test"), "en", ctx.Head(), body)
	})

	html := getBody(t, handler)
	if count := strings.Count(html, navScriptOpenTag); count != 1 {
		t.Fatalf("expected exactly one navigation script tag, got %d\n%s", count, html)
	}
}
