//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"testing"
	"time"
)

func TestDarwinWebMessagesRoundTrip(t *testing.T) {
	messages := make(chan string, 8)
	runTestApp(t, Options{
		HTML: `<!doctype html><body>Native bridge test<script>
chrome.webview.addEventListener('message', function (event) {
  if (event.data.indexOf('Native ') === 0) chrome.webview.postMessage('received:' + event.data);
});
chrome.webview.postMessage('ready');
</script>`,
		OnWebMessage: func(message string) { messages <- message },
	}, func(app *App, _ *darwinApp) {
		next := func(want string) {
			select {
			case got := <-messages:
				if got != want {
					t.Errorf("native web message = %q, want %q", got, want)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("native web message %q never arrived", want)
			}
		}
		next("ready")
		message := "Native \"quotes\", Unicode ♫, and\nnewlines"
		if err := app.PostMessage(message); err != nil {
			t.Error(err)
			return
		}
		next("received:" + message)
	})
}
