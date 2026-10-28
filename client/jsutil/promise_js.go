//go:build js && wasm

package jsutil

import (
	"errors"
	"fmt"
	"syscall/js"
)

// settlement is how a promise ended: fulfilled with value, or rejected with it.
type settlement struct {
	value    js.Value
	rejected bool
}

// AwaitPromise blocks the calling goroutine on a JS Promise and returns the
// resolved value or an error. This is the foundational async primitive for
// WebGPU (adapter/device/pipeline-async) and any other Promise-based browser
// API the client touches.
//
// The passed js.Value must be a thenable. Nil/undefined returns an error
// immediately. The goroutine blocks until the Promise settles.
//
// The two Go handlers only signal a channel, and the browser runs them as
// microtasks, after the call into JavaScript that attached them has returned.
// AwaitPromise therefore never runs Go code inside a call from Go into
// JavaScript. That matters because Go's js/wasm runtime keeps every such nested
// call on one shared stack, and two goroutines that nest at the same time can
// corrupt it (see the package documentation).
func AwaitPromise(p js.Value) (js.Value, error) {
	if p.IsNull() || p.IsUndefined() {
		return js.Undefined(), errors.New("jsutil: awaitPromise on null value")
	}
	settled := make(chan settlement, 1)
	handler := func(rejected bool) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			value := js.Undefined()
			if len(args) > 0 {
				value = args[0]
			}
			// A thenable that misbehaves may call a handler twice; never block here.
			select {
			case settled <- settlement{value: value, rejected: rejected}:
			default:
			}
			return nil
		})
	}
	onFulfilled, onRejected := handler(false), handler(true)
	defer onFulfilled.Release()
	defer onRejected.Release()
	// Promise.resolve returns a native promise unchanged. A foreign thenable is
	// assimilated in a microtask, so even one whose then() calls back at once
	// cannot run a handler inside this call.
	js.Global().Get("Promise").Call("resolve", p).Call("then", onFulfilled, onRejected)
	result := <-settled
	if result.rejected {
		return js.Undefined(), fmt.Errorf("promise rejected: %s", Describe(result.value))
	}
	return result.value, nil
}

// NewPromiseWithResolvers returns a pending Promise and the functions that
// settle it, like the browser's Promise.withResolvers.
//
// Use it instead of js.Global().Get("Promise").New(js.FuncOf(executor)). The
// Promise constructor calls its executor before it returns, so a Go executor
// runs nested inside the call that creates the Promise. This function hands the
// constructor a native function, so no Go code runs inside it.
//
// A Promise implementation that does not call its executor at once, which the
// standard Promise always does, yields undefined resolvers.
func NewPromiseWithResolvers() (promise, resolve, reject js.Value) {
	// Array.prototype.push, bound to a fresh array, is a native function: the
	// constructor's call executor(resolve, reject) becomes slots.push(resolve,
	// reject), which stores both functions where Go can read them.
	slots := js.Global().Get("Array").New()
	executor := slots.Get("push").Call("bind", slots)
	promise = js.Global().Get("Promise").New(executor)
	return promise, slots.Index(0), slots.Index(1)
}

// Describe returns a best-effort human string for a js.Value — used for error
// messages where we want the .message of an Error object if present.
//
// Describe never panics, whatever the value is. A promise can reject with
// anything: a number, a symbol, a BigInt, a revoked Proxy, an object without a
// toString method, or an object whose message getter throws. Describe runs
// inside Go callbacks (the Scene3D frame rejection handlers call it), where a
// panic would stop the whole program.
func Describe(v js.Value) string {
	if text, ok := describeByType(v); ok {
		return text
	}
	if text, ok := describeByString(v); ok {
		return text
	}
	return "<unprintable value>"
}

// describeByType reads an Error's message or converts the value by its type.
// It fails when Go cannot classify the value (syscall/js panics on a BigInt) or
// when JavaScript throws while reading it.
//
// It reads message with Reflect.get through Call. Value.Get cannot do this
// safely: wasm_exec.js guards only valueCall, valueInvoke and valueNew with
// try/catch, so a getter or Proxy trap that throws inside valueGet escapes into
// the host and stops the Go program, where a throw inside Call becomes a panic
// that recover can stop.
func describeByType(v js.Value) (text string, ok bool) {
	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()
	switch v.Type() {
	case js.TypeNull:
		return "<null>", true
	case js.TypeUndefined:
		return "<undefined>", true
	case js.TypeString:
		return v.String(), true
	case js.TypeObject, js.TypeFunction:
		// An Error with an empty message reads better as "Error" than as nothing.
		if message := js.Global().Get("Reflect").Call("get", v, "message"); message.Type() == js.TypeString && message.String() != "" {
			return message.String(), true
		}
	}
	return describeByString(v)
}

// describeByString applies JavaScript's String() conversion.
func describeByString(v js.Value) (text string, ok bool) {
	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()
	return js.Global().Call("String", v).String(), true
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
