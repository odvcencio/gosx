// Slice Y.D composite-parameter regression tests — pin Y.C's
// retrospective handoff guarantee.
//
// Y.C's retrospective documented: "OpFieldSet on a parameter-typed
// receiver already propagates because Value.Fields is reference-
// typed." Y.D inherits this verbatim — composite arguments are passed
// by Value-by-value copy but their Fields/Items storage is shared, so
// the callee's mutations land in the caller's storage.
//
// The tests below pin this behavior so a future Y.E refactor (or any
// VM change to Value semantics) doesn't silently regress it.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_StructParamMutationPropagates verifies that a callee
// mutating a struct param's field via OpFieldSet reflects back into
// the caller's local — Y.C's retrospective Decision 2.
func TestY_D_StructParamMutationPropagates(t *testing.T) {
	src := []byte(`package handlers

type point struct {
	X float64
	Y float64
}

func bump(p point) {
	p.X = p.X + 100.0
}

func F() float64 {
	pt := point{X: 1.0, Y: 2.0}
	bump(pt)
	return pt.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 101.0 {
		t.Errorf("F() = %f, want 101.0 (struct param mutation must propagate per Y.C retrospective)", got.Num)
	}
}

// TestY_D_MapParamMutationPropagates verifies the same guarantee for
// map-typed parameters. graph_surface.go's `updateNode(node, gPos)`
// shape relies on this for the package-level position map.
func TestY_D_MapParamMutationPropagates(t *testing.T) {
	src := []byte(`package handlers

func setKey(m map[string]float64, k string, v float64) {
	m[k] = v
}

func F() float64 {
	m := map[string]float64{"a": 1.0}
	setKey(m, "b", 42.0)
	return m["a"] + m["b"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 43.0 {
		t.Errorf("F() = %f, want 43.0 (map param mutation must propagate per Y.C retrospective)", got.Num)
	}
}

// TestY_D_SliceParamElementMutationPropagates verifies slice element
// writes (OpIndexSet) made by a callee land in the caller's slice.
// graph_surface.go's `zeroForces(fx)` pattern depends on this.
func TestY_D_SliceParamElementMutationPropagates(t *testing.T) {
	src := []byte(`package handlers

func zeroOut(s []float64) {
	for i := 0; i < len(s); i = i + 1 {
		s[i] = 0.0
	}
}

func F() float64 {
	s := []float64{1.0, 2.0, 3.0}
	zeroOut(s)
	return s[0] + s[1] + s[2]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 0.0 {
		t.Errorf("F() = %f, want 0.0 (slice element mutation through param must propagate)", got.Num)
	}
}

// TestY_D_ScalarParamIsByValue verifies that scalar (non-composite)
// parameter mutations do NOT propagate — matching Go's normal scalar
// pass-by-value semantics. This is the symmetric guarantee that
// catches a regression if someone "fixes" the composite case by
// passing every Value by reference indiscriminately.
func TestY_D_ScalarParamIsByValue(t *testing.T) {
	src := []byte(`package handlers

func tryBump(n int) {
	n = n + 100
}

func F() int {
	x := 7
	tryBump(x)
	return x
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 7 {
		t.Errorf("F() = %d, want 7 (scalar param must NOT mutate caller's value)", int(got.Num))
	}
}
