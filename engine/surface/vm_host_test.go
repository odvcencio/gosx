// Slice v0.22.1 — VM-side canvas host receiver tests.
//
// These tests pin the contract for NewCanvasHostReceiver: the bridge
// that takes a *vm.VM + *Canvas, exposes itself as a vm.HostReceiver,
// and translates the bytecode-side `c.StartLoop(closure)` into the
// existing Canvas.startLoop machinery (which on WASM rides the
// __gosx_surface_request_frame / __gosx_surface_frame rAF path, and
// on host stubs is driven by RunFrames).
//
// The TDD failing-first commit pins:
//   1. StartLoop accepts a ClosureVal and stores it as the canvas step.
//   2. RunFrames(n) invokes the bound closure n times with monotonic dt.
//   3. Calling StartLoop a second time replaces the prior closure
//      (idempotent restart).
//   4. Dispose drops the closure reference so leaked closures don't
//      keep VM frames live.
//   5. Unknown methods surface as host_call_error diagnostics on the
//      caller's VM, not panics.

package surface

import (
	"runtime"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// newTestVMWithFunc builds a minimal VM whose only synthetic FuncDef
// pushes a marker into a captured slice each time it runs. Returns the
// VM plus a pointer to the marker slice so tests can assert call count.
func newTestVMWithFunc(t *testing.T, funcName string) (*vm.VM, *[]float64) {
	t.Helper()
	// The FuncDef body increments a captured counter via OpHostCall
	// against a recorder bound under "rec". Body: rec.Tick(dt).
	// Expr ids: 0 = OpLocalGet("dt"), 1 = OpHostCall("rec.Tick", [0]).
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "dt"},
			{Op: program.OpHostCall, Value: "rec.Tick", Operands: []program.ExprID{0}},
		},
		Funcs: []program.FuncDef{
			{Name: funcName, Params: []string{"dt"}, Body: []program.ExprID{1}},
		},
	}
	machine := vm.NewVM(prog, nil)
	ticks := &[]float64{}
	machine.BindHost("rec", &tickRecorder{ticks: ticks})
	return machine, ticks
}

// tickRecorder is a HostReceiver that appends the first arg as a
// float64 to its ticks slice each time Tick is called.
type tickRecorder struct {
	ticks *[]float64
}

func (r *tickRecorder) Call(method string, args []vm.Value) (vm.Value, error) {
	if method == "Tick" && len(args) == 1 {
		*r.ticks = append(*r.ticks, args[0].Num)
	}
	return vm.ZeroValue(program.TypeAny), nil
}

// closureForFunc constructs a ClosureVal pointing at the named
// synthetic FuncDef. Used by tests to feed StartLoop a real closure
// without going through the lowerer.
func closureForFunc(name string) vm.Value {
	return vm.NewHostClosure(name, nil)
}

// TestCanvasHostReceiver_StartLoopStoresClosure verifies that calling
// StartLoop with a ClosureVal arg stores it as the canvas's step
// function (observable via RunFrames).
func TestCanvasHostReceiver_StartLoopStoresClosure(t *testing.T) {
	machine, ticks := newTestVMWithFunc(t, "__test_step_1")
	canvas := newNoopCanvas()
	recv := NewCanvasHostReceiver(machine, canvas)

	cv := closureForFunc("__test_step_1")
	if _, err := recv.Call("StartLoop", []vm.Value{cv}); err != nil {
		t.Fatalf("StartLoop returned error: %v", err)
	}

	if recv.HasLoop() != true {
		t.Fatalf("HasLoop = false after StartLoop; want true")
	}

	recv.RunFrames(3)
	if got := len(*ticks); got != 3 {
		t.Fatalf("ticks count = %d, want 3 (closure should run on each RunFrames)", got)
	}
}

// TestCanvasHostReceiver_RunFramesDeltaMonotonic verifies the dt
// argument passed to the closure grows monotonically (each frame
// reports a positive elapsed-time delta).
func TestCanvasHostReceiver_RunFramesDeltaMonotonic(t *testing.T) {
	machine, ticks := newTestVMWithFunc(t, "__test_step_2")
	canvas := newNoopCanvas()
	recv := NewCanvasHostReceiver(machine, canvas)

	cv := closureForFunc("__test_step_2")
	if _, err := recv.Call("StartLoop", []vm.Value{cv}); err != nil {
		t.Fatalf("StartLoop returned error: %v", err)
	}
	recv.RunFrames(4)

	if len(*ticks) != 4 {
		t.Fatalf("ticks count = %d, want 4", len(*ticks))
	}
	// First tick is the zero-delta frame (no prior timestamp). Subsequent
	// ticks must be strictly positive (RunFrames advances the synthetic
	// clock by one frame each call).
	if (*ticks)[0] != 0 {
		t.Errorf("first tick dt = %v, want 0", (*ticks)[0])
	}
	for i := 1; i < len(*ticks); i++ {
		if (*ticks)[i] <= 0 {
			t.Errorf("tick[%d] dt = %v, want > 0", i, (*ticks)[i])
		}
	}
}

// TestCanvasHostReceiver_StartLoopIdempotent verifies that calling
// StartLoop a second time replaces the prior closure rather than
// running both: the second closure must be the one RunFrames invokes.
func TestCanvasHostReceiver_StartLoopIdempotent(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "dt"},
			{Op: program.OpHostCall, Value: "rec.TickA", Operands: []program.ExprID{0}},
			{Op: program.OpHostCall, Value: "rec.TickB", Operands: []program.ExprID{0}},
		},
		Funcs: []program.FuncDef{
			{Name: "__test_first", Params: []string{"dt"}, Body: []program.ExprID{1}},
			{Name: "__test_second", Params: []string{"dt"}, Body: []program.ExprID{2}},
		},
	}
	machine := vm.NewVM(prog, nil)
	var aTicks, bTicks []float64
	machine.BindHost("rec", &abRecorder{a: &aTicks, b: &bTicks})

	recv := NewCanvasHostReceiver(machine, newNoopCanvas())

	if _, err := recv.Call("StartLoop", []vm.Value{closureForFunc("__test_first")}); err != nil {
		t.Fatalf("first StartLoop: %v", err)
	}
	recv.RunFrames(1)
	if _, err := recv.Call("StartLoop", []vm.Value{closureForFunc("__test_second")}); err != nil {
		t.Fatalf("second StartLoop: %v", err)
	}
	recv.RunFrames(2)

	if len(aTicks) != 1 {
		t.Errorf("first closure tick count = %d, want 1 (run once before swap)", len(aTicks))
	}
	if len(bTicks) != 2 {
		t.Errorf("second closure tick count = %d, want 2 (post-swap)", len(bTicks))
	}
}

type abRecorder struct {
	a, b *[]float64
}

func (r *abRecorder) Call(method string, args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.ZeroValue(program.TypeAny), nil
	}
	switch method {
	case "TickA":
		*r.a = append(*r.a, args[0].Num)
	case "TickB":
		*r.b = append(*r.b, args[0].Num)
	}
	return vm.ZeroValue(program.TypeAny), nil
}

// TestCanvasHostReceiver_DisposeDropsClosure verifies that Dispose
// cancels the loop and drops the closure reference so it doesn't keep
// the captured VM frames alive past surface teardown.
func TestCanvasHostReceiver_DisposeDropsClosure(t *testing.T) {
	machine, ticks := newTestVMWithFunc(t, "__test_dispose")
	recv := NewCanvasHostReceiver(machine, newNoopCanvas())

	if _, err := recv.Call("StartLoop", []vm.Value{closureForFunc("__test_dispose")}); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	recv.RunFrames(2)
	if len(*ticks) != 2 {
		t.Fatalf("pre-dispose ticks = %d, want 2", len(*ticks))
	}

	recv.Dispose()

	if recv.HasLoop() {
		t.Errorf("HasLoop = true after Dispose; want false (closure must be dropped)")
	}

	// Post-dispose RunFrames is a no-op — the closure should be gone.
	recv.RunFrames(5)
	if len(*ticks) != 2 {
		t.Errorf("post-dispose ticks = %d, want 2 (no new invocations after Dispose)", len(*ticks))
	}
}

// TestCanvasHostReceiver_StartLoopRejectsNonClosure verifies that
// calling StartLoop with a non-closure value records a diagnostic-style
// error rather than panicking; the loop must remain inactive.
func TestCanvasHostReceiver_StartLoopRejectsNonClosure(t *testing.T) {
	machine := vm.NewVM(&program.Program{}, nil)
	recv := NewCanvasHostReceiver(machine, newNoopCanvas())

	_, err := recv.Call("StartLoop", []vm.Value{vm.StringVal("not-a-closure")})
	if err == nil {
		t.Fatal("StartLoop with non-closure arg returned nil error; want non-nil")
	}
	if recv.HasLoop() {
		t.Errorf("HasLoop = true after rejected StartLoop; want false")
	}
}

// TestCanvasHostReceiver_StartLoopRejectsBadArity verifies that
// calling StartLoop with the wrong number of args surfaces an error.
func TestCanvasHostReceiver_StartLoopRejectsBadArity(t *testing.T) {
	machine := vm.NewVM(&program.Program{}, nil)
	recv := NewCanvasHostReceiver(machine, newNoopCanvas())

	if _, err := recv.Call("StartLoop", nil); err == nil {
		t.Errorf("StartLoop() returned nil error; want non-nil")
	}
	if _, err := recv.Call("StartLoop", []vm.Value{closureForFunc("x"), closureForFunc("y")}); err == nil {
		t.Errorf("StartLoop(x,y) returned nil error; want non-nil")
	}
	if recv.HasLoop() {
		t.Errorf("HasLoop = true after rejected calls; want false")
	}
}

// TestBindCanvas_DisposeOnContextClose verifies that the BindCanvas
// convenience helper correctly cancels the loop and unbinds the host
// receiver from the VM when the surface context is closed.
func TestBindCanvas_DisposeOnContextClose(t *testing.T) {
	machine, ticks := newTestVMWithFunc(t, "__test_bind_dispose")
	ctx := NewContext(nil)

	recv := BindCanvas(machine, "c", newNoopCanvas(), ctx)

	if _, err := recv.Call("StartLoop", []vm.Value{closureForFunc("__test_bind_dispose")}); err != nil {
		t.Fatalf("StartLoop: %v", err)
	}
	recv.RunFrames(2)
	if len(*ticks) != 2 {
		t.Fatalf("pre-close ticks = %d, want 2", len(*ticks))
	}

	// Verify the host is bound under "c".
	if got, ok := machine.LookupHost("c"); !ok || got == nil {
		t.Fatal("host receiver not bound under 'c' after BindCanvas")
	}

	// Closing the context fires the watcher goroutine; give it a
	// moment to run.
	ctx.Close()
	// Synchronization without sleeps: poll the receiver state via
	// the (already-tested) HasLoop / LookupHost contract.
	for i := 0; i < 100; i++ {
		_, bound := machine.LookupHost("c")
		if !recv.HasLoop() && !bound {
			break
		}
		runtime.Gosched()
	}

	if recv.HasLoop() {
		t.Errorf("HasLoop = true after ctx.Close; want false (Dispose must drop closure)")
	}
	if _, bound := machine.LookupHost("c"); bound {
		t.Errorf("host receiver still bound after ctx.Close; want unbound")
	}
}

// TestCanvasHostReceiver_DispatchesCanvasMethods verifies that the
// receiver forwards regular canvas method calls (Width/Height/...) to
// the wrapped *Canvas rather than treating them as unknown methods.
func TestCanvasHostReceiver_DispatchesCanvasMethods(t *testing.T) {
	machine := vm.NewVM(&program.Program{}, nil)
	canvas := &Canvas{impl: &stubImpl{w: 1024, h: 768}}
	recv := NewCanvasHostReceiver(machine, canvas)

	got, err := recv.Call("Width", nil)
	if err != nil {
		t.Fatalf("Width: %v", err)
	}
	if int(got.Num) != 1024 {
		t.Errorf("Width = %v, want 1024", got.Num)
	}

	got, err = recv.Call("Height", nil)
	if err != nil {
		t.Fatalf("Height: %v", err)
	}
	if int(got.Num) != 768 {
		t.Errorf("Height = %v, want 768", got.Num)
	}
}
