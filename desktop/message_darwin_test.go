//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDarwinAlertButtons(t *testing.T) {
	for _, tc := range []struct {
		buttons MessageButtons
		titles  []string
		results []MessageResult
	}{
		{"", []string{"OK"}, []MessageResult{MessageResultOK}},
		{MessageOK, []string{"OK"}, []MessageResult{MessageResultOK}},
		{MessageOKCancel, []string{"OK", "Cancel"}, []MessageResult{MessageResultOK, MessageResultCancel}},
		{MessageYesNo, []string{"Yes", "No"}, []MessageResult{MessageResultYes, MessageResultNo}},
		{MessageRetryCancel, []string{"Retry", "Cancel"}, []MessageResult{MessageResultRetry, MessageResultCancel}},
	} {
		var titles []string
		var results []MessageResult
		for _, b := range alertButtons(tc.buttons) {
			titles, results = append(titles, b.title), append(results, b.result)
		}
		if !reflect.DeepEqual(titles, tc.titles) || !reflect.DeepEqual(results, tc.results) {
			t.Errorf("alertButtons(%q) = %q -> %q, want %q -> %q", tc.buttons, titles, results, tc.titles, tc.results)
		}
	}
}

func TestDarwinAlertStyle(t *testing.T) {
	for _, tc := range []struct {
		kind MessageKind
		want uintptr
	}{
		{"", nsAlertStyleInformational},
		{MessageInfo, nsAlertStyleInformational},
		{MessageQuestion, nsAlertStyleInformational},
		{MessageWarning, nsAlertStyleWarning},
		{MessageError, nsAlertStyleCritical},
	} {
		if got := alertStyle(tc.kind); got != tc.want {
			t.Errorf("alertStyle(%q) = %d, want %d", tc.kind, got, tc.want)
		}
	}
	// NSAlertStyle's values, from NSAlert.h: a swapped pair would show the wrong icon.
	if nsAlertStyleWarning != 0 || nsAlertStyleInformational != 1 || nsAlertStyleCritical != 2 || nsAlertFirstButtonReturn != 1000 {
		t.Error("an AppKit alert constant has the wrong value")
	}
}

// The buttons answer 1000, 1001, ... in the order they were added; anything else
// (the app asked to quit, the modal session was aborted) is not a button.
func TestDarwinAlertResult(t *testing.T) {
	yesNo := alertButtons(MessageYesNo)
	for _, tc := range []struct {
		name     string
		buttons  []alertButton
		response int64
		want     MessageResult
		wantErr  bool
	}{
		{"first button", yesNo, 1000, MessageResultYes, false},
		{"second button", yesNo, 1001, MessageResultNo, false},
		{"a third button that was not added", yesNo, 1002, "", true},
		{"the stop response", yesNo, -1000, "", true},
		{"the abort response", yesNo, -1001, "", true},
		{"zero", yesNo, 0, "", true},
		{"one button, first", alertButtons(MessageOK), 1000, MessageResultOK, false},
		{"one button, second", alertButtons(MessageOK), 1001, "", true},
		{"no buttons", nil, 1000, "", true},
	} {
		got, err := alertResult(tc.buttons, tc.response)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: alertResult(%d) = %q, %v; want %q (error %v)", tc.name, tc.response, got, err, tc.want, tc.wantErr)
		}
	}
}

// alertReadout is an NSAlert as AppKit reports it.
type alertReadout struct {
	message, informative string
	style                uintptr
	titles, keys         []string
}

func readAlert(alert uintptr) alertReadout {
	r := alertReadout{
		message:     goString(send0(alert, objcSel("messageText"))),
		informative: goString(send0(alert, objcSel("informativeText"))),
		style:       send0(alert, objcSel("alertStyle")),
	}
	buttons := send0(alert, objcSel("buttons"))
	for i, n := uintptr(0), send0(buttons, objcSel("count")); i < n; i++ {
		button := send1(buttons, objcSel("objectAtIndex:"), i)
		r.titles = append(r.titles, goString(send0(button, objcSel("title"))))
		r.keys = append(r.keys, goString(send0(button, objcSel("keyEquivalent"))))
	}
	return r
}

// The alert is built from the options the way the Windows box reads them: title
// and text, an icon for the kind, and the buttons in order with the first as the
// default.
func TestDarwinNewAlertBuildsTheAlert(t *testing.T) {
	requireGUI(t)
	for _, tc := range []struct {
		name string
		opts MessageOptions
		want alertReadout
	}{
		{
			name: "title, text and the defaults",
			opts: MessageOptions{Title: "Heads up", Text: "Something happened."},
			want: alertReadout{message: "Heads up", informative: "Something happened.", style: nsAlertStyleInformational, titles: []string{"OK"}, keys: []string{"\r"}},
		},
		{
			name: "a warning with OK and Cancel",
			opts: MessageOptions{Title: "Overwrite?", Text: "The file exists.", Kind: MessageWarning, Buttons: MessageOKCancel},
			want: alertReadout{message: "Overwrite?", informative: "The file exists.", style: nsAlertStyleWarning, titles: []string{"OK", "Cancel"}, keys: []string{"\r", "\x1b"}},
		},
		{
			name: "an error with Retry and Cancel",
			opts: MessageOptions{Title: "Failed", Text: "Disk full.", Kind: MessageError, Buttons: MessageRetryCancel},
			want: alertReadout{message: "Failed", informative: "Disk full.", style: nsAlertStyleCritical, titles: []string{"Retry", "Cancel"}, keys: []string{"\r", "\x1b"}},
		},
		{
			name: "a question with Yes and No",
			opts: MessageOptions{Title: "Quit?", Text: "Unsaved work will be lost.", Kind: MessageQuestion, Buttons: MessageYesNo},
			want: alertReadout{message: "Quit?", informative: "Unsaved work will be lost.", style: nsAlertStyleInformational, titles: []string{"Yes", "No"}, keys: []string{"\r", ""}},
		},
		{
			name: "without a title the text is the headline",
			opts: MessageOptions{Text: "Only text."},
			want: alertReadout{message: "Only text.", informative: "", style: nsAlertStyleInformational, titles: []string{"OK"}, keys: []string{"\r"}},
		},
		{
			name: "multi-line text and non-ASCII",
			opts: MessageOptions{Title: "日本語 🍎", Text: "line one\nline two \"quoted\""},
			want: alertReadout{message: "日本語 🍎", informative: "line one\nline two \"quoted\"", style: nsAlertStyleInformational, titles: []string{"OK"}, keys: []string{"\r"}},
		},
		{
			name: "nothing at all",
			opts: MessageOptions{},
			want: alertReadout{message: "", informative: "", style: nsAlertStyleInformational, titles: []string{"OK"}, keys: []string{"\r"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got alertReadout
			onMain(func() {
				withPool(func() {
					alert := newAlert(tc.opts, alertButtons(tc.opts.Buttons))
					if alert == nil {
						t.Error("newAlert returned nil")
						return
					}
					defer alert.Release()
					got = readAlert(alert.obj)
				})
			})
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("alert = %+v\nwant   %+v", got, tc.want)
			}
		})
	}
}

// pressAlertButton arranges, from the alertHook, for button index of the alert to
// be pressed once the alert is running, and returns what stops that, for the hook
// to return: it must run as soon as the alert ends, because a timer left on the
// run loop would press the dead alert's button during the next modal session and
// answer that one. A modal alert
// blocks the main thread until someone answers it; this is the someone. A
// repeating timer on the main run loop, in every mode (the modal session runs in
// its own), sends the button performClick:, so the press lands whenever the
// session is up and no earlier press is lost. +[NSTimer timerWithTimeInterval:
// target:selector:userInfo:repeats:] takes its interval as a double, which
// msgSendZeroFP3 zeroes: a repeating timer with no interval fires as often as
// the run loop allows.
func pressAlertButton(t *testing.T, alert uintptr, index int) (stop func()) {
	t.Helper()
	buttons := send0(alert, objcSel("buttons"))
	if n := int(send0(buttons, objcSel("count"))); index >= n {
		t.Errorf("the alert has %d buttons, cannot press #%d", n, index)
		return func() {}
	}
	button := send1(buttons, objcSel("objectAtIndex:"), uintptr(index))
	timer, _, _ := syscall_syscall6(fnMsgSendZeroFP3, objcClass("NSTimer"),
		objcSel("timerWithTimeInterval:target:selector:userInfo:repeats:"),
		button, objcSel("performClick:"), 0, 1)
	timer = objcRetain(timer)
	send2(send0(objcClass("NSRunLoop"), objcSel("currentRunLoop")), objcSel("addTimer:forMode:"), timer, nsString("kCFRunLoopCommonModes"))
	return func() {
		send0(timer, objcSel("invalidate"))
		objcRelease(timer)
	}
}

// ShowMessage end to end, on the main thread: a real NSAlert is built, shown
// application-modal, answered by pressing each button in turn, and the button
// becomes the result.
func TestDarwinShowMessageAnswersEachButton(t *testing.T) {
	requireGUI(t)
	for _, tc := range []struct {
		name  string
		opts  MessageOptions
		press int
		want  MessageResult
		style uintptr
	}{
		{"OK", MessageOptions{Title: "t", Text: "x"}, 0, MessageResultOK, nsAlertStyleInformational},
		{"OK of OK/Cancel", MessageOptions{Title: "t", Text: "x", Buttons: MessageOKCancel, Kind: MessageWarning}, 0, MessageResultOK, nsAlertStyleWarning},
		{"Cancel of OK/Cancel", MessageOptions{Title: "t", Text: "x", Buttons: MessageOKCancel, Kind: MessageWarning}, 1, MessageResultCancel, nsAlertStyleWarning},
		{"Yes", MessageOptions{Title: "t", Text: "x", Buttons: MessageYesNo, Kind: MessageQuestion}, 0, MessageResultYes, nsAlertStyleInformational},
		{"No", MessageOptions{Title: "t", Text: "x", Buttons: MessageYesNo, Kind: MessageError}, 1, MessageResultNo, nsAlertStyleCritical},
		{"Retry", MessageOptions{Title: "t", Text: "x", Buttons: MessageRetryCancel}, 0, MessageResultRetry, nsAlertStyleInformational},
		{"Cancel of Retry/Cancel", MessageOptions{Title: "t", Text: "x", Buttons: MessageRetryCancel}, 1, MessageResultCancel, nsAlertStyleInformational},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen alertReadout
			var hookThread uint32
			alertHook = func(alert uintptr) func() {
				seen, hookThread = readAlert(alert), currentThreadID()
				return pressAlertButton(t, alert, tc.press)
			}
			defer func() { alertHook = nil }()

			var got MessageResult
			var err error
			onMain(func() { got, err = ShowMessage(tc.opts) })
			if err != nil || got != tc.want {
				t.Errorf("ShowMessage = %q, %v; want %q", got, err, tc.want)
			}
			if seen.style != tc.style {
				t.Errorf("alert style = %d, want %d", seen.style, tc.style)
			}
			if hookThread != dispatchMainThread {
				t.Errorf("the alert was built on thread %d, want the main thread", hookThread)
			}
		})
	}
}

// App.ShowMessage from a goroutine while the app runs: the call crosses to the
// main thread through the dispatcher, the alert runs there, and the goroutine
// gets the answer.
func TestDarwinShowMessageFromAnotherGoroutineWhileRunning(t *testing.T) {
	var threads []uint32
	alertHook = func(alert uintptr) func() {
		threads = append(threads, currentThreadID())
		return pressAlertButton(t, alert, 1)
	}
	defer func() { alertHook = nil }()

	runTestApp(t, Options{Title: "Alert Test"}, func(app *App, da *darwinApp) {
		got, err := app.ShowMessage(MessageOptions{Title: "Continue?", Text: "x", Buttons: MessageYesNo, Kind: MessageQuestion})
		if err != nil || got != MessageResultNo {
			t.Errorf("App.ShowMessage = %q, %v; want no", got, err)
		}
		// The main thread is back in its pump: other calls go through.
		if err := app.SetTitle("after the alert"); err != nil {
			t.Errorf("SetTitle after the alert: %v", err)
		}
		// So does the package-level function, which has no App.
		got, err = ShowMessage(MessageOptions{Title: "Again", Text: "x", Buttons: MessageOKCancel})
		if err != nil || got != MessageResultCancel {
			t.Errorf("ShowMessage = %q, %v; want cancel", got, err)
		}
	})
	if len(threads) != 2 || threads[0] != dispatchMainThread || threads[1] != dispatchMainThread {
		t.Errorf("alerts were built on threads %v, want two on the main thread", threads)
	}
}

// With no app running and the caller off the main thread, nothing can run the
// alert: the main goroutine is not pumping events for anyone. That is an error the
// caller can read, not a hang or a crash.
func TestDarwinShowMessageOffTheMainThreadWithoutAnApp(t *testing.T) {
	if activeApp.Load() != nil {
		t.Skip("an app is running")
	}
	ranHook := false
	alertHook = func(uintptr) func() { ranHook = true; return nil }
	defer func() { alertHook = nil }()

	_, err := ShowMessage(MessageOptions{Title: "x", Text: "y"})
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "main thread") {
		t.Errorf("ShowMessage = %v, want an ErrUnsupported error naming the main thread", err)
	}
	if ranHook {
		t.Error("an alert was built off the main thread")
	}
	_, err = (&App{}).ShowMessage(MessageOptions{Title: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("App.ShowMessage = %v, want ErrUnsupported", err)
	}
}

// The options are validated before any alert exists.
func TestDarwinShowMessageValidatesOptions(t *testing.T) {
	ranHook := false
	alertHook = func(uintptr) func() { ranHook = true; return nil }
	defer func() { alertHook = nil }()
	for _, opts := range []MessageOptions{
		{Kind: "fatal"},
		{Buttons: "abort-retry-ignore"},
		{Title: "a\x00b"},
		{Text: "a\x00b"},
	} {
		if _, err := ShowMessage(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("ShowMessage(%+v) = %v, want ErrInvalidOptions", opts, err)
		}
	}
	if ranHook {
		t.Error("an alert was built for invalid options")
	}
}

// A real alert for a person to answer. Run it with GOSX_DARWIN_INTERACTIVE=1 and
// click the button you like; it reports which.
func TestDarwinShowMessageInteractive(t *testing.T) {
	requireInteractive(t)
	var got MessageResult
	var err error
	onMain(func() {
		got, err = ShowMessage(MessageOptions{
			Title:   "GoSX desktop test",
			Text:    "Click any button. The test reports which one you chose.",
			Kind:    MessageQuestion,
			Buttons: MessageYesNo,
		})
	})
	if err != nil {
		t.Fatalf("ShowMessage: %v", err)
	}
	t.Logf("you chose %q", got)
}
