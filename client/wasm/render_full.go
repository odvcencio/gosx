//go:build js && wasm && !gosx_tiny_islands_only

package main

import (
	"fmt"
	"sync"
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu"
	"github.com/odvcencio/gosx/render/gpu/jsgpu"
)

// vmValueOf wraps a Go primitive into a vm.Value for injection into the
// shared signal store. Matches the type set that client/vm recognizes for
// signal payloads.
func vmValueOf(v any) vm.Value {
	switch t := v.(type) {
	case bool:
		return vm.BoolVal(t)
	case int:
		return vm.IntVal(t)
	case int64:
		return vm.IntVal(int(t))
	case float64:
		return vm.FloatVal(t)
	case string:
		return vm.StringVal(t)
	}
	return vm.StringVal(fmt.Sprint(v))
}

// engineRenderer holds the per-engine GPU resources for the first-party
// render path. One entry per engine ID in engineRendererRegistry.
type engineRenderer struct {
	canvasID string
	device   gpu.Device
	surface  gpu.Surface
	renderer *bundle.Renderer
}

// engineRendererRegistry is a process-wide map of engine ID → renderer. The
// WASM runtime has no concurrency beyond goroutines cooperating on the JS
// event loop, but a mutex keeps the invariants clear.
var (
	engineRendererMu       sync.Mutex
	engineRendererRegistry = make(map[string]*engineRenderer)
)

// registerRenderToCanvasRuntime wires up the new __gosx_render_engine_to_canvas
// JS entry point. Call from main.registerEngineRuntime.
func registerRenderToCanvasRuntime(b *bridge.Bridge) {
	setRuntimeFunc("__gosx_render_engine_to_canvas", renderEngineToCanvasRuntimeFunc(b))
	setRuntimeFunc("__gosx_dispose_engine_renderer", disposeEngineRendererRuntimeFunc())
	setRuntimeFunc("__gosx_pick_engine", pickEngineRuntimeFunc(b))
}

// renderEngineToCanvasRuntimeFunc returns a js.Func exposed as
// __gosx_render_engine_to_canvas(engineID, canvasID, timeSeconds, width, height).
// On first call for a given engine, it opens a WebGPU device bound to the
// canvas and constructs a bundle.Renderer. Subsequent calls reuse both.
//
// Returns js.Null on success, a js.Value error string on failure.
func renderEngineToCanvasRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 5 {
			return jsErrorf("need 5 args (engineID, canvasID, timeSeconds, width, height)")
		}
		engineID := args[0].String()
		canvasID := args[1].String()
		timeSec := args[2].Float()
		width := args[3].Int()
		height := args[4].Int()

		er, err := ensureEngineRenderer(engineID, canvasID)
		if err != nil {
			return jsError(err)
		}
		bundleData, err := b.RenderEngine(engineID, width, height, timeSec)
		if err != nil {
			return jsError(fmt.Errorf("render engine %q: %w", engineID, err))
		}
		if err := er.renderer.Frame(bundleData, width, height, timeSec); err != nil {
			return jsError(fmt.Errorf("frame engine %q: %w", engineID, err))
		}
		return js.Null()
	})
}

// pickEngineRuntimeFunc wires the bundle.Renderer's GPU picking path to a
// JS entry point: __gosx_pick_engine(engineID, x, y, eventType?) returns a
// Promise that resolves with the object ID under the cursor.
//
// ID 0 means "background". Non-zero IDs are per-instance (instance_index+1
// per RenderInstancedMesh) and stable within a frame — the bundle.Renderer
// holds a one-in-flight request policy, so a caller issuing picks faster
// than the GPU readback latency will only get the most recent resolved.
//
// When eventType is one of "hover"/"down"/"select", the bridge also writes
// the matching SceneEventSignals shared signals ($scene.event.<field>) so
// a .gsx component subscribed to those signals rerenders automatically.
// Unknown or empty eventType returns the ID without touching signals.
func pickEngineRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3+ args (engineID, x, y, [eventType])")
		}
		engineID := args[0].String()
		x := args[1].Int()
		y := args[2].Int()
		eventType := ""
		if len(args) >= 4 && args[3].Type() == js.TypeString {
			eventType = args[3].String()
		}

		engineRendererMu.Lock()
		er, ok := engineRendererRegistry[engineID]
		engineRendererMu.Unlock()
		if !ok {
			return jsErrorf(fmt.Sprintf("no renderer for engine %q", engineID))
		}

		promiseCtor := js.Global().Get("Promise")
		return promiseCtor.New(js.FuncOf(func(_ js.Value, pargs []js.Value) any {
			resolve := pargs[0]
			er.renderer.QueuePick(x, y, func(id uint32) {
				// Push the pick result into SceneEventSignals via the shared
				// store, if the caller tagged the event with a known kind.
				pushPickToSignals(b, engineID, eventType, x, y, id)
				resolve.Invoke(float64(id))
			})
			return nil
		}))
	})
}

// pushPickToSignals writes the pick outcome into the SceneEventSignals
// namespace for an engine. Signal names follow the $scene.event convention
// baked into engine/builder.go's DeclareSceneEventSignals.
func pushPickToSignals(b *bridge.Bridge, engineID, eventType string, x, y int, id uint32) {
	if eventType == "" {
		return
	}
	store := b.GetStore()
	if store == nil {
		return
	}
	// Base fields — pointer position + revision + type — always update.
	set := func(name string, value any) {
		store.Set(name, vmValueOf(value))
	}
	set("$scene.event.pointerX", float64(x))
	set("$scene.event.pointerY", float64(y))
	set("$scene.event.type", eventType)
	set("$scene.event.targetIndex", float64(int32(id)-1))
	set("$scene.event.targetID", pickIDToString(id))
	set("$scene.event.revision", float64(nextPickRevision()))

	// Event-type-specific projections.
	switch eventType {
	case "hover":
		set("$scene.event.hovered", id != 0)
		set("$scene.event.hoverIndex", float64(int32(id)-1))
		set("$scene.event.hoverID", pickIDToString(id))
	case "down":
		set("$scene.event.down", id != 0)
		set("$scene.event.downIndex", float64(int32(id)-1))
		set("$scene.event.downID", pickIDToString(id))
	case "select":
		set("$scene.event.selected", id != 0)
		set("$scene.event.selectedIndex", float64(int32(id)-1))
		set("$scene.event.selectedID", pickIDToString(id))
	case "click":
		// A click is (down + release) in the same spot. Increment the click
		// counter so computed signals that watch clickCount can run their
		// handlers. SelectedID follows click for Figma-style single-select.
		set("$scene.event.clickCount", float64(nextClickCount(engineID)))
		set("$scene.event.selected", id != 0)
		set("$scene.event.selectedIndex", float64(int32(id)-1))
		set("$scene.event.selectedID", pickIDToString(id))
	}
}

// pickIDToString renders an integer pick ID as a string the engine runtime
// can compare to RenderInstancedMesh.ID. Uses the same decimal form the
// engine uses internally.
func pickIDToString(id uint32) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

// Per-engine click counter + global pick revision — both live in the
// pickAnalytics map so the signals mirror what the JS renderer used to
// emit.
var (
	pickAnalyticsMu  sync.Mutex
	pickAnalytics    = make(map[string]uint64)
	pickRevisionSeq  uint64
)

func nextPickRevision() uint64 {
	pickAnalyticsMu.Lock()
	defer pickAnalyticsMu.Unlock()
	pickRevisionSeq++
	return pickRevisionSeq
}

func nextClickCount(engineID string) uint64 {
	pickAnalyticsMu.Lock()
	defer pickAnalyticsMu.Unlock()
	pickAnalytics[engineID]++
	return pickAnalytics[engineID]
}

// disposeEngineRendererRuntimeFunc exposes a JS entry point for explicitly
// tearing down the renderer attached to an engine. Safe to call on an unknown
// engine ID (no-op).
func disposeEngineRendererRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		disposeEngineRenderer(args[0].String())
		return js.Null()
	})
}

// ensureEngineRenderer lazily constructs the renderer for engineID bound to
// canvasID. If a renderer already exists for engineID and the canvasID has
// changed, the old one is disposed first — sites that remount engines should
// usually dispose explicitly, but this covers the accidental-remount case.
func ensureEngineRenderer(engineID, canvasID string) (*engineRenderer, error) {
	engineRendererMu.Lock()
	defer engineRendererMu.Unlock()

	if er, ok := engineRendererRegistry[engineID]; ok {
		if er.canvasID == canvasID {
			return er, nil
		}
		// Canvas mismatch — tear down and rebuild.
		er.renderer.Destroy()
		er.device.Destroy()
		delete(engineRendererRegistry, engineID)
	}

	device, surface, err := jsgpu.Open(canvasID)
	if err != nil {
		return nil, fmt.Errorf("open webgpu on canvas %q: %w", canvasID, err)
	}
	r, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		device.Destroy()
		return nil, fmt.Errorf("new renderer for engine %q: %w", engineID, err)
	}

	er := &engineRenderer{
		canvasID: canvasID,
		device:   device,
		surface:  surface,
		renderer: r,
	}
	engineRendererRegistry[engineID] = er
	return er, nil
}

// disposeEngineRenderer releases the renderer attached to an engine, if any.
// Idempotent.
func disposeEngineRenderer(engineID string) {
	engineRendererMu.Lock()
	defer engineRendererMu.Unlock()
	er, ok := engineRendererRegistry[engineID]
	if !ok {
		return
	}
	delete(engineRendererRegistry, engineID)
	er.renderer.Destroy()
	er.device.Destroy()
}
