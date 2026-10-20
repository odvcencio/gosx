package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestMotionPageUsesNativeSceneStatus(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	dir := filepath.Dir(testFile)
	module, found := route.DefaultFileModuleRegistry().Lookup(filepath.Join(dir, "page.gsx"))
	if !found || module.Load == nil {
		t.Fatal("motion page module has no loader")
	}
	ctx := &route.RouteContext{Request: httptest.NewRequest(http.MethodGet, "/docs/motion", nil)}
	if _, err := module.Load(ctx, route.FilePage{RoutePath: "/docs/motion"}); err != nil {
		t.Fatalf("load motion page: %v", err)
	}
	head := gosx.RenderHTML(ctx.Head())
	if strings.Contains(head, "motion-adapter-report.js") {
		t.Fatal("motion page must use Scene3D status bindings instead of an app script")
	}

	source, err := os.ReadFile(filepath.Join(dir, "page.gsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"data-gosx-scene3d-status-scope", `data-gosx-scene3d-status="renderer"`, `data-gosx-scene3d-status="adapter"`} {
		if !strings.Contains(string(source), marker) {
			t.Errorf("motion page is missing %q", marker)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "..", "public", "motion-adapter-report.js")); !os.IsNotExist(err) {
		t.Fatal("motion docs must not ship an app-authored adapter reporter")
	}
}
