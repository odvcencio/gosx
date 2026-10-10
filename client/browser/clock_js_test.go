//go:build js && wasm

package browser

import (
	"math"
	"syscall/js"
	"testing"
)

func browserMethod(t *testing.T, target js.Value, name string, fn func(js.Value, []js.Value) any) js.Func {
	t.Helper()
	callback := js.FuncOf(fn)
	target.Set(name, callback)
	t.Cleanup(callback.Release)
	return callback
}
func replaceBrowserGlobal(t *testing.T, name string, value js.Value) {
	t.Helper()
	g, object := js.Global(), js.Global().Get("Object")
	previous := object.Call("getOwnPropertyDescriptor", g, name)
	object.Call("defineProperty", g, name, map[string]any{"value": value, "writable": true, "configurable": true})
	t.Cleanup(func() {
		if previous.IsUndefined() {
			g.Delete(name)
		} else {
			object.Call("defineProperty", g, name, previous)
		}
	})
}

func TestTimersOwnObjectIDsAndSelfDispose(t *testing.T) {
	g := js.Global()
	previousTimeout, previousInterval, previousClearTimeout, previousClearInterval := g.Get("setTimeout"), g.Get("setInterval"), g.Get("clearTimeout"), g.Get("clearInterval")
	t.Cleanup(func() {
		g.Set("setTimeout", previousTimeout)
		g.Set("setInterval", previousInterval)
		g.Set("clearTimeout", previousClearTimeout)
		g.Set("clearInterval", previousClearInterval)
	})
	id := g.Get("Object").New()
	id.Set("timer", 1)
	var callback js.Value
	var delay float64
	sets, clears, calls := 0, 0, 0
	set := js.FuncOf(func(_ js.Value, args []js.Value) any { sets++; callback = args[0]; delay = args[1].Float(); return id })
	defer set.Release()
	clear := js.FuncOf(func(_ js.Value, args []js.Value) any {
		clears++
		if !args[0].Equal(id) {
			t.Error("timer ID changed")
		}
		return nil
	})
	defer clear.Release()
	g.Set("setTimeout", timerHostWrapper(previousTimeout, set.Value))
	g.Set("setInterval", set)
	g.Set("clearTimeout", g.Get("Function").New("original", "capture", "token", "return function(id){return id===token?capture(id):original(id)}").Invoke(previousClearTimeout, clear, id))
	g.Set("clearInterval", clear)
	if Timeout(nil, 1) != nil || sets != 0 {
		t.Fatal("nil callback scheduled")
	}
	one := Timeout(func() { calls++ }, math.NaN())
	if delay != 0 || !one.Active() {
		t.Fatal("timeout registration")
	}
	callback.Invoke()
	if calls != 1 || one.Active() || !one.released {
		t.Fatal("timeout did not retire itself")
	}
	one.Stop()
	if clears != 0 {
		t.Fatal("fired timeout canceled again")
	}
	cancelled := Timeout(func() { t.Fatal("cancelled timer fired") }, -1)
	cancelled.Stop()
	cancelled.Stop()
	if clears != 1 || cancelled.Active() {
		t.Fatal("timeout cancellation duplicated")
	}
	var repeated *Timer
	repeated = Interval(func() { calls++; repeated.Stop(); repeated.Dispose() }, math.Inf(1))
	if delay != math.MaxInt32 {
		t.Fatal("interval delay was not bounded")
	}
	callback.Invoke()
	if calls != 2 || clears != 2 || repeated.Active() {
		t.Fatal("interval self-disposal")
	}
}

func TestTimerCancelFailureRetainsInertHandlerForRetry(t *testing.T) {
	g := js.Global()
	oldSet, oldClear := g.Get("setInterval"), g.Get("clearInterval")
	defer func() { g.Set("setInterval", oldSet); g.Set("clearInterval", oldClear) }()
	var callback js.Value
	set := js.FuncOf(func(_ js.Value, args []js.Value) any { callback = args[0]; return 42 })
	defer set.Release()
	g.Set("setInterval", set)
	g.Set("clearInterval", g.Get("Function").New("throw new Error('cancel denied')"))
	calls := 0
	timer := Interval(func() { calls++ }, 1)
	timer.Stop()
	if timer.Active() || timer.released || timer.callback != nil {
		t.Fatal("failed cancellation retained app callback or released live host handler")
	}
	callback.Invoke()
	if calls != 0 {
		t.Fatal("failed cancellation delivered app callback")
	}
	clears := 0
	clear := js.FuncOf(func(js.Value, []js.Value) any { clears++; return nil })
	defer clear.Release()
	g.Set("clearInterval", clear)
	timer.Stop()
	timer.Stop()
	if clears != 1 || !timer.released {
		t.Fatal("failed cancellation could not retry")
	}
}

func TestTimerConstructionFailureHasNoLateAppCallback(t *testing.T) {
	g := js.Global()
	old := g.Get("setTimeout")
	defer g.Set("setTimeout", old)
	g.Set("setTimeout", js.Undefined())
	if Timeout(func() { t.Fatal("unsupported timer") }, 1) != nil {
		t.Fatal("missing scheduler accepted")
	}
	var retained js.Value
	capture := js.FuncOf(func(_ js.Value, args []js.Value) any { retained = args[0]; return nil })
	defer capture.Release()
	// The custom host retains the function and then throws before returning ID.
	set := g.Get("Function").New("capture", "return function(fn){ capture(fn); throw new Error('scheduler failed') }").Invoke(capture)
	g.Set("setTimeout", timerHostWrapper(old, set))
	if Timeout(func() { t.Fatal("failed constructor invoked app callback") }, 1) != nil {
		t.Fatal("throwing scheduler accepted")
	}
	retained.Invoke() // A late terminal timeout safely releases its inert handler.
}

func TestNowUsesCachedClockObjectWithFreshReadings(t *testing.T) {
	old := performanceClock
	defer func() { performanceClock = old }()
	clock := js.Global().Get("Object").New()
	reads := 0
	now := js.FuncOf(func(js.Value, []js.Value) any { reads++; return float64(reads) * 2.5 })
	defer now.Release()
	clock.Set("now", now)
	performanceClock = clock
	if Now() != 2.5 || Now() != 5 || reads != 2 {
		t.Fatal("clock returned cached readings")
	}
	if WallNow() <= 0 {
		t.Fatal("wall clock unavailable")
	}
}

// The Go WASM scheduler also calls global setTimeout. Forward its native
// callbacks unchanged while intercepting only Go function wrappers under test.
func timerHostWrapper(original, capture js.Value) js.Value {
	return js.Global().Get("Function").New("original", "capture", "return function(fn,ms){if(Function.prototype.toString.call(fn).includes('_pendingEvent'))return capture(fn,ms);return Reflect.apply(original,this,arguments)}").Invoke(original, capture)
}
