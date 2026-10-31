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

// validityTarget returns a JavaScript event target that checks its validity as a
// form control does. While target.valid is false, checkValidity and
// reportValidity fire a cancelable invalid event before they return.
// reportValidity then logs "reported" if no listener canceled the event during
// the dispatch, and "withheld" if one did: the browser decides that when the
// dispatch ends. The listen method adds a native listener that appends a label
// to target.log.
func validityTarget() js.Value {
	return nativeValue(`(function () {
		const target = new EventTarget();
		target.log = [];
		target.valid = false;
		function fire() {
			return target.dispatchEvent(new Event("invalid", { cancelable: true }));
		}
		target.checkValidity = function () {
			if (target.valid) return true;
			fire();
			return false;
		};
		target.reportValidity = function () {
			if (target.valid) return true;
			target.log.push(fire() ? "reported" : "withheld");
			return false;
		};
		target.listen = function (name, label) {
			target.addEventListener(name, function () { target.log.push(label); });
		};
		return target;
	})()`)
}

// CheckValidity and ReportValidity make the browser fire invalid before they
// return, and ReportValidity then reports the problem unless a listener canceled
// the event. A Go listener must not run inside the call (see package
// m31labs.dev/gosx/client/jsutil), so it runs after the browser has delivered the
// event, and its PreventDefault comes too late to withhold the report.
func TestValidityChecksNeverRunGoListenersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	calls := []struct {
		name string
		call func(Element) bool
		want string
	}{
		{"CheckValidity", Element.CheckValidity, "js-before js-after go"},
		{"ReportValidity", Element.ReportValidity, "js-before js-after reported go"},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			probe.Reset()
			target := validityTarget()
			element := FromJS(target)
			target.Call("listen", "invalid", "js-before")
			listener := element.On("invalid", func(e Event) {
				e.PreventDefault()
				target.Get("log").Call("push", "go")
			})
			defer listener.Dispose()
			target.Call("listen", "invalid", "js-after")
			if c.call(element) {
				t.Fatalf("%s returned true for an invalid control", c.name)
			}
			probe.AssertNone(t, "%s", c.name)
			if got := logOf(target); got != c.want {
				t.Fatalf("%s: the Go listener must run when the call returns, after the other listeners, and cannot withhold the report: got %q, want %q", c.name, got, c.want)
			}

			// A valid control fires nothing, and the result comes back unchanged.
			probe.Reset()
			target.Get("log").Set("length", 0)
			target.Set("valid", true)
			if !c.call(element) {
				t.Fatalf("%s returned false for a valid control", c.name)
			}
			probe.AssertNone(t, "%s of a valid control", c.name)
			if got := logOf(target); got != "" {
				t.Fatalf("%s of a valid control ran listeners: %q", c.name, got)
			}
		})
	}
}

// focusedChild returns a JavaScript parent and a child that has focus. Taking
// the child out of the parent, with child.remove or a write to parent.textContent
// or parent.innerHTML, makes the browser fire blur and focusout on the child
// before the call returns, while the child is still connected, as Chrome for
// Testing 156 does. The child is disconnected when the call returns. The listen
// method adds a native listener to the child that appends a label, and whether
// the child is connected then, to log.
func focusedChild() js.Value {
	return nativeValue(`(function () {
		const log = [];
		const parent = new EventTarget();
		const child = new EventTarget();
		child.isConnected = true;
		function removeFocused() {
			child.dispatchEvent(new Event("blur"));
			child.dispatchEvent(new Event("focusout", { bubbles: true }));
			child.isConnected = false;
		}
		child.remove = removeFocused;
		for (const name of ["textContent", "innerHTML"]) {
			Object.defineProperty(parent, name, {
				configurable: true,
				get() { return ""; },
				set(value) { removeFocused(); },
			});
		}
		return {
			log, parent, child,
			listen(name, label) {
				child.addEventListener(name, function () {
					log.push(label + ":" + name + (child.isConnected ? ":connected" : ":removed"));
				});
			},
		};
	})()`)
}

// Taking the focused element out of the document fires blur and focusout before
// the call returns: Element.Remove on the element, and SetText and ReplaceMarkup
// on an ancestor. A Go listener of those events must not run inside the call. It
// runs when the call returns, after the other listeners and after the removal.
func TestRemovingTheFocusedNodeNeverRunsGoListenersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	calls := []struct {
		name string
		call func(parent, child Element)
	}{
		{"Remove", func(_, child Element) { child.Remove() }},
		{"SetText", func(parent, _ Element) { parent.SetText("replacement") }},
		{"ReplaceMarkup", func(parent, _ Element) { parent.ReplaceMarkup(Markup("<b>replacement</b>")) }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			probe.Reset()
			fixture := focusedChild()
			parent, child := FromJS(fixture.Get("parent")), FromJS(fixture.Get("child"))
			log := fixture.Get("log")
			var listeners []*Listener
			for _, name := range []string{"blur", "focusout"} {
				fixture.Call("listen", name, "js-before")
				listeners = append(listeners, child.On(name, func(e Event) {
					if !e.CurrentTarget().Equal(child) {
						t.Error("the event lost its current target")
					}
					state := "removed"
					if child.Connected() {
						state = "connected"
					}
					log.Call("push", "go:"+e.Type()+":"+state)
				}))
				fixture.Call("listen", name, "js-after")
			}
			defer func() {
				for _, l := range listeners {
					l.Dispose()
				}
			}()
			c.call(parent, child)
			probe.AssertNone(t, "%s", c.name)
			want := "js-before:blur:connected js-after:blur:connected js-before:focusout:connected js-after:focusout:connected go:blur:removed go:focusout:removed"
			if got := logOf(fixture); got != want {
				t.Fatalf("%s: the Go listeners must run when the call returns, after the other listeners and after the removal:\n got %q\nwant %q", c.name, got, want)
			}
		})
	}
}

// recovered runs f and returns what it panicked with, or nil.
func recovered(f func()) (value any) {
	defer func() { value = recover() }()
	f()
	return nil
}

// A write that the browser refuses throws in the setter, for example innerHTML
// under a Trusted Types policy. js.Value.Set lets that exception out through the
// Go runtime and ends the program. Through jsutil.Call it becomes a panic that
// the caller can recover.
func TestWritesThatTheBrowserRefusesPanicInsteadOfEndingTheProgram(t *testing.T) {
	refusing := nativeValue(`(function () {
		const target = {};
		for (const name of ["textContent", "innerHTML"]) {
			Object.defineProperty(target, name, { set(value) { throw new TypeError("blocked by policy"); } });
		}
		return target;
	})()`)
	writes := []struct {
		name  string
		write func(Element)
	}{
		{"SetText", func(e Element) { e.SetText("replacement") }},
		{"ReplaceMarkup", func(e Element) { e.ReplaceMarkup(Markup("<b>replacement</b>")) }},
	}
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			failure := recovered(func() { w.write(FromJS(refusing)) })
			err, ok := failure.(js.Error)
			if !ok || !strings.Contains(err.Error(), "blocked by policy") {
				t.Fatalf("%s panicked with %v (%T), want the JavaScript error", w.name, failure, failure)
			}
		})
	}
}
