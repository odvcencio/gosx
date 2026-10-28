//go:build js && wasm

package client

import (
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/jsreentry"
)

// installNativeBinding installs window.__gosx.netplay as plain JavaScript that
// reports a failure the way client/runtime/host/netplay.ts does: open() calls
// cfg.onState("failed") before it returns 0 when the connection cannot start,
// and signal(id, "netplay.failure", ...) calls it before it returns. A fixture
// written as a Go function would itself be a call from JavaScript into Go,
// which these tests count.
func installNativeBinding(t *testing.T, failOpen bool) js.Value {
	t.Helper()
	g := js.Global()
	hadHost := !g.Get("__gosx").IsUndefined()
	if !hadHost {
		g.Set("__gosx", g.Get("Object").New())
	}
	host := g.Get("__gosx")
	previous := host.Get("netplay")
	api := g.Get("Function").New(`
		const api = { closed: [] };
		api.supported = function () { return true; };
		api.open = function (cfg) {
			api.cfg = cfg;
			if (!api.failOpen) return 7;
			cfg.onSignal("netplay.offer", '{"session":"s1"}');
			cfg.onState("failed");
			return 0;
		};
		api.signal = function (id, event) {
			if (event === "netplay.failure") api.cfg.onState("failed");
		};
		api.send = function () { return true; };
		api.recv = function () { return -1; };
		api.close = function (id) { api.closed.push(id); };
		return api;`).Invoke()
	api.Set("failOpen", failOpen)
	host.Set("netplay", api)
	t.Cleanup(func() {
		host.Set("netplay", previous)
		if !hadHost {
			g.Set("__gosx", js.Undefined())
		}
	})
	return api
}

// The binding reports "failed" synchronously when open() cannot start a
// connection and when signal() carries a failure. A Go callback must not run
// inside those calls (see package m31labs.dev/gosx/client/jsutil). It runs
// once the binding call has returned, and before Open or Signal returns, so
// the caller still sees the link fail before it sees Open's error.
func TestJSLinkStateCallbacksNeverRunInsideTheCallIntoTheBinding(t *testing.T) {
	probe := jsreentry.Install(t)

	t.Run("Open fails at once", func(t *testing.T) {
		probe.Reset() // a browser's stdout shim also calls into Go, for the "=== RUN" line
		installNativeBinding(t, true)
		link, err := newBrowserLink()
		if err != nil {
			t.Fatal(err)
		}
		var seen []string
		err = link.Open(rtcConfig{Session: "s1"},
			func(event string, _ []byte) { seen = append(seen, "signal:"+event) },
			func(state string) { seen = append(seen, "state:"+state) })
		if err == nil {
			t.Fatal("a refused connection must make Open fail")
		}
		if got := strings.Join(seen, " "); got != "signal:netplay.offer state:failed" {
			t.Fatalf("both callbacks must have run, in order, before Open returned: %q", got)
		}
		probe.AssertNone(t, "Open")
	})

	t.Run("Signal carries a failure", func(t *testing.T) {
		probe.Reset()
		api := installNativeBinding(t, false)
		link, err := newBrowserLink()
		if err != nil {
			t.Fatal(err)
		}
		var states []string
		if err := link.Open(rtcConfig{Session: "s1"}, func(string, []byte) {}, func(state string) { states = append(states, state) }); err != nil {
			t.Fatal(err)
		}
		link.Signal("netplay.failure", []byte(`{"session":"s1"}`))
		if got := strings.Join(states, " "); got != "failed" {
			t.Fatalf("the failure must have reached the Go callback before Signal returned: %q", got)
		}
		probe.AssertNone(t, "Signal")
		link.Close()
		if api.Get("closed").Length() != 1 {
			t.Fatalf("Close reached the binding %d times", api.Get("closed").Length())
		}
	})
}
