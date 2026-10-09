//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"m31labs.dev/gosx/client/jsutil"
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func TestDesktopTypedServicesAndSynchronousInitiation(t *testing.T) {
	original := js.Global().Get("gosxDesktop")
	defer js.Global().Set("gosxDesktop", original)
	object := func() js.Value { return js.Global().Get("Object").New() }
	bridge, diagnostics, privacy, window := object(), object(), object(), object()
	functions := []js.Func{}
	defer func() {
		for _, fn := range functions {
			fn.Release()
		}
	}()
	method := func(target js.Value, name string, fn func([]js.Value) any) {
		wrapped := js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
		functions = append(functions, wrapped)
		target.Set(name, wrapped)
	}
	resolved := func(value any) any { return js.Global().Get("Promise").Call("resolve", value) }
	bridge.Set("__gosxDesktopBridge", true)
	bridge.Set("window", window)
	method(bridge, "service", func(args []js.Value) any {
		switch args[0].String() {
		case "diagnostics":
			return diagnostics
		case "privacySettings":
			return privacy
		default:
			t.Error("unknown private service")
			return js.Undefined()
		}
	})
	method(diagnostics, "preview", func([]js.Value) any {
		return resolved(map[string]any{"id": "reviewed-plan", "totalSizeText": "12 B", "files": []any{map[string]any{"name": "client.log", "size": 12}}})
	})
	method(diagnostics, "export", func(args []js.Value) any {
		if args[0].String() != "reviewed-plan" {
			t.Error("export did not bind reviewed plan")
		}
		return resolved(nil)
	})
	method(privacy, "sendBrowserErrorReports", func([]js.Value) any { return resolved(true) })
	method(privacy, "checkForUpdates", func([]js.Value) any { return resolved(false) })
	method(privacy, "setBrowserErrorReports", func(args []js.Value) any {
		if !args[0].Get("enabled").Bool() {
			t.Error("reports preference changed")
		}
		return resolved(nil)
	})
	method(privacy, "setCheckForUpdates", func(args []js.Value) any {
		if args[0].Get("enabled").Bool() {
			t.Error("update preference changed")
		}
		return js.Global().Get("Promise").Call("reject", "update setting denied")
	})
	method(privacy, "deleteMyData", func([]js.Value) any { return resolved(nil) })
	fullscreenStarted := false
	method(window, "setFullscreen", func(args []js.Value) any { fullscreenStarted = args[0].Bool(); return resolved(nil) })
	js.Global().Set("gosxDesktop", bridge)
	desktop := Desktop()
	if !desktop.DiagnosticsAvailable() || !desktop.PrivacyAvailable() || !desktop.WindowAvailable() {
		t.Fatal("typed desktop capabilities lost")
	}
	errorsDone := make(chan error, 1)
	done := func(err error) { errorsDone <- err }
	desktop.SetFullscreen(true, done)
	if !fullscreenStarted {
		t.Fatal("fullscreen operation left user gesture before initiation")
	}
	if err := <-errorsDone; err != nil {
		t.Fatal(err)
	}
	planDone := make(chan DiagnosticsPlan, 1)
	desktop.PreviewDiagnostics(func(plan DiagnosticsPlan, err error) {
		if err != nil {
			t.Error(err)
		}
		planDone <- plan
	})
	plan := <-planDone
	if plan.ID != "reviewed-plan" || plan.TotalSizeText != "12 B" || len(plan.Files) != 1 || plan.Files[0].Name != "client.log" || plan.Files[0].Size != 12 {
		t.Fatal("diagnostics plan changed", plan)
	}
	desktop.ExportDiagnostics(plan.ID, done)
	if err := <-errorsDone; err != nil {
		t.Fatal(err)
	}
	boolDone := make(chan bool, 1)
	desktop.BrowserErrorReports(func(value bool, err error) {
		if err != nil {
			t.Error(err)
		}
		boolDone <- value
	})
	if !<-boolDone {
		t.Fatal("reports preference lost")
	}
	desktop.CheckForUpdates(func(value bool, err error) {
		if err != nil {
			t.Error(err)
		}
		boolDone <- value
	})
	if <-boolDone {
		t.Fatal("update preference lost")
	}
	desktop.SetBrowserErrorReports(true, done)
	if err := <-errorsDone; err != nil {
		t.Fatal(err)
	}
	desktop.SetCheckForUpdates(false, done)
	if err := <-errorsDone; err == nil || !strings.Contains(err.Error(), "update setting denied") {
		t.Fatal("desktop promise rejection lost", err)
	}
	desktop.DeleteMyData(done)
	if err := <-errorsDone; err != nil {
		t.Fatal(err)
	}
	bridge.Set("__gosxDesktopBridge", false)
	if desktop.ServicesAvailable() || desktop.WindowAvailable() {
		t.Fatal("unbranded bridge admitted")
	}
	desktop.DeleteMyData(done)
	if err := <-errorsDone; !errors.Is(err, ErrDesktopUnavailable) {
		t.Fatal("unbranded service invoked", err)
	}
}

func TestDesktopCancelledWaitAndLateRejection(t *testing.T) {
	original := js.Global().Get("gosxDesktop")
	defer js.Global().Set("gosxDesktop", original)
	promise, _, reject := fetchTestPromise()
	preview := js.FuncOf(func(js.Value, []js.Value) any { return promise })
	defer preview.Release()
	service := js.FuncOf(func(js.Value, []js.Value) any { return map[string]any{"preview": preview} })
	defer service.Release()
	js.Global().Set("gosxDesktop", map[string]any{"__gosxDesktopBridge": true, "service": service})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	Desktop().WithContext(ctx).PreviewDiagnostics(func(_ DiagnosticsPlan, err error) { done <- err })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("desktop cancellation identity lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("desktop retained unsettled promise")
	}
	reject.Invoke("late desktop failure")
	_, _ = jsutil.AwaitPromise(js.Global().Get("Promise").Call("resolve", nil))
	select {
	case err := <-done:
		t.Fatal("completion called twice", err)
	default:
	}
}
