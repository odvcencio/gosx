// Slice Y.E.4 / Slice Y.G — graph_surface.go end-to-end lowering check.
//
// Lowers a real 450-line GoSX surface program and verifies the lowerer produces
// a clean Program.
//
// Pre-Y.E:   40 issues
// Post-Y.E:  1 issue (the *ast.FuncLit closure in Mount's StartLoop)
// Post-Y.G:  0 issues (Y.G's FuncLit closure lowering closes the
//            last residual; the entire Mount handler — props decode,
//            initPositions seed, StartLoop with closure body — lowers
//            cleanly).
//
// THE FIXTURE IS VENDORED. Both tests below used to read
// $HOME/work/hyphae/cmd/hypha-viz/graphsurface/graph_surface.go and skip when it
// was absent. That path lives in a DIFFERENT repository, so both tests were
// skipped unconditionally in continuous integration and on every machine without
// a hyphae checkout. A test that depends on a sibling checkout is not a test.
//
// testdata/graph_surface.go is a copy taken on 2026-07-26, so the tests now run
// everywhere. Set GOSX_TEST_GRAPH_SURFACE_PATH to lower a different file instead,
// which is how to check the vendored copy against a live checkout.

package golower

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// vendoredGraphSurface is the pinned fixture. It is a .go file under testdata, so
// the go tool never compiles it as part of this package.
const vendoredGraphSurface = "testdata/graph_surface.go"

// TestY_E_GraphSurfaceEndToEnd lowers the real graph_surface.go and
// pins the post-Y.G issue count. The test is the canonical proof that
// Y.E's surface coverage + Y.G's FuncLit lowering combine to give
// graph_surface.go a fully-supported lowering path.
func TestY_E_GraphSurfaceEndToEnd(t *testing.T) {
	path := graphSurfacePath(t)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	prog, lerr := LowerFile(src)
	if prog == nil {
		t.Fatal("LowerFile returned a nil Program — the lowerer should never drop the whole file")
	}
	// Issue count target: 0 (Y.G's FuncLit lowering closes the residual).
	if lerr != nil {
		t.Fatalf("LowerFile: expected 0 residual issues, got %d:\n%s",
			lowerErrorIssueCount(lerr), lerr.Error())
	}
}

// TestY_E_GraphSurfaceHandlersAreEmitted verifies the Program from
// lowering graph_surface.go carries every handler the file declares,
// even though one handler (Mount) has a deferred FuncLit residual.
// This pins the lowerer's "keep going on diagnostics" contract.
func TestY_E_GraphSurfaceHandlersAreEmitted(t *testing.T) {
	path := graphSurfacePath(t)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	prog, _ := LowerFile(src) // ignore residual errors
	if prog == nil {
		t.Fatal("nil Program")
	}
	expected := []string{
		"Mount", "OnDown", "OnMove", "OnUp", "OnZoom", "OnDouble",
		"OnResize", "stepLayout", "typeColor", "isGraftKind",
		"initPositions", "draw", "screenToWorld", "nodeAt",
	}
	have := make(map[string]bool, len(prog.Handlers))
	for _, h := range prog.Handlers {
		have[h.Name] = true
	}
	for _, name := range expected {
		if !have[name] {
			t.Errorf("Handler %s missing from lowered Program; got: %v", name, handlerNames(prog.Handlers))
		}
	}
}

// graphSurfacePath returns the file to lower. It never returns "": a missing
// fixture is a broken checkout, so it fails rather than skips.
//
// GOSX_TEST_GRAPH_SURFACE_PATH overrides the vendored copy. An override that
// names a file that is not there is a mistake worth reporting, so it fails too
// instead of falling back and reporting a pass for the wrong file.
func graphSurfacePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GOSX_TEST_GRAPH_SURFACE_PATH"); p != "" {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("GOSX_TEST_GRAPH_SURFACE_PATH=%s cannot be read: %v", p, err)
		}
		t.Logf("lowering the override at %s instead of %s", p, vendoredGraphSurface)
		return p
	}
	path := filepath.FromSlash(vendoredGraphSurface)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the vendored fixture %s is missing: %v\n"+
			"It is a pinned copy of a real GoSX surface program and it must ship with the "+
			"repository, or these two end-to-end lowering tests cover nothing.", path, err)
	}
	return path
}

func lowerErrorIssueCount(err error) int {
	if err == nil {
		return 0
	}
	le, ok := err.(*LowerError)
	if !ok {
		return 1 // unstructured error; count as one residual for the assertion
	}
	return len(le.Issues)
}

func handlerNames(handlers []program.Handler) []string {
	out := make([]string, len(handlers))
	for i, h := range handlers {
		out[i] = h.Name
	}
	return out
}
