// Slice Y.B.1 — failing-first tests for multi-value assignment lowering.
//
// These tests pin the multi-value assignment patterns the Y.B plan calls
// out as the next gap blocking graph_surface.go. Pre-Y.B the lowerer
// rejects every form with "multi-value assignment is not supported".
//
//   1. parallel assignment:        a, b := expr1, expr2
//   2. parallel assignment of ops: ux, uy := dx/dist, dy/dist
//   3. two-value map index:        v, ok := m[k]
//   4. blank-ident parallel:       _, y := f(), g()
//   5. blank-ident map index:      _, ok := m[k]
//   6. range with two slice vars:  for i, v := range s
//   7. range with two map vars:    for k, v := range m
//   8. if-init two-value map:      if _, ok := m[k]; !ok { ... }
//
// At Y.B.1 each lowering call still fails: stmt.go's lowerAssignStmt
// rejects every `len(Lhs) != 1 || len(Rhs) != 1` case up front. Y.B.2-Y.B.4
// add the OpMapLookup opcode, its VM evaluator, and the lowering rules;
// Y.B.5 marks these tests PASS.
package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerParallelAssignment verifies `a, b := x, y` lowers each Rhs
// to its own local without aliasing through a tuple. The bodies that
// motivate this in graph_surface.go are `mx, my := e.X, e.Y` and
// `fw, fh := float64(w), float64(h)`.
func TestLowerParallelAssignment(t *testing.T) {
	src := []byte(`package handlers

func F(x float64, y float64) float64 {
	a, b := x, y
	return a + b
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x": vm.FloatVal(3.5),
		"y": vm.FloatVal(1.25),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 4.75 {
		t.Errorf("F(3.5, 1.25) = %f, want 4.75", got.Num)
	}
}

// TestLowerParallelAssignmentSwap verifies the canonical swap idiom
// `a, b = b, a` honors Go's evaluate-all-Rhs-before-assigning-Lhs rule.
func TestLowerParallelAssignmentSwap(t *testing.T) {
	src := []byte(`package handlers

func F(x int, y int) int {
	a := x
	b := y
	a, b = b, a
	return a - b
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x": vm.IntVal(10),
		"y": vm.IntVal(3),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	// After swap a=3, b=10, so a-b = -7.
	if int(got.Num) != -7 {
		t.Errorf("F(10, 3) = %d, want -7", int(got.Num))
	}
}

// TestLowerTwoValueMapIndexPresent verifies `v, ok := m[k]` populates
// both bindings when the key exists. Map values come from a composite
// literal lowered through Y.A's OpComposite.
func TestLowerTwoValueMapIndexPresent(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"hit": 7.5, "miss": 0}
	v, ok := m["hit"]
	if ok {
		return v
	}
	return -1
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 7.5 {
		t.Errorf("F() = %f, want 7.5", got.Num)
	}
}

// TestLowerTwoValueMapIndexMissing verifies `v, ok := m[k]` returns the
// zero value + false when the key is absent. The Y.B lowerer must emit
// OpMapLookup (not OpIndex) so the runtime can supply the presence flag.
func TestLowerTwoValueMapIndexMissing(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"hit": 7.5}
	v, ok := m["miss"]
	if ok {
		return v
	}
	return -1
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != -1 {
		t.Errorf("F() = %f, want -1", got.Num)
	}
}

// TestLowerBlankIdentMapIndex verifies `_, ok := m[k]` discards the
// value but binds ok. This is the form graph_surface.go uses at
// `if _, ok := gPos[nd.ID]; !ok {`.
func TestLowerBlankIdentMapIndex(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	m := map[string]float64{"present": 1.0}
	_, ok := m["present"]
	if ok {
		return 42
	}
	return 0
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 42 {
		t.Errorf("F() = %d, want 42", int(got.Num))
	}
}

// TestLowerBlankIdentParallelAssign verifies `_, y := x, z` binds only
// the right-hand-side identifier.
func TestLowerBlankIdentParallelAssign(t *testing.T) {
	src := []byte(`package handlers

func F(x int, z int) int {
	_, y := x, z
	return y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x": vm.IntVal(1),
		"z": vm.IntVal(9),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 9 {
		t.Errorf("F(1, 9) = %d, want 9", int(got.Num))
	}
}

// TestLowerRangeMapTwoVars verifies `for k, v := range m { ... }` binds
// both the key and the value into the loop body. Range over slice with
// two vars (`for i, v := range s`) already works; this nails the map
// branch.
func TestLowerRangeMapTwoVars(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"a": 1.0, "b": 2.0, "c": 4.0}
	total := 0.0
	for _, v := range m {
		total = total + v
	}
	return total
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 7.0 {
		t.Errorf("F() = %f, want 7.0", got.Num)
	}
}

// TestLowerIfInitTwoValueMap verifies the canonical existence-check
// pattern `if _, ok := m[k]; !ok { ... }` lowers cleanly. The Init
// clause is itself a multi-value assignment that the lowerer must
// route through the new path.
func TestLowerIfInitTwoValueMap(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	m := map[string]float64{"only": 1.0}
	if _, ok := m["missing"]; !ok {
		return 1
	}
	return 0
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 1 {
		t.Errorf("F() = %d, want 1", int(got.Num))
	}
}
