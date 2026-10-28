//go:build js && wasm

package browser

import (
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/jsreentry"
)

// windowLikeGlobal gives the Node global object the three window methods that
// the typed services use: addEventListener, removeEventListener and
// dispatchEvent, backed by a native EventTarget. It also adds the ErrorEvent
// class that a browser has. It returns the log that the native listeners write.
// Everything here is plain JavaScript, because a fixture written as a Go
// function would itself be a call from JavaScript into Go.
func windowLikeGlobal(t *testing.T) js.Value {
	t.Helper()
	fixture := js.Global().Get("Function").New(`
		const target = new EventTarget();
		const log = [];
		class ErrorEvent extends Event {
			constructor(type, init) { super(type); this.message = init.message; this.filename = init.filename; }
		}
		return {
			log,
			ErrorEvent,
			addEventListener: target.addEventListener.bind(target),
			removeEventListener: target.removeEventListener.bind(target),
			dispatchEvent: target.dispatchEvent.bind(target),
			listen(name, label) { target.addEventListener(name, function () { log.push(label); }); },
		};`).Invoke()
	for _, name := range []string{"addEventListener", "removeEventListener", "dispatchEvent", "ErrorEvent"} {
		replaceBrowserGlobal(t, name, fixture.Get(name))
	}
	return fixture
}

func fixtureLog(fixture js.Value) string {
	log := fixture.Get("log")
	words := make([]string, log.Length())
	for i := range words {
		words[i] = log.Index(i).String()
	}
	return strings.Join(words, " ")
}

// DispatchResize and DispatchError make the browser run the window's listeners.
// A Go listener must not run inside that call (see package
// m31labs.dev/gosx/client/jsutil). It runs once the browser has delivered the
// event to its other listeners, and before the function returns.
func TestDispatchResizeAndErrorNeverRunGoListenersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	fixture := windowLikeGlobal(t)
	cases := []struct {
		name, event string
		dispatch    func() error
	}{
		{"DispatchResize", "resize", func() error { DispatchResize(); return nil }},
		{"DispatchError", "error", func() error { return DispatchError("boom", "app.go") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probe.Reset()
			fixture.Get("log").Set("length", 0)
			fixture.Call("listen", c.event, "js-before")
			var message string
			listener := Window().On(c.event, func(e Event) {
				message = e.Type()
				fixture.Get("log").Call("push", "go")
			})
			defer listener.Dispose()
			fixture.Call("listen", c.event, "js-after")
			if err := c.dispatch(); err != nil {
				t.Fatal(err)
			}
			probe.AssertNone(t, "%s", c.name)
			if got := fixtureLog(fixture); got != "js-before js-after go" {
				t.Fatalf("the Go listener must have run when %s returns, after the other listeners: %q", c.name, got)
			}
			if message != c.event {
				t.Fatalf("the Go listener saw event %q, want %q", message, c.event)
			}
		})
	}
}
