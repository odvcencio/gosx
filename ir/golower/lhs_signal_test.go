// Slice Y.C.5 — package-level signal mutation tests.
//
// graph_surface.go's `fx []float64`, `fy []float64`, `gPos map[string]vec2`,
// `gVel map[string]vec2`, `gTx tx`, and `gDrag drag` are all package-level
// vars that the handlers mutate via LHS selector / indexed-set forms.
// These tests pin that OpFieldSet / OpIndexSet on a *signal-backed*
// collection mutates the underlying storage so subsequent OpSignalGet
// reads see the new value.
//
// **Critical implementation note.** The mutation works because
// Value.Fields and Value.Items are reference types (map / slice). When
// OpSignalGet returns Value-by-value, the inner Fields map and Items
// slice still alias the same storage held by the signal. The mutation
// does NOT call signal.Set(), so signal subscribers are not notified
// — but no engine-surface handler subscribes to its own state today,
// and the canvas re-draws on every frame anyway, so the visible
// behavior matches the author's expectations.
//
// If a future surface needs reactivity over Y.C mutations, the lowerer
// can wrap OpFieldSet / OpIndexSet on signal targets in a follow-up
// OpSignalSet that re-stores the (already-mutated) collection. This
// is documented in the Y.C retrospective as a Y.D / Y.E follow-up.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerPackageVarSliceIndexSet exercises slice-index-set against a
// package-level signal. Mirrors `fx[i] = 0` from stepLayout where fx
// is a package var (becomes a signal in our model).
func TestLowerPackageVarSliceIndexSet(t *testing.T) {
	src := []byte(`package handlers

var fx = []float64{1.0, 2.0, 3.0, 4.0}

func F() float64 {
	fx[1] = 99.0
	fx[3] += 5.0
	return fx[1] + fx[3]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 99 + (4 + 5) = 108.
	if got.Num != 108.0 {
		t.Errorf("F() = %f, want 108.0", got.Num)
	}
}

// TestLowerPackageVarMapKeySet exercises map-key-set against a
// package-level signal. Mirrors `gPos[id] = vec2{...}` from
// OnPointer-down.
func TestLowerPackageVarMapKeySet(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

var gPos = map[string]vec2{"existing": vec2{1.0, 2.0}}

func F() float64 {
	gPos["new"] = vec2{42.0, 7.5}
	gPos["existing"] = vec2{100.0, 200.0}
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
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 42 + 100 = 142.
	if got.Num != 142.0 {
		t.Errorf("F() = %f, want 142.0", got.Num)
	}
}

// TestLowerPackageVarStructFieldRoundTrip pins that two separate
// handler invocations both see the mutated package-level struct. This
// is the lifecycle that matters for engine-surface handlers: OnFrame
// fires every frame, accumulating mutations to gTx across many calls.
func TestLowerPackageVarStructFieldRoundTrip(t *testing.T) {
	src := []byte(`package handlers

type tx struct {
	X float64
	Scale float64
}

var gTx = tx{X: 0.0, Scale: 1.0}

func Step() {
	gTx.X = gTx.X + 1.0
	gTx.Scale = gTx.Scale * 2.0
}

func Read() float64 {
	return gTx.X + gTx.Scale
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	stepHandler := findHandler(t, prog.Handlers, "Step")
	readHandler := findHandler(t, prog.Handlers, "Read")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)

	// Three frames.
	for i := 0; i < 3; i++ {
		machine.EvalWithFrame(stepHandler.Body[0])
	}
	got := machine.EvalWithFrame(readHandler.Body[0])
	// X = 0 + 1 + 1 + 1 = 3; Scale = 1 * 2 * 2 * 2 = 8; sum = 11.
	if got.Num != 11.0 {
		t.Errorf("Read() after 3 steps = %f, want 11.0", got.Num)
	}
}

// TestLowerPackageVarMapWriteThenReadVisible pins a subtle contract:
// after `m["new"] = v`, reading `m["new"]` in the same handler must
// return v. This sounds obvious but matters because the OpIndexSet
// path goes through the same Fields map the signal owns, and any
// stale Value snapshot (e.g., if OpIndex were memoized) would break
// this. Mirrors the OnPointer-down sequence:
//
//	gPos[id] = vec2{wx, wy}
//	p, ok := gPos[id]   // ok must be true; p must match the inserted value
func TestLowerPackageVarMapWriteThenReadVisible(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

var gPos = map[string]vec2{}

func F() float64 {
	gPos["fresh"] = vec2{X: 7.0, Y: 8.0}
	p, ok := gPos["fresh"]
	if !ok {
		return -1.0
	}
	return p.X + p.Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 15.0 {
		t.Errorf("F() = %f, want 15.0 (7 + 8)", got.Num)
	}
}

// TestLowerPackageVarSliceInForLoop exercises a tight for-loop that
// repeatedly mutates a package-level slice signal — the canonical
// stepLayout repulsion-accumulator shape.
func TestLowerPackageVarSliceInForLoop(t *testing.T) {
	src := []byte(`package handlers

var acc = []float64{0.0, 0.0, 0.0, 0.0}

func F() float64 {
	for i := 0; i < 4; i = i + 1 {
		acc[i] += float64(i) * 2.0
	}
	return acc[0] + acc[1] + acc[2] + acc[3]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 0 + 2 + 4 + 6 = 12.
	if got.Num != 12.0 {
		t.Errorf("F() = %f, want 12.0", got.Num)
	}
}
