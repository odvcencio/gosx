//go:build js && wasm

package browser

import (
	"os"
	"syscall/js"
	"testing"
)

func loadTelemetry(t *testing.T, enabled bool) (js.Value, map[string]js.Value, *int) {
	t.Helper()
	window := testBrowserWindow(t)
	window.Set("__gosx_telemetry_config", map[string]any{"enabled": enabled, "endpoint": "/_gosx/client-events", "flushInterval": 100000})
	events := map[string]js.Value{}
	listen := js.FuncOf(func(_ js.Value, args []js.Value) any { events[args[0].String()] = args[1]; return nil })
	t.Cleanup(listen.Release)
	window.Set("addEventListener", listen)
	window.Get("document").Set("addEventListener", listen)
	requests := new(int)
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		*requests++
		return js.Global().Get("Promise").Call("resolve", map[string]any{"status": 200, "ok": true})
	})
	beacon := js.FuncOf(func(_ js.Value, args []js.Value) any { *requests++; return true })
	t.Cleanup(fetch.Release)
	t.Cleanup(beacon.Release)
	window.Set("fetch", fetch)
	window.Get("navigator").Set("sendBeacon", beacon)
	source, err := os.ReadFile("../js/bootstrap-src/04-telemetry.ts")
	if err != nil {
		t.Fatal(err)
	}
	js.Global().Call("eval", "(function(){\n"+string(source)+"\n})();")
	t.Cleanup(func() { SetTelemetryEnabled(false) })
	return window, events, requests
}

func TestTelemetryConsentDiscardsQueueAndBlocksUnload(t *testing.T) {
	window, events, requests := loadTelemetry(t, true)
	sid := TelemetrySession()
	if sid == "" || !TelemetryReady() {
		t.Fatal("existing runtime session unavailable")
	}
	if !EmitTelemetry("error", "engine", "queued before choice", nil) {
		t.Fatal("emitter unavailable")
	}
	telemetry := window.Get("__gosx").Get("telemetry")
	if telemetry.Call("snapshot").Get("queueDepth").Int() != 1 {
		t.Fatal("enabled report not queued")
	}
	SetTelemetryEnabled(false)
	if enabled, found := TelemetryConfigEnabled(); !found || enabled {
		t.Fatal("configuration failed to change")
	}
	if telemetry.Call("snapshot").Get("queueDepth").Int() != 0 || TelemetrySession() != "" {
		t.Fatal("opt-out retained pending report or exposed SID")
	}
	EmitTelemetry("error", "engine", "suppressed", nil)
	events["error"].Invoke(map[string]any{"message": "suppressed browser error"})
	events["pagehide"].Invoke()
	if *requests != 0 || telemetry.Call("snapshot").Get("queueDepth").Int() != 0 {
		t.Fatal("opted-out telemetry attempted a request")
	}
	SetTelemetryEnabled(true)
	if TelemetrySession() != sid {
		t.Fatal("existing correlation SID changed")
	}
	EmitTelemetry("error", "engine", "new report", nil)
	telemetry.Call("flush", map[string]any{"beacon": true})
	if *requests != 1 {
		t.Fatalf("fresh consent sent %d requests", *requests)
	}
	if telemetry.Call("snapshot").Get("attemptedEvents").Int() != 1 {
		t.Fatal("stale queued reports replayed after opt-in")
	}
}

func TestPreloadedTelemetryOptOutCanEnableWithoutPriorIdentifier(t *testing.T) {
	window, events, requests := loadTelemetry(t, false)
	telemetry := window.Get("__gosx").Get("telemetry")
	if TelemetrySession() != "" || telemetry.Call("snapshot").Get("session").String() != "" {
		t.Fatal("startup opt-out created a SID")
	}
	EmitTelemetry("error", "runtime", "disabled", nil)
	events["pagehide"].Invoke()
	if *requests != 0 {
		t.Fatal("startup opt-out sent reports")
	}
	SetTelemetryEnabled(true)
	if TelemetrySession() == "" {
		t.Fatal("fresh consent could not enable report correlation")
	}
	EmitTelemetry("error", "runtime", "enabled", map[string]any{"test": true})
	telemetry.Call("flush", map[string]any{"beacon": true})
	if *requests != 1 {
		t.Fatal("fresh consent could not enable reports")
	}
}

func TestTelemetryCookieAndConfigFallbacks(t *testing.T) {
	window := testBrowserWindow(t)
	if enabled, found := TelemetryConfigEnabled(); !enabled || found {
		t.Fatal("missing preference default")
	}
	window.Set("__gosx_telemetry_config", map[string]any{"enabled": false, "endpoint": "/custom"})
	if enabled, found := TelemetryConfigEnabled(); enabled || !found {
		t.Fatal("preloaded preference lost")
	}
	SetTelemetryEnabled(true)
	if window.Get("__gosx_telemetry_config").Get("endpoint").String() != "/custom" {
		t.Fatal("setter replaced existing configuration")
	}
	if err := SetCookie("choice=false; Path=/"); err != nil {
		t.Fatal(err)
	}
	if Cookie() != "choice=false; Path=/" {
		t.Fatal("cookie transport")
	}
	if TelemetrySession() != "" {
		t.Fatal("missing telemetry session should be empty")
	}
}

func TestDeniedCookiePropertyIsNonfatal(t *testing.T) {
	window := testBrowserWindow(t)
	document := window.Get("document")
	throwing := js.Global().Get("Function").New("throw new Error('cookie denied')")
	js.Global().Get("Object").Call("defineProperty", document, "cookie", map[string]any{"get": throwing, "set": throwing, "configurable": true})
	if Cookie() != "" {
		t.Fatal("denied cookie did not report unavailable")
	}
	if SetCookie("choice=false") == nil {
		t.Fatal("denied cookie assignment succeeded")
	}
}
