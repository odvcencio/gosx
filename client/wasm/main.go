//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
)

func main() {
	b := bridge.New()

	// Export hydrate function
	js.Global().Set("__gosx_hydrate", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 5 {
			return js.ValueOf("error: need 5 args (islandID, componentName, propsJSON, programData, format)")
		}
		islandID := args[0].String()
		componentName := args[1].String()
		propsJSON := args[2].String()
		format := args[4].String()

		// Convert programData (Uint8Array or string) to []byte
		var programData []byte
		if args[3].Type() == js.TypeString {
			programData = []byte(args[3].String())
		} else {
			// Uint8Array
			length := args[3].Get("length").Int()
			programData = make([]byte, length)
			js.CopyBytesToGo(programData, args[3])
		}

		err := b.HydrateIsland(islandID, componentName, propsJSON, programData, format)
		if err != nil {
			return js.ValueOf("error: " + err.Error())
		}
		return js.Null()
	}))

	// Export action dispatch function
	js.Global().Set("__gosx_action", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return js.Null()
		}
		islandID := args[0].String()
		handlerName := args[1].String()
		eventDataJSON := args[2].String()

		patches, err := b.DispatchAction(islandID, handlerName, eventDataJSON)
		if err != nil {
			js.Global().Get("console").Call("error", "[gosx/wasm] dispatch error:", err.Error())
			return js.Null()
		}

		if len(patches) > 0 {
			patchJSON, err := bridge.MarshalPatches(patches)
			if err != nil {
				return js.ValueOf("marshal:" + err.Error())
			}
			js.Global().Call("__gosx_apply_patches", islandID, patchJSON)
			return js.ValueOf(len(patches))
		}
		return js.ValueOf(0)
	}))

	// Export dispose function
	js.Global().Set("__gosx_dispose", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.Null()
		}
		b.DisposeIsland(args[0].String())
		return js.Null()
	}))

	// Signal that runtime is ready
	readyFn := js.Global().Get("__gosx_runtime_ready")
	if readyFn.Truthy() {
		readyFn.Invoke()
	}

	// Block forever — WASM must not exit
	select {}
}
