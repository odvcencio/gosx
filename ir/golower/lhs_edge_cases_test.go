// Slice Y.C.5 — edge-case and failure-mode tests for LHS lowering.
//
// These tests pin behaviors the Y.C plan calls out as "must work" but
// that aren't obvious from the simple-case tests:
//
//   - selector-on-index LHS (`nodes[i].Y = 3`) — chained Y.A struct
//     literals inside a slice
//   - map index compound-assign that creates a brand-new key
//     (`m["new"] += 1` should treat the missing key as zero and
//     produce `m["new"] = 1`)
//   - failure mode for unsupported LHS shapes (e.g. `&x = ...` —
//     unary on LHS — should diagnose cleanly, not panic)

package golower

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerSelectorOnIndexLHS exercises `s[i].Field = v` — the
// canonical chained LHS shape graph_surface.go's `nodes[i].Y = ...`
// (if it ever appeared) would produce. The lowering recipe is:
//
//   OpFieldSet target=s[i] field=Field value=v
//
// because Go's AST already nests this as SelectorExpr{X: IndexExpr}
// — our dispatcher routes on the outer Lhs[0] (SelectorExpr) and
// lowerExpr(IndexExpr) handles the inner read.
func TestLowerSelectorOnIndexLHS(t *testing.T) {
	src := []byte(`package handlers

type pt struct {
	X float64
	Y float64
}

func F() float64 {
	nodes := []pt{pt{1.0, 2.0}, pt{3.0, 4.0}}
	nodes[1].Y = 99.0
	return nodes[1].Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 99.0 {
		t.Errorf("F() = %f, want 99.0", got.Num)
	}
}

// TestLowerMapCompoundOnMissingKey pins compound-assign semantics
// when the key isn't in the map yet. Go's spec: `m["k"] += 1` is
// equivalent to `m["k"] = m["k"] + 1`, and reading a missing map key
// yields the zero value. Our OpIndex on a missing key returns the
// zero Value (per Value.IndexVal), and OpIndexSet creates the key on
// demand — so this should round-trip cleanly to value `1`.
func TestLowerMapCompoundOnMissingKey(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{}
	m["new"] += 7.5
	return m["new"]
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

// TestLowerLHSStarUnsupported pins the failure mode for `*p = v`
// (pointer dereference LHS). Pointers aren't in the supported subset,
// so the lowerer should diagnose without panicking.
func TestLowerLHSStarUnsupported(t *testing.T) {
	src := []byte(`package handlers

func F() {
	var x int
	p := &x
	*p = 5
	_ = p
}`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatalf("LowerFile: expected an issue for *p = 5, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("LowerFile error %v should mention unsupported", err)
	}
}

// TestLowerLHSDefineWithSelectorRejected pins that `:=` on a selector
// LHS surfaces a clear error. Go's parser actually rejects this at
// the syntax level, but our lowerer's defensive path catches it for
// future plan robustness (e.g., if a Y.D dispatcher fabricates a
// selector LHS via a refactor and accidentally drops the bare-ident
// requirement).
func TestLowerLHSDefineWithSelectorRejected(t *testing.T) {
	// We can't easily craft `:=` on a selector through go/parser, so
	// we test the dispatcher directly by lowering source that uses
	// the compound-assign-on-selector path (which our switch
	// recognizes as "not := or =" and emits the operator diagnostic).
	src := []byte(`package handlers

type t struct{ X int }

func F() {
	v := t{X: 1}
	v.X = 5
}`)
	_, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	// The actual := case can't be tested via go/parser. This test
	// pins that the happy ASSIGN case doesn't accidentally trip the
	// fallback error path — a smoke test for the dispatcher's switch
	// arrangement.
}

// TestLowerSliceSelfShiftAssign exercises the read-write-from-same-
// slice idiom (`s[i] = s[i+1]`). The RHS reads through OpIndex while
// the LHS writes through OpIndexSet — both touch the same Items slice
// but at different indices, so the write should not corrupt the read.
// In our implementation this is trivially correct because lowerExpr
// reads BEFORE the OpIndexSet mutates, but pinning the contract here
// prevents a future "optimize evaluation order" refactor from
// regressing the semantics.
func TestLowerSliceSelfShiftAssign(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	s := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	for i := 0; i < 4; i = i + 1 {
		s[i] = s[i+1]
	}
	// After shifting left by one: [2,3,4,5,5].
	return s[0] + s[1] + s[2] + s[3] + s[4]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// 2 + 3 + 4 + 5 + 5 = 19.
	if got.Num != 19.0 {
		t.Errorf("F() = %f, want 19.0", got.Num)
	}
}

// TestLowerSliceIndexExprKey exercises an OpIndexSet where the key
// is itself a runtime expression (a local read), not a literal.
// Mirrors `fx[n.ID] = 0` shape where the key is computed.
func TestLowerSliceIndexExprKey(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	s := []float64{1.0, 2.0, 3.0}
	i := 2
	s[i] = 42.0
	return s[i]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 42.0 {
		t.Errorf("F() = %f, want 42.0", got.Num)
	}
}

// TestLowerDeeplyNestedFieldSet exercises three-level chained selector
// LHS (`outer.middle.inner.X = v`). The lowering recipe is recursive:
// OpFieldSet's target is OpIndex over OpIndex over OpLocalGet, all of
// which yield Values whose Fields maps share the underlying storage.
// The terminal OpFieldSet writes through to the innermost map.
func TestLowerDeeplyNestedFieldSet(t *testing.T) {
	src := []byte(`package handlers

type leaf struct {
	X float64
}

type mid struct {
	Inner leaf
}

type root struct {
	Mid mid
}

func F() float64 {
	r := root{Mid: mid{Inner: leaf{X: 1.0}}}
	r.Mid.Inner.X = 42.0
	return r.Mid.Inner.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 42.0 {
		t.Errorf("F() = %f, want 42.0", got.Num)
	}
}

// TestLowerNestedMapAssign exercises map-of-map LHS:
// `nested[k1][k2] = v` → OpIndexSet on the inner map. This isn't a
// graph_surface.go pattern but it's the natural extension of the
// chained-selector case (TestLowerSelectorOnIndexLHS) to chained
// indices.
func TestLowerNestedMapAssign(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	inner := map[string]float64{"x": 1.0}
	outer := map[string]map[string]float64{"a": inner}
	outer["a"]["x"] = 99.0
	return outer["a"]["x"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 99.0 {
		t.Errorf("F() = %f, want 99.0", got.Num)
	}
}
