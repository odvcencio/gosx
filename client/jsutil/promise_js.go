//go:build js && wasm

package jsutil

import (
	"errors"
	"fmt"
	"syscall/js"
)

// AwaitPromise blocks the calling goroutine on a JS Promise and returns the
// resolved value or an error. This is the foundational async primitive for
// WebGPU (adapter/device/pipeline-async) and any other Promise-based browser
// API the client touches.
//
// The passed js.Value must be a thenable. Nil/undefined returns an error
// immediately. The goroutine blocks until the Promise settles.
func AwaitPromise(p js.Value) (js.Value, error) {
	if p.IsNull() || p.IsUndefined() {
		return js.Undefined(), errors.New("jsutil: awaitPromise on null value")
	}
	done := make(chan js.Value, 1)
	errc := make(chan js.Value, 1)
	then := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			done <- args[0]
		} else {
			done <- js.Undefined()
		}
		return nil
	})
	catch := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			errc <- args[0]
		} else {
			errc <- js.Undefined()
		}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	p.Call("then", then).Call("catch", catch)
	select {
	case v := <-done:
		return v, nil
	case e := <-errc:
		return js.Undefined(), fmt.Errorf("promise rejected: %s", Describe(e))
	}
}

// Describe returns a best-effort human string for a js.Value — used for error
// messages where we want the .message of an Error object if present.
func Describe(v js.Value) string {
	if v.IsNull() {
		return "<null>"
	}
	if v.IsUndefined() {
		return "<undefined>"
	}
	if v.Type() == js.TypeString {
		return v.String()
	}
	if v.Get("message").Type() == js.TypeString {
		return v.Get("message").String()
	}
	return v.Call("toString").String()
}

// ManagedFunc wraps a js.Func with an explicit lifetime. Callers must call
// Release when the callback is no longer needed to avoid leaking JS->Go
// bridge entries. Not safe for concurrent use.
type ManagedFunc struct {
	fn       js.Func
	released bool
}

// NewManagedFunc wraps a Go callback as a reference-counted js.Func.
func NewManagedFunc(fn func(this js.Value, args []js.Value) any) *ManagedFunc {
	return &ManagedFunc{fn: js.FuncOf(fn)}
}

// Value returns the underlying js.Value for passing to JS APIs.
func (m *ManagedFunc) Value() js.Value {
	return m.fn.Value
}

// Release frees the underlying js.Func. Idempotent so defer patterns compose
// safely even when ownership has already transferred.
func (m *ManagedFunc) Release() {
	if m == nil || m.released {
		return
	}
	m.fn.Release()
	m.released = true
}

// NewUint8ArrayFromBytes allocates a JS Uint8Array and copies src into it.
// Intended for one-shot writes (GPU buffer uploads, texture uploads).
func NewUint8ArrayFromBytes(src []byte) js.Value {
	u8 := js.Global().Get("Uint8Array").New(len(src))
	if len(src) > 0 {
		js.CopyBytesToJS(u8, src)
	}
	return u8
}
