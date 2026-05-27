// Slice Y.D graph_surface end-to-end test — exercises the full
// Y.A+Y.B+Y.C+Y.D stack against a fixture modeled after
// graph_surface.go's `nodeAt` / `screenToWorld` / `updateNode`
// helpers calling each other across handlers.
//
// The fixture intentionally mirrors the kinds of cross-handler
// dispatch the real graph_surface.go uses: a multi-return helper
// for coordinate transforms, a composite-returning helper for new
// node allocation, and a recursive helper (depth-bounded) that
// approximates the iterative drag-update fan-out without needing
// the full canvas surface.

package golower

import (
	"math"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_GraphSurfaceHelpersEndToEnd lowers the full graph-surface-
// style fixture and verifies the returned value matches a Go
// reference computation. Failure here means one of the Y.D building
// blocks (registry, dispatch, multi-return, composite return) silently
// regressed in a way the smaller tests didn't catch.
func TestY_D_GraphSurfaceHelpersEndToEnd(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

type node struct {
	ID  string
	Pos vec2
}

func screenToWorld(sx float64, sy float64) (float64, float64) {
	return sx*2.0 + 10.0, sy*2.0 - 5.0
}

func makeNode(id string, x float64, y float64) node {
	return node{ID: id, Pos: vec2{X: x, Y: y}}
}

func magSq(v vec2) float64 {
	return v.X*v.X + v.Y*v.Y
}

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

func F() float64 {
	// Multi-return user function call.
	wx, wy := screenToWorld(3.0, 4.0)            // 16.0, 3.0

	// Call returning composite, with composite arg threading.
	n := makeNode("origin", wx, wy)              // node{ID:"origin", Pos:vec2{16,3}}

	// Composite arg flow + scalar return.
	m := magSq(n.Pos)                            // 256 + 9 = 265

	// Recursion.
	f := fact(5)                                 // 120

	return m + float64(f)                        // 385
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	want := refGraphSurfaceHelpers()
	if math.Abs(got.Num-want) > 1e-9 {
		t.Errorf("F() = %f, want %f", got.Num, want)
	}
}

// refGraphSurfaceHelpers is the Go reference for the fixture above.
// Keeping it explicit (rather than computing inline) makes future
// fixture tweaks easy to validate against a single source of truth.
func refGraphSurfaceHelpers() float64 {
	wx := 3.0*2.0 + 10.0 // 16.0
	wy := 4.0*2.0 - 5.0  // 3.0
	m := wx*wx + wy*wy   // 265
	f := 1
	for i := 1; i <= 5; i++ {
		f *= i
	}
	return m + float64(f) // 385
}

// TestY_D_GraphSurfaceMutationThroughCallee combines the composite-
// parameter mutation guarantee with cross-call dispatch: a helper
// that mutates a map argument is invoked from another helper which
// is itself called from the handler. The mutation must propagate
// through both call frames.
func TestY_D_GraphSurfaceMutationThroughCallee(t *testing.T) {
	src := []byte(`package handlers

func setPos(positions map[string]float64, id string, val float64) {
	positions[id] = val
}

func ensureSeeded(positions map[string]float64) {
	setPos(positions, "a", 1.0)
	setPos(positions, "b", 2.0)
}

func F() float64 {
	gPos := map[string]float64{}
	ensureSeeded(gPos)
	return gPos["a"] + gPos["b"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 3.0 {
		t.Errorf("F() = %f, want 3.0 (map mutation through nested call frames)", got.Num)
	}
}

// TestY_D_GraphSurfaceForwardReference verifies that a handler may
// call a user function declared LATER in the same file. Go allows
// this without forward declarations because the type-check pass sees
// all top-level decls before checking bodies; Y.D's registry pre-pass
// preserves the same property because scanUserFuncs runs before any
// body lowers.
func TestY_D_GraphSurfaceForwardReference(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	return helperDefinedLater(10)
}

func helperDefinedLater(n int) int {
	return n + 32
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 42 {
		t.Errorf("F() = %d, want 42 (forward reference must resolve via registry pre-pass)", int(got.Num))
	}
}
