package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

// The adapter report is demo diagnostics. It ships as a page-scoped lifecycle
// script on this route, not in the core bootstrap that every GoSX page loads.
func TestMotionPageLoadsAdapterReportAsPageScript(t *testing.T) {
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
	if !strings.Contains(head, `src="/motion-adapter-report.js"`) || !strings.Contains(head, `data-gosx-script="lifecycle"`) {
		t.Fatalf("motion page head does not load /motion-adapter-report.js as a lifecycle script:\n%s", head)
	}

	source, err := os.ReadFile(filepath.Join(dir, "..", "..", "..", "public", "motion-adapter-report.js"))
	if err != nil {
		t.Fatalf("read motion-adapter-report.js: %v", err)
	}
	if strings.Contains(string(source), "innerHTML") {
		t.Fatal("motion-adapter-report.js must write text only")
	}
	// The script writes inside the tree it observes; observing childList would
	// let its own textContent write re-trigger it without end.
	if regexp.MustCompile(`childList\s*:`).Match(source) {
		t.Fatal("motion-adapter-report.js must observe only the scene truth attribute, not childList")
	}
}
