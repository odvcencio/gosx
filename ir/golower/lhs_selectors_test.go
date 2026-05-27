// Slice Y.C.1 — failing-first tests for LHS selector / indexed-set
// lowering.
//
// These tests pin the three assignment shapes Y.C's plan calls out as
// the next gap blocking graph_surface.go. Pre-Y.C the lowerer rejects
// every form with "left-hand side must be a simple identifier" (from
// the single-LHS path in lowerAssignStmt) because the existing identName
// helper only accepts *ast.Ident.
//
//   1. struct field set:        node.X = 5
//   2. struct field compound:   node.X += 1
//   3. nested struct field:     gTx.Translate.X = 0   (chained selector)
//   4. slice index set:         nodes[i].Y = 3        (selector-on-index)
//   5. simple slice index set:  s[i] = v
//   6. map key set:             m[k] = v
//   7. map key set (literal):   m["foo"] = 12.5
//
// At Y.C.1 each lowering call still fails or emits an issue: stmt.go's
// lowerAssignStmt's `identName(s.Lhs[0])` returns false for any
// non-Ident LHS, producing the "left-hand side must be a simple
// identifier" diagnostic. Y.C.2-Y.C.4 add OpFieldSet / OpIndexSet
// opcodes, their VM evaluators, and the lowering dispatch; Y.C.5
// marks these tests PASS.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerStructFieldSet verifies `node.X = expr` writes back into
// node's Fields map in place. The receiver is a local that holds an
// ObjectVal constructed via Y.A's OpComposite; after the assignment,
// reading node.X must see the new value.
func TestLowerStructFieldSet(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

func F() float64 {
	v := vec2{1.0, 2.0}
	v.X = 12.5
	return v.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 12.5 {
		t.Errorf("F() = %f, want 12.5", got.Num)
	}
}

// TestLowerStructFieldCompoundSet verifies `node.X += 1` reads then
// writes back, mirroring the compound-assign form used in
// graph_surface.go's stepLayout (e.g., `fx[a.ID] += ux * force` once
// indexed-set works for the slice case).
func TestLowerStructFieldCompoundSet(t *testing.T) {
	src := []byte(`package handlers

type point struct {
	X float64
	Y float64
}

func F() float64 {
	p := point{10.0, 0.0}
	p.X += 5.5
	p.X *= 2.0
	return p.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// (10 + 5.5) * 2 = 31.
	if got.Num != 31.0 {
		t.Errorf("F() = %f, want 31.0", got.Num)
	}
}

// TestLowerSliceIndexSet verifies `s[i] = expr` writes into the slice
// at the given integer index. Mirrors `fx[n.ID] = 0` from stepLayout
// (with n.ID approximated as an integer index here; the map-keyed
// form is exercised by TestLowerMapKeySet).
func TestLowerSliceIndexSet(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	s := []float64{1.0, 2.0, 3.0}
	s[1] = 9.5
	return s[1]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 9.5 {
		t.Errorf("F() = %f, want 9.5", got.Num)
	}
}

// TestLowerSliceIndexCompoundSet covers `s[i] += delta`, which Y.C must
// support because graph_surface.go uses `fx[a.ID] += ux * force` in
// the repulsion accumulation loop.
func TestLowerSliceIndexCompoundSet(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	s := []float64{1.0, 2.0, 3.0}
	s[0] += 4.5
	s[2] -= 1.5
	return s[0] + s[2]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// (1+4.5) + (3-1.5) = 5.5 + 1.5 = 7.
	if got.Num != 7.0 {
		t.Errorf("F() = %f, want 7.0", got.Num)
	}
}

// TestLowerMapKeySet verifies `m[k] = expr` writes into the map at the
// given key. Mirrors `gPos[gDrag.NodeID] = vec2{wx, wy}` from
// graph_surface.go's OnMove handler.
func TestLowerMapKeySet(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"a": 1.0, "b": 2.0}
	m["a"] = 7.5
	m["c"] = 11.0
	return m["a"] + m["c"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 18.5 {
		t.Errorf("F() = %f, want 18.5", got.Num)
	}
}

// TestLowerMapKeyCompoundSet covers `m[k] += delta`. Less common in
// graph_surface.go but the symmetry with slice-index compound matters
// — once the LHS read path exists, compound forms come for free.
func TestLowerMapKeyCompoundSet(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"sum": 10.0}
	m["sum"] += 2.5
	return m["sum"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 12.5 {
		t.Errorf("F() = %f, want 12.5", got.Num)
	}
}

// TestLowerChainedFieldSet verifies the chained-selector LHS
// `parent.child.X = ...` writes through the intermediate struct
// without making a copy. graph_surface.go uses `gTx.X = ...` at the
// top level (single-step selector); this test pins the two-step
// behavior so future surfaces (e.g., the editor's `state.tx.X = ...`)
// just work.
func TestLowerChainedFieldSet(t *testing.T) {
	src := []byte(`package handlers

type pt struct {
	X float64
	Y float64
}

type holder struct {
	Inner pt
}

func F() float64 {
	h := holder{Inner: pt{X: 1.0, Y: 2.0}}
	h.Inner.X = 99.0
	return h.Inner.X
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
