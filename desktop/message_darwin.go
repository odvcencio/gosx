//go:build darwin && !ios && (amd64 || arm64)

package desktop

import "fmt"

// alertButton is one button of the native alert and the result it stands for.
type alertButton struct {
	title  string
	result MessageResult
}

// alertButtons returns the buttons for a button set, in the order they are added
// to the NSAlert: the first is the default button (the Return key), and AppKit
// binds Escape to a button titled "Cancel".
func alertButtons(buttons MessageButtons) []alertButton {
	switch buttons {
	case MessageOKCancel:
		return []alertButton{{"OK", MessageResultOK}, {"Cancel", MessageResultCancel}}
	case MessageYesNo:
		return []alertButton{{"Yes", MessageResultYes}, {"No", MessageResultNo}}
	case MessageRetryCancel:
		return []alertButton{{"Retry", MessageResultRetry}, {"Cancel", MessageResultCancel}}
	default: // MessageOK, and the empty default
		return []alertButton{{"OK", MessageResultOK}}
	}
}

// alertStyle maps a message kind to an NSAlertStyle. AppKit has no question
// style: it shows the application icon for informational alerts, which is what a
// question is.
func alertStyle(kind MessageKind) uintptr {
	switch kind {
	case MessageWarning:
		return nsAlertStyleWarning
	case MessageError:
		return nsAlertStyleCritical
	default: // MessageInfo, MessageQuestion, and the empty default
		return nsAlertStyleInformational
	}
}

// alertResult maps the NSModalResponse of runModal to a result: the buttons
// answer NSAlertFirstButtonReturn, then the values after it, in the order they
// were added.
func alertResult(buttons []alertButton, response int64) (MessageResult, error) {
	index := response - nsAlertFirstButtonReturn
	if index < 0 || index >= int64(len(buttons)) {
		return "", fmt.Errorf("desktop: the alert ended without a button (response %d)", response)
	}
	return buttons[index].result, nil
}

// alertHook, when set, is called with the NSAlert after it is built and before it
// runs, on the main thread. The function it returns, if any, is called when the
// alert has ended, on the same thread. Tests use it to look at the alert and to
// arrange for a button to be pressed, since a modal alert blocks until someone
// does, and to take that arrangement away again.
var alertHook func(alert uintptr) (after func())

// newAlert returns an owned NSAlert built from options and the buttons for them.
// The message text is the bold first line and the informative text the smaller
// text under it; an alert with no title puts its text on the first line, because
// an empty bold line is what AppKit would otherwise draw.
func newAlert(options MessageOptions, buttons []alertButton) *objcRef {
	alert := ownObjC(objcNew(objcClass("NSAlert")))
	if alert == nil {
		return nil
	}
	headline, detail := options.Title, options.Text
	if headline == "" {
		headline, detail = detail, ""
	}
	send1(alert.obj, objcSel("setMessageText:"), nsString(headline))
	send1(alert.obj, objcSel("setInformativeText:"), nsString(detail))
	send1(alert.obj, objcSel("setAlertStyle:"), alertStyle(options.Kind))
	for _, button := range buttons {
		send1(alert.obj, objcSel("addButtonWithTitle:"), nsString(button.title))
	}
	return alert
}

// runAlert shows the alert for options and blocks until a button is pressed. It
// runs on the main thread, inside an autorelease pool. The alert is application
// modal: -[NSAlert runModal] runs its own event loop, so the app's events (and the
// calls other goroutines have queued) wait until the alert is answered.
func runAlert(options MessageOptions) (MessageResult, error) {
	if objcClass("NSAlert") == 0 {
		return "", fmt.Errorf("%w: NSAlert is unavailable", ErrUnsupported)
	}
	app := send0(objcClass("NSApplication"), objcSel("sharedApplication"))
	if app == 0 {
		return "", fmt.Errorf("%w: NSApplication is unavailable", ErrUnsupported)
	}
	// A process that was not started from a bundle begins with the prohibited
	// activation policy, under which AppKit cannot put a window on screen. Run
	// makes the policy regular; an alert shown before it (the example host's
	// startup error) only needs the accessory policy.
	if send0(app, objcSel("activationPolicy")) == nsApplicationActivationPolicyProhibited {
		send1(app, objcSel("setActivationPolicy:"), nsApplicationActivationPolicyAccessory)
	}

	buttons := alertButtons(options.Buttons)
	alert := newAlert(options, buttons)
	if alert == nil {
		return "", fmt.Errorf("desktop: NSAlert init failed")
	}
	defer alert.Release()
	if alertHook != nil {
		if after := alertHook(alert.obj); after != nil {
			defer after()
		}
	}
	send1(app, objcSel("activateIgnoringOtherApps:"), 1)
	return alertResult(buttons, int64(send0(alert.obj, objcSel("runModal"))))
}

// showPlatformMessage is the native message box: an NSAlert, run on the main
// thread (directly when the caller is on it, through the running app otherwise).
// owner is ignored. AppKit would attach the alert to a window as a sheet, but a
// sheet reports its result through a block, which a later slice can supply; the
// alert is application modal instead, which is what a MessageBox without a
// parent is.
func showPlatformMessage(options MessageOptions, owner uintptr) (MessageResult, error) {
	var result MessageResult
	err := onMainThread("ShowMessage", func() error {
		var err error
		withPool(func() { result, err = runAlert(options) })
		return err
	})
	return result, err
}
