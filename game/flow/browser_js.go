//go:build js && wasm

package flow

import "syscall/js"

// AttachBrowser calls f.Blur on window blur and f.Hidden on visibilitychange.
// It returns a function that removes the listeners.
func AttachBrowser(f *Flow) (detach func()) {
	win := js.Global()
	doc := win.Get("document")
	blur := js.FuncOf(func(js.Value, []js.Value) any { f.Blur(); return nil })
	vis := js.FuncOf(func(js.Value, []js.Value) any { f.Hidden(doc.Get("hidden").Truthy()); return nil })
	win.Call("addEventListener", "blur", blur)
	doc.Call("addEventListener", "visibilitychange", vis)
	return func() {
		win.Call("removeEventListener", "blur", blur)
		doc.Call("removeEventListener", "visibilitychange", vis)
		blur.Release()
		vis.Release()
	}
}
