//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/crdt"
	rootengine "github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/highlight"
	"github.com/odvcencio/gosx/textlayout"
)

func registerRuntime(b *bridge.Bridge) {
	crdtBridge := bridge.NewCRDTBridge()
	b.SetPatchCallback(func(islandID, patchJSON string) {
		js.Global().Call("__gosx_apply_patches", islandID, patchJSON)
	})
	setRuntimeFunc("__gosx_hydrate", hydrateRuntimeFunc(b))
	setRuntimeFunc("__gosx_hydrate_engine", hydrateEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_action", actionRuntimeFunc(b))
	setRuntimeFunc("__gosx_dispose", disposeRuntimeFunc(b))
	setRuntimeFunc("__gosx_tick_engine", tickEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_render_engine", renderEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_engine_dispose", disposeEngineRuntimeFunc(b))
	setRuntimeFunc("__gosx_highlight", highlightRuntimeFunc())
	setRuntimeFunc("__gosx_set_shared_signal", sharedSignalRuntimeFunc(b))
	setRuntimeFunc("__gosx_set_input_batch", inputBatchRuntimeFunc(b))
	setRuntimeFunc("__gosx_text_layout", textLayoutRuntimeFunc())
	setRuntimeFunc("__gosx_text_layout_metrics", textLayoutMetricsRuntimeFunc())
	setRuntimeFunc("__gosx_text_layout_ranges", textLayoutRangesRuntimeFunc())
	setRuntimeFunc("__gosx_crdt_init", crdtInitRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_sync", crdtSyncRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_put", crdtPutRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_get", crdtGetRuntimeFunc(crdtBridge))
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

func disposeRuntimeFunc(b *bridge.Bridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeIsland(args[0].String())
		return js.Null()
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
		b.DisposeEngine(args[0].String())
		return js.Null()
	})
}

func highlightRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.ValueOf("")
		}
		lang := highlight.LangGo
		if len(args) > 1 && args[1].Type() == js.TypeString && args[1].String() != "" {
			lang = args[1].String()
		}
		return js.ValueOf(highlight.HTML(lang, args[0].String()))
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

func textLayoutRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutText(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			textlayout.LayoutOptions{
				MaxWidth:   args[2].Float(),
				LineHeight: lineHeight,
			},
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func textLayoutMetricsRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutTextMetrics(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			textlayout.LayoutOptions{
				MaxWidth:   args[2].Float(),
				LineHeight: lineHeight,
			},
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func textLayoutRangesRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutTextRanges(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			textlayout.LayoutOptions{
				MaxWidth:   args[2].Float(),
				LineHeight: lineHeight,
			},
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func crdtInitRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Type() == js.TypeUndefined || args[0].Type() == js.TypeNull {
			if err := b.InitDoc(nil); err != nil {
				return jsError(err)
			}
			return js.Null()
		}
		data, err := decodeProgramData(args[0])
		if err != nil {
			return jsError(err)
		}
		if err := b.InitDoc(data); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtSyncRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (syncMsg)")
		}
		msg, err := decodeProgramData(args[0])
		if err != nil {
			return jsError(err)
		}
		reply, err := b.Sync(msg)
		if err != nil {
			return jsError(err)
		}
		if len(reply) == 0 {
			return js.Null()
		}
		return bytesToJS(reply)
	})
}

func crdtPutRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (obj, prop, value)")
		}
		valueJSON, err := normalizeJSONArg(args[2], "null")
		if err != nil {
			return jsError(err)
		}
		if err := b.Put(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()), valueJSON); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtGetRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (obj, prop)")
		}
		valueJSON, err := b.Get(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()))
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", valueJSON)
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
		return hydrateCall{}, fmt.Errorf("need 5 args (islandID, componentName, propsJSON, programData, format)")
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
		return nil, fmt.Errorf("programData must be a string, Uint8Array, or ArrayBuffer")
	}
	length := uint8Array.Get("length").Int()
	programData := make([]byte, length)
	js.CopyBytesToGo(programData, uint8Array)
	return programData, nil
}

func bytesToJS(data []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(arr, data)
	return arr
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
			return "", fmt.Errorf("JSON global not available")
		}
		out := jsonGlobal.Call("stringify", value)
		if out.Type() != js.TypeString {
			return "", fmt.Errorf("argument must be JSON-serializable")
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

func logRuntimeError(prefix string, err error) {
	js.Global().Get("console").Call("error", "[gosx/wasm] "+prefix+":", err.Error())
}

func jsError(err error) js.Value {
	if err == nil {
		return js.Null()
	}
	return js.ValueOf("error: " + err.Error())
}

func jsErrorf(format string, args ...any) js.Value {
	return js.ValueOf("error: " + fmt.Sprintf(format, args...))
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
