//go:build js && wasm

package settings

import "syscall/js"

// WatchSystemMotion reads the prefers-reduced-motion media query into m and
// keeps it current. It returns a function that stops watching.
func WatchSystemMotion(m *Manager) (stop func()) {
	mql := js.Global().Call("matchMedia", "(prefers-reduced-motion: reduce)")
	if !mql.Truthy() {
		return func() {}
	}
	m.SetSystemReducedMotion(mql.Get("matches").Truthy())
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			m.SetSystemReducedMotion(args[0].Get("matches").Truthy())
		}
		return nil
	})
	mql.Call("addEventListener", "change", fn)
	return func() {
		mql.Call("removeEventListener", "change", fn)
		fn.Release()
	}
}
