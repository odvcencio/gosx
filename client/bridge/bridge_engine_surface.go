//go:build !gosx_tiny_islands_only

// Engine-surface hydration — Bridge entry point.
//
// This is the WASM-resident peer of larch's CanvasHostReceiver bridge
// (engine/surface/vm_host.go from gosx v0.22.1, PR #18). The receiver
// wraps a *surface.Canvas as a vm.HostReceiver so bytecode handlers can
// call canvas methods (FillRect, Arc, …) and register the
// c.StartLoop(func(dt){…}) animation loop closure. What was missing
// until this file landed: a WASM-side entry point that:
//
//  1. Decodes a Program JSON the JS bootstrap fetched from
//     data-gosx-engine-bytecode (surface.Renderer.Mount emits this).
//  2. Constructs a fresh vm.VM bound to that Program.
//  3. Wires the SignalDefs into reactive signals (Y.B/Y.C local-write +
//     OpAssign reads/writes through the signal layer).
//  4. Binds a CanvasHostReceiver under "c" — the convention every
//     `//gosx:engine surface` author writes against.
//  5. Invokes the Mount handler so package-var seeding, props decode,
//     and c.StartLoop(...) registration run end-to-end.
//
// After Mount returns the instance is parked in the bridge map; rAF
// ticks (driven by JS — see __gosx_tick_engine_surface in
// client/wasm/engine_surface_full.go) call TickEngineSurface, which
// hands off to the receiver's RunFrames; DOM events
// (data-gosx-on-click etc.) route through DispatchEngineSurfaceEvent,
// which sets ev.X / ev.Y / ev.button / … props on the VM and invokes
// the matching handler (OnClick, OnPointerDown, OnResize, …).
//
// Disposal mirrors the receiver contract: DisposeEngineSurface calls
// recv.Dispose() (drops the loop closure, clears canvas.stepFn so a
// stale rAF callback no-ops) and removes the entry from the bridge map.
//
// This file is gated by `!gosx_tiny_islands_only` because the
// engine/surface dependency (and the CanvasBoardAdapter dependency it
// shares with hydrateCanvas2D) is non-trivial. The islands-only stub
// at bridge_engine_surface_islands.go records the call shape but
// returns the same "not available" error every other engine-surface
// path returns in the tiny build.

package bridge

import (
	"fmt"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/engine/surface"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

// EngineSurfaceEventKind identifies which handler the dispatcher should
// invoke when DispatchEngineSurfaceEvent fires. The integer values are
// pinned to the JS bootstrap protocol (engine/surface/runtime/runtime.go
// table) so the two sides stay in lockstep without a separate enum.
type EngineSurfaceEventKind int

const (
	EngineSurfaceEventMount         EngineSurfaceEventKind = 0
	EngineSurfaceEventClick         EngineSurfaceEventKind = 1
	EngineSurfaceEventDblClick      EngineSurfaceEventKind = 2
	EngineSurfaceEventPointerDown   EngineSurfaceEventKind = 3
	EngineSurfaceEventPointerMove   EngineSurfaceEventKind = 4
	EngineSurfaceEventPointerUp     EngineSurfaceEventKind = 5
	EngineSurfaceEventPointerCancel EngineSurfaceEventKind = 6
	EngineSurfaceEventWheel         EngineSurfaceEventKind = 7
	EngineSurfaceEventKeyDown       EngineSurfaceEventKind = 8
	EngineSurfaceEventKeyUp         EngineSurfaceEventKind = 9
	EngineSurfaceEventResize        EngineSurfaceEventKind = 10
	EngineSurfaceEventDispose       EngineSurfaceEventKind = 11
)

// engineSurfaceHandlerName returns the canonical Go handler name a
// .gsx + companion-Go author writes for an event kind. Kept aligned
// with the //gosx:engine surface authoring contract documented in
// engine/surface/surface.go's package doc.
func engineSurfaceHandlerName(k EngineSurfaceEventKind) string {
	switch k {
	case EngineSurfaceEventMount:
		return "Mount"
	case EngineSurfaceEventClick:
		return "OnClick"
	case EngineSurfaceEventDblClick:
		return "OnDblClick"
	case EngineSurfaceEventPointerDown:
		return "OnPointerDown"
	case EngineSurfaceEventPointerMove:
		return "OnPointerMove"
	case EngineSurfaceEventPointerUp:
		return "OnPointerUp"
	case EngineSurfaceEventPointerCancel:
		return "OnPointerCancel"
	case EngineSurfaceEventWheel:
		return "OnWheel"
	case EngineSurfaceEventKeyDown:
		return "OnKeyDown"
	case EngineSurfaceEventKeyUp:
		return "OnKeyUp"
	case EngineSurfaceEventResize:
		return "OnResize"
	case EngineSurfaceEventDispose:
		return "OnDispose"
	}
	return ""
}

// engineSurfaceInstance bundles the live state of one mounted surface.
// Stored in Bridge.engineSurfaces, keyed by the placeholder id the JS
// bootstrap minted (typically "gosx-engine-surface-<n>").
type engineSurfaceInstance struct {
	machine *vm.VM
	canvas  *surface.Canvas
	recv    *surface.CanvasHostReceiver
	prog    *program.Program

	// handlerByName maps "Mount" / "OnClick" / … to the handler's first
	// expression id. We compute this once at hydrate time so event
	// dispatch is a single map probe; absent handlers report no-op.
	handlerByName map[string]program.ExprID
}

// HydrateEngineSurface decodes a bytecode program for an engine surface
// component, spins up a VM, binds the canvas host receiver, and runs the
// Mount handler. After this returns successfully the instance is parked
// under id; subsequent DispatchEngineSurfaceEvent / TickEngineSurface /
// DisposeEngineSurface calls find it via the same key.
//
// The canvas argument is supplied by the JS bootstrap (via
// surface.NewJSCanvas wrapping the <canvas> element); host tests pass a
// surface.NewCanvasFromHostImpl-built recorder.
//
// Re-hydrating the same id silently disposes the prior instance — this
// matches HydrateIsland's "first dispose, then build fresh" pattern so
// pages that re-render their surface placeholder don't leak VM
// references.
func (b *Bridge) HydrateEngineSurface(id, componentName, propsJSON string, programData []byte, format string, canvas *surface.Canvas) error {
	prog, err := decodeEngineSurfaceProgram(programData, format)
	if err != nil {
		return fmt.Errorf("decode engine surface program %q: %w", componentName, err)
	}

	if existing, ok := b.engineSurfaces[id]; ok {
		if inst, ok := existing.(*engineSurfaceInstance); ok {
			inst.recv.Dispose()
		}
		delete(b.engineSurfaces, id)
	}

	machine := vm.NewVM(prog, nil)

	// Initialize package-level signals from their Init exprs. This is the
	// equivalent of how vm.NewIsland constructs signals — we keep the
	// shape narrow (no shared-signal wiring yet) because engine-surface
	// authors today consume signals as in-VM mutable package vars per
	// the Y.D / Y.E / Y.F / Y.G lowering convention.
	initializeEngineSurfaceSignals(machine, prog)

	recv := surface.NewCanvasHostReceiver(machine, canvas)
	machine.BindHost("c", recv)

	// "ctx" is the other half of the surface author contract. The
	// ContextHostReceiver decodes propsJSON into the surface's typed
	// props struct via OpHostCall("ctx.PropsInto", [&props]) — Y.G's
	// eager struct zero-init guarantees props.Fields is non-nil at the
	// call site, and Y.C's in-place mutation propagates the writes
	// back to the handler's local.
	ctxRecv := surface.NewContextHostReceiver([]byte(propsJSON))
	machine.BindHost("ctx", ctxRecv)

	inst := &engineSurfaceInstance{
		machine:       machine,
		canvas:        canvas,
		recv:          recv,
		prog:          prog,
		handlerByName: indexEngineSurfaceHandlers(prog),
	}
	b.engineSurfaces[id] = inst

	// Invoke Mount with a fresh frame so OpLocalDecl / OpAssign land in
	// per-handler locals (per X.A's EvalWithFrame contract). Missing
	// Mount handler is OK — some surfaces only react to events.
	if mountID, ok := inst.handlerByName["Mount"]; ok {
		machine.EvalWithFrame(mountID)
	}

	return nil
}

// DispatchEngineSurfaceEvent routes an event into the surface's handler
// of the matching kind. The float payload + string payload arrive from
// JS (DOM event listeners) in the same shape engine/surface/runtime's
// __gosx_surface_event uses, so the JS side does not need to translate
// per-event-kind.
//
// Event coords land in props named "ev.X" / "ev.Y" / "ev.button" / … so
// handlers wired by the lowerer can read them via OpPropGet. The naming
// matches what Y.E's `ev.X` + `ev.Y` lowering already produces.
//
// Missing instance / missing handler are silent no-ops — surfaces are
// allowed to omit any handler they don't need (per the Surface struct's
// optional-fields contract).
func (b *Bridge) DispatchEngineSurfaceEvent(id string, kind EngineSurfaceEventKind, floats []float64, payloadStr string) error {
	inst, ok := lookupEngineSurfaceInstance(b, id)
	if !ok {
		return nil
	}

	handlerName := engineSurfaceHandlerName(kind)
	if handlerName == "" {
		return nil
	}
	exprID, ok := inst.handlerByName[handlerName]
	if !ok {
		return nil
	}

	prev := stageEngineSurfaceEventProps(inst.machine, kind, floats, payloadStr)
	defer restoreEngineSurfaceProps(inst.machine, prev)

	inst.machine.EvalWithFrame(exprID)
	return nil
}

func lookupEngineSurfaceInstance(b *Bridge, id string) (*engineSurfaceInstance, bool) {
	entry, ok := b.engineSurfaces[id]
	if !ok {
		return nil, false
	}
	inst, ok := entry.(*engineSurfaceInstance)
	return inst, ok
}

// TickEngineSurface drives n animation frames against the surface's
// CanvasHostReceiver (which forwards into the receiver's stored loop
// closure via vm.InvokeClosure). The frame budget mirrors
// CanvasHostReceiver.RunFrames — the host-side test harness uses this
// same entry to drive deterministic frame counts; the WASM-side rAF
// loop calls it with n=1 once per browser frame.
//
// Returns nil even when id is unknown so the JS side can call this
// freely after disposal without checking; the rAF callback often
// outlives the explicit dispose path.
func (b *Bridge) TickEngineSurface(id string, n int) error {
	inst, ok := lookupEngineSurfaceInstance(b, id)
	if !ok {
		return nil
	}
	inst.recv.RunFrames(n)
	return nil
}

// DisposeEngineSurface drops the loop closure (so the captured Mount
// frame is eligible for GC), clears the canvas step fn (so any pending
// rAF callback no-ops), and removes the instance from the bridge map.
// Idempotent — calling twice on the same id is safe.
func (b *Bridge) DisposeEngineSurface(id string) {
	inst, ok := lookupEngineSurfaceInstance(b, id)
	if !ok {
		return
	}
	inst.recv.Dispose()
	delete(b.engineSurfaces, id)
}

// EngineSurfaceCount reports the number of live engine-surface
// instances. Exposed primarily for tests; production callers don't
// need it.
func (b *Bridge) EngineSurfaceCount() int {
	return len(b.engineSurfaces)
}

// decodeEngineSurfaceProgram parses the program emitted by
// engine/surface.LowerToBytecode. Format mirrors HydrateIsland's
// accepted set (json + bin) with "" defaulting to json. Surface kind
// is set to Canvas2D per ADR 0003 — every engine surface today is a
// Canvas2D root. Future Scene3D-root surfaces will bump the
// discriminator.
func decodeEngineSurfaceProgram(data []byte, format string) (*program.Program, error) {
	if format == "" {
		format = "json"
	}
	prog, err := DecodeProgram(data, format)
	if err != nil {
		return nil, err
	}
	prog.Surface = program.SurfaceCanvas2D
	return prog, nil
}

// indexEngineSurfaceHandlers builds the "handler-name → first ExprID"
// lookup used by DispatchEngineSurfaceEvent. Each Handler.Body is a
// single OpSeq id per the golower convention (see
// ir/golower/decl.go:lowerFuncDecl); we index Body[0].
func indexEngineSurfaceHandlers(prog *program.Program) map[string]program.ExprID {
	out := make(map[string]program.ExprID, len(prog.Handlers))
	for _, h := range prog.Handlers {
		if len(h.Body) == 0 {
			continue
		}
		out[h.Name] = h.Body[0]
	}
	return out
}

// engineSurfacePropSnapshot records the prior value (or absence) of a
// VM prop slot so a per-event staging pass can restore exactly the
// caller-visible state after the handler returns.
type engineSurfacePropSnapshot struct {
	name    string
	value   vm.Value
	present bool
}

// stageEngineSurfaceEventProps writes the per-kind event coords/keys
// into the VM's prop map and returns a slice of snapshots the caller
// must hand back to restoreEngineSurfaceProps. Names mirror what the
// lowerer emits for `ev.X` / `ev.Y` / … field reads on the
// surface.PointerEvent / WheelEvent / KeyEvent / ResizeEvent struct
// fields (see engine/surface/surface.go).
func stageEngineSurfaceEventProps(machine *vm.VM, kind EngineSurfaceEventKind, floats []float64, payloadStr string) []engineSurfacePropSnapshot {
	type assignment struct {
		name  string
		value vm.Value
	}
	var assignments []assignment
	switch kind {
	case EngineSurfaceEventClick,
		EngineSurfaceEventDblClick,
		EngineSurfaceEventPointerDown,
		EngineSurfaceEventPointerMove,
		EngineSurfaceEventPointerUp,
		EngineSurfaceEventPointerCancel:
		// [x, y, button, buttons, modifier]
		assignments = []assignment{
			{"ev.X", floatAt(floats, 0)},
			{"ev.Y", floatAt(floats, 1)},
			{"ev.Button", floatAt(floats, 2)},
			{"ev.Buttons", floatAt(floats, 3)},
			{"ev.Modifier", floatAt(floats, 4)},
		}
	case EngineSurfaceEventWheel:
		// [x, y, deltaX, deltaY, modifier]
		assignments = []assignment{
			{"ev.X", floatAt(floats, 0)},
			{"ev.Y", floatAt(floats, 1)},
			{"ev.DeltaX", floatAt(floats, 2)},
			{"ev.DeltaY", floatAt(floats, 3)},
			{"ev.Modifier", floatAt(floats, 4)},
		}
	case EngineSurfaceEventKeyDown, EngineSurfaceEventKeyUp:
		// [modifier]; payloadStr is "key\tcode"
		key, code := splitKeyPayload(payloadStr)
		assignments = []assignment{
			{"ev.Key", vm.StringVal(key)},
			{"ev.Code", vm.StringVal(code)},
			{"ev.Modifier", floatAt(floats, 0)},
		}
	case EngineSurfaceEventResize:
		// [width, height, dpr]
		assignments = []assignment{
			{"ev.Width", floatAt(floats, 0)},
			{"ev.Height", floatAt(floats, 1)},
			{"ev.DPR", floatAt(floats, 2)},
		}
	}

	snapshots := make([]engineSurfacePropSnapshot, 0, len(assignments))
	for _, a := range assignments {
		prev, ok := machine.GetProp(a.name)
		snapshots = append(snapshots, engineSurfacePropSnapshot{name: a.name, value: prev, present: ok})
		machine.SetProp(a.name, a.value)
	}
	return snapshots
}

func restoreEngineSurfaceProps(machine *vm.VM, snapshots []engineSurfacePropSnapshot) {
	for _, s := range snapshots {
		if s.present {
			machine.SetProp(s.name, s.value)
			continue
		}
		machine.DeleteProp(s.name)
	}
}

func floatAt(xs []float64, i int) vm.Value {
	if i < 0 || i >= len(xs) {
		return vm.FloatVal(0)
	}
	return vm.FloatVal(xs[i])
}

func splitKeyPayload(s string) (key, code string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// initializeEngineSurfaceSignals registers each SignalDef as a fresh
// signal on the VM, seeded with the eval'd Init expression. This is
// what makes `gName = props.Name` in a Mount handler actually persist
// across handler invocations.
func initializeEngineSurfaceSignals(machine *vm.VM, prog *program.Program) {
	for _, def := range prog.Signals {
		initVal := machine.Eval(def.Init)
		machine.SetSignal(def.Name, signal.New(initVal))
	}
}
