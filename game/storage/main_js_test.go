//go:build js && wasm

package storage

import (
	"os"
	"syscall/js"
	"testing"
)

// Node's WASM harness has no browser storage. Supply it for the existing
// platform-default backend tests; individual policy tests replace it locally.
func TestMain(m *testing.M) {
	g := js.Global()
	previous := g.Get("localStorage")
	var callbacks []js.Func
	if !previous.Truthy() {
		values := map[string]string{}
		store := g.Get("Object").New()
		bind := func(name string, fn func([]js.Value) any) {
			callback := js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
			callbacks = append(callbacks, callback)
			store.Set(name, callback)
		}
		bind("getItem", func(args []js.Value) any {
			if value, found := values[args[0].String()]; found {
				return value
			}
			return nil
		})
		bind("setItem", func(args []js.Value) any { values[args[0].String()] = args[1].String(); return nil })
		bind("removeItem", func(args []js.Value) any { delete(values, args[0].String()); return nil })
		g.Set("localStorage", store)
	}
	code := m.Run()
	g.Set("localStorage", previous)
	for _, callback := range callbacks {
		callback.Release()
	}
	os.Exit(code)
}
