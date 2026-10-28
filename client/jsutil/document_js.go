//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"sync"
	"syscall/js"
)

// DocumentContext derives a context canceled when the current document begins
// native navigation, reload, or pagehide. It is already canceled on an outgoing
// document, including calls from late-loaded WASM. Use it for credential-bearing
// background requests: cookies can rotate before the old document unloads.
//
// Cancel releases the browser listener and must be called when work finishes.
// A canceled navigation or BFCache restore allows a new call to return a fresh
// live context; previously canceled operations stay canceled. GoSX enhanced
// navigation does not cancel this document scope: parent must carry the engine
// or page lifetime. The core GoSX bootstrap must be installed before use.
//
// The browser aborts the document signal inside the call that starts a
// navigation: location.assign, location.replace, a form submission or a link
// click, which can be a call from Go. A Go listener on the signal would then run
// inside that call (see the package documentation). DocumentContext therefore
// listens with the native resolve function of a promise (see
// NewPromiseWithResolvers) and cancels the context from a goroutine that waits
// on the promise. The context is canceled a few microtasks after the abort, as
// soon as the program next returns to JavaScript, and not inside the call that
// starts the navigation. A call that starts after the abort sees the aborted
// signal and returns a context that is already canceled.
func DocumentContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, errors.New("jsutil: document parent context is required")
	}
	api := js.Global().Get("__gosx")
	for _, key := range []string{"host", "lifecycle"} {
		if !api.Truthy() {
			return nil, nil, errors.New("jsutil: GoSX document lifecycle is not loaded")
		}
		api = api.Get(key)
	}
	if !api.Truthy() || api.Get("documentSignal").Type() != js.TypeFunction {
		return nil, nil, errors.New("jsutil: GoSX document lifecycle is not loaded")
	}
	signal := api.Call("documentSignal")
	ctx, cancel := context.WithCancel(parent)
	if ctx.Err() != nil || signal.Get("aborted").Bool() {
		cancel()
		return ctx, cancel, nil
	}
	aborted, onAbort, _ := NewPromiseWithResolvers()
	if onAbort.Type() != js.TypeFunction {
		cancel()
		return nil, nil, errors.New("jsutil: Promise did not run its executor synchronously")
	}
	// onAbort is a native function: the abort event settles the promise, and no
	// Go code runs while the browser dispatches the event.
	signal.Call("addEventListener", "abort", onAbort)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			signal.Call("removeEventListener", "abort", onAbort)
		})
	}
	go func() {
		// The wait ends when the signal aborts or when ctx ends, whichever is
		// first. Either way the context is canceled and the listener removed.
		_, _ = AwaitPromiseContext(ctx, aborted)
		cleanup()
	}()
	return ctx, cleanup, nil
}
