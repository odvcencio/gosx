// Slice Y.E.4 / Slice Y.G — graph_surface.go end-to-end lowering check.
//
// Lowers the actual graph_surface.go from `~/work/hyphae/cmd/hypha-viz`
// and verifies the lowerer produces a clean Program.
//
// Pre-Y.E:   40 issues
// Post-Y.E:  1 issue (the *ast.FuncLit closure in Mount's StartLoop)
// Post-Y.G:  0 issues (Y.G's FuncLit closure lowering closes the
//            last residual; the entire Mount handler — props decode,
//            initPositions seed, StartLoop with closure body — lowers
//            cleanly).
//
// The test runs only when GOSX_TEST_GRAPH_SURFACE_PATH is set or the
// default hyphae checkout is present. CI runners without the hyphae
// repo skip silently — the synthetic fixture above (TestGraphSurface*)
// is the always-on coverage.

package golower

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestY_E_GraphSurfaceEndToEnd lowers the real graph_surface.go and
// pins the post-Y.G issue count. The test is the canonical proof that
// Y.E's surface coverage + Y.G's FuncLit lowering combine to give
// graph_surface.go a fully-supported lowering path.
func TestY_E_GraphSurfaceEndToEnd(t *testing.T) {
	path := graphSurfacePath()
	if path == "" {
		t.Skip("graph_surface.go not present; set GOSX_TEST_GRAPH_SURFACE_PATH to enable")
	}
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
	path := graphSurfacePath()
	if path == "" {
		t.Skip("graph_surface.go not present")
	}
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

// graphSurfacePath returns the source path of the hyphae graph
// surface, or "" when not present.
func graphSurfacePath() string {
	if p := os.Getenv("GOSX_TEST_GRAPH_SURFACE_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "work", "hyphae", "cmd", "hypha-viz", "graphsurface", "graph_surface.go"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
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
