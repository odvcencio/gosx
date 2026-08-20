package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// TestSidecarCSSAppearsAfterFirstRender proves that a sidecar stylesheet added
// next to an existing page reaches the next render. The CSS node cache used to
// key on the path alone, so it never noticed a new or edited file.
func TestSidecarCSSAppearsAfterFirstRender(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main class="page">Reload page</main>
}
`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(&server.DocumentContext{
			Request: ctx.Request, Status: ctx.StatusCode(), Title: "Reload", Head: ctx.Head(), Body: body,
			BodyAttrs: ctx.BodyAttrsValue(), Nonce: ctx.Nonce(),
		})
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler := router.Build()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(first.Body.String(), "cornflowerblue") {
		t.Fatalf("expected no sidecar CSS on the first render")
	}

	writeRouteFile(t, root, "page.css", `.page { color: cornflowerblue; }`)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(second.Body.String(), "cornflowerblue") {
		t.Fatalf("expected the new sidecar CSS after it appeared, got %q", second.Body.String())
	}
}

// TestSidecarCSSEditReachesNextRender proves that an edit to an existing
// sidecar stylesheet reaches the next render.
func TestSidecarCSSEditReachesNextRender(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "page.gsx", `package app

func Page() Node {
	return <main class="page">Edit page</main>
}
`)
	writeRouteFile(t, root, "page.css", `.page { color: seagreen; }`)

	router := NewRouter()
	router.SetLayout(func(ctx *RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(&server.DocumentContext{
			Request: ctx.Request, Status: ctx.StatusCode(), Title: "Edit", Head: ctx.Head(), Body: body,
			BodyAttrs: ctx.BodyAttrsValue(), Nonce: ctx.Nonce(),
		})
	})
	if err := router.AddDir(root, FileRoutesOptions{}); err != nil {
		t.Fatal(err)
	}
	handler := router.Build()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(first.Body.String(), "seagreen") {
		t.Fatalf("expected the original CSS, got %q", first.Body.String())
	}

	// Move the modification time forward so the change is visible at one-second
	// filesystem resolution.
	cssPath := filepath.Join(root, "page.css")
	if err := os.WriteFile(cssPath, []byte(`.page { color: darkorange; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cssPath, future, future); err != nil {
		t.Fatal(err)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(second.Body.String(), "darkorange") {
		t.Fatalf("expected the edited CSS on the next render, got %q", second.Body.String())
	}
}
