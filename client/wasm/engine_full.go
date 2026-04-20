//go:build js && wasm && !gosx_tiny_islands_only

package main

import (
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	rootengine "github.com/odvcencio/gosx/engine"
)

func registerEngineRuntime(b *bridge.Bridge) {
	setRuntimeFunc("__gosx_hydrate_engine", hydrateEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_tick_engine", tickEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_render_engine", renderEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_engine_dispose", disposeEngineRuntimeFunc(b))
	// First-party GPU path: renders the engine's bundle into a canvas via
	// render/bundle, bypassing the JS scene renderer.
	registerRenderToCanvasRuntime(b)
}

func hydrateEngineRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		call, err := parseHydrateCall(args)
		if err != nil {
			return jsError(err)
		}
		commands, err := b.HydrateEngine(call.islandID, call.componentName, call.propsJSON, call.programData, call.format)
		if err != nil {
			return jsError(err)
		}
		return marshalEngineCommandResult(commands)
	})
}

func tickEngineRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (engineID)")
		}
		commands, err := b.TickEngine(args[0].String())
		if err != nil {
			return jsError(err)
		}
		return marshalEngineCommandResult(commands)
	})
}

func renderEngineRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 4 {
			return jsErrorf("need 4 args (engineID, timeSeconds, width, height)")
		}
		bundle, err := b.RenderEngine(
			args[0].String(),
			args[2].Int(),
			args[3].Int(),
			args[1].Float(),
		)
		if err != nil {
			return jsError(err)
		}
		bundleJSON, err := bridge.MarshalEngineRenderBundle(bundle)
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(bundleJSON)
	})
}

func disposeEngineRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		engineID := args[0].String()
		// Tear down the first-party renderer if this engine was using it.
		// No-op when the engine rendered through the JS path.
		disposeEngineRenderer(engineID)
		b.DisposeEngine(engineID)
		return js.Null()
	})
}

func marshalEngineCommandResult(commands []rootengine.Command) any {
	if len(commands) == 0 {
		return js.ValueOf("[]")
	}
	commandJSON, err := bridge.MarshalEngineCommands(commands)
	if err != nil {
		return js.ValueOf("marshal:" + err.Error())
	}
	return js.ValueOf(commandJSON)
}
