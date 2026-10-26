//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/engine"
	enginewasm "m31labs.dev/gosx/engine/wasm"
)

type fixtureHandle struct {
	mount js.Value
}

type fixtureEvent struct {
	EngineID string         `json:"engineID"`
	Payload  map[string]any `json:"payload"`
}

func (h *fixtureHandle) Dispose() {
	if h == nil || !h.mount.Truthy() {
		return
	}
	count := h.mount.Get("dataset").Get("disposeCount")
	next := 1
	if count.Type() == js.TypeString {
		_, _ = fmt.Sscan(count.String(), &next)
		next++
	}
	h.mount.Get("dataset").Set("disposeCount", fmt.Sprint(next))
}

func mountFixture(ctx enginewasm.Context) (enginewasm.Handle, error) {
	mount := ctx.Mount()
	if !mount.Truthy() {
		return nil, fmt.Errorf("GoWASMFixture requires a mount")
	}
	props := map[string]any{}
	if err := ctx.DecodeProps(&props); err != nil {
		return nil, err
	}
	label, _ := props["label"].(string)
	mount.Set("textContent", label)
	mount.Get("dataset").Set("engineID", ctx.ID())
	mount.Get("dataset").Set("wasmCapability", fmt.Sprint(ctx.HasCapability(engine.CapWASM)))
	if err := ctx.Emit("mounted", fixtureEvent{
		EngineID: ctx.ID(),
		Payload:  map[string]any{"label": label, "mounted": true},
	}); err != nil {
		return nil, err
	}
	if err := ctx.Emit("invalid", func() {}); err == nil {
		return nil, fmt.Errorf("expected an unencodable event detail to fail")
	}
	mount.Get("dataset").Set("emitErrorSafe", "true")
	mount.Get("dataset").Set("mounted", "true")
	return &fixtureHandle{mount: mount}, nil
}

// The same Go loop runs on the page and in a framework-managed worker.
func mountFrameFixture(ctx enginewasm.Context) (enginewasm.Handle, error) {
	loop, stop := context.WithCancel(context.Background())
	thread := "main"
	if ctx.Thread() == engine.ThreadWorker {
		thread = "worker"
	}
	go func() {
		count := 0
		_ = enginewasm.EachFrame(loop, func(frame enginewasm.Frame) error {
			count++
			return ctx.Emit("frame", struct {
				EngineID    string  `json:"engineID"`
				Thread      string  `json:"thread"`
				HasDocument bool    `json:"hasDocument"`
				Count       int     `json:"count"`
				Delta       float64 `json:"delta"`
			}{ctx.ID(), thread, js.Global().Get("document").Truthy(), count, frame.Delta})
		})
	}()
	return enginewasm.HandleFunc(stop), nil
}

func main() {
	if err := enginewasm.Register("GoWASMFixture", mountFixture); err != nil {
		panic(err)
	}
	if err := enginewasm.Register("GoWASMFixtureAlias", mountFixture); err != nil {
		panic(err)
	}
	for _, name := range []string{"GoWASMFrameFixture", "GoWASMFrameWorkerFixture"} {
		if err := enginewasm.Register(name, mountFrameFixture); err != nil {
			panic(err)
		}
	}
	select {}
}
