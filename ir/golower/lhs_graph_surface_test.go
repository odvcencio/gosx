// Slice Y.C.5 — graph_surface.go-pattern regression tests.
//
// These tests pin the *exact* LHS shapes that graph_surface.go's
// handlers use, so any future refactor of Y.C's dispatcher catches
// regressions against the canonical hard case. The fixture in each
// test is a minimal restatement of the relevant graph_surface.go
// excerpt, scrubbed of dependencies (surface.Canvas, math intrinsics,
// user-function calls) that other Y.* slices cover.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerGraphSurfaceMatrixZoom mirrors the OnZoom handler shape
// `gTx.X = mx - factor*(mx-gTx.X)` — a self-referential RHS that reads
// the field being assigned and combines it with another expression.
// The lowering must read gTx.X once on the RHS (via OpFieldGet through
// OpIndex) and write it back via OpFieldSet; the values seen on the
// RHS must be the *original* (pre-assignment) values, not the partial
// updates from earlier assignments in the same handler.
func TestLowerGraphSurfaceMatrixZoom(t *testing.T) {
	src := []byte(`package handlers

type tx struct {
	X float64
	Y float64
	Scale float64
}

func F() float64 {
	gTx := tx{X: 100.0, Y: 50.0, Scale: 2.0}
	mx := 200.0
	my := 150.0
	factor := 1.5
	gTx.X = mx - factor*(mx-gTx.X)
	gTx.Y = my - factor*(my-gTx.Y)
	gTx.Scale = gTx.Scale * factor
	return gTx.X + gTx.Y + gTx.Scale
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// gTx.X = 200 - 1.5 * (200 - 100) = 200 - 150 = 50.
	// gTx.Y = 150 - 1.5 * (150 - 50) = 150 - 150 = 0.
	// gTx.Scale = 2 * 1.5 = 3.
	// sum = 50 + 0 + 3 = 53.
	if got.Num != 53.0 {
		t.Errorf("F() = %f, want 53.0", got.Num)
	}
}

// TestLowerGraphSurfaceMapKeyWriteback mirrors the stepLayout
// writeback pattern: read the velocity by ID, mutate the local copy,
// write back to the shared map. This is the canonical "Go's value
// semantics + author writeback" pattern that motivated the in-place
// mutation decision documented in client/vm/lhs_set.go.
//
//   v := gVel[n.ID]
//   v.X = (v.X + delta) * damping
//   gVel[n.ID] = v
//
// The test confirms the writeback round-trips through the map: after
// stepping a single ID, `gVel["n0"].X` reflects the mutated value.
func TestLowerGraphSurfaceMapKeyWriteback(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

func F() float64 {
	gVel := map[string]vec2{"n0": vec2{1.0, 2.0}}
	id := "n0"
	v := gVel[id]
	v.X = (v.X + 3.0) * 2.0
	gVel[id] = v
	out := gVel["n0"]
	return out.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// (1 + 3) * 2 = 8.
	if got.Num != 8.0 {
		t.Errorf("F() = %f, want 8.0", got.Num)
	}
}

// TestLowerGraphSurfaceForceAccumulation mirrors the repulsion-loop
// accumulator pattern: a slice that starts at zero and gets `+=` on
// every iteration. Tests the slice-index compound-assign path.
//
//   fx := []float64{0, 0, 0}
//   for i := 0; i < 3; i++ {
//       fx[i] += float64(i) * 10
//   }
//   return fx[0] + fx[1] + fx[2]
func TestLowerGraphSurfaceForceAccumulation(t *testing.T) {
	src := []byte(`package handlers

func F() float64 {
	fx := []float64{0.0, 0.0, 0.0}
	for i := 0; i < 3; i = i + 1 {
		fx[i] += float64(i) * 10.0
	}
	return fx[0] + fx[1] + fx[2]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// 0 + 10 + 20 = 30.
	if got.Num != 30.0 {
		t.Errorf("F() = %f, want 30.0", got.Num)
	}
}

// TestLowerGraphSurfacePackageVarStructField mirrors `gTx.X = ...`
// where gTx is a package-level struct variable (a signal in our model).
// The struct is constructed at package init via a composite literal
// (Y.A), and the handler mutates one field at a time.
//
// Because package-level vars become *signals* (not locals), this also
// exercises the OpFieldSet path through signal resolution — the
// target sub-expression is OpSignalGet → ObjectVal whose Fields map
// the OpFieldSet mutates in place. Critically, the test confirms the
// mutation is visible through subsequent OpSignalGet reads: because
// Value.Fields is a map (reference type), every signal-get returns a
// Value sharing the same underlying Fields map, so the OpFieldSet
// write propagates without a signal.Set() roundtrip.
func TestLowerGraphSurfacePackageVarStructField(t *testing.T) {
	src := []byte(`package handlers

type tx struct {
	X float64
	Y float64
	Scale float64
}

var gTx = tx{X: 0.0, Y: 0.0, Scale: 1.0}

func F() float64 {
	gTx.X = 10.0
	gTx.Y = 20.0
	gTx.Scale = 2.5
	return gTx.X + gTx.Y + gTx.Scale
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	// Package vars become program.SignalDefs. Tests that don't go
	// through the full island bootstrap (which calls initSignals)
	// must initialize signals manually so OpLocalGet / OpFieldSet
	// can resolve them.
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 10 + 20 + 2.5 = 32.5.
	if got.Num != 32.5 {
		t.Errorf("F() = %f, want 32.5", got.Num)
	}
}

// TestLowerGraphSurfaceMapOfStructInsert mirrors the OnPointer-down
// pattern `gPos[id] = vec2{wx, wy}` — inserting a fresh struct into
// an existing map under a new key. Confirms OpIndexSet creates the
// key on demand AND that the inserted Value is a fresh ObjectVal
// from the inline composite literal.
func TestLowerGraphSurfaceMapOfStructInsert(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

func F() float64 {
	gPos := map[string]vec2{"existing": vec2{1.0, 2.0}}
	gPos["new"] = vec2{42.0, 7.5}
	a := gPos["new"]
	b := gPos["existing"]
	return a.X + b.X
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	// 42 + 1 = 43.
	if got.Num != 43.0 {
		t.Errorf("F() = %f, want 43.0", got.Num)
	}
}

// TestLowerGraphSurfaceBoolAndIntFieldSet mirrors `gDrag.HasMoved = true`
// and `gDrag.StartX = e.X` from graph_surface.go's OnDown handler.
// Mixed-type struct field sets surface a subtle bug if the OpFieldSet
// evaluator coerces or stringifies the value before storing it (it
// shouldn't — the value should land as-stored, preserving its kind).
func TestLowerGraphSurfaceBoolAndIntFieldSet(t *testing.T) {
	src := []byte(`package handlers

type drag struct {
	Active   bool
	HasMoved bool
	StartX   int
	StartY   int
}

func F() int {
	d := drag{Active: false, HasMoved: false, StartX: 0, StartY: 0}
	d.Active = true
	d.HasMoved = true
	d.StartX = 17
	d.StartY = 25
	if d.Active && d.HasMoved {
		return d.StartX + d.StartY
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
	if int(got.Num) != 42 {
		t.Errorf("F() = %d, want 42", int(got.Num))
	}
}

// TestLowerGraphSurfaceMapKeyByExprNotLiteral mirrors `fx[a.ID] = 0`
// where the key is a non-literal expression (a field access). Confirms
// the lowerer doesn't shortcut on literal-only keys; the OpIndexSet
// key operand can be any lowered expression.
func TestLowerGraphSurfaceMapKeyByExprNotLiteral(t *testing.T) {
	src := []byte(`package handlers

type node struct {
	ID string
}

func F() float64 {
	n := node{ID: "alpha"}
	fx := map[string]float64{"alpha": 5.0, "beta": 10.0}
	fx[n.ID] = 99.0
	return fx["alpha"]
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
