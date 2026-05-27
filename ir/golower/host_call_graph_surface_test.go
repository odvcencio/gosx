// Slice Y.E.4 — graph_surface-shape integration tests for the VM
// side of OpHostCall. Pairs with TestY_E_GraphSurfaceEndToEnd
// (lowering smoke test) by exercising the *runtime* dispatch shape
// for the canvas patterns graph_surface.go's draw and stepLayout
// handlers produce.
//
// Tier 5 in the X/Y test pyramid: synthetic fixtures + real
// HostRecorder + VM evaluation, end-to-end. The actual hyphae file
// stays the lowering smoke target; this file pins the call sequence
// the lowered Program emits when evaluated.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_E_GraphSurfaceDrawShape lowers a stepwise distillation of
// graph_surface.go's draw handler and asserts the HostRecorder
// observes exactly the expected canvas call sequence.
func TestY_E_GraphSurfaceDrawShape(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func Draw(c *surface.Canvas, x1 float64, y1 float64, x2 float64, y2 float64, color string) {
	c.BeginPath()
	c.MoveTo(x1, y1)
	c.LineTo(x2, y2)
	c.SetStrokeStyle(color)
	c.SetLineWidth(1.5)
	c.Stroke()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Draw")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x1":    vm.FloatVal(10),
		"y1":    vm.FloatVal(20),
		"x2":    vm.FloatVal(30),
		"y2":    vm.FloatVal(40),
		"color": vm.StringVal("rgba(120,100,75,0.25)"),
	})
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])

	wantMethods := []string{"BeginPath", "MoveTo", "LineTo", "SetStrokeStyle", "SetLineWidth", "Stroke"}
	if len(rec.Calls) != len(wantMethods) {
		t.Fatalf("call count = %d, want %d; got=%+v", len(rec.Calls), len(wantMethods), rec.Calls)
	}
	for i, want := range wantMethods {
		if rec.Calls[i].Method != want {
			t.Errorf("call %d = %q, want %q", i, rec.Calls[i].Method, want)
		}
	}
	// Spot-check arg propagation.
	if rec.Calls[1].Args[0].Num != 10 || rec.Calls[1].Args[1].Num != 20 {
		t.Errorf("MoveTo args = %+v, want [10, 20]", rec.Calls[1].Args)
	}
	if rec.Calls[2].Args[0].Num != 30 || rec.Calls[2].Args[1].Num != 40 {
		t.Errorf("LineTo args = %+v, want [30, 40]", rec.Calls[2].Args)
	}
	if rec.Calls[3].Args[0].Str != "rgba(120,100,75,0.25)" {
		t.Errorf("SetStrokeStyle arg = %q, want \"rgba(120,100,75,0.25)\"", rec.Calls[3].Args[0].Str)
	}
}

// TestY_E_GraphSurfaceStepLayoutShape exercises the per-tick force
// table allocation pattern from stepLayout (`make(map[string]float64,
// len(gNodes))`) plus a host call into the canvas at the end.
// Demonstrates that Y.E's OpMake and OpHostCall co-evaluate cleanly
// in a single handler — the exact pattern stepLayout uses.
func TestY_E_GraphSurfaceStepLayoutShape(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func Step(c *surface.Canvas, n int) int {
	fx := make(map[string]float64, n)
	fx["a"] = 1.0
	fx["b"] = 2.0
	c.SetFillStyle("debug")
	return len(fx)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Step")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"n": vm.IntVal(8),
	})
	machine.BindHost("c", rec)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 2 {
		t.Errorf("Step returned %d, want 2 (len of fx after two writes)", int(got.Num))
	}
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "SetFillStyle" {
		t.Errorf("expected one SetFillStyle host call; got %+v", rec.Calls)
	}
}

// TestY_E_GraphSurfaceUserFnCallsCanvas verifies that a user-defined
// helper which calls the canvas dispatches through the bound
// HostReceiver correctly — Y.D's user-fn dispatch composes cleanly
// with Y.E's host-call dispatch (no special integration needed per
// Y.D's retrospective handoff).
func TestY_E_GraphSurfaceUserFnCallsCanvas(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func drawEdge(c *surface.Canvas, x1 float64, y1 float64, x2 float64, y2 float64) {
	c.BeginPath()
	c.MoveTo(x1, y1)
	c.LineTo(x2, y2)
	c.Stroke()
}

func Draw(c *surface.Canvas) {
	drawEdge(c, 0, 0, 10, 10)
	drawEdge(c, 10, 10, 20, 20)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Draw")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, nil)
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	// Two drawEdge calls × 4 canvas calls each = 8 total.
	if len(rec.Calls) != 8 {
		t.Errorf("call count = %d, want 8; got=%+v", len(rec.Calls), rec.Calls)
	}
}
