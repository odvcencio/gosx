//go:build js && wasm

package browser

import (
	"math"
	"syscall/js"
)

// Cache clock objects, not readings. Hot callbacks make one host call.
var performanceClock = js.Global().Get("performance")
var wallClock = js.Global().Get("Date")

// Now returns the browser monotonic clock in milliseconds.
func Now() float64 { return performanceClock.Call("now").Float() }

// WallNow returns milliseconds since the Unix epoch.
func WallNow() float64 { return wallClock.Call("now").Float() }

// Timer owns one host timer. Stop is reentrant and can retry a failed native
// cancellation. A failed cancellation keeps only an inert Go callback alive
// until cancellation succeeds or a timeout's terminal callback arrives.
type Timer struct {
	id                         js.Value
	fn                         js.Func
	callback                   func()
	interval, active, released bool
}

// Timeout invokes fn once after ms milliseconds.
func Timeout(fn func(), ms float64) *Timer { return newTimer(fn, ms, false) }

// Interval invokes fn repeatedly subject to background throttling.
func Interval(fn func(), ms float64) *Timer { return newTimer(fn, ms, true) }

func newTimer(fn func(), ms float64, interval bool) (timer *Timer) {
	if fn == nil {
		return nil
	}
	if math.IsNaN(ms) || ms < 0 {
		ms = 0
	}
	if ms > math.MaxInt32 {
		ms = math.MaxInt32
	}
	method := "setTimeout"
	if interval {
		method = "setInterval"
	}
	if js.Global().Get(method).Type() != js.TypeFunction {
		return nil
	}
	owned := &Timer{interval: interval, active: true, callback: fn}
	owned.fn = js.FuncOf(func(js.Value, []js.Value) any {
		callback := owned.callback
		if !owned.interval {
			owned.active = false
			owned.callback = nil
			defer owned.release()
		}
		if callback != nil {
			callback()
		}
		return nil
	})
	defer func() {
		if recover() != nil {
			// A throwing custom scheduler may already own the callback but
			// supplied no timer ID. Retain an inert handler rather than turn
			// a possible late invocation into a released-function trap.
			owned.active = false
			owned.callback = nil
			timer = nil
		}
	}()
	owned.id = js.Global().Call(method, owned.fn, ms)
	return owned
}

// Active reports whether the user callback may still fire.
func (t *Timer) Active() bool { return t != nil && t.active }
func (t *Timer) release() {
	if t.released {
		return
	}
	t.released = true
	t.fn.Release()
}

// Stop suppresses the callback and requests native cancellation.
func (t *Timer) Stop() {
	if t == nil || t.released {
		return
	}
	t.active = false
	t.callback = nil
	method := "clearTimeout"
	if t.interval {
		method = "clearInterval"
	}
	defer func() { _ = recover() }()
	js.Global().Call(method, t.id)
	t.release()
}

// Dispose satisfies the engine resource lifecycle.
func (t *Timer) Dispose() { t.Stop() }
