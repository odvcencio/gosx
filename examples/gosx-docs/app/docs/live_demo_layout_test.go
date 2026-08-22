package docs

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/route"
)

func docsLayoutModule(t *testing.T) route.FileModule {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	module, found := route.DefaultFileModuleRegistry().Lookup(filepath.Join(filepath.Dir(testFile), "layout.gsx"))
	if !found {
		t.Fatal("docs layout module is not registered")
	}
	if module.Bindings == nil {
		t.Fatal("docs layout module has no bindings")
	}
	return module
}

func TestDocsLayoutBindsLiveDemoOnlyWhereMapped(t *testing.T) {
	module := docsLayoutModule(t)

	bindings := module.Bindings(&route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/docs/compiler", nil)}, route.FilePage{RoutePath: "/docs/compiler"}, nil)
	live, ok := bindings.Values["docsLiveDemo"].(map[string]any)
	if !ok {
		t.Fatalf("docsLiveDemo for /docs/compiler = %#v, want demo map", bindings.Values["docsLiveDemo"])
	}
	if live["href"] != "/demos/playground" || live["title"] != "GoSX Playground" {
		t.Fatalf("docsLiveDemo for /docs/compiler = %#v", live)
	}

	for _, path := range []string{"/docs", "/docs/getting-started"} {
		bindings := module.Bindings(&route.RouteContext{Request: httptest.NewRequest(http.MethodGet, path, nil)}, route.FilePage{RoutePath: path}, nil)
		if bindings.Values["docsLiveDemo"] != nil {
			t.Fatalf("docsLiveDemo for %s = %#v, want nil", path, bindings.Values["docsLiveDemo"])
		}
	}
}

// TestGuidePagesRenderSeeItLiveLink proves the user-facing half of the
// cross-navigation: mapped guides link to their proving demo in the rendered
// header, and unmapped guides render no such link.
func TestGuidePagesRenderSeeItLiveLink(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	router := route.NewRouter()
	if err := router.AddDir(filepath.Dir(filepath.Dir(testFile)), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("add app routes: %v", err)
	}

	rendered := func(path string) string {
		response := httptest.NewRecorder()
		router.Build().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, response.Code)
		}
		return response.Body.String()
	}

	hubs := rendered("/docs/hubs")
	for _, want := range []string{"See it live", `class="docs-header__live"`, `href="/demos/collab"`, "Collab Editor"} {
		if !strings.Contains(hubs, want) {
			t.Fatalf("rendered /docs/hubs is missing %q", want)
		}
	}

	compiler := rendered("/docs/compiler")
	if !strings.Contains(compiler, `href="/demos/playground"`) {
		t.Fatal("rendered /docs/compiler does not link to the playground demo")
	}

	images := rendered("/docs/images")
	if strings.Contains(images, "See it live") {
		t.Fatal("rendered /docs/images renders a live-demo link without a catalog pairing")
	}
}
