//go:build js && wasm && !gosx_tiny_islands_only

// Engine-surface JS bridge — the missing wire between the server-side
// data-gosx-engine-bytecode attribute and the Bridge.HydrateEngineSurface
// path. The JS bootstrap (client/js/bootstrap-src/26b-feature-engines-prefix.js)
// fetches the bytecode JSON the placeholder references, then calls
// __gosx_hydrate_engine_surface to spin up the in-VM CanvasHostReceiver
// against the surface's <canvas> element. From there:
//
//  - JS DOM event listeners route through __gosx_dispatch_engine_surface_event
//    (kind + Float64Array payload + optional string), which lands in
//    Bridge.DispatchEngineSurfaceEvent.
//
//  - rAF ticks route through __gosx_tick_engine_surface, which lands in
//    Bridge.TickEngineSurface(id, 1). The receiver's RunFrames is the
//    canonical entry — it invokes the loop closure that c.StartLoop(...)
//    captured during Mount.
//
//  - MutationObserver / navigation teardown calls __gosx_dispose_engine_surface,
//    which lands in Bridge.DisposeEngineSurface (drops the loop closure
//    so the captured VM frame is GC-eligible).
//
// Pairs with engine_surface_islands_only.go, which is the elision stub
// the tiny build uses to keep the engine/surface dependency out.

package main

import (
	"syscall/js"

	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/engine/surface"
)

func registerEngineSurfaceRuntime(b *bridge.Bridge) {
	setRuntimeFunc("__gosx_hydrate_engine_surface", hydrateEngineSurfaceFunc(b))
	setRuntimeFunc("__gosx_dispatch_engine_surface_event", dispatchEngineSurfaceEventFunc(b))
	setRuntimeFunc("__gosx_tick_engine_surface", tickEngineSurfaceFunc(b))
	setRuntimeFunc("__gosx_dispose_engine_surface", disposeEngineSurfaceFunc(b))
}

// hydrateEngineSurfaceFunc parses the JS call shape and forwards into
// the bridge.
//
// Call shape (from JS):
//
//	__gosx_hydrate_engine_surface(id, componentName, propsJSON, programData, format, canvasEl)
//
// programData is normalized exactly like the other hydrate dispatchers
// (string | Uint8Array | ArrayBuffer). canvasEl is the live
// HTMLCanvasElement; we wrap it via surface.NewJSCanvas, which gives the
// CanvasHostReceiver a real CanvasRenderingContext2D to draw against
// (clearRect / fillRect / arc / stroke / fill / …).
func hydrateEngineSurfaceFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 6 {
			return jsErrorf("need 6 args (id, componentName, propsJSON, programData, format, canvasEl)")
		}
		id := args[0].String()
		componentName := args[1].String()
		propsJSON := args[2].String()
		programData, err := decodeProgramData(args[3])
		if err != nil {
			return jsError(err)
		}
		format := args[4].String()
		canvasEl := args[5]
		canvas := surface.NewJSCanvas(canvasEl)
		if err := b.HydrateEngineSurface(id, componentName, propsJSON, programData, format, canvas); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// dispatchEngineSurfaceEventFunc routes a single DOM event into the
// surface's matching handler. The JS bootstrap calls this from each
// DOM listener with the same (kind, payloadFloats, payloadStr) shape
// the legacy __gosx_surface_event used — so the existing listener
// wiring carries over with minimal change.
//
// Call shape:
//
//	__gosx_dispatch_engine_surface_event(id, kind, payloadFloats, payloadStr)
//
// payloadFloats is a Float64Array (or null/undefined when an event
// has no float payload). payloadStr is the keyboard "key\tcode"
// composite string, or "" for non-keyboard events.
func dispatchEngineSurfaceEventFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return jsErrorf("need 4 args (id, kind, payloadFloats, payloadStr)")
		}
		id := args[0].String()
		kind := bridge.EngineSurfaceEventKind(args[1].Int())
		floats := decodeFloat64Array(args[2])
		payloadStr := args[3].String()
		if err := b.DispatchEngineSurfaceEvent(id, kind, floats, payloadStr); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// tickEngineSurfaceFunc drives one (or more) rAF frames against the
// surface's bound CanvasHostReceiver. The default n=1 reflects the
// natural rAF cadence; callers that want to batch frames (e.g. a fast
// catchup after a tab regains focus) can pass a larger n. Unknown ids
// are silent no-ops by design — see DisposeEngineSurface's docstring
// for the race tolerance contract.
//
// Call shape:
//
//	__gosx_tick_engine_surface(id, n?)
//
// n defaults to 1 when omitted.
func tickEngineSurfaceFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (id, [n])")
		}
		id := args[0].String()
		n := 1
		if len(args) >= 2 && args[1].Type() == js.TypeNumber {
			if v := args[1].Int(); v > 0 {
				n = v
			}
		}
		if err := b.TickEngineSurface(id, n); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// disposeEngineSurfaceFunc tears down the surface instance — drops the
// loop closure (so the captured Mount frame is GC-eligible) and
// removes the bridge entry.
//
// Call shape:
//
//	__gosx_dispose_engine_surface(id)
func disposeEngineSurfaceFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeEngineSurface(args[0].String())
		return js.Null()
	})
}

// decodeFloat64Array converts a JS Float64Array (or array-like) into a
// Go []float64. Null/undefined values yield nil so callers don't need
// to special-case payload-less events.
func decodeFloat64Array(v js.Value) []float64 {
	if v.IsNull() || v.IsUndefined() {
		return nil
	}
	if v.Type() != js.TypeObject {
		return nil
	}
	lengthVal := v.Get("length")
	if lengthVal.Type() != js.TypeNumber {
		return nil
	}
	n := lengthVal.Int()
	if n <= 0 {
		return nil
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = v.Index(i).Float()
	}
	return out
}
