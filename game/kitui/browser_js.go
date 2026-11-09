//go:build js && wasm

package kitui

import (
	"syscall/js"

	"m31labs.dev/gosx"
)

// Handlers receive the events of kit elements.
type Handlers struct {
	// OnAction gets data-kit-action buttons: name and optional value.
	OnAction func(name, value string)
	// OnSetting gets data-kit-setting controls: key and value.
	OnSetting func(key, value string)
	// OnRebind gets data-kit-rebind buttons already split into parts.
	OnRebind func(ctx, action string, slot int)
}

// Bind listens for "gosx:kit" events under root and calls h. It returns a
// function that stops listening. The Script() node must be on the page.
func Bind(root js.Value, h Handlers) (stop func()) {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		d := args[0].Get("detail")
		kind, name, value := d.Get("kind").String(), d.Get("name").String(), d.Get("value").String()
		switch kind {
		case "action":
			if h.OnAction != nil {
				h.OnAction(name, value)
			}
		case "setting":
			if h.OnSetting != nil {
				h.OnSetting(name, value)
			}
		case "rebind":
			if ctx, action, slot, ok := ParseRebindID(name); ok && h.OnRebind != nil {
				h.OnRebind(ctx, action, slot)
			}
		}
		return nil
	})
	root.Call("addEventListener", "gosx:kit", fn)
	return func() {
		root.Call("removeEventListener", "gosx:kit", fn)
		fn.Release()
	}
}

// Render replaces root's content with the HTML of node. Use it to refresh a
// panel after its state changes. Focus inside root is lost; call it only on
// state changes, not every frame.
func Render(root js.Value, node gosx.Node) {
	root.Set("innerHTML", gosx.RenderHTML(node))
}
