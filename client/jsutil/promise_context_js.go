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
	cancelled := js.Global().Get("Object").New()
	var resolveCancel js.Value
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolveCancel = args[0]
		return nil
	})
	cancelPromise := js.Global().Get("Promise").New(executor)
	executor.Release() // Promise invokes its executor synchronously.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			resolveCancel.Invoke(cancelled)
		case <-done:
		}
	}()
	value, err := AwaitPromise(js.Global().Get("Promise").Call("race", []any{promise, cancelPromise}))
	if value.Equal(cancelled) || ctx.Err() != nil {
		return js.Undefined(), ctx.Err()
	}
	return value, err
}
