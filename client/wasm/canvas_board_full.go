//go:build js && wasm && !gosx_tiny_islands_only

// Canvas2D paint-loop JS bridge — the missing wire between a hydrated
// gosx.CanvasBoard (a <canvas data-gosx-surface-kind="canvas2d"> placeholder
// the unified __gosx_hydrate dispatcher turns into a vm.CanvasBoardAdapter)
// and the JS canvas2d render loop. The board hydrates through __gosx_hydrate
// already (Phase 1d/2 dispatcher → bridge.HydrateReconciler → CanvasBoardAdapter);
// this file exposes the per-frame surface the JS bootstrap drives:
//
//   - __gosx_render_canvas(id, width, height, timeSeconds) → RenderBundle JSON.
//     The JS painter (bootstrap-src/26b1-canvas2d-painter.js) parses this and
//     replays it onto the canvas's 2D context using the OrthoCamera2D screen
//     transform. The bundle is marshaled with the same MarshalEngineRenderBundle
//     helper the scene3d render path uses (engine_full.go), so the wire shape
//     matches engine.RenderBundle's JSON tags exactly.
//
//   - __gosx_tick_canvas(id) reconciles the board adapter (signal-driven dirty
//     tracking) ahead of the render. Commands are dropped on the floor here —
//     the canvas2d paint path is bundle-driven, not command-driven — but the
//     tick keeps the adapter's resolved-node snapshot current.
//
//   - __gosx_dispose_canvas(id) tears the adapter down (MutationObserver /
//     navigation teardown). Idempotent; unknown ids are no-ops.
//
// Mirrors engine_surface_full.go's registration shape. Pairs with
// canvas_board_islands_only.go, the elision stub the tiny build uses to keep
// the canvas2d adapter (and render/bundle) out of the islands-only binary.

package main

import (
	"syscall/js"

	"m31labs.dev/gosx/client/bridge"
)

func registerCanvasBoardRuntime(b *bridge.Bridge) {
	setRuntimeFunc("__gosx_tick_canvas", tickCanvasFunc(b))
	setRuntimeFunc("__gosx_render_canvas", renderCanvasFunc(b))
	setRuntimeFunc("__gosx_canvas_event", canvasEventFunc(b))
	setRuntimeFunc("__gosx_dispose_canvas", disposeCanvasFunc(b))
}

// canvasEventFunc routes a single board interaction event (pan / zoom / pick)
// into the named board's adapter via Bridge.CanvasBoardEvent. Pan and zoom
// mutate the adapter's runtime camera so the next __gosx_render_canvas frame
// paints the new view; pick hit-tests through the camera and writes the result
// into $surface.event.* (ADR 0007). Mirrors __gosx_dispatch_engine_surface_event
// for the canvas2d surface — same (id, kind, floats, payloadStr) shape — so the
// JS bootstrap's canvas2d branch wires its DOM listeners the same way the
// engine-surface branch does.
//
// Call shape:
//
//	__gosx_canvas_event(id, kind, floats, payloadStr)
//
// kind is the integer CanvasBoardEventKind (1=pan, 2=zoom, 3=pick). floats is a
// Float64Array carrying the kind-specific numeric payload; payloadStr is
// reserved (pass ""). Returns null on success, an error string on failure
// (including unknown id) — cheap to call from a high-frequency pointermove.
func canvasEventFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3+ args (id, kind, floats, [payloadStr])")
		}
		id := args[0].String()
		kind := bridge.CanvasBoardEventKind(args[1].Int())
		floats := decodeFloat64Array(args[2])
		payloadStr := ""
		if len(args) >= 4 && args[3].Type() == js.TypeString {
			payloadStr = args[3].String()
		}
		if err := b.CanvasBoardEvent(id, kind, floats, payloadStr); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// tickCanvasFunc reconciles a board adapter so its resolved-node snapshot
// reflects the latest signal writes before the next render. The canvas2d
// paint path does not consume the returned commands (it re-renders the whole
// bundle each frame), so this returns null on success and an error string on
// failure — cheap to call every rAF tick.
//
// Call shape: __gosx_tick_canvas(id)
func tickCanvasFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (id)")
		}
		if _, err := b.TickCanvasBoard(args[0].String()); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

// renderCanvasFunc builds a 2D-mode RenderBundle for the board and returns it
// as a JSON string. The JS painter parses the string and draws it.
//
// Call shape: __gosx_render_canvas(id, width, height, timeSeconds)
func renderCanvasFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return jsErrorf("need 4 args (id, width, height, timeSeconds)")
		}
		bundleData, err := b.RenderCanvasBoard(
			args[0].String(),
			args[1].Int(),
			args[2].Int(),
			args[3].Float(),
		)
		if err != nil {
			return jsError(err)
		}
		bundleJSON, err := bridge.MarshalEngineRenderBundle(bundleData)
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(bundleJSON)
	})
}

// disposeCanvasFunc tears down a board adapter. Idempotent; unknown ids are
// silent no-ops (matching the engine-surface dispose race tolerance).
//
// Call shape: __gosx_dispose_canvas(id)
func disposeCanvasFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeCanvasBoard(args[0].String())
		return js.Null()
	})
}
