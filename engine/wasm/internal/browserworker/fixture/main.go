//go:build js && wasm

// Command fixture is the Go WASM engine the real-browser worker tests run. The
// page chooses its behavior through engine props, so one binary covers every
// scenario. It reports to the test through ctx.Emit("report", ...), which works
// in a worker (the port relays it) and on the main thread (fallback).
package main

import (
	"syscall/js"

	"m31labs.dev/gosx/engine/wasm"
)

func report(ctx wasm.Context, kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["kind"] = kind
	if err := ctx.Emit("report", data); err != nil {
		js.Global().Get("console").Call("error", "report failed: "+err.Error())
	}
}

func main() {
	err := wasm.Register("WorkerFixture", func(ctx wasm.Context) (wasm.Handle, error) {
		var props struct {
			Scenario string `json:"scenario"`
		}
		_ = ctx.DecodeProps(&props)
		_, hasPort := ctx.Port()
		report(ctx, "mounted", map[string]any{
			"thread":      string(ctx.Thread()),
			"fallback":    ctx.ThreadFallbackReason(),
			"hasDocument": !js.Global().Get("document").IsUndefined(),
			"hasPort":     hasPort,
			"scenario":    props.Scenario,
		})
		return wasm.HandleFunc(func() { report(ctx, "disposed", nil) }), nil
	})
	if err != nil {
		js.Global().Get("console").Call("error", err.Error())
	}
	select {}
}
