package actions

import "testing"

func TestControlKind(t *testing.T) {
	for _, c := range []struct {
		tag, typ string
		editable bool
		want     string
	}{
		{"INPUT", "text", false, ControlText},
		{"input", "", false, ControlText},
		{"input", "search", false, ControlText},
		{"input", "number", false, ControlText},
		{"INPUT", "RANGE", false, ControlRange},
		{"input", "checkbox", false, ControlToggle},
		{"input", "radio", false, ControlToggle},
		{"input", "submit", false, ControlButton},
		{"TEXTAREA", "", false, ControlText},
		{"SELECT", "", false, ControlSelect},
		{"DIV", "", true, ControlText},
		{"DIV", "", false, ""},
		{"BUTTON", "", false, ""},
		{"BODY", "", false, ""},
		{"", "", false, ""},
	} {
		if got := ControlKind(c.tag, c.typ, c.editable); got != c.want {
			t.Errorf("ControlKind(%q, %q, %v) = %q, want %q", c.tag, c.typ, c.editable, got, c.want)
		}
	}
}

// A control keeps the keys it acts on; KeepUnusedKeys sends the game the rest.
func TestKeepUnusedKeys(t *testing.T) {
	for _, c := range []struct {
		kind, code string
		keep       bool
	}{
		// A text field uses every key but Escape and the function keys.
		{ControlText, "KeyW", false},
		{ControlText, "Space", false},
		{ControlText, "ArrowLeft", false},
		{ControlText, "Enter", false},
		{ControlText, "Escape", true},
		{ControlText, "F5", true},
		{ControlText, "F12", true},
		{ControlText, "Fn", false},
		// A menu steps with the arrows, opens with Space and Enter, and jumps
		// to an option on a letter or a digit.
		{ControlSelect, "ArrowDown", false},
		{ControlSelect, "Home", false},
		{ControlSelect, "PageDown", false},
		{ControlSelect, "Enter", false},
		{ControlSelect, "Space", false},
		{ControlSelect, "KeyQ", false},
		{ControlSelect, "Digit4", false},
		{ControlSelect, "Numpad4", false},
		{ControlSelect, "Escape", true},
		{ControlSelect, "ShiftLeft", true},
		{ControlSelect, "Tab", true},
		// A slider uses the arrows, Home, End and the page keys.
		{ControlRange, "ArrowRight", false},
		{ControlRange, "End", false},
		{ControlRange, "Space", true},
		{ControlRange, "KeyF", true},
		{ControlRange, "Digit1", true},
		{ControlRange, "Escape", true},
		// A checkbox or a radio button uses Space and the arrows.
		{ControlToggle, "Space", false},
		{ControlToggle, "ArrowUp", false},
		{ControlToggle, "Enter", true},
		{ControlToggle, "KeyE", true},
		// An input that is a button uses Space and Enter.
		{ControlButton, "Space", false},
		{ControlButton, "Enter", false},
		{ControlButton, "ArrowLeft", true},
		// Not a form control: nothing is the control's.
		{"", "Space", true},
	} {
		if got := KeepUnusedKeys(c.code, c.kind); got != c.keep {
			t.Errorf("KeepUnusedKeys(%q, %q) = %v, want %v", c.code, c.kind, got, c.keep)
		}
		if ControlUsesKey(c.kind, c.code) == c.keep {
			t.Errorf("ControlUsesKey(%q, %q) should be the opposite of KeepUnusedKeys", c.kind, c.code)
		}
	}
}
