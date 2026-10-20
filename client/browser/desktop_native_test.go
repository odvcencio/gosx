//go:build !(js && wasm)

package browser

import (
	"errors"
	"testing"
)

func TestNativeDesktopCapabilitiesAndTypedFailure(t *testing.T) {
	desktop := Desktop()
	if desktop.ServicesAvailable() || desktop.DiagnosticsAvailable() || desktop.PrivacyAvailable() || desktop.WindowAvailable() {
		t.Fatal("native host exposed a browser desktop service")
	}
	calls := 0
	done := func(err error) {
		calls++
		if !errors.Is(err, ErrDesktopUnavailable) {
			t.Errorf("unavailable operation: %v", err)
		}
	}
	desktop.SetFullscreen(true, done)
	desktop.ExportDiagnostics("reviewed-plan", done)
	desktop.SetBrowserErrorReports(true, done)
	desktop.SetCheckForUpdates(false, done)
	desktop.DeleteMyData(done)
	desktop.PreviewDiagnostics(func(plan DiagnosticsPlan, err error) {
		done(err)
		if plan.ID != "" || len(plan.Files) != 0 {
			t.Error("native preview invented an export plan")
		}
	})
	desktop.BrowserErrorReports(func(enabled bool, err error) {
		done(err)
		if enabled {
			t.Error("native reports enabled")
		}
	})
	desktop.CheckForUpdates(func(enabled bool, err error) {
		done(err)
		if enabled {
			t.Error("native updates enabled")
		}
	})
	if calls != 8 {
		t.Fatalf("completion delivery count %d", calls)
	}
	desktop.SetFullscreen(false, nil)
	desktop.PreviewDiagnostics(nil)
	desktop.DeleteMyData(nil)
}
