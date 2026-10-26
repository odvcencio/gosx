//go:build js && wasm

package host

import (
	"encoding/json"
	"sync"
	"syscall/js"
)

// NewForwardedInput subscribes to the page input an engine worker receives.
// ok is false outside an engine worker, where the main thread reads the
// browser directly (NavigatorSource and the shared $input.* signals). The page
// forwards keyboard, pointer and gamepad input only for an engine that
// declares the matching capability (engine.CapKeyboard, CapPointer,
// CapGamepad).
func NewForwardedInput() (input *ForwardedInput, ok bool) {
	root := js.Global().Get("__gosx")
	if !root.Truthy() {
		return nil, false
	}
	bridge := root.Get("worker")
	if !bridge.Truthy() || bridge.Get("onInput").Type() != js.TypeFunction {
		return nil, false
	}
	input = newForwardedInput()
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Type() != js.TypeString {
			return nil
		}
		var event InputEvent
		if json.Unmarshal([]byte(args[0].String()), &event) == nil {
			input.Apply(event)
		}
		return nil
	})
	unsubscribe := bridge.Call("onInput", callback)
	var once sync.Once
	input.close = func() {
		once.Do(func() {
			if unsubscribe.Type() == js.TypeFunction {
				unsubscribe.Invoke()
			}
			callback.Release()
		})
	}
	return input, true
}

// Close stops listening and releases the Go callback.
func (f *ForwardedInput) Close() {
	if f != nil && f.close != nil {
		f.close()
	}
}
