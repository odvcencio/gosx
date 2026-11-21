package actions

import "strings"

// The kinds of form control a key can be aimed at. A game decides, by kind
// and by key, whether the key reaches it as well (BrowserConfig.KeepKey).
const (
	// ControlText is a field the player types into: a text-like input, a
	// textarea or a contenteditable element.
	ControlText = "text"
	// ControlSelect is a select menu.
	ControlSelect = "select"
	// ControlRange is a slider.
	ControlRange = "range"
	// ControlToggle is a checkbox or a radio button.
	ControlToggle = "toggle"
	// ControlButton is an input that acts as a button: button, submit, reset,
	// image, file or color.
	ControlButton = "button"
)

// ControlKind names the kind of form control an element is, from its tag
// name, its type attribute and whether it is contenteditable. It returns ""
// for an element that is not a form control, such as a button element, a
// link or the page itself: keys aimed at those always reach the game.
func ControlKind(tag, inputType string, contentEditable bool) string {
	switch strings.ToLower(tag) {
	case "textarea":
		return ControlText
	case "select":
		return ControlSelect
	case "input":
		switch strings.ToLower(inputType) {
		case "range":
			return ControlRange
		case "checkbox", "radio":
			return ControlToggle
		case "button", "submit", "reset", "image", "file", "color":
			return ControlButton
		}
		return ControlText
	}
	if contentEditable {
		return ControlText
	}
	return ""
}

// ControlUsesKey reports whether a form control of that kind acts on the key
// itself, by the browser's own rules: a text field uses every key but Escape
// and the function keys; a menu uses the arrows, Home, End, the page keys,
// Enter, Space and, to jump to an option, letters and digits; a slider uses
// the arrows, Home, End and the page keys; a toggle uses Space and the
// arrows; a button uses Space and Enter. code is a KeyboardEvent.code.
//
// A game that wants every key the control does not use sets
// BrowserConfig.KeepKey to KeepUnusedKeys.
func ControlUsesKey(kind, code string) bool {
	switch kind {
	case ControlText:
		return code != "Escape" && !functionKey(code)
	case ControlSelect:
		return navigationKey(code) || code == "Enter" || code == "NumpadEnter" || code == "Space" ||
			strings.HasPrefix(code, "Key") || strings.HasPrefix(code, "Digit") || strings.HasPrefix(code, "Numpad")
	case ControlRange:
		return navigationKey(code)
	case ControlToggle:
		return code == "Space" || arrowKey(code)
	case ControlButton:
		return code == "Space" || code == "Enter" || code == "NumpadEnter"
	}
	return false
}

// RepeatClicks reports whether a repeat of a held key presses the element.
// The browser repeats a key that is held down. Each repeat of Enter is a
// click on a button, a link or a summary; a repeat of Space arms a button,
// which then clicks when Space is let go. tag is the element's tag name,
// inputType its type attribute and role its role attribute.
//
// BrowserConfig.StopRepeatClicks uses it to keep a held key from pressing a
// control the focus moved to while the key was down.
func RepeatClicks(code, tag, inputType, role string) bool {
	if code != "Enter" && code != "NumpadEnter" && code != "Space" {
		return false
	}
	switch strings.ToLower(tag) {
	case "button", "a", "summary":
		return true
	case "input":
		return ControlKind(tag, inputType, false) == ControlButton
	}
	return strings.EqualFold(role, "button") || strings.EqualFold(role, "link")
}

// KeepUnusedKeys is a BrowserConfig.KeepKey that sends the game every key
// the form control does not use itself (ControlUsesKey): Escape from a text
// field, a letter from a slider, a digit from a checkbox.
func KeepUnusedKeys(code, kind string) bool { return !ControlUsesKey(kind, code) }

func arrowKey(code string) bool {
	return code == "ArrowUp" || code == "ArrowDown" || code == "ArrowLeft" || code == "ArrowRight"
}

func navigationKey(code string) bool {
	return arrowKey(code) || code == "Home" || code == "End" || code == "PageUp" || code == "PageDown"
}

// functionKey reports F1 to F24.
func functionKey(code string) bool {
	if len(code) < 2 || code[0] != 'F' {
		return false
	}
	for _, c := range code[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
