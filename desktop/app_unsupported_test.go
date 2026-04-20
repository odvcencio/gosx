//go:build !windows || (windows && !amd64 && !arm64)

package desktop

import (
	"errors"
	"net/http"
	"testing"
)

// TestUnsupportedAppRejectsAllBridgeAndWindowCalls locks down the stub
// surface: every method on the platformApp interface must report
// ErrUnsupported when the backend isn't implemented. Prevents macOS/Linux
// stubs from silently returning nil success on new APIs.
//
// Build-tagged to non-Windows so the unsupportedApp symbol resolves —
// Windows builds compile app_windows.go instead.
func TestUnsupportedAppRejectsAllBridgeAndWindowCalls(t *testing.T) {
	stub := unsupportedApp{}

	checks := []struct {
		name string
		err  error
	}{
		{"PostMessage", stub.PostMessage("noop")},
		{"ExecuteScript", stub.ExecuteScript("1")},
		{"OpenDevTools", stub.OpenDevTools()},
		{"PrependBootstrapScript", stub.PrependBootstrapScript("noop")},
		{"Minimize", stub.Minimize()},
		{"Maximize", stub.Maximize()},
		{"Restore", stub.Restore()},
		{"Focus", stub.Focus()},
		{"SetTitle", stub.SetTitle("ok")},
		{"Serve", stub.Serve("app://assets/*", http.NotFoundHandler())},
	}
	for _, c := range checks {
		if !errors.Is(c.err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", c.name, c.err)
		}
	}
}
