//go:build js && wasm

package browser

import (
	"strconv"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/browserdom"
	"m31labs.dev/gosx/internal/jsreentry"
)

// Navigate to a fragment of the current page makes the browser fire popstate
// before location.assign returns. A GoSX page's popstate handler
// (client/runtime/host/navigation.ts, onPopState) then asks the document
// whether a listener claims the history entry, with the cancelable
// gosx:popstate event, and navigates in place when none does. A Go listener must
// not run inside that call (see package m31labs.dev/gosx/client/jsutil). It runs
// when Navigate returns, after the page's listeners, so a Go gosx:popstate
// listener can no longer claim the entry.
//
// The fixture is plain JavaScript, because a fixture written as a Go function
// would itself be a call from JavaScript into Go. It needs a location that the
// test can replace, which a browser does not allow, so this test skips there;
// TestNavigateToAFragmentInABrowserNeverRunsGoListenersInsideTheCall uses the
// real one.
func TestNavigateToAFragmentNeverRunsGoListenersInsideTheCall(t *testing.T) {
	descriptor := js.Global().Get("Object").Call("getOwnPropertyDescriptor", js.Global(), "location")
	if descriptor.Truthy() && !descriptor.Get("configurable").Bool() {
		t.Skip("this host does not let a test replace location")
	}
	probe := jsreentry.Install(t)
	fixture := windowLikeGlobal(t)
	page := js.Global().Get("Function").New("window", "log", `
		const document = new EventTarget();
		const assigned = [];
		window.addEventListener("popstate", function () {
			const proceed = document.dispatchEvent(new Event("gosx:popstate", { cancelable: true }));
			log.push(proceed ? "page:navigates" : "page:claimed");
		});
		return {
			document,
			assigned,
			location: {
				assign(url) {
					assigned.push(url);
					window.dispatchEvent(new Event("popstate"));
				},
			},
		};`).Invoke(js.Global(), fixture.Get("log"))
	replaceBrowserGlobal(t, "location", page.Get("location"))
	log := fixture.Get("log")
	popstate := Window().On("popstate", func(Event) { log.Call("push", "go:popstate") })
	defer popstate.Dispose()
	claim := browserdom.Listen(page.Get("document"), "gosx:popstate", func(e browserdom.Event) {
		e.PreventDefault()
		log.Call("push", "go:gosx:popstate")
	})
	defer claim.Dispose()
	fixture.Call("listen", "popstate", "js-after")
	probe.Reset()

	Navigate("#next")

	probe.AssertNone(t, "Navigate")
	if got, want := fixtureLog(fixture), "page:navigates js-after go:gosx:popstate go:popstate"; got != want {
		t.Fatalf("the Go listeners must run when Navigate returns, after the page's, and cannot claim the entry:\n got %q\nwant %q", got, want)
	}
	if assigned := page.Get("assigned"); assigned.Length() != 1 || assigned.Index(0).String() != "#next" {
		t.Fatalf("location.assign saw %d call(s), want one with %q", assigned.Length(), "#next")
	}
}

// navigations numbers the fragments that the test below navigates to, so that
// each run (with -test.count above one) goes to a new fragment: a navigation to
// the current URL fires no popstate.
var navigations int

// The same call on a real page. The browser fires popstate for a navigation to a
// new fragment before location.assign returns, and fires hashchange later. This
// test needs a browser DOM and skips under the Node runner.
func TestNavigateToAFragmentInABrowserNeverRunsGoListenersInsideTheCall(t *testing.T) {
	if doc := js.Global().Get("document"); !doc.Truthy() || doc.Get("createElement").Type() != js.TypeFunction {
		t.Skip("requires a browser DOM")
	}
	probe := jsreentry.Install(t)
	fixture := js.Global().Get("Object").New()
	log := js.Global().Get("Array").New()
	fixture.Set("log", log)
	record := js.Global().Get("Function").New("log", "label", `return function () { log.push(label); };`)
	before, after := record.Invoke(log, "js-before"), record.Invoke(log, "js-after")
	window := js.Global()
	window.Call("addEventListener", "popstate", before)
	defer window.Call("removeEventListener", "popstate", before)
	popstate := Window().On("popstate", func(Event) { log.Call("push", "go") })
	defer popstate.Dispose()
	window.Call("addEventListener", "popstate", after)
	defer window.Call("removeEventListener", "popstate", after)
	navigations++
	fragment := "#gosx-navigate-test-" + strconv.Itoa(navigations)
	probe.Reset()

	Navigate(fragment)

	probe.AssertNone(t, "Navigate")
	if got, want := fixtureLog(fixture), "js-before js-after go"; got != want {
		t.Fatalf("the Go popstate listener must run when Navigate returns, after the page's:\n got %q\nwant %q", got, want)
	}
	if hash := CurrentLocation().Hash; hash != fragment {
		t.Fatalf("the location hash is %q, want %q", hash, fragment)
	}
}
