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
	listener := js.FuncOf(func(js.Value, []js.Value) any { cancel(); return nil })
	signal.Call("addEventListener", "abort", listener)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			signal.Call("removeEventListener", "abort", listener)
			listener.Release()
		})
	}
	go func() { <-ctx.Done(); cleanup() }()
	return ctx, cleanup, nil
}
