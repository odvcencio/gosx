//go:build !gosx_tiny_islands_only

// Engine-surface hydration is the JS-side counterpart to the Go-side
// CanvasHostReceiver bridge larch shipped in PR #18 (gosx v0.22.1). Where
// the canvas2d adapter (TestHydrateReconcilerCanvas2DSucceeds) wires the
// <CanvasBoard> primitive program shape (EngineNodes-driven reconciler),
// engine surfaces (per ADR 0003 / ADR 0005) ship a handler-driven program
// shape: Mount + OnClick + OnFrame + ... lowered by ir/golower from .gsx
// + companion .go.
//
// The contract this file pins:
//
//   - bridge.HydrateEngineSurface(id, name, propsJSON, programData, format, canvas)
//     decodes the program, constructs a fresh VM, registers signals,
//     binds a surface.CanvasHostReceiver under "c" (the convention from
//     vm_host.go's BindCanvas docstring), and invokes the Mount handler.
//
//   - The VM stays alive after Mount returns so the bound CanvasHostReceiver
//     can re-invoke the StartLoop closure on each rAF tick and so DOM
//     events route through DispatchEngineSurfaceEvent.
//
//   - DisposeEngineSurface tears the instance down (canvas.SetStepFn(nil),
//     drops the closure reference, removes the bridge entry) so the
//     captured VM frame is GC-able and a stale rAF callback no longer
//     re-enters InvokeClosure on a torn-down VM.
//
// See also: ../../engine/surface/vm_host.go (CanvasHostReceiver +
// BindCanvas), ~/.hyphae/spaces/m31labs-gosx/lessons/2026-05-27-y-g-funclit-and-nil-fields-retrospective.md
// for the closure machinery the loop consumes.

package bridge

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/engine/surface"
	islandprogram "m31labs.dev/gosx/island/program"
)

// engineSurfaceRecordingCanvas implements surface.HostCanvasImpl so the
// test can assert which drawing calls a Mount handler made through the
// host-call bridge. Mirrors the parity-test harness pattern Y.F established
// (see hyphae/cmd/hypha-viz before the dogfood-parity deletion).
type engineSurfaceRecordingCanvas struct {
	calls       []string
	fillStyle   string
	lastFillX   float64
	lastFillW   float64
}

func (r *engineSurfaceRecordingCanvas) Width() int                                    { return 400 }
func (r *engineSurfaceRecordingCanvas) Height() int                                   { return 300 }
func (r *engineSurfaceRecordingCanvas) Clear()                                        { r.calls = append(r.calls, "Clear") }
func (r *engineSurfaceRecordingCanvas) ClearRect(x, y, w, h float64)                  { r.calls = append(r.calls, "ClearRect") }
func (r *engineSurfaceRecordingCanvas) FillRect(x, y, w, h float64) {
	r.calls = append(r.calls, "FillRect")
	r.lastFillX = x
	r.lastFillW = w
}
func (r *engineSurfaceRecordingCanvas) BeginPath()                                    { r.calls = append(r.calls, "BeginPath") }
func (r *engineSurfaceRecordingCanvas) MoveTo(x, y float64)                           {}
func (r *engineSurfaceRecordingCanvas) LineTo(x, y float64)                           {}
func (r *engineSurfaceRecordingCanvas) Arc(x, y, ra, s, e float64)                    {}
func (r *engineSurfaceRecordingCanvas) Stroke()                                       {}
func (r *engineSurfaceRecordingCanvas) Fill()                                         {}
func (r *engineSurfaceRecordingCanvas) FillText(text string, x, y float64)            {}
func (r *engineSurfaceRecordingCanvas) SetFillStyle(css string)                       { r.fillStyle = css; r.calls = append(r.calls, "SetFillStyle:"+css) }
func (r *engineSurfaceRecordingCanvas) SetStrokeStyle(css string)                     {}
func (r *engineSurfaceRecordingCanvas) SetLineWidth(w float64)                        {}
func (r *engineSurfaceRecordingCanvas) SetFont(css string)                            {}
func (r *engineSurfaceRecordingCanvas) SetTextAlign(s string)                         {}
func (r *engineSurfaceRecordingCanvas) Save()                                         {}
func (r *engineSurfaceRecordingCanvas) Restore()                                      {}
func (r *engineSurfaceRecordingCanvas) Translate(x, y float64)                        {}
func (r *engineSurfaceRecordingCanvas) Scale(x, y float64)                            {}
func (r *engineSurfaceRecordingCanvas) Rotate(rad float64)                            {}
func (r *engineSurfaceRecordingCanvas) SetTransform(a, b, c, d, e, f float64)         {}
func (r *engineSurfaceRecordingCanvas) RequestFrame()                                 {}

// TestHydrateEngineSurfaceRunsMountHandler is the canonical acceptance: a
// minimal Mount handler that paints two FillRects through the canvas host
// receiver must run to completion when HydrateEngineSurface is called with
// the program. This is the proof point that the JS bootstrap's call to the
// new entry point produces the same effects (drawing calls + canvas state
// updates) that the legacy per-component-WASM path produced.
func TestHydrateEngineSurfaceRunsMountHandler(t *testing.T) {
	// Mount body:
	//   c.SetFillStyle("#abc")
	//   c.FillRect(10, 20, 30, 40)
	//   c.FillRect(50, 60, 70, 80)
	prog := &islandprogram.Program{
		Name: "MiniMount",
		Exprs: []islandprogram.Expr{
			// 0: "#abc"
			{Op: islandprogram.OpLitString, Value: "#abc", Type: islandprogram.TypeString},
			// 1: c.SetFillStyle("#abc")
			{Op: islandprogram.OpHostCall, Value: "c.SetFillStyle", Operands: []islandprogram.ExprID{0}, Type: islandprogram.TypeAny},
			// 2-5: FillRect args (10, 20, 30, 40)
			{Op: islandprogram.OpLitFloat, Value: "10", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "20", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "30", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "40", Type: islandprogram.TypeFloat},
			// 6: c.FillRect(10, 20, 30, 40)
			{Op: islandprogram.OpHostCall, Value: "c.FillRect", Operands: []islandprogram.ExprID{2, 3, 4, 5}, Type: islandprogram.TypeAny},
			// 7-10: FillRect args (50, 60, 70, 80)
			{Op: islandprogram.OpLitFloat, Value: "50", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "60", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "70", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "80", Type: islandprogram.TypeFloat},
			// 11: c.FillRect(50, 60, 70, 80)
			{Op: islandprogram.OpHostCall, Value: "c.FillRect", Operands: []islandprogram.ExprID{7, 8, 9, 10}, Type: islandprogram.TypeAny},
			// 12: OpSeq wrapping the body
			{Op: islandprogram.OpSeq, Operands: []islandprogram.ExprID{1, 6, 11}, Type: islandprogram.TypeAny},
		},
		Handlers: []islandprogram.Handler{
			{Name: "Mount", Body: []islandprogram.ExprID{12}},
		},
	}
	data, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("encode prog: %v", err)
	}

	rec := &engineSurfaceRecordingCanvas{}
	canvas := surface.NewCanvasFromHostImpl(rec)

	b := New()
	if err := b.HydrateEngineSurface("surface-1", "MiniMount", `{}`, data, "json", canvas); err != nil {
		t.Fatalf("HydrateEngineSurface failed: %v", err)
	}

	if b.EngineSurfaceCount() != 1 {
		t.Errorf("EngineSurfaceCount = %d, want 1", b.EngineSurfaceCount())
	}

	wantCalls := []string{"SetFillStyle:#abc", "FillRect", "FillRect"}
	if len(rec.calls) != len(wantCalls) {
		t.Fatalf("got %d canvas calls, want %d: %v", len(rec.calls), len(wantCalls), rec.calls)
	}
	for i, want := range wantCalls {
		if rec.calls[i] != want {
			t.Errorf("call %d: got %q, want %q", i, rec.calls[i], want)
		}
	}
	if rec.lastFillX != 50 || rec.lastFillW != 70 {
		t.Errorf("last FillRect args: x=%v w=%v, want x=50 w=70", rec.lastFillX, rec.lastFillW)
	}

	b.DisposeEngineSurface("surface-1")
	if b.EngineSurfaceCount() != 0 {
		t.Errorf("DisposeEngineSurface left instance behind: %d", b.EngineSurfaceCount())
	}
}

// TestHydrateEngineSurfaceRejectsMalformedProgram pins the contract that a
// malformed JSON program surfaces as a clean error, not a panic — matching
// HydrateReconciler's behavior for canvas2d / dom / scene3d.
func TestHydrateEngineSurfaceRejectsMalformedProgram(t *testing.T) {
	rec := &engineSurfaceRecordingCanvas{}
	canvas := surface.NewCanvasFromHostImpl(rec)

	b := New()
	err := b.HydrateEngineSurface("surface-bad", "Broken", `{}`, []byte(`{not json`), "json", canvas)
	if err == nil {
		t.Fatalf("expected decode error for malformed program")
	}
}

// TestHydrateEngineSurfaceDispatchEvent verifies that DispatchEngineSurfaceEvent
// routes an event into the corresponding handler (kind→handler-name mapping
// from engine/surface/runtime/runtime.go's table). Uses a click event with a
// handler that paints FillRect using the event coordinates — proves that
// post-Mount handler dispatch reaches the live VM with its CanvasHostReceiver.
func TestHydrateEngineSurfaceDispatchEvent(t *testing.T) {
	// OnClick body: c.FillRect(ev.X, ev.Y, 5, 5)
	//
	// The dispatcher stages event coords into VM props named "ev.X", "ev.Y"
	// (mirrors how the surface lowerer exposes pointer events — see the
	// $surface.event.* signal contract per ADR 0007). The handler reads them
	// via OpPropGet.
	prog := &islandprogram.Program{
		Name: "MiniClick",
		Props: []islandprogram.PropDef{
			{Name: "ev.X", Type: islandprogram.TypeFloat},
			{Name: "ev.Y", Type: islandprogram.TypeFloat},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpPropGet, Value: "ev.X", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpPropGet, Value: "ev.Y", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpHostCall, Value: "c.FillRect", Operands: []islandprogram.ExprID{0, 1, 2, 3}, Type: islandprogram.TypeAny},
			{Op: islandprogram.OpSeq, Operands: []islandprogram.ExprID{4}, Type: islandprogram.TypeAny},
		},
		Handlers: []islandprogram.Handler{
			{Name: "OnClick", Body: []islandprogram.ExprID{5}},
		},
	}
	data, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("encode prog: %v", err)
	}

	rec := &engineSurfaceRecordingCanvas{}
	canvas := surface.NewCanvasFromHostImpl(rec)

	b := New()
	if err := b.HydrateEngineSurface("surface-click", "MiniClick", `{}`, data, "json", canvas); err != nil {
		t.Fatalf("HydrateEngineSurface failed: %v", err)
	}
	// Click at (123, 45). KindClick=1 per the engine/surface/runtime table.
	if err := b.DispatchEngineSurfaceEvent("surface-click", EngineSurfaceEventClick, []float64{123, 45, 0, 0, 0}, ""); err != nil {
		t.Fatalf("DispatchEngineSurfaceEvent failed: %v", err)
	}
	if len(rec.calls) != 1 || rec.calls[0] != "FillRect" {
		t.Fatalf("expected one FillRect call, got %v", rec.calls)
	}
	if rec.lastFillX != 123 {
		t.Errorf("FillRect x = %v, want 123", rec.lastFillX)
	}
	b.DisposeEngineSurface("surface-click")
}

// TestHydrateEngineSurfaceStartLoopTicksThroughBridge verifies the canonical
// hard path: a Mount handler that calls c.StartLoop(func(dt) { ... }) registers
// a closure that subsequent rAF ticks (TickEngineSurface) invoke. This is the
// Y.G FuncLit closure path lowered into a Program — pinning that the bridge
// correctly stitches the CanvasHostReceiver to the VM's InvokeClosure.
//
// Because Y.G FuncLit lowering is exercised by ir/golower tests already, this
// test bypasses the lowerer and constructs the closure machinery directly:
// the Mount handler emits OpClosure (referencing a synthetic FuncDef) and
// passes the resulting ClosureVal as the StartLoop argument. The synthetic
// FuncDef records draw work (FillRect) so each TickEngineSurface produces an
// observable canvas call.
func TestHydrateEngineSurfaceStartLoopTicksThroughBridge(t *testing.T) {
	prog := &islandprogram.Program{
		Name: "TickLoop",
		Funcs: []islandprogram.FuncDef{
			{
				Name:    "__step",
				Params:  []string{"dt"},
				Results: 0,
				// Body: c.FillRect(1, 2, 3, 4)
				Body: []islandprogram.ExprID{6},
			},
		},
		Exprs: []islandprogram.Expr{
			// 0: OpClosure naming "__step", no captures
			{Op: islandprogram.OpClosure, Value: "__step", Type: islandprogram.TypeAny},
			// 1: c.StartLoop(closure)
			{Op: islandprogram.OpHostCall, Value: "c.StartLoop", Operands: []islandprogram.ExprID{0}, Type: islandprogram.TypeAny},
			// 2: OpSeq wrapping Mount body
			{Op: islandprogram.OpSeq, Operands: []islandprogram.ExprID{1}, Type: islandprogram.TypeAny},
			// 3-6: c.FillRect(1, 2, 3, 4) for the synthetic step body
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "3", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "4", Type: islandprogram.TypeFloat},
			// 7 unused — keep table indexable
		},
		Handlers: []islandprogram.Handler{
			{Name: "Mount", Body: []islandprogram.ExprID{2}},
		},
	}
	// The __step body needs OpHostCall referencing the literals; that expr is
	// id 6 by the slot reserved in Funcs[0].Body. Rebuild the expression table
	// in the right order so id 6 is the FillRect call.
	prog.Exprs = []islandprogram.Expr{
		// 0: OpClosure naming "__step"
		{Op: islandprogram.OpClosure, Value: "__step", Type: islandprogram.TypeAny},
		// 1: c.StartLoop(closure)
		{Op: islandprogram.OpHostCall, Value: "c.StartLoop", Operands: []islandprogram.ExprID{0}, Type: islandprogram.TypeAny},
		// 2: OpSeq wrapping Mount body
		{Op: islandprogram.OpSeq, Operands: []islandprogram.ExprID{1}, Type: islandprogram.TypeAny},
		// 3-6: literal args for FillRect
		{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
		{Op: islandprogram.OpLitFloat, Value: "2", Type: islandprogram.TypeFloat},
		{Op: islandprogram.OpLitFloat, Value: "3", Type: islandprogram.TypeFloat},
		// 6: c.FillRect(1, 2, 3, 4) — body of __step
		{Op: islandprogram.OpHostCall, Value: "c.FillRect", Operands: []islandprogram.ExprID{3, 4, 5, 7}, Type: islandprogram.TypeAny},
		// 7: literal 4 (last arg)
		{Op: islandprogram.OpLitFloat, Value: "4", Type: islandprogram.TypeFloat},
	}
	data, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("encode prog: %v", err)
	}

	rec := &engineSurfaceRecordingCanvas{}
	canvas := surface.NewCanvasFromHostImpl(rec)

	b := New()
	if err := b.HydrateEngineSurface("surface-loop", "TickLoop", `{}`, data, "json", canvas); err != nil {
		t.Fatalf("HydrateEngineSurface failed: %v", err)
	}

	// Mount registered the closure but did not yet tick; FillRect should not
	// have fired yet.
	if len(rec.calls) != 0 {
		t.Fatalf("pre-tick: expected no canvas calls, got %v", rec.calls)
	}

	// Drive 3 frames manually (bypasses rAF, mirrors RunFrames test path).
	if err := b.TickEngineSurface("surface-loop", 3); err != nil {
		t.Fatalf("TickEngineSurface: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Fatalf("post-tick: expected 3 FillRect calls, got %d: %v", len(rec.calls), rec.calls)
	}
	for i, c := range rec.calls {
		if c != "FillRect" {
			t.Errorf("call %d: got %q, want FillRect", i, c)
		}
	}

	b.DisposeEngineSurface("surface-loop")
	if b.EngineSurfaceCount() != 0 {
		t.Errorf("DisposeEngineSurface left instance behind: %d", b.EngineSurfaceCount())
	}
	// After dispose, additional ticks must be a no-op (the receiver dropped
	// the loop closure; ticking a missing id is silently fine).
	if err := b.TickEngineSurface("surface-loop", 1); err != nil {
		t.Fatalf("post-dispose TickEngineSurface returned error: %v", err)
	}
	if len(rec.calls) != 3 {
		t.Errorf("post-dispose tick produced new calls: %d", len(rec.calls))
	}
}

// _ keeps vm imported even when no test refers to it directly. The package is
// pulled in indirectly through surface.NewCanvasFromHostImpl + the bridge
// HostReceiver wiring.
var _ = vm.IntVal
