//go:build !windows || (windows && !amd64 && !arm64)

package desktop

import (
	"errors"
	"net/http"
	"runtime"
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

	_, openErr := stub.OpenFileDialog(OpenFileOptions{})
	_, saveErr := stub.SaveFileDialog(SaveFileOptions{})
	_, clipErr := stub.Clipboard()

	checks := []struct {
		name string
		err  error
	}{
		{"PostMessage", stub.PostMessage("noop")},
		{"Reload", stub.Reload()},
		{"ExecuteScript", stub.ExecuteScript("1")},
		{"OpenDevTools", stub.OpenDevTools()},
		{"PrependBootstrapScript", stub.PrependBootstrapScript("noop")},
		{"Minimize", stub.Minimize()},
		{"Maximize", stub.Maximize()},
		{"Restore", stub.Restore()},
		{"Focus", stub.Focus()},
		{"SetTitle", stub.SetTitle("ok")},
		{"Serve", stub.Serve("app://assets/*", http.NotFoundHandler())},
		{"OpenFileDialog", openErr},
		{"SaveFileDialog", saveErr},
		{"Clipboard", clipErr},
		{"SetClipboard", stub.SetClipboard("ok")},
		{"OpenURL", stub.OpenURL("https://example.test")},
		{"SetFullscreen", stub.SetFullscreen(true)},
		{"SetMinSize", stub.SetMinSize(320, 200)},
		{"SetMaxSize", stub.SetMaxSize(2560, 1440)},
		{"RegisterProtocol", stub.RegisterProtocol("gosx-test")},
		{"RegisterFileType", stub.RegisterFileType(".gsx", "", "GoSX Component")},
		{"SetMenuBar", stub.SetMenuBar(Menu{})},
		{"SetTray", stub.SetTray(TrayOptions{})},
		{"CloseTray", stub.CloseTray()},
		{"Notify", stub.Notify(Notification{Title: "hello"})},
		{"SetFileDropHandler", stub.SetFileDropHandler(func([]string) {})},
	}
	for _, c := range checks {
		if !errors.Is(c.err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", c.name, c.err)
		}
	}
	if _, err := stub.NewWindow(WindowOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("NewWindow: err = %v, want ErrUnsupported", err)
	}
}

func TestDesktopWebView2RuntimeVersionUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	if _, err := WebView2RuntimeVersion(""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("WebView2RuntimeVersion error = %v, want ErrUnsupported", err)
	}
}

func TestShowMessageUnsupportedPlatform(t *testing.T) {
	if _, err := ShowMessage(MessageOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ShowMessage error = %v, want ErrUnsupported", err)
	}
	if _, err := (&App{}).ShowMessage(MessageOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("App.ShowMessage error = %v, want ErrUnsupported", err)
	}
}

func TestUnsupportedSingleInstanceAPI(t *testing.T) {
	if lock, first, err := AcquireSingleInstance("com.example.test"); lock != nil || first || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("AcquireSingleInstance() = (%v, %v, %v), want (nil, false, ErrUnsupported)", lock, first, err)
	}
	if err := ForwardToFirstInstance("com.example.test", nil, ""); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ForwardToFirstInstance() error = %v, want ErrUnsupported", err)
	}
	var lock *InstanceLock
	if err := lock.Close(); err != nil {
		t.Fatalf("Close() on nil InstanceLock error = %v, want nil", err)
	}
}
