package actions

import (
	"strconv"
	"strings"
)

// PadStyle chooses how gamepad buttons are named.
type PadStyle uint8

const (
	// PadGeneric names buttons "Button 0", "Button 1" and so on, except for
	// the shared D-pad, bumper and stick names.
	PadGeneric PadStyle = iota
	// PadXbox uses A, B, X, Y, LB, RB, View, Menu.
	PadXbox
	// PadPlayStation uses Cross, Circle, Square, Triangle, L1, R1, Create, Options.
	PadPlayStation
)

var padButtonNames = [...][3]string{
	// generic, xbox, playstation; indexes follow the W3C standard mapping
	0:  {"Button 0", "A", "Cross"},
	1:  {"Button 1", "B", "Circle"},
	2:  {"Button 2", "X", "Square"},
	3:  {"Button 3", "Y", "Triangle"},
	4:  {"Left Bumper", "LB", "L1"},
	5:  {"Right Bumper", "RB", "R1"},
	6:  {"Left Trigger", "LT", "L2"},
	7:  {"Right Trigger", "RT", "R2"},
	8:  {"Back", "View", "Create"},
	9:  {"Start", "Menu", "Options"},
	10: {"Left Stick Click", "LS Click", "L3"},
	11: {"Right Stick Click", "RS Click", "R3"},
	12: {"D-pad Up", "D-pad Up", "D-pad Up"},
	13: {"D-pad Down", "D-pad Down", "D-pad Down"},
	14: {"D-pad Left", "D-pad Left", "D-pad Left"},
	15: {"D-pad Right", "D-pad Right", "D-pad Right"},
	16: {"Home", "Guide", "PS"},
}

var keyNames = map[string]string{
	"Space": "Space", "Enter": "Enter", "NumpadEnter": "Numpad Enter", "Escape": "Esc",
	"Tab": "Tab", "Backspace": "Backspace", "Delete": "Delete", "Insert": "Insert",
	"Home": "Home", "End": "End", "PageUp": "Page Up", "PageDown": "Page Down",
	"ArrowUp": "Up", "ArrowDown": "Down", "ArrowLeft": "Left", "ArrowRight": "Right",
	"ShiftLeft": "Left Shift", "ShiftRight": "Right Shift",
	"ControlLeft": "Left Ctrl", "ControlRight": "Right Ctrl",
	"AltLeft": "Left Alt", "AltRight": "Right Alt",
	"MetaLeft": "Left Meta", "MetaRight": "Right Meta",
	"CapsLock": "Caps Lock", "Backquote": "`", "Minus": "-", "Equal": "=",
	"BracketLeft": "[", "BracketRight": "]", "Backslash": "\\", "Semicolon": ";",
	"Quote": "'", "Comma": ",", "Period": ".", "Slash": "/",
	"NumpadAdd": "Numpad +", "NumpadSubtract": "Numpad -",
	"NumpadMultiply": "Numpad *", "NumpadDivide": "Numpad /", "NumpadDecimal": "Numpad .",
}

var mouseNames = [...]string{"Left Click", "Middle Click", "Right Click", "Mouse 4", "Mouse 5"}

var axisNames = [...][2]string{ // negative, positive
	0: {"Left Stick Left", "Left Stick Right"},
	1: {"Left Stick Up", "Left Stick Down"},
	2: {"Right Stick Left", "Right Stick Right"},
	3: {"Right Stick Up", "Right Stick Down"},
}

var axisBase = [...]string{"Left Stick X", "Left Stick Y", "Right Stick X", "Right Stick Y"}

// Label returns a short display name for s, such as "W", "Left Click" or "A".
// An axis returns its name without direction; use BindingLabel for that.
func Label(s Source, style PadStyle) string {
	if s.Zero() {
		return "Unbound"
	}
	switch s.Kind {
	case KindKey:
		if name, ok := keyNames[s.Code]; ok {
			return name
		}
		if strings.HasPrefix(s.Code, "Key") && len(s.Code) == 4 {
			return s.Code[3:]
		}
		if strings.HasPrefix(s.Code, "Digit") && len(s.Code) == 6 {
			return s.Code[5:]
		}
		if strings.HasPrefix(s.Code, "Numpad") {
			return "Numpad " + s.Code[6:]
		}
		return s.Code
	case KindMouseButton:
		if n, err := strconv.Atoi(strings.TrimPrefix(s.Code, "Mouse")); err == nil && n >= 0 && n < len(mouseNames) {
			return mouseNames[n]
		}
		return s.Code
	case KindMouseMotion:
		switch s.Code {
		case CodeMouseDX:
			return "Mouse Move X"
		case CodeMouseDY:
			return "Mouse Move Y"
		case CodeWheelX:
			return "Wheel X"
		case CodeWheelY:
			return "Wheel"
		}
		return s.Code
	case KindPadButton:
		n, err := strconv.Atoi(strings.TrimPrefix(s.Code, "button"))
		if err != nil || n < 0 || n >= len(padButtonNames) || padButtonNames[n][0] == "" {
			return "Button " + strings.TrimPrefix(s.Code, "button")
		}
		return padButtonNames[n][style%3]
	case KindPadAxis:
		n, err := strconv.Atoi(strings.TrimPrefix(s.Code, "axis"))
		if err != nil || n < 0 || n >= len(axisNames) {
			return "Axis " + strings.TrimPrefix(s.Code, "axis")
		}
		return axisBase[n]
	}
	return s.Code
}

// BindingLabel is Label for a Binding. A gamepad axis includes its direction
// ("Left Stick Left" for axis0 with Scale -1).
func BindingLabel(b Binding, style PadStyle) string {
	if b.Source.Kind == KindPadAxis {
		n, err := strconv.Atoi(strings.TrimPrefix(b.Source.Code, "axis"))
		if err == nil && n >= 0 && n < len(axisNames) {
			if b.scale() < 0 {
				return axisNames[n][0]
			}
			return axisNames[n][1]
		}
		suffix := "+"
		if b.scale() < 0 {
			suffix = "-"
		}
		return Label(b.Source, style) + suffix
	}
	return Label(b.Source, style)
}
