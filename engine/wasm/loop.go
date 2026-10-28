package wasm

import "m31labs.dev/gosx/engine"

// SetSignalsFromEventLoop is SetSignals for code that runs on a goroutine of its
// own and writes shared signals, such as a ServeCallsAsync handler that
// publishes state.
//
// On the main thread a write to a shared signal tells every subscriber before it
// returns. A write made inside a call from Go into JavaScript can run a Go
// subscriber inside that call, as the browser does for a function made with
// js.FuncOf. Go is then entered again while the call into JavaScript is still
// on the stack. That is a nested entry. Go's WebAssembly runtime keeps the
// entries in progress on one stack and assumes that entries from different
// goroutines never interleave, so two goroutines that nest at once can stop the
// program. SetSignal, SetSignals and SetChangedSignals make their write inside
// such a call.
//
// Here the browser makes the write from the event loop, where no call from Go is
// pending, and the goroutine waits until it is done. The subscribers are told
// before this function returns, as they are for SetSignals. In a worker a write
// only posts a message to the page, which tells no Go function, so this is
// SetSignals there.
//
// Call it from a goroutine, not from inside a callback that JavaScript made into
// Go (a SubscribeSignal handler, an event listener, a ServeCalls handler): that
// callback holds up the event loop that the write waits for, and the engine
// deadlocks.
func (c Context) SetSignalsFromEventLoop(values map[string]any) error {
	if c.Thread() == engine.ThreadWorker {
		return c.SetSignals(values)
	}
	return c.setSignalsFromEventLoop(values)
}
