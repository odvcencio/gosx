//go:build js && wasm

package jsutil

import (
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/jsreentry"
)

// text converts any JavaScript value the way String(v) does.
func text(v js.Value) string { return js.Global().Get("String").Invoke(v).String() }

// A call that JavaScript makes to a Callback outside Call, for example from a
// timer, enters Go at once with the arguments and the receiver it was given.
func TestCallbackEntersGoAtOnceWhenJavaScriptCallsItOutsideCall(t *testing.T) {
	probe := jsreentry.Install(t)
	type seen struct {
		args []string
		this string
	}
	got := make(chan seen, 1)
	callback := NewCallback(func(this js.Value, args []js.Value) {
		s := seen{this: text(this)}
		for _, arg := range args {
			s.args = append(s.args, text(arg))
		}
		got <- s
	})
	defer callback.Release()
	holder := native(`({ name: "holder", toString() { return "holder"; } })`)
	holder.Set("callback", callback.Value())
	js.Global().Call("setTimeout", native(`function (holder) { return function () { holder.callback("a", "b"); }; }`).Invoke(holder), 0)
	select {
	case s := <-got:
		if strings.Join(s.args, ",") != "a,b" || s.this != "holder" {
			t.Fatalf("callback saw args %v and this %q", s.args, s.this)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the callback never ran")
	}
	probe.AssertNone(t, "a callback that a timer calls")
}

// Inside Call, JavaScript only records the callbacks it calls. Call runs them
// after the JavaScript call has returned, in the order JavaScript called them,
// with the arguments and the receiver of each call, and before it returns.
func TestCallRunsRecordedCallbacksInOrderAfterTheJavaScriptCallReturns(t *testing.T) {
	probe := jsreentry.Install(t)
	target := native(`({
		finished: false,
		fire(first, second, third) {
			first("one", 1);
			this.holder.callback("two");
			second();
			third("three");
			this.finished = true;
			return "result";
		},
	})`)
	var order []string
	var doneWhenRun []bool
	record := func(label string) *Callback {
		return NewCallback(func(this js.Value, args []js.Value) {
			parts := []string{label}
			for _, arg := range args {
				parts = append(parts, text(arg))
			}
			order = append(order, strings.Join(parts, ":"))
			doneWhenRun = append(doneWhenRun, target.Get("finished").Bool())
			if label == "method" && !this.Equal(target.Get("holder")) {
				t.Error("a callback called as a method lost its receiver")
			}
			if label != "method" && !this.IsUndefined() {
				t.Errorf("%s: a callback called as a function saw this = %v", label, this)
			}
		})
	}
	first, method, second, third := record("first"), record("method"), record("second"), record("third")
	defer first.Release()
	defer method.Release()
	defer second.Release()
	defer third.Release()
	holder := js.Global().Get("Object").New()
	holder.Set("callback", method.Value())
	target.Set("holder", holder)
	probe.Reset()

	result := Call(target, "fire", first.Value(), second.Value(), third.Value())
	if result.String() != "result" {
		t.Fatalf("Call lost the result: %v", result)
	}
	if got, want := strings.Join(order, " "), "first:one:1 method:two second third:three"; got != want {
		t.Fatalf("callbacks ran %q, want %q", got, want)
	}
	for i, done := range doneWhenRun {
		if !done {
			t.Fatalf("callback %d ran before the JavaScript call finished", i)
		}
	}
	probe.AssertNone(t, "callbacks recorded inside Call")
}

// A JavaScript error ends the call, and Call still runs what was recorded
// before it, then raises the error as js.Value.Call does. The gate returns to
// its normal state: the next call from JavaScript enters Go at once.
func TestCallRunsRecordedCallbacksWhenJavaScriptThrowsAndRestoresTheGate(t *testing.T) {
	probe := jsreentry.Install(t)
	target := native(`({ fire(callback) { callback("before"); throw new Error("boom"); } })`)
	var order []string
	entered := make(chan string, 2)
	callback := NewCallback(func(_ js.Value, args []js.Value) {
		order = append(order, args[0].String())
		select {
		case entered <- args[0].String():
		default:
		}
	})
	defer callback.Release()
	probe.Reset()

	func() {
		defer func() {
			failure := recover()
			if failure == nil || !strings.Contains(fmt.Sprint(failure), "boom") {
				t.Fatalf("the JavaScript error was lost: %v", failure)
			}
		}()
		Call(target, "fire", callback.Value())
	}()
	if strings.Join(order, " ") != "before" {
		t.Fatalf("the callback recorded before the error did not run: %q", order)
	}
	<-entered
	probe.AssertNone(t, "a call that throws")

	// The gate is back to forwarding: a timer reaches Go at once.
	js.Global().Call("setTimeout", callback.Value(), 0, "later")
	select {
	case got := <-entered:
		if got != "later" {
			t.Fatalf("got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("after a failed Call the callback no longer enters Go: the gate stayed in recording mode")
	}
}

// A callback that runs after the JavaScript call may call Call itself. The
// callbacks of the inner call run before the inner Call returns, so the order is
// depth first, as it is when listeners run inside the call.
func TestCallbackThatCallsCallRunsItsCallbacksBeforeItContinues(t *testing.T) {
	probe := jsreentry.Install(t)
	target := native(`({ fire(callback, ...rest) { callback(...rest); } })`)
	var order []string
	var outer, inner *Callback
	inner = NewCallback(func(js.Value, []js.Value) { order = append(order, "inner") })
	outer = NewCallback(func(js.Value, []js.Value) {
		order = append(order, "outer:start")
		Call(target, "fire", inner.Value())
		order = append(order, "outer:end")
	})
	defer outer.Release()
	defer inner.Release()
	probe.Reset()
	Call(target, "fire", outer.Value())
	if got, want := strings.Join(order, " "), "outer:start inner outer:end"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	probe.AssertNone(t, "nested Call")
}

// A function made with js.FuncOf can still enter Go inside a Call. If that Go
// code makes a Call of its own, the inner Call runs only the calls it recorded
// itself. The calls recorded before the entry belong to the outer Call, which
// runs them after the JavaScript call has returned.
func TestInnerCallRunsOnlyItsOwnRecordedCalls(t *testing.T) {
	target := native(`({ fire(first, foreign) { first(); foreign(); } , inner(second) { second(); } })`)
	var order []string
	first := NewCallback(func(js.Value, []js.Value) { order = append(order, "first") })
	second := NewCallback(func(js.Value, []js.Value) { order = append(order, "second") })
	defer first.Release()
	defer second.Release()
	foreign := js.FuncOf(func(js.Value, []js.Value) any {
		order = append(order, "foreign:start")
		Call(target, "inner", second.Value())
		order = append(order, "foreign:end")
		return nil
	})
	defer foreign.Release()
	Call(target, "fire", first.Value(), foreign.Value)
	if got, want := strings.Join(order, " "), "foreign:start second foreign:end first"; got != want {
		t.Fatalf("got %q, want %q: the inner Call must not run the outer Call's recorded calls", got, want)
	}
}

// A callback that was recorded and then released before Call reached it does not
// run, so a listener that an earlier listener disposed stays silent.
func TestReleasedCallbackIsDroppedFromTheRecordedCalls(t *testing.T) {
	probe := jsreentry.Install(t)
	target := native(`({ fire(a, b) { a(); b(); } })`)
	var order []string
	var second *Callback
	first := NewCallback(func(js.Value, []js.Value) {
		order = append(order, "first")
		second.Release()
	})
	second = NewCallback(func(js.Value, []js.Value) { order = append(order, "second") })
	defer first.Release()
	probe.Reset()
	Call(target, "fire", first.Value(), second.Value())
	if strings.Join(order, " ") != "first" {
		t.Fatalf("got %q, want only the first callback", order)
	}
	second.Release() // idempotent
	probe.AssertNone(t, "released callback")
}

// With no live Callback, Call is js.Value.Call.
func TestCallWithoutCallbacksIsAPlainCall(t *testing.T) {
	target := native(`({ add(a, b) { return a + b; } })`)
	if got := Call(target, "add", 2, 3).Int(); got != 5 {
		t.Fatalf("Call = %d", got)
	}
	callback := NewCallback(func(js.Value, []js.Value) {})
	callback.Release()
	if got := Call(target, "add", 4, 5).Int(); got != 9 {
		t.Fatalf("Call after release = %d", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("calling a missing method must panic, as js.Value.Call does")
			}
		}()
		Call(target, "missing")
	}()
}

// A function made with js.FuncOf is not a Callback. It still enters Go inside
// the call, and the probe sees it. This is the boundary the package
// documentation states.
func TestPlainFuncOfStillEntersGoInsideCall(t *testing.T) {
	probe := jsreentry.Install(t)
	target := native(`({ fire(callback) { callback(); } })`)
	keeper := NewCallback(func(js.Value, []js.Value) {})
	defer keeper.Release()
	ran := false
	raw := js.FuncOf(func(js.Value, []js.Value) any { ran = true; return nil })
	defer raw.Release()
	probe.Reset()
	Call(target, "fire", raw)
	if !ran || probe.Count() != 1 {
		t.Fatalf("a js.Func must still enter Go inside Call: ran=%v count=%d", ran, probe.Count())
	}
}

// Many goroutines call Call at once with a busy collector. Every callback runs
// once, none of them inside a JavaScript call, and the gate ends in its normal
// state.
func TestCallFromManyGoroutinesRunsEveryCallbackOnceWithoutNesting(t *testing.T) {
	previous := debug.SetGCPercent(5)
	defer debug.SetGCPercent(previous)
	probe := jsreentry.Install(t)
	const goroutines, rounds = 16, 24
	var wg sync.WaitGroup
	failures := make(chan string, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			target := native(`({ fire(a, b) { a(1); b(2); a(3); } })`)
			var a, b int
			ca := NewCallback(func(_ js.Value, args []js.Value) { a += args[0].Int() })
			cb := NewCallback(func(_ js.Value, args []js.Value) { b += args[0].Int() })
			defer ca.Release()
			defer cb.Release()
			var garbage [][]byte
			for i := 0; i < rounds; i++ {
				garbage = append(garbage[:0], make([]byte, 48<<10), make([]byte, 16<<10))
				Call(target, "fire", ca.Value(), cb.Value())
			}
			_ = garbage
			if a != 4*rounds || b != 2*rounds {
				failures <- fmt.Sprintf("goroutine %d: sums a=%d b=%d, want %d and %d", g, a, b, 4*rounds, 2*rounds)
			}
		}(g)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	probe.AssertNone(t, "concurrent Call")
	if probe.MaxDepth() != 1 {
		t.Fatalf("calls into Go nested to depth %d", probe.MaxDepth())
	}
	theGate.mu.Lock()
	active := len(theGate.frames)
	theGate.mu.Unlock()
	if active != 0 {
		t.Fatalf("the gate was left with %d active calls", active)
	}
}

// Calls from different goroutines can overlap: one goroutine's JavaScript call
// has returned and it has not yet finished its Call when another goroutine starts
// its own. The test makes that order by hand. Each Call runs only the calls
// recorded during its own JavaScript call, whichever ends first, and the gate
// forwards again once both have ended.
func TestOverlappingCallsEachRunOnlyTheirOwnRecordedCalls(t *testing.T) {
	var order []string
	a := NewCallback(func(js.Value, []js.Value) { order = append(order, "a") })
	b := NewCallback(func(js.Value, []js.Value) { order = append(order, "b") })
	defer a.Release()
	defer b.Release()
	g := &theGate

	first := g.begin()
	a.Value().Invoke() // the JavaScript call of the first Call records a
	second := g.begin()
	b.Value().Invoke() // the JavaScript call of the second Call records b
	g.end(first)       // the first Call ends first, though the second began later
	if got := strings.Join(order, " "); got != "a" {
		t.Fatalf("the first Call ran %q, want only a", got)
	}
	b.Value().Invoke() // the second Call's trap is still in place: recorded again
	if got := strings.Join(order, " "); got != "a" {
		t.Fatalf("after the first Call ended the second Call's trap was gone: b ran at once: %q", got)
	}
	g.end(second)
	if got := strings.Join(order, " "); got != "a b b" {
		t.Fatalf("after both ended: %q, want a b b", got)
	}

	// Both Calls have ended: a call from JavaScript enters Go at once.
	entered := make(chan struct{}, 1)
	c := NewCallback(func(js.Value, []js.Value) { entered <- struct{}{} })
	defer c.Release()
	js.Global().Call("setTimeout", c.Value(), 0)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the gate still recorded after both Calls ended")
	}
}
