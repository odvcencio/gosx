package desktop

import (
	"fmt"
	"strings"
)

// MessageKind selects the icon used by a native message box.
type MessageKind string

const (
	MessageInfo     MessageKind = "info"
	MessageWarning  MessageKind = "warning"
	MessageError    MessageKind = "error"
	MessageQuestion MessageKind = "question"
)

// MessageButtons selects the button set used by a native message box.
type MessageButtons string

const (
	MessageOK          MessageButtons = "ok"
	MessageOKCancel    MessageButtons = "ok-cancel"
	MessageYesNo       MessageButtons = "yes-no"
	MessageRetryCancel MessageButtons = "retry-cancel"
)

// MessageResult is the button selected by the user.
type MessageResult string

const (
	MessageResultOK     MessageResult = "ok"
	MessageResultCancel MessageResult = "cancel"
	MessageResultYes    MessageResult = "yes"
	MessageResultNo     MessageResult = "no"
	MessageResultRetry  MessageResult = "retry"
)

// MessageOptions configures a native message box. Empty Kind and Buttons
// select info and ok, respectively.
type MessageOptions struct {
	Title   string
	Text    string
	Kind    MessageKind
	Buttons MessageButtons
}

// ShowMessage displays a native message box without an owner window.
func ShowMessage(options MessageOptions) (MessageResult, error) {
	if err := validateMessageOptions(options); err != nil {
		return "", err
	}
	return showPlatformMessage(options, 0)
}

// ShowMessage displays a native message box owned by the app's primary window
// when it exists.
func (a *App) ShowMessage(options MessageOptions) (MessageResult, error) {
	if err := validateMessageOptions(options); err != nil {
		return "", err
	}
	var owner uintptr
	if window := a.Window(); window != nil {
		owner = window.Handle()
	}
	return showPlatformMessage(options, owner)
}

func validateMessageOptions(options MessageOptions) error {
	switch options.Kind {
	case "", MessageInfo, MessageWarning, MessageError, MessageQuestion:
	default:
		return fmt.Errorf("%w: unknown message kind %q", ErrInvalidOptions, options.Kind)
	}
	switch options.Buttons {
	case "", MessageOK, MessageOKCancel, MessageYesNo, MessageRetryCancel:
	default:
		return fmt.Errorf("%w: unknown message buttons %q", ErrInvalidOptions, options.Buttons)
	}
	for name, value := range map[string]string{"title": options.Title, "text": options.Text} {
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("%w: %s contains NUL", ErrInvalidOptions, name)
		}
	}
	return nil
}

// messageBoxFlags translates portable message options into Win32 MessageBoxW
// flags. The numeric values are kept platform-neutral to permit table tests.
func messageBoxFlags(kind MessageKind, buttons MessageButtons) uint32 {
	var flags uint32 = 0x00010000 // MB_SETFOREGROUND
	switch kind {
	case "", MessageInfo:
		flags |= 0x00000040 // MB_ICONINFORMATION
	case MessageWarning:
		flags |= 0x00000030 // MB_ICONWARNING
	case MessageError:
		flags |= 0x00000010 // MB_ICONERROR
	case MessageQuestion:
		flags |= 0x00000020 // MB_ICONQUESTION
	}
	switch buttons {
	case "", MessageOK:
		flags |= 0x00000000 // MB_OK
	case MessageOKCancel:
		flags |= 0x00000001 // MB_OKCANCEL
	case MessageYesNo:
		flags |= 0x00000004 // MB_YESNO
	case MessageRetryCancel:
		flags |= 0x00000005 // MB_RETRYCANCEL
	}
	return flags
}

func messageResultFromID(id int) MessageResult {
	switch id {
	case 1: // IDOK
		return MessageResultOK
	case 2: // IDCANCEL
		return MessageResultCancel
	case 4: // IDRETRY
		return MessageResultRetry
	case 6: // IDYES
		return MessageResultYes
	case 7: // IDNO
		return MessageResultNo
	default:
		return ""
	}
}

// focusStateTracker reports true only when an initial state is observed or
// the focused state changes thereafter.
type focusStateTracker struct {
	known   bool
	focused bool
}

func (tracker *focusStateTracker) Update(focused bool) bool {
	if tracker.known && tracker.focused == focused {
		return false
	}
	tracker.known = true
	tracker.focused = focused
	return true
}
