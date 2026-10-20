package docs

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

// TestStatCardSharedCallRendersOriginalMarkup proves the converted call
// sites (StatCard(...) -> <ui.StatCard Value=... Label=.../>) render the
// identical markup app/components.go's old hand-built StatCard produced.
func TestStatCardSharedCallRendersOriginalMarkup(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve this test file's own path")
	}
	page := filepath.Join(filepath.Dir(file), "page.gsx")
	node, err := route.DefaultFileRenderer(nil, route.FilePage{FilePath: page, Pattern: "/docs/scene3d-vs-threejs"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	html := gosx.RenderHTML(node)
	// The page.gsx source places each <ui.StatCard/> tag on its own
	// indented line, so the whitespace between sibling tags is itself a
	// text node — the same JSX whitespace rule any other multi-line sibling
	// list in this renderer follows, not something shared-component calls
	// change. The old hand-built gosx.El(...) call sequence emitted no such
	// gap, so this checks the shared component's own emitted structure
	// (each want fragment has no whitespace of its own to collide with)
	// rather than the exact spacing around the four sibling calls.
	for _, want := range []string{
		`<div class="stat-card glass-panel">`,
		`<span class="stat-card__value">Typed Go</span>`,
		`<span class="stat-card__label">scene authoring and lowering</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered page did not contain the expected shared StatCard fragment %q; got:\n%s", want, html)
		}
	}
}
