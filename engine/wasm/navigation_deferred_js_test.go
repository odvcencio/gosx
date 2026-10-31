//go:build js && wasm

package wasm

import (
	"context"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/browserdom"
	"m31labs.dev/gosx/internal/jsreentry"
)

// navigationHost returns a JavaScript engine context whose navigate behaves as
// the page's does: it is an async function that dispatches the navigation
// events on the document before its first await. A same-document navigation
// (a hash change, or the current URL) dispatches gosx:navigate there too, after
// it moves focus. host.listeners stands for the document, host.listen adds a
// page-script listener, and host.log collects what the page scripts and the Go
// listeners saw. It is plain JavaScript, because a fixture written as a Go
// function would itself be a call from JavaScript into Go.
func navigationHost() js.Value {
	return js.Global().Get("Function").New(`return (function () {
		const host = {
			log: [],
			listeners: new EventTarget(),
			listen(type, label) {
				host.listeners.addEventListener(type, function () { host.log.push(label); });
			},
			navigate: async function (target, options) {
				host.target = target;
				host.listeners.dispatchEvent(new CustomEvent("gosx:navigation:state", { detail: { reason: "navigate:start" } }));
				host.listeners.dispatchEvent(new CustomEvent("gosx:navigate", { detail: { url: target } }));
				await Promise.resolve();
				return true;
			},
		};
		return host;
	})()`).Invoke()
}

// Context.Navigate calls the page's async navigate(). Before its first await,
// that function dispatches gosx:navigation:state on the document, and a
// same-document navigation also dispatches gosx:navigate. A Go listener of those
// events must not run inside the call that starts the navigation: Go's js/wasm
// runtime can stop the program when JavaScript enters Go inside a call that Go
// made (see package m31labs.dev/gosx/client/jsutil). The Go listeners run after
// the page's listeners have run, in the order the events were dispatched, and
// before Navigate returns.
func TestNavigateNeverRunsGoListenersInsideTheCall(t *testing.T) {
	probe := jsreentry.Install(t)
	host := navigationHost()
	ctx := Context{value: host}
	log := host.Get("log")
	host.Call("listen", "gosx:navigation:state", "js:state")
	host.Call("listen", "gosx:navigate", "js:navigate")
	state := browserdom.Listen(host.Get("listeners"), "gosx:navigation:state", func(browserdom.Event) {
		log.Call("push", "go:state")
	})
	defer state.Dispose()
	navigated := browserdom.Listen(host.Get("listeners"), "gosx:navigate", func(browserdom.Event) {
		log.Call("push", "go:navigate")
	})
	defer navigated.Dispose()
	probe.Reset()

	if err := ctx.Navigate(context.Background(), "/next", NavigationOptions{}); err != nil {
		t.Fatal(err)
	}
	probe.AssertNone(t, "Navigate")
	if got, want := hostLog(host), "js:state js:navigate go:state go:navigate"; got != want {
		t.Fatalf("the Go listeners must have run when Navigate returns, after the page's listeners: got %q, want %q", got, want)
	}
	if got := host.Get("target").String(); got != "/next" {
		t.Fatalf("navigate saw target %q", got)
	}
}
