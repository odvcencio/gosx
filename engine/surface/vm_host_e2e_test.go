// End-to-end proof: a VM dispatches OpHostCall("c.StartLoop", [closure])
// through CanvasHostReceiver, and the receiver-installed step fn
// drives the closure via the same path canvas_js.go's TickFrame uses
// on WASM.
//
// This stops short of importing ir/golower (which would create an
// import cycle since engine/surface already depends on it). Instead
// we construct a Program by hand whose shape matches what the lowerer
// emits for `c.StartLoop(func(dt float64) { rec.Tick(dt) })`. That
// keeps the test focused on the OpHostCall → CanvasHostReceiver →
// canvas.startLoop hand-off rather than re-testing the lowerer.

package surface

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// TestCanvasHostReceiver_E2E_OpHostCallDispatchAndStep proves the
// full round-trip:
//
//   1. Lowerer emits OpHostCall("c.StartLoop", [OpClosure ...]) (we
//      construct this by hand).
//   2. VM evaluates the OpHostCall → CanvasHostReceiver.Call.
//   3. Receiver wraps the ClosureVal in a Go func(dt) and installs it
//      on the canvas.
//   4. Driving the canvas-installed step fn (what TickFrame does on
//      WASM) invokes the closure via vm.InvokeClosure, which records
//      ticks against the bound recorder.
func TestCanvasHostReceiver_E2E_OpHostCallDispatchAndStep(t *testing.T) {
	// Synthetic FuncDef body: `OpHostCall("rec.Tick", [OpLocalGet("dt")])`
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "dt"},                                            // 0
			{Op: program.OpHostCall, Value: "rec.Tick", Operands: []program.ExprID{0}},       // 1
			{Op: program.OpClosure, Value: "__e2e_synth_closure"},                            // 2
			{Op: program.OpHostCall, Value: "c.StartLoop", Operands: []program.ExprID{2}},    // 3
		},
		Funcs: []program.FuncDef{
			{Name: "__e2e_synth_closure", Params: []string{"dt"}, Body: []program.ExprID{1}},
		},
		Handlers: []program.Handler{
			{Name: "Mount", Body: []program.ExprID{3}},
		},
	}

	machine := vm.NewVM(prog, nil)

	// Bind the recording host first so the closure body's
	// OpHostCall("rec.Tick", ...) has a target.
	var ticks []float64
	machine.BindHost("rec", &tickRecorder{ticks: &ticks})

	// Bind the canvas host receiver under "c" via the convenience
	// helper; nil ctx — explicit Dispose at end of test.
	canvasRec := &recordingCanvasImpl{}
	canvas := &Canvas{impl: canvasRec}
	recv := BindCanvas(machine, "c", canvas, nil)
	defer recv.Dispose()

	// Drive the Mount handler: this evaluates the OpHostCall that
	// hands the closure to c.StartLoop.
	machine.EvalWithFrame(prog.Handlers[0].Body[0])

	if !recv.HasLoop() {
		t.Fatal("HasLoop = false after Mount; OpHostCall(c.StartLoop) did not install closure")
	}
	if canvasRec.stepFn == nil {
		t.Fatal("canvas step fn nil after Mount; CanvasHostReceiver did not call canvas.startLoop")
	}

	// Now drive the canvas step the way __gosx_surface_frame /
	// TickFrame would on WASM. Each step must dispatch through
	// vm.InvokeClosure into the synthetic FuncDef body, which fires
	// rec.Tick(dt).
	canvasRec.stepFn(0.016)
	canvasRec.stepFn(0.020)
	canvasRec.stepFn(0.018)

	if got := len(ticks); got != 3 {
		t.Fatalf("rec.Tick called %d times after 3 step-fn invocations; want 3", got)
	}
	wantDTs := []float64{0.016, 0.020, 0.018}
	for i, want := range wantDTs {
		if ticks[i] != want {
			t.Errorf("tick[%d] dt = %v, want %v", i, ticks[i], want)
		}
	}
}
