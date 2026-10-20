//go:build js && wasm

package browser

import (
	"m31labs.dev/gosx/internal/browserdom"
	"syscall/js"
)

// Observer retains an inert handler after native disconnect failure. Dispose
// may be retried; no app callback runs after the first disposal request.
type Observer struct {
	value            js.Value
	fn               js.Func
	changed          func()
	active, released bool
}

func ObserveAttributes(element Element, attributes []string, changed func()) (observer *Observer) {
	if !element.Valid() || changed == nil || len(attributes) == 0 {
		return nil
	}
	var owned *Observer
	defer func() {
		if recover() != nil {
			if owned != nil {
				owned.Dispose()
			}
			observer = nil
		}
	}()
	constructor := js.Global().Get("MutationObserver")
	if constructor.Type() != js.TypeFunction {
		return nil
	}
	owned = &Observer{active: true, changed: changed}
	owned.fn = js.FuncOf(func(js.Value, []js.Value) any {
		if callback := owned.changed; owned.active && callback != nil {
			callback()
		}
		return nil
	})
	owned.value = constructor.New(owned.fn)
	names := make([]any, len(attributes))
	for i, name := range attributes {
		names[i] = name
	}
	owned.value.Call("observe", browserdom.Value(element), map[string]any{"attributes": true, "attributeFilter": names})
	return owned
}
func (o *Observer) Dispose() {
	if o == nil || o.released {
		return
	}
	o.active = false
	o.changed = nil
	// If construction threw after retaining our callback, no native instance
	// was returned to disconnect. Retain the inert callback safely.
	if !o.value.Truthy() {
		return
	}
	defer func() { _ = recover() }()
	o.value.Call("disconnect")
	o.released = true
	o.fn.Release()
}
