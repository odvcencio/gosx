//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"syscall/js"
)

// AwaitPromiseContext waits for a browser promise until it settles or ctx is
// cancelled. Cancellation stops this wait, not the underlying browser operation;
// use that operation's AbortController or disposal API when it supports one.
// Call from a goroutine, as with AwaitPromise.
//
// The source promise retains only native Promise.race handlers. Go callbacks
// belong to the race and are released on cancellation, even when a third-party
// promise never settles. A later rejection is still observed by the native race.
//
// Like AwaitPromise, it never runs Go code inside a call from Go into
// JavaScript. The cancellation signal is a native promise, made without a Go
// executor (see NewPromiseWithResolvers), and the Go handlers only signal a
// channel from a microtask.
func AwaitPromiseContext(ctx context.Context, promise js.Value) (js.Value, error) {
	if ctx == nil {
		return js.Undefined(), errors.New("jsutil: promise context is required")
	}
	if promise.IsNull() || promise.IsUndefined() {
		return js.Undefined(), errors.New("jsutil: awaitPromise on null value")
	}
	if ctx.Done() == nil {
		return AwaitPromise(promise)
	}
	cancelPromise, resolveCancel, _ := NewPromiseWithResolvers()
	if resolveCancel.Type() != js.TypeFunction {
		return js.Undefined(), errors.New("jsutil: Promise did not run its executor synchronously")
	}
	cancelled := js.Global().Get("Object").New()
	stop := context.AfterFunc(ctx, func() {
		// A native resolve function does not throw; the recover keeps an
		// unexpected failure from stopping the program on this goroutine.
		defer func() { _ = recover() }()
		resolveCancel.Invoke(cancelled)
	})
	defer stop()
	value, err := AwaitPromise(js.Global().Get("Promise").Call("race", []any{promise, cancelPromise}))
	if value.Equal(cancelled) || ctx.Err() != nil {
		return js.Undefined(), ctx.Err()
	}
	return value, err
}
