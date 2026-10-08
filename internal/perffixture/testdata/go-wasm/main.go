//go:build js && wasm

package main

import (
	"syscall/js"

	enginewasm "m31labs.dev/gosx/engine/wasm"
)

func main() {
	if err := enginewasm.Register("BudgetEngine", mount); err != nil {
		panic("fixture registration unavailable")
	}
	select {}
}

func mount(ctx enginewasm.Context) (enginewasm.Handle, error) {
	button := js.Global().Get("document").Call("createElement", "button")
	button.Set("type", "button")
	button.Set("textContent", "Count: 0")
	count := 0
	disposed := false
	click := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		if !disposed {
			count++
			button.Set("textContent", "Count: "+js.Global().Get("String").Invoke(count).String())
		}
		return nil
	})
	button.Call("addEventListener", "click", click)
	ctx.Mount().Call("replaceChildren", button)
	if err := ctx.Emit("first-tick", map[string]any{"usable": true}); err != nil {
		button.Call("removeEventListener", "click", click)
		click.Release()
		return nil, err
	}
	return enginewasm.HandleFunc(func() {
		if disposed {
			return
		}
		disposed = true
		button.Call("removeEventListener", "click", click)
		ctx.Mount().Call("replaceChildren")
		click.Release()
	}), nil
}
