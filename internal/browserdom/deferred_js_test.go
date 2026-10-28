//go:build js && wasm

package browserdom

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

// nativeValue evaluates a JavaScript expression. The fixtures below are plain
// JavaScript because a fixture written as a Go function would itself be a call
// from JavaScript into Go, and these tests count exactly those calls.
func nativeValue(expression string) js.Value {
	return js.Global().Get("Function").New("return (" + expression + ")").Invoke()
}

// browserLikeTarget returns a JavaScript event target whose focus, blur and
// click methods dispatch the event of the same name before they return, as a
// browser does. Its listen method adds a native listener that appends a label
// to target.log.
func browserLikeTarget() js.Value {
	return nativeValue(`(function () {
		const target = new EventTarget();
		target.log = [];
		for (const name of ["focus", "blur", "click"]) {
			target[name] = function () { this.dispatchEvent(new Event(name)); };
		}
		target.listen = function (name, label) {
			target.addEventListener(name, function () { target.log.push(label); });
		};
		return target;
	})()`)
}

func logOf(target js.Value) string {
	log := target.Get("log")
	words := make([]string, log.Length())
	for i := range words {
		words[i] = log.Index(i).String()
	}
	return strings.Join(words, " ")
}

// Element.Dispatch, Focus, Blur and Click make the browser run the listeners of
// the event. A Go listener must not run inside that call: Go's js/wasm runtime
// can stop the program when JavaScript enters Go inside a call that Go made
// (see package m31labs.dev/gosx/client/jsutil). The Go listener runs once the
// browser has delivered the event to its other listeners, and before the
// method returns.
func TestGoListenersNeverRunInsideTheCallThatTriggersThem(t *testing.T) {
	probe := jsreentry.Install(t)
	calls := []struct {
		name, event string
		call        func(Element)
	}{
		{"Dispatch", "ping", func(e Element) { e.Dispatch("ping", false) }},
		{"Focus", "focus", Element.Focus},
		{"Blur", "blur", Element.Blur},
		{"Click", "click", Element.Click},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			probe.Reset()
			target := browserLikeTarget()
			element := FromJS(target)
			target.Call("listen", c.event, "js-before")
			listener := element.On(c.event, func(Event) { target.Get("log").Call("push", "go") })
			defer listener.Dispose()
			target.Call("listen", c.event, "js-after")
			c.call(element)
			probe.AssertNone(t, "%s", c.name)
			if got := logOf(target); got != "js-before js-after go" {
				t.Fatalf("the Go listener must have run when %s returns, after the other listeners: %q", c.name, got)
			}
		})
	}
}

// What the deferral keeps: Go listeners run in the order the browser called
// them, a listener that an earlier one disposes does not run, Once holds, an
// event that a listener dispatches reaches its listeners before that listener
// returns, and CurrentTarget names the element the listener is attached to.
func TestDeferredGoListenersKeepTheirOrderAndLifecycle(t *testing.T) {
	probe := jsreentry.Install(t)
	target := browserLikeTarget()
	element := FromJS(target)
	var order []string
	note := func(label string) { order = append(order, label) }

	var second, once *Listener
	first := element.On("ping", func(e Event) {
		note("first")
		if !e.CurrentTarget().Equal(element) || !e.Target().Equal(element) {
			t.Error("the event lost its target or current target")
		}
		second.Dispose()
	})
	second = element.On("ping", func(Event) { note("second") })
	third := element.On("ping", func(Event) {
		note("third:start")
		element.Dispatch("pong", false)
		note("third:end")
	})
	pong := element.On("pong", func(Event) { note("pong") })
	once = element.On("ping", func(Event) { note("once") }, ListenerOptions{Once: true})
	defer func() {
		for _, l := range []*Listener{first, second, third, pong, once} {
			l.Dispose()
		}
	}()

	element.Dispatch("ping", false)
	if got, want := strings.Join(order, " "), "first third:start pong third:end once"; got != want {
		t.Fatalf("first dispatch ran %q, want %q", got, want)
	}
	order = order[:0]
	element.Dispatch("ping", false)
	if got, want := strings.Join(order, " "), "first third:start pong third:end"; got != want {
		t.Fatalf("second dispatch ran %q, want %q", got, want)
	}
	probe.AssertNone(t, "listeners that dispatch, dispose and run once")
}

// An event that the page raises still reaches a Go listener at once, inside the
// browser's own dispatch, so PreventDefault keeps working there.
func TestGoListenersStillRunInsideDispatchesThatPageCodeStarts(t *testing.T) {
	probe := jsreentry.Install(t)
	target := nativeValue(`(function () {
		const target = new EventTarget();
		target.fire = function () {
			const event = new Event("press", { cancelable: true });
			target.notPrevented = target.dispatchEvent(event);
		};
		return target;
	})()`)
	ran := make(chan struct{})
	listener := FromJS(target).On("press", func(e Event) {
		e.PreventDefault()
		close(ran)
	})
	defer listener.Dispose()
	js.Global().Call("setTimeout", target.Get("fire"), 0)
	<-ran
	// The page's dispatch finishes only after Go has gone idle, so sleep until
	// it has recorded its result.
	for deadline := time.Now().Add(5 * time.Second); target.Get("notPrevented").IsUndefined(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the page's dispatch never finished")
		}
	}
	if target.Get("notPrevented").Bool() {
		t.Fatal("a Go listener of a page-raised event could not cancel it")
	}
	probe.AssertNone(t, "a page-raised event")
}

// Many goroutines dispatch to Go listeners while the collector runs. No
// dispatch may enter Go inside the browser's call, and every listener runs once.
func TestGoListenersNeverNestUnderConcurrencyAndGCPressure(t *testing.T) {
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
			target := browserLikeTarget()
			element := FromJS(target)
			hits := 0
			listener := element.On("ping", func(Event) { hits++ })
			defer listener.Dispose()
			var garbage [][]byte
			for i := 0; i < rounds; i++ {
				garbage = append(garbage[:0], make([]byte, 48<<10), make([]byte, 16<<10))
				element.Dispatch("ping", false)
			}
			_ = garbage
			if hits != rounds {
				failures <- fmt.Sprintf("goroutine %d: the listener ran %d times, want %d", g, hits, rounds)
			}
		}(g)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	probe.AssertNone(t, "concurrent dispatches")
	if probe.MaxDepth() != 1 {
		t.Fatalf("calls into Go nested to depth %d", probe.MaxDepth())
	}
}
