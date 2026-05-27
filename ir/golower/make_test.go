// Slice Y.E.1.1 — failing-first tests for the `make(...)` builtin.
//
// Pre-Y.E the lowerer treats `make` as a bare-identifier function call
// and falls through to the user-fn registry probe — which fails with
// "calls to user-defined function \"make\" are not supported" because
// no FuncDef ever registers under that name. graph_surface.go's
// stepLayout uses two `make(map[string]float64, len(gNodes))` calls
// (lines 231-232), which is the dominant Y.E.1 motivating shape.
//
// The patterns pinned here cover the three forms graph_surface.go and
// the broader engine-surface authoring contract actually use:
//
//   1. make(map[K]V)            — zero-arg empty-map allocation
//   2. make(map[K]V, hint)      — capacity-hinted map (hint ignored at runtime)
//   3. make([]T, 0)             — empty slice allocation
//   4. make([]T, n)             — pre-sized slice (n zero-values)
//   5. make([]T, 0, cap)        — explicit cap argument (cap ignored)
//
// At Y.E.1.1 each lowering call still fails. Y.E.1.2 adds the OpMake
// opcode, Y.E.1.3 the VM evaluator, Y.E.1.4 the lowerCallExpr dispatch
// (`case "make":` BEFORE the user-fn registry probe per Y.D's handoff),
// Y.E.1.5 marks these tests PASS.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerMakeEmptyMap verifies `make(map[string]float64)` allocates
// an empty map that can be read + written through the normal Y.B / Y.C
// dispatchers.
func TestLowerMakeEmptyMap(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := make(map[string]float64)
	m["k"] = 3.5
	return m["k"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 3.5 {
		t.Errorf("F() = %f, want 3.5", got.Num)
	}
}

// TestLowerMakeMapWithHint verifies that the capacity hint is accepted
// (and ignored — Go's runtime ignores it for map iteration semantics)
// rather than tripping an arity error. Matches the graph_surface.go
// stepLayout shape: `make(map[string]float64, len(gNodes))`.
func TestLowerMakeMapWithHint(t *testing.T) {
	src := []byte(`package handlers

func F(n int) int {
	m := make(map[string]float64, n)
	m["a"] = 1
	m["b"] = 2
	return len(m)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"n": vm.IntVal(8),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 2 {
		t.Errorf("F(8) = %d, want 2", int(got.Num))
	}
}

// TestLowerMakeEmptySlice verifies `make([]T, 0)` allocates an empty
// slice that participates in `append` and `len`.
func TestLowerMakeEmptySlice(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	s := make([]int, 0)
	s = append(s, 7)
	s = append(s, 8)
	return len(s)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 2 {
		t.Errorf("F() = %d, want 2", int(got.Num))
	}
}

// TestLowerMakeSizedSlice verifies `make([]T, n)` allocates a slice of
// length n pre-filled with the type's zero value. Reading any in-range
// index returns the zero Value; writing through OpIndexSet mutates it.
func TestLowerMakeSizedSlice(t *testing.T) {
	src := []byte(`package handlers

func F(n int) int {
	s := make([]int, n)
	s[0] = 11
	s[2] = 22
	return s[0] + s[2] + len(s)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"n": vm.IntVal(3),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 36 {
		t.Errorf("F(3) = %d, want 36 (11 + 22 + 3)", int(got.Num))
	}
}

// TestLowerMakeSliceWithCap verifies the three-arg form `make([]T, n, cap)`
// drops the cap argument cleanly (the VM has no capacity concept).
func TestLowerMakeSliceWithCap(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	s := make([]int, 0, 100)
	s = append(s, 1)
	s = append(s, 2)
	s = append(s, 3)
	return len(s)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 3 {
		t.Errorf("F() = %d, want 3", int(got.Num))
	}
}
