//go:build js && wasm

package main

import (
	"errors"
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
)

func registerRuntime(b *bridge.Bridge) {
	b.SetSharedSignalCallback(func(name, valueJSON string) {
		notify := js.Global().Get("__gosx_notify_shared_signal")
		if notify.Type() == js.TypeFunction {
			notify.Invoke(name, valueJSON)
		}
	})
	b.SetPatchCallback(func(islandID, patchJSON string) {
		js.Global().Call("__gosx_apply_patches", islandID, patchJSON)
	})
	setRuntimeFunc("__gosx_hydrate", hydrateRuntimeFunc(b))
	setRuntimeFunc("__gosx_action", actionRuntimeFunc(b))
	setRuntimeFunc("__gosx_dispose", disposeRuntimeFunc(b))
	registerEngineRuntime(b)
	registerHighlightRuntime()
	setRuntimeFunc("__gosx_set_shared_signal", sharedSignalRuntimeFunc(b))
	setRuntimeFunc("__gosx_get_shared_signal", sharedSignalGetRuntimeFunc(b))
	setRuntimeFunc("__gosx_set_input_batch", inputBatchRuntimeFunc(b))
	registerTextLayoutRuntime()
	registerCRDTRuntime()
}

func setRuntimeFunc(name string, fn js.Func) {
	js.Global().Set(name, fn)
}

func hydrateRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		call, err := parseHydrateCall(args)
		if err != nil {
			return jsError(err)
		}
		if err := b.HydrateIsland(call.islandID, call.componentName, call.propsJSON, call.programData, call.format); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func actionRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return js.Null()
		}
		patches, err := b.DispatchAction(args[0].String(), args[1].String(), args[2].String())
		if err != nil {
			logRuntimeError("dispatch error", err)
			return jsError(err)
		}
		return applyPatchedResult(args[0].String(), patches)
	})
}

func disposeRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeIsland(args[0].String())
		return js.Null()
	})
}

func sharedSignalRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (signalName, valueJSON)")
		}
		if err := b.SetSharedSignalJSON(args[0].String(), args[1].String()); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func sharedSignalGetRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (signalName)")
		}
		valueJSON, err := b.GetSharedSignalJSON(args[0].String())
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(valueJSON)
	})
}

func inputBatchRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (batchJSON)")
		}
		batchJSON, err := normalizeJSONArg(args[0], "{}")
		if err != nil {
			return jsError(err)
		}
		if err := b.SetSharedSignalBatchJSON(batchJSON); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

type hydrateCall struct {
	islandID      string
	componentName string
	propsJSON     string
	programData   []byte
	format        string
}

func parseHydrateCall(args []js.Value) (hydrateCall, error) {
	if len(args) < 5 {
		return hydrateCall{}, errors.New("need 5 args (islandID, componentName, propsJSON, programData, format)")
	}
	programData, err := decodeProgramData(args[3])
	if err != nil {
		return hydrateCall{}, err
	}
	return hydrateCall{
		islandID:      args[0].String(),
		componentName: args[1].String(),
		propsJSON:     args[2].String(),
		programData:   programData,
		format:        args[4].String(),
	}, nil
}

func decodeProgramData(value js.Value) ([]byte, error) {
	if value.Type() == js.TypeString {
		return []byte(value.String()), nil
	}
	uint8Array := normalizeUint8Array(value)
	if uint8Array.IsUndefined() || uint8Array.IsNull() {
		return nil, errors.New("programData must be a string, Uint8Array, or ArrayBuffer")
	}
	length := uint8Array.Get("length").Int()
	programData := make([]byte, length)
	js.CopyBytesToGo(programData, uint8Array)
	return programData, nil
}

func normalizeUint8Array(value js.Value) js.Value {
	if value.InstanceOf(js.Global().Get("ArrayBuffer")) {
		return js.Global().Get("Uint8Array").New(value)
	}
	return value
}

func normalizeJSONArg(value js.Value, fallback string) (string, error) {
	switch value.Type() {
	case js.TypeUndefined, js.TypeNull:
		return fallback, nil
	case js.TypeString:
		return value.String(), nil
	default:
		jsonGlobal := js.Global().Get("JSON")
		if !jsonGlobal.Truthy() {
			return "", errors.New("JSON global not available")
		}
		out := jsonGlobal.Call("stringify", value)
		if out.Type() != js.TypeString {
			return "", errors.New("argument must be JSON-serializable")
		}
		return out.String(), nil
	}
}

func applyPatchedResult(islandID string, patches []vm.PatchOp) any {
	if len(patches) == 0 {
		return js.ValueOf(0)
	}
	patchJSON, err := bridge.MarshalPatches(patches)
	if err != nil {
		return js.ValueOf("marshal:" + err.Error())
	}
	js.Global().Call("__gosx_apply_patches", islandID, patchJSON)
	return js.ValueOf(len(patches))
}

func logRuntimeError(prefix string, err error) {
	js.Global().Get("console").Call("error", "[gosx/wasm] "+prefix+":", err.Error())
}

func jsError(err error) js.Value {
	if err == nil {
		return js.Null()
	}
	return js.ValueOf("error: " + err.Error())
}

func jsErrorf(message string) js.Value {
	return js.ValueOf("error: " + message)
}

func notifyRuntimeReady() {
	readyFn := js.Global().Get("__gosx_runtime_ready")
	if readyFn.Truthy() {
		readyFn.Invoke()
	}
}

func main() {
	b := bridge.New()
	registerRuntime(b)
	notifyRuntimeReady()

	// Block forever — WASM must not exit
	select {}
}
