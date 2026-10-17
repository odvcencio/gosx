//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// AppKit runs on the process's main thread, and App.Run pumps its event loop
// there. init (lifecycle_darwin.go) locked the main goroutine to that thread, but
// the testing package runs every test on a goroutine of its own, which is not on
// it. TestMain therefore keeps the main goroutine for itself: it runs the tests
// on another goroutine and serves onMain calls here, on the main thread, until
// they finish.

var mainCalls = make(chan func())

func TestMain(m *testing.M) {
	code := make(chan int, 1)
	go func() { code <- m.Run() }()
	for {
		select {
		case fn := <-mainCalls:
			fn()
		case c := <-code:
			os.Exit(c)
		}
	}
}

// onMain runs fn on the main thread and returns when it has finished. fn may
// block for as long as it likes, App.Run included; the calling test goroutine is
// free to call App methods meanwhile.
func onMain(fn func()) {
	done := make(chan struct{})
	mainCalls <- func() {
		defer close(done)
		fn()
	}
	<-done
}

// requireGUI skips a test that opens a window unless the machine can show one:
// not under -short, and only in an Aqua (login window server) session.
func requireGUI(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("opens a native window; skipped in -short mode")
	}
	out, err := exec.Command("launchctl", "managername").Output()
	if err != nil {
		t.Skipf("cannot determine the launchd session type: %v", err)
	}
	if session := strings.TrimSpace(string(out)); session != "Aqua" {
		t.Skipf("launchd session is %q, not Aqua: no window server to connect to", session)
	}
}

var screenLockedPattern = regexp.MustCompile(`<key>CGSSessionScreenIsLocked</key>\s*<true/>`)

// screenLocked reports whether the login session's screen is locked. While it is,
// AppKit cannot show a window and keeps every window it was asked to order
// front, closed or not (a plain NSWindow ordered front and closed is still
// listed by -[NSApp windows] afterwards), so assertions about visibility and
// window lifetime do not hold.
func screenLocked() bool {
	out, err := exec.Command("ioreg", "-n", "Root", "-d1", "-a").Output()
	return err == nil && screenLockedPattern.Match(out)
}
