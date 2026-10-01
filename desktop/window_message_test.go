package desktop

import (
	"errors"
	"testing"
)

func TestMessageBoxFlagMapping(t *testing.T) {
	cases := []struct {
		kind    MessageKind
		buttons MessageButtons
		want    uint32
	}{
		{MessageInfo, MessageOK, 0x00000040 | 0x00000000 | 0x00010000},
		{MessageWarning, MessageOKCancel, 0x00000030 | 0x00000001 | 0x00010000},
		{MessageError, MessageYesNo, 0x00000010 | 0x00000004 | 0x00010000},
		{MessageQuestion, MessageRetryCancel, 0x00000020 | 0x00000005 | 0x00010000},
	}
	for _, tc := range cases {
		if got := messageBoxFlags(tc.kind, tc.buttons); got != tc.want {
			t.Errorf("messageBoxFlags(%q, %q) = %#x, want %#x", tc.kind, tc.buttons, got, tc.want)
		}
	}
}

func TestMessageBoxResultMapping(t *testing.T) {
	cases := []struct {
		id   int
		want MessageResult
	}{
		{1, MessageResultOK},
		{2, MessageResultCancel},
		{4, MessageResultRetry},
		{6, MessageResultYes},
		{7, MessageResultNo},
		{0, ""},
	}
	for _, tc := range cases {
		if got := messageResultFromID(tc.id); got != tc.want {
			t.Errorf("messageResultFromID(%d) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestValidateMessageOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts MessageOptions
		want error
	}{
		{"defaults", MessageOptions{}, nil},
		{"all values", MessageOptions{Title: "title", Text: "text", Kind: MessageQuestion, Buttons: MessageRetryCancel}, nil},
		{"unknown kind", MessageOptions{Kind: "fatal"}, ErrInvalidOptions},
		{"unknown buttons", MessageOptions{Buttons: "all"}, ErrInvalidOptions},
		{"title NUL", MessageOptions{Title: "bad\x00title"}, ErrInvalidOptions},
		{"text NUL", MessageOptions{Text: "bad\x00text"}, ErrInvalidOptions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMessageOptions(tc.opts); !errors.Is(err, tc.want) {
				t.Fatalf("validateMessageOptions error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWindowHandleOnNil(t *testing.T) {
	var window *Window
	if got := window.Handle(); got != 0 {
		t.Fatalf("nil Window.Handle() = %d, want 0", got)
	}
}

func TestAppWindowBeforeRun(t *testing.T) {
	app := &App{impl: &recordingPlatformApp{}}
	if got := app.Window(); got != nil {
		t.Fatalf("App.Window() before Run = %p, want nil", got)
	}
}

func TestFocusStateTrackerDeduplicatesChanges(t *testing.T) {
	var tracker focusStateTracker
	for _, tc := range []struct {
		focused bool
		want    bool
	}{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
		{true, true},
	} {
		if got := tracker.Update(tc.focused); got != tc.want {
			t.Errorf("Update(%v) = %v, want %v", tc.focused, got, tc.want)
		}
	}
}
