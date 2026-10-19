//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDarwinJavaScriptDialogs(t *testing.T) {
	requireGUI(t)
	old := runWebAlert
	defer func() { runWebAlert = old }()
	calls := 0
	runWebAlert = func(alert uintptr) uintptr {
		calls++
		message, _ := cfStringToString(send0(alert, objcSel("informativeText")))
		want := []string{"Notice", "Cancel this", "Accept this", "Choose a name", "Cancel name"}
		if calls > len(want) || message != want[calls-1] {
			t.Errorf("dialog %d message=%q", calls, message)
			return 1001
		}
		title, _ := cfStringToString(send0(alert, objcSel("messageText")))
		if title != "This page" {
			t.Errorf("controlling page title=%q", title)
		}
		if calls == 4 || calls == 5 {
			input := send0(alert, objcSel("accessoryView"))
			value, _ := cfStringToString(send0(input, objcSel("stringValue")))
			if value != "Original" {
				t.Errorf("prompt default=%q", value)
			}
			send1(input, objcSel("setStringValue:"), nsString("Native ♫ answer"))
		}
		if calls == 2 || calls == 5 {
			return 1001
		}
		return nsAlertFirstButtonReturn
	}
	messages := make(chan string, 4)
	runTestApp(t, Options{OnWebMessage: func(message string) {
		select {
		case messages <- message:
		default:
		}
	}}, func(app *App, _ *darwinApp) {
		script := `alert('Notice');var no=confirm('Cancel this');var yes=confirm('Accept this');var name=prompt('Choose a name','Original');var cancelled=prompt('Cancel name','Original');chrome.webview.postMessage(JSON.stringify({no,yes,name,cancelled}));`
		if err := app.ExecuteScript(script); err != nil {
			t.Error(err)
			return
		}
		select {
		case message := <-messages:
			var got struct {
				No, Yes   bool
				Name      string
				Cancelled *string
			}
			if err := json.Unmarshal([]byte(message), &got); err != nil {
				t.Error(err)
				return
			}
			if got.No || !got.Yes || got.Name != "Native ♫ answer" || got.Cancelled != nil || calls != 5 {
				t.Errorf("native dialog results=%s calls=%d", message, calls)
			}
		case <-time.After(8 * time.Second):
			t.Error("JavaScript dialogs did not complete")
		}
	})
}
