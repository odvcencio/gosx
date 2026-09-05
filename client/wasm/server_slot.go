//go:build js && wasm

package main

import (
	"encoding/json"
	"m31labs.dev/gosx/client/bridge"
	"syscall/js"
)

func registerServerSlotRuntime(b *bridge.Bridge) {
	api := js.Global().Get("Object").New()
	api.Set("version", 1)
	api.Set("prepare", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 5 {
			return `{"error":"ServerSlot requires five preparation arguments"}`
		}
		data, err := decodeProgramData(args[3])
		if err != nil {
			out, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(out)
		}
		prepared, err := b.PrepareServerSlots(args[0].String(), args[1].String(), args[2].String(), data, args[4].String())
		if err != nil {
			out, _ := json.Marshal(map[string]string{"error": err.Error()})
			return string(out)
		}
		out, err := json.Marshal(prepared)
		if err != nil {
			_ = b.AbortServerSlots(args[0].String(), prepared.Token)
			return `{"error":"ServerSlot response encoding failed"}`
		}
		return string(out)
	}))
	api.Set("commit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 2 {
			return "ServerSlot requires id and token"
		}
		if err := b.CommitServerSlots(args[0].String(), args[1].String()); err != nil {
			return err.Error()
		}
		return ""
	}))
	api.Set("abort", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 2 {
			return "ServerSlot requires id and token"
		}
		if err := b.AbortServerSlots(args[0].String(), args[1].String()); err != nil {
			return err.Error()
		}
		return ""
	}))
	runtimeFacade().Set("serverSlots", api)
}
