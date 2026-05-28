// Package surface — VM-side canvas host receiver bridge (v0.22.1).
//
// CanvasHostReceiver is the production glue that finally makes
// `c.StartLoop(func(dt float64) { ... })` run per-frame when a surface
// handler executes as shared-VM bytecode (the path Y.G unlocked).
//
// # The handoff chain
//
// 1. The bytecode handler lowers to OpHostCall("c.StartLoop", [closure])
//    where `closure` evaluates to a vm.ClosureVal carrying the
//    synthetic FuncDef name and a captured-frame pointer (Y.G).
// 2. The VM dispatches OpHostCall into the receiver bound under "c" —
//    this CanvasHostReceiver.
// 3. CanvasHostReceiver.Call("StartLoop", [closure]) wraps the
//    ClosureVal in a Go `func(dt float64)` that calls
//    vm.InvokeClosure(closure, [vm.FloatVal(dt)]) and hands the wrapped
//    Go closure to the underlying *Canvas.StartLoop. From there:
//
//      - On WASM (canvas_js.go): startLoop stores the step fn and
//        kicks the JS rAF loop via __gosx_surface_request_frame, which
//        cycles back as __gosx_surface_frame → Canvas.TickFrame →
//        stepFn(dt). vm.InvokeClosure runs on the WASM main thread
//        (same goroutine as the JS callback under the Go wasm runtime),
//        so the captured-by-reference frame remains valid.
//
//      - On host/stub (canvas_stub_host.go): the underlying startLoop
//        is a no-op (no browser to schedule against). Tests drive the
//        loop manually by calling RunFrames(n) on the receiver, which
//        directly invokes the bound closure n times against a
//        synthetic monotonic clock.
//
// # Cleanup contract
//
// Dispose() (called by the surface runtime on dismount — see
// runtime/canvas_js.go's __gosx_surface_dispose handler) drops the
// stored closure reference and clears the canvas step fn so the
// captured VM frame can be GC'd. Idempotent: calling Dispose twice is
// safe; a subsequent StartLoop is also legal (and the host receiver
// is re-usable).
//
// # Idempotent restart
//
// Calling StartLoop twice replaces the prior closure. The canvas-side
// stepFn pointer is overwritten in lockstep, so the rAF callback only
// ever invokes the most recently-registered closure. This matches the
// Canvas.StartLoop contract documented in surface.go.
//
// # Why not extend HostCanvasImpl
//
// HostCanvasImpl (canvas_host_impl.go) is the test-time CPU rasterizer
// seam Y.F added — it deliberately omits startLoop because hosts
// drive their own loop. The CanvasHostReceiver lives one level up: it
// adapts a *Canvas (which DOES carry a startLoop method that already
// wires the rAF path on WASM) to the vm.HostReceiver contract, so the
// VM-driven path benefits from the existing rAF plumbing without
// duplicating it.

package surface

import (
	"fmt"

	"m31labs.dev/gosx/client/vm"
)

// CanvasHostReceiver bridges a *Canvas to the vm.HostReceiver
// interface so bytecode handlers can call canvas methods (including
// the closure-bearing StartLoop) via OpHostCall.
//
// Construct one per surface mount via NewCanvasHostReceiver and bind
// it under the surface author's chosen identifier ("c" by convention):
//
//	recv := surface.NewCanvasHostReceiver(machine, canvas)
//	machine.BindHost("c", recv)
//
// On surface dismount, call recv.Dispose() to drop the loop closure
// reference and clear the canvas step function — this prevents the
// captured VM frame from being kept alive by the rAF schedule.
type CanvasHostReceiver struct {
	vm     *vm.VM
	canvas *Canvas

	// loop holds the most recently-registered animation-loop closure,
	// or the zero Value when no loop is active. We keep it explicitly
	// (rather than only inside the canvas.stepFn) so HasLoop() and
	// RunFrames() have a single source of truth for the active state.
	loop vm.Value
}

// NewCanvasHostReceiver wraps machine + canvas as a vm.HostReceiver
// suitable for the surface author's canvas parameter binding.
//
// Either argument may be nil; nil canvas falls back to a no-op canvas
// (StartLoop accepts the closure but no rAF schedule is ever kicked).
// Nil machine causes StartLoop to record a "no VM bound" error per
// the panic-free engine-surface contract.
func NewCanvasHostReceiver(machine *vm.VM, canvas *Canvas) *CanvasHostReceiver {
	if canvas == nil {
		canvas = newNoopCanvas()
	}
	return &CanvasHostReceiver{vm: machine, canvas: canvas}
}

// BindCanvas is the convenience helper future bytecode-VM hydration
// paths will use: it constructs a CanvasHostReceiver, binds it under
// name (conventionally "c") on the VM, and spawns a single watcher
// goroutine that calls Dispose when ctx is torn down.
//
// Returns the receiver so callers can drive RunFrames in tests or
// hold a reference for explicit teardown. ctx may be nil — callers
// that own their teardown can omit the watcher and call Dispose
// directly.
//
// This is the natural "wire it once" entry point; both sides
// (StartLoop closure capture + Dispose-on-dismount) are handled
// together so future hydration code doesn't have to remember the
// dispose hook.
func BindCanvas(machine *vm.VM, name string, canvas *Canvas, ctx *Context) *CanvasHostReceiver {
	recv := NewCanvasHostReceiver(machine, canvas)
	if machine != nil {
		machine.BindHost(name, recv)
	}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			recv.Dispose()
			if machine != nil {
				machine.BindHost(name, nil)
			}
		}()
	}
	return recv
}

// Call satisfies vm.HostReceiver. The method name comes from the
// source-level `c.<Method>` call as it was lowered by Y.E's host-call
// path. Argument values are pre-evaluated by the VM.
//
// StartLoop is the only method that consumes a ClosureVal arg; every
// other canvas method is forwarded to the underlying *Canvas via
// dispatchCanvasMethod. Unknown methods surface as
// "unknown method <name>" errors which the VM records as a
// host_call_error diagnostic.
func (r *CanvasHostReceiver) Call(method string, args []vm.Value) (vm.Value, error) {
	if method == "StartLoop" {
		return r.startLoop(args)
	}
	return r.dispatchCanvasMethod(method, args)
}

// startLoop is the closure-bearing dispatch: validate args, wrap the
// ClosureVal in a Go func(dt) that invokes it via vm.InvokeClosure,
// and hand the wrapped closure to canvas.StartLoop so the existing
// rAF schedule picks it up.
//
// Idempotency: replacing the previous closure here also overwrites
// the canvas.stepFn (canvas_js.go: startLoop sets c.stepFn = step),
// so subsequent __gosx_surface_frame ticks invoke only the latest
// closure. The previous closure's captured frame becomes eligible for
// GC as soon as the host drops its reference (i.e., this assignment).
func (r *CanvasHostReceiver) startLoop(args []vm.Value) (vm.Value, error) {
	if len(args) != 1 {
		return vm.ZeroValue(0), fmt.Errorf("StartLoop expects 1 arg, got %d", len(args))
	}
	cv := args[0]
	if !vm.IsClosure(cv) {
		return vm.ZeroValue(0), fmt.Errorf("StartLoop arg must be a closure, got %v", cv.Type)
	}
	if r.vm == nil {
		return vm.ZeroValue(0), fmt.Errorf("StartLoop: no VM bound to host receiver")
	}

	r.loop = cv
	machine := r.vm
	step := func(dt float64) {
		machine.InvokeClosure(cv, []vm.Value{vm.FloatVal(dt)})
	}
	r.canvas.impl.startLoop(step)
	return vm.ZeroValue(0), nil
}

// HasLoop reports whether StartLoop has been called with a valid
// closure that hasn't yet been cleared by Dispose. Primarily a test
// observability hook; production callers should not need this.
func (r *CanvasHostReceiver) HasLoop() bool {
	return vm.IsClosure(r.loop)
}

// Dispose cancels the active animation loop (if any) by clearing the
// canvas step function and dropping the receiver's closure reference.
// Called by the surface runtime when the canvas element is detached
// from the DOM (__gosx_surface_dispose path) to prevent the closure
// from keeping the captured VM frame alive past surface teardown.
//
// Idempotent: safe to call multiple times. After Dispose, a fresh
// StartLoop call re-arms the loop (the receiver is re-usable).
func (r *CanvasHostReceiver) Dispose() {
	r.loop = vm.Value{}
	if r.canvas != nil {
		// SetStepFn is the existing exported seam on Canvas that
		// canvas_js.go uses to install the step fn. Setting it to nil
		// makes TickFrame a no-op for any pending rAF callbacks.
		r.canvas.SetStepFn(nil)
	}
}

// RunFrames synchronously invokes the bound animation loop closure n
// times with a synthetic monotonically-increasing dt. Intended for
// host-side unit tests of bytecode handlers (where there is no
// browser to schedule rAF callbacks). Returns immediately if no loop
// is active.
//
// Each frame's dt is the elapsed time since the previous RunFrames
// invocation in the same call (frame 0 dt = 0, frame i dt =
// frameDuration for i > 0). This matches the canvas_js.go contract
// where TickFrame computes dt = ts - lastTS and the first frame
// records 0.
//
// On WASM, the production rAF path drives invocations directly via
// canvas.TickFrame; this method is not used in the browser.
func (r *CanvasHostReceiver) RunFrames(n int) {
	if n <= 0 || !r.HasLoop() || r.vm == nil {
		return
	}
	const frameDuration = 1.0 / 60.0 // seconds (~16.67ms at 60fps)
	for i := 0; i < n; i++ {
		dt := frameDuration
		if i == 0 {
			dt = 0
		}
		r.vm.InvokeClosure(r.loop, []vm.Value{vm.FloatVal(dt)})
	}
}

// dispatchCanvasMethod handles non-StartLoop calls against the
// underlying canvas. The dispatch table mirrors the public methods on
// *Canvas; methods that return a Value (Width, Height) wrap the
// result in vm.IntVal, void methods return the zero Value.
//
// The set is deliberately conservative: only methods authors actually
// call from a bytecode-lowered handler need an entry here. Adding a
// new method is a one-line case in the switch. Unknown methods
// surface as an error which the VM records as host_call_error.
func (r *CanvasHostReceiver) dispatchCanvasMethod(method string, args []vm.Value) (vm.Value, error) {
	c := r.canvas
	switch method {
	case "Width":
		return vm.IntVal(c.Width()), nil
	case "Height":
		return vm.IntVal(c.Height()), nil
	case "Clear":
		c.Clear()
	case "ClearRect":
		c.ClearRect(arg(args, 0), arg(args, 1), arg(args, 2), arg(args, 3))
	case "FillRect":
		c.FillRect(arg(args, 0), arg(args, 1), arg(args, 2), arg(args, 3))
	case "BeginPath":
		c.BeginPath()
	case "MoveTo":
		c.MoveTo(arg(args, 0), arg(args, 1))
	case "LineTo":
		c.LineTo(arg(args, 0), arg(args, 1))
	case "Arc":
		c.Arc(arg(args, 0), arg(args, 1), arg(args, 2), arg(args, 3), arg(args, 4))
	case "Stroke":
		c.Stroke()
	case "Fill":
		c.Fill()
	case "FillText":
		c.FillText(argStr(args, 0), arg(args, 1), arg(args, 2))
	case "SetFillStyle":
		c.SetFillStyle(argStr(args, 0))
	case "SetStrokeStyle":
		c.SetStrokeStyle(argStr(args, 0))
	case "SetLineWidth":
		c.SetLineWidth(arg(args, 0))
	case "SetFont":
		c.SetFont(argStr(args, 0))
	case "SetTextAlign":
		c.SetTextAlign(argStr(args, 0))
	case "Save":
		c.Save()
	case "Restore":
		c.Restore()
	case "Translate":
		c.Translate(arg(args, 0), arg(args, 1))
	case "Scale":
		c.Scale(arg(args, 0), arg(args, 1))
	case "Rotate":
		c.Rotate(arg(args, 0))
	case "SetTransform":
		c.SetTransform(arg(args, 0), arg(args, 1), arg(args, 2), arg(args, 3), arg(args, 4), arg(args, 5))
	case "RequestFrame":
		c.RequestFrame()
	default:
		return vm.ZeroValue(0), fmt.Errorf("unknown canvas method %q", method)
	}
	return vm.ZeroValue(0), nil
}

// arg returns args[i].Num or 0 if i is out of range. Centralizes the
// bounds-tolerance contract so each dispatch case stays one line.
func arg(args []vm.Value, i int) float64 {
	if i < 0 || i >= len(args) {
		return 0
	}
	return args[i].Num
}

// argStr returns args[i].Str or "" if i is out of range.
func argStr(args []vm.Value, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	return args[i].Str
}
