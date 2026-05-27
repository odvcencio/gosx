// Slice Y.A.1 — failing-first tests for composite literal lowering.
//
// These tests pin the five representative composite-literal patterns the
// Y.A plan calls out as the simplest gaps blocking graph_surface.go:
//
//   1. positional struct literal:  vec2{X, Y}
//   2. named struct literal:       Node{ID: "n1", Pos: p}
//   3. empty struct literal:       vec2{}
//   4. slice literal:              []Node{a, b}
//   5. map literal:                map[string]float64{"x": 1.5}
//
// At Y.A.1 each lowering call still fails: the lowerer reports
// "unsupported expression *ast.CompositeLit" because expr.go has no
// CompositeLit case yet. Y.A.2-Y.A.4 add the OpComposite opcode, its
// VM evaluator, and the lowering rules; Y.A.5 marks these tests PASS.
package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerPositionalStructLiteral verifies `vec2{x, y}` lowers to a
// composite value whose Fields["X"] / Fields["Y"] hold the operand
// values in declaration order.
func TestLowerPositionalStructLiteral(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct{ X, Y float64 }

func F(x float64, y float64) float64 {
	v := vec2{x, y}
	return v.X + v.Y
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

// TestLowerNamedStructLiteral verifies `Node{ID: id, Pos: pos}` lowers
// such that the resulting Value.Fields map is keyed by the explicit
// field names.
func TestLowerNamedStructLiteral(t *testing.T) {
	src := []byte(`package handlers

type Node struct {
	ID  string
	Pos float64
}

func F(id string, pos float64) string {
	n := Node{ID: id, Pos: pos}
	return n.ID
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"id":  vm.StringVal("n42"),
		"pos": vm.FloatVal(2.5),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "n42" {
		t.Errorf("F() = %q, want %q", got.Str, "n42")
	}
}

// TestLowerEmptyStructLiteral verifies `vec2{}` produces a Value with
// the field slots set to their zero values (so `v.X` reads as 0).
func TestLowerEmptyStructLiteral(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct{ X, Y float64 }

func F() float64 {
	v := vec2{}
	return v.X + v.Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 0 {
		t.Errorf("F() = %f, want 0", got.Num)
	}
}

// TestLowerSliceLiteral verifies `[]Node{a, b}` materializes an array
// Value whose items are the operand values, observable through len().
func TestLowerSliceLiteral(t *testing.T) {
	src := []byte(`package handlers

type Node struct{ ID string }

func F() int {
	xs := []Node{Node{ID: "a"}, Node{ID: "b"}, Node{ID: "c"}}
	return len(xs)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 3 {
		t.Errorf("len([]Node{a,b,c}) = %d, want 3", int(got.Num))
	}
}

// TestLowerMapLiteral verifies `map[string]float64{"x": 1.5}` lowers
// to a Value whose Fields map carries the keys, observable through
// the indexing operator.
func TestLowerMapLiteral(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	m := map[string]float64{"x": 1.5, "y": 2.5}
	return m["x"] + m["y"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 4.0 {
		t.Errorf("F() = %f, want 4.0", got.Num)
	}
}

// TestLowerNestedStructInSlice covers the graph_surface.go-style
// pattern of building a `[]Node` whose elements are named struct
// literals — exercises both slice and (nested) struct composites in
// the same expression.
func TestLowerNestedStructInSlice(t *testing.T) {
	src := []byte(`package handlers

type Node struct {
	ID  string
	Pos float64
}

func F() string {
	xs := []Node{
		Node{ID: "first", Pos: 1.0},
		Node{ID: "second", Pos: 2.0},
	}
	return xs[1].ID
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "second" {
		t.Errorf("F() = %q, want %q", got.Str, "second")
	}
}

// TestLowerMapOfStructValues covers `map[string]vec2{ "origin": vec2{0,0} }`
// — the pattern hyphae graph_surface.go uses for gPos / gVel.
func TestLowerMapOfStructValues(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct{ X, Y float64 }

func F() float64 {
	m := map[string]vec2{
		"a": vec2{1.0, 2.0},
		"b": vec2{3.0, 4.0},
	}
	return m["b"].Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 4.0 {
		t.Errorf("F() = %f, want 4.0", got.Num)
	}
}

// TestLowerEmptyMapLiteral covers `map[string]vec2{}` — the
// initialization pattern hyphae graph_surface.go uses for package-level
// `gPos = map[string]vec2{}`.
func TestLowerEmptyMapLiteral(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct{ X, Y float64 }

func F() int {
	m := map[string]vec2{}
	return len(m)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 0 {
		t.Errorf("F() = %d, want 0", int(got.Num))
	}
}

// TestLowerPackageVarMapLiteral verifies that a package-level
// `var x = map[string]vec2{}` declaration produces a SignalDef whose
// Init evaluates to an empty map Value at runtime. This is the
// hyphae graph_surface.go pattern (`var gPos = map[string]vec2{}`).
func TestLowerPackageVarMapLiteral(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct{ X, Y float64 }

var positions = map[string]vec2{}

func Count() int { return len(positions) }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	if len(prog.Signals) != 1 || prog.Signals[0].Name != "positions" {
		t.Fatalf("Signals = %+v, want one entry named positions", prog.Signals)
	}
	machine := vm.NewVM(prog, nil)
	// Initialize the signal from its Init expr — mirrors how the
	// runtime sets up signals before invoking handlers.
	initVal := machine.Eval(prog.Signals[0].Init)
	if initVal.Fields == nil {
		t.Errorf("positions init = %+v, want empty Fields map", initVal)
	}
}
