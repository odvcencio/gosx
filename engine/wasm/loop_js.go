//go:build js && wasm

package wasm

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// fromEventLoop calls the context method name with args from a microtask, and
// waits for the call to finish. It returns what the method returned.
//
// A write to a shared signal tells every subscriber before it returns, and this
// engine's own subscriptions are Go functions. When Go makes the write, Go is
// entered again while the call into JavaScript is still on the stack. That is a
// nested entry. Go's WebAssembly runtime keeps the entries in progress on one
// stack and assumes that entries from different goroutines never interleave, so
// a goroutine that nests while another one does can stop the program (see
// client/jsutil). A server that answers from several goroutines must not nest.
//
// Here the browser makes the call. Promise.resolve().then(bound) queues a native
// function that runs once the stack is empty, and the goroutine waits on the
// promise; the callbacks that wake it only signal a channel. Every subscriber is
// then told while Go is not inside any call into JavaScript.
func (c Context) fromEventLoop(method string, args ...any) (js.Value, error) {
	fn := c.value.Get(method)
	if fn.Type() != js.TypeFunction {
		return js.Undefined(), fmt.Errorf("the engine context has no %s", method)
	}
	bound := fn.Call("bind", append([]any{c.value}, args...)...)
	return jsutil.AwaitPromise(js.Global().Get("Promise").Call("resolve").Call("then", bound))
}

// setSignalsFromEventLoop is SetSignals, with the write made by the browser from
// the event loop (see fromEventLoop). It returns after the write has been made.
func (c Context) setSignalsFromEventLoop(values map[string]any) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("set shared signals: %v", failure)
		}
	}()
	if c.value.Get("setSignalsJSON").Type() != js.TypeFunction {
		// An older host. It has no way to write from the event loop.
		return c.SetSignals(values)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	result, err := c.fromEventLoop("setSignalsJSON", string(encoded), false)
	if err != nil {
		return fmt.Errorf("set shared signals: %w", err)
	}
	if result.Type() == js.TypeString && result.String() != "" {
		return fmt.Errorf("set shared signals: %s", result.String())
	}
	return nil
}

// trimCallsFromEventLoop asks the page to drop the calls at or below through from
// the mailbox in the call signal, in one step and from the event loop. The page
// reads and rewrites the signal itself, so a call that an island appends cannot
// slip in between. A page whose context has no trimCalls leaves the mailbox as
// the writer left it; the writer then drops the answered calls itself.
func (c Context) trimCallsFromEventLoop(signal string, through uint64) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("trim calls: %v", failure)
		}
	}()
	if c.value.Get("trimCalls").Type() != js.TypeFunction {
		return nil
	}
	_, err = c.fromEventLoop("trimCalls", signal, float64(through))
	return err
}
