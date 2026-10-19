//go:build js && wasm

package browser

import (
	"encoding/json"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/desktop/bridge"
)

func TestDesktopCapabilitiesRequireRegisteredMethods(t *testing.T) {
	original := js.Global().Get("gosxDesktop")
	defer js.Global().Set("gosxDesktop", original)
	window := js.Global().Get("Object").New()
	js.Global().Get("Function").New("window", bridge.BootstrapScript()).Invoke(window)
	host := window.Get("gosxDesktop")
	js.Global().Set("gosxDesktop", host)
	desktop := Desktop()
	check := func(services, diagnostics, privacy, fullscreen bool) {
		t.Helper()
		if desktop.ServicesAvailable() != services || desktop.DiagnosticsAvailable() != diagnostics ||
			desktop.PrivacyAvailable() != privacy || desktop.WindowAvailable() != fullscreen {
			t.Fatalf("capabilities = services:%t diagnostics:%t privacy:%t window:%t, want %t %t %t %t",
				desktop.ServicesAvailable(), desktop.DiagnosticsAvailable(), desktop.PrivacyAvailable(), desktop.WindowAvailable(),
				services, diagnostics, privacy, fullscreen)
		}
	}
	set := func(methods []string, revision int) {
		data, _ := json.Marshal(map[string]any{"methods": methods, "revision": revision})
		host.Call("__setMethodCapabilities", js.Global().Get("JSON").Call("parse", string(data)))
	}
	// An unbound proxy still synthesizes every service and method, but cannot
	// establish capability support. No async discovery or readiness wait occurs.
	if host.Call("service", "privacySettings").Get("deleteMyData").Type() != js.TypeFunction {
		t.Fatal("raw service proxy lost")
	}
	check(false, false, false, false)
	set([]string{"gosx.desktop.window.setFullscreen"}, 1)
	check(false, false, false, true)
	set([]string{"gosx.desktop.service.diagnostics.preview"}, 2)
	check(true, false, false, false)
	methods := []string{
		"gosx.desktop.service.diagnostics.preview", "gosx.desktop.service.diagnostics.export",
		"gosx.desktop.service.privacySettings.sendBrowserErrorReports",
		"gosx.desktop.service.privacySettings.checkForUpdates",
		"gosx.desktop.service.privacySettings.setBrowserErrorReports",
		"gosx.desktop.service.privacySettings.setCheckForUpdates",
	}
	set(methods, 3)
	check(true, true, false, false)
	set(append(methods, "gosx.desktop.service.privacySettings.deleteMyData"), 4)
	check(true, true, true, false)
	set([]string{}, 5)
	check(false, false, false, false)
	// Older hosts can still be explicitly invoked, but cannot advertise bound
	// services from proxy presence alone.
	legacy := js.Global().Get("Object").New()
	legacy.Set("__gosxDesktopBridge", true)
	legacy.Set("service", host.Get("service"))
	legacy.Set("window", host.Get("window"))
	js.Global().Set("gosxDesktop", legacy)
	check(false, false, false, false)
}
