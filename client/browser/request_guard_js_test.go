//go:build js && wasm

package browser

import (
	"os"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/jsutil"
)

func testBrowserWindow(t *testing.T) js.Value {
	t.Helper()
	global := js.Global()
	previous := global.Get("window")
	window := global.Get("Object").New()
	window.Set("location", map[string]any{"href": "https://example.test/"})
	window.Set("navigator", global.Get("Object").New())
	window.Set("document", map[string]any{"cookie": ""})
	window.Set("__gosx", global.Get("Object").New())
	global.Set("window", window)
	t.Cleanup(func() { global.Set("window", previous) })
	return window
}

func loadRequestGuard(t *testing.T) {
	t.Helper()
	source, err := os.ReadFile("../runtime/host/request-guard.ts")
	if err != nil {
		t.Fatal(err)
	}
	js.Global().Call("eval", string(source))
}

func TestRequestGuardSuppressesPathsAndPreservesCallShape(t *testing.T) {
	window := testBrowserWindow(t)
	gosx, navigator := window.Get("__gosx"), window.Get("navigator")
	calls := 0
	methods := []struct {
		target js.Value
		name   string
		beacon bool
	}{{window, "fetch", false}, {gosx, "request", false}, {navigator, "sendBeacon", true}}
	for _, method := range methods {
		method := method
		original := js.FuncOf(func(receiver js.Value, args []js.Value) any {
			calls++
			if !receiver.Equal(method.target) || len(args) != 2 || args[1].Get("sentinel").String() != "body" {
				t.Error("forwarded receiver or argument list changed")
			}
			if method.beacon {
				return false
			}
			return js.Global().Get("Promise").Call("resolve", map[string]any{"ok": true, "status": 201})
		})
		method.target.Set(method.name, original)
		t.Cleanup(original.Release)
	}
	loadRequestGuard(t)
	guard, err := GuardRequests(RequestPolicy{BlockedPaths: []string{"/_gosx/client-events"}})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Dispose()
	for _, method := range methods {
		for _, input := range []any{"/_gosx/client-events?session=1", "https://elsewhere.test/_gosx/client-events", map[string]any{"url": "/_gosx/client-events"}, map[string]any{"href": "https://example.test/_gosx/%63lient-events"}} {
			result := method.target.Call(method.name, input, map[string]any{"sentinel": "body"})
			if method.beacon {
				if !result.Bool() {
					t.Fatal("suppressed beacon rejected")
				}
			} else {
				response, err := jsutil.AwaitPromise(result)
				if err != nil {
					t.Fatal(err)
				}
				if response.Get("status").Int() != 204 || !response.Get("ok").Bool() {
					t.Fatal("suppressed response")
				}
			}
		}
		result := method.target.Call(method.name, "/api/match", map[string]any{"sentinel": "body"})
		if method.beacon {
			if result.Bool() {
				t.Fatal("beacon result changed")
			}
		} else {
			response, err := jsutil.AwaitPromise(result)
			if err != nil || response.Get("status").Int() != 201 {
				t.Fatal("request result changed", err)
			}
		}
	}
	if calls != 3 {
		t.Fatalf("opted-out telemetry reached originals: calls=%d", calls)
	}
	guard.Dispose()
	guard.Dispose()
	for _, method := range methods {
		method.target.Call(method.name, "/_gosx/client-events", map[string]any{"sentinel": "body"})
	}
	if calls != 6 {
		t.Fatal("disposal did not restore original methods")
	}
}

func TestRequestGuardRetainedWrapperSurvivesDisposeAndReinstall(t *testing.T) {
	window := testBrowserWindow(t)
	calls := 0
	original := js.FuncOf(func(receiver js.Value, args []js.Value) any {
		calls++
		if !receiver.Equal(window) || len(args) != 1 {
			t.Error("forwarding changed")
		}
		return js.Global().Get("Promise").Call("resolve", map[string]any{"status": 200})
	})
	defer original.Release()
	window.Set("fetch", original)
	loadRequestGuard(t)
	guard, err := GuardRequests(RequestPolicy{BlockedPaths: []string{"/_gosx/client-events"}})
	if err != nil {
		t.Fatal(err)
	}
	retained := window.Get("fetch")
	external := js.FuncOf(func(receiver js.Value, args []js.Value) any {
		values := make([]any, len(args))
		for i, arg := range args {
			values[i] = arg
		}
		return retained.Call("apply", receiver, js.ValueOf(values))
	})
	defer external.Release()
	window.Set("fetch", external)
	guard.Dispose()
	guard.Dispose()
	if !window.Get("fetch").Equal(external.Value) {
		t.Fatal("dispose replaced a newer wrapper")
	}
	window.Call("fetch", "/_gosx/client-events")
	if calls != 1 {
		t.Fatal("retained wrapper did not become inert")
	}
	second, err := GuardRequests(RequestPolicy{BlockedPaths: []string{"/_gosx/client-events"}})
	if err != nil {
		t.Fatal(err)
	}
	window.Call("fetch", "/_gosx/client-events")
	if calls != 1 {
		t.Fatal("reinstalled policy did not block")
	}
	second.Dispose()
	window.Call("fetch", "/_gosx/client-events")
	if calls != 2 {
		t.Fatal("retained wrapper chain was broken")
	}
}
