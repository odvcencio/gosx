//go:build js && wasm && !gosx_tiny_islands_only

package main

import (
	"fmt"
	"sync"
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/odvcencio/gosx/render/gpu"
	"github.com/odvcencio/gosx/render/gpu/jsgpu"
)

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
