// Slice Y.E.4 — graph_surface.go end-to-end lowering check.
//
// Lowers the actual graph_surface.go from `~/work/hyphae/cmd/hypha-viz`
// and verifies the issue count drops to the Y.E exit target.
//
// Pre-Y.E:   40 issues
// Post-Y.E:  1 issue (the *ast.FuncLit closure in Mount's StartLoop —
//            deferred to Y.G or a future "expression-language breadth"
//            slice; not in Y.E's plan-defined scope).
//
// The test runs only when GOSX_TEST_GRAPH_SURFACE_PATH is set or the
// default hyphae checkout is present. CI runners without the hyphae
// repo skip silently — the synthetic fixture above (TestGraphSurface*)
// is the always-on coverage.

package golower

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestY_E_GraphSurfaceEndToEnd lowers the real graph_surface.go and
// pins the post-Y.E issue count. The test is the canonical proof that
// Y.E's surface coverage matches the plan's exit criteria.
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
	// Issue count target: 1 (the FuncLit closure in StartLoop).
	const wantIssues = 1
	if lerr == nil {
		if wantIssues == 0 {
			return
		}
		t.Fatalf("LowerFile: expected %d residual issue(s), got 0", wantIssues)
	}
	got := lowerErrorIssueCount(lerr)
	if got != wantIssues {
		t.Errorf("LowerFile: post-Y.E issue count = %d, want %d. Errors:\n%s", got, wantIssues, lerr.Error())
	}
	// The one residual must be the FuncLit case — fail loudly if any
	// other diagnostic creeps back so a regression in Y.E's coverage
	// is visible immediately.
	if !strings.Contains(lerr.Error(), "FuncLit") {
		t.Errorf("post-Y.E residual should be the FuncLit (closure) gap; got:\n%s", lerr.Error())
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
