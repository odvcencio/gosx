package actions

import (
	"errors"
	"strconv"
	"strings"
)

// Kind classifies an input Source.
type Kind uint8

const (
	// KindKey is a keyboard key, named by KeyboardEvent.code ("KeyW").
	KindKey Kind = iota + 1
	// KindMouseButton is a pointer button, "Mouse0".."MouseN".
	KindMouseButton
	// KindMouseMotion is relative motion: "MouseDX", "MouseDY" (pointer lock
	// movement) and "WheelX", "WheelY". Motion is summed over one frame.
	KindMouseMotion
	// KindPadButton is a gamepad button, "button0".."buttonN".
	KindPadButton
	// KindPadAxis is a gamepad axis, "axis0".."axisN", in [-1,1].
	KindPadAxis
	// KindTouchButton is an on-screen button that a finger, a pen or the
	// mouse holds down. Its code is the name the page gives it ("sprint").
	KindTouchButton
	// KindTouchAxis is one axis of an on-screen stick, "<stick>.x" or
	// "<stick>.y", in [-1,1]. As on a gamepad, x grows to the right and y
	// grows downward.
	KindTouchAxis
)

// Motion source codes.
const (
	CodeMouseDX = "MouseDX"
	CodeMouseDY = "MouseDY"
	CodeWheelX  = "WheelX"
	CodeWheelY  = "WheelY"
)

// Device names. Gamepads are PadDevice(index).
const (
	DeviceKeyboard = "keyboard"
	DeviceMouse    = "mouse"
	// DeviceTouch is the screen: its on-screen sticks and buttons.
	DeviceTouch = "touch"
)

// PadDevice returns the device name for the gamepad at browser slot index.
func PadDevice(index int) string { return "pad:" + strconv.Itoa(index) }

// IsPadDevice reports whether device names a gamepad.
func IsPadDevice(device string) bool { return strings.HasPrefix(device, "pad:") }

// Source identifies one physical input.
type Source struct {
	Kind Kind
	Code string
}

// Key returns the keyboard source for a KeyboardEvent.code such as "KeyW".
func Key(code string) Source { return Source{KindKey, code} }

// MouseButton returns the source for pointer button n (0 is the primary).
func MouseButton(n int) Source { return Source{KindMouseButton, "Mouse" + strconv.Itoa(n)} }

// Motion returns a mouse motion source; code is one of the Code* constants.
func Motion(code string) Source { return Source{KindMouseMotion, code} }

// PadButton returns the source for gamepad button n.
func PadButton(n int) Source { return Source{KindPadButton, "button" + strconv.Itoa(n)} }

// PadAxis returns the source for gamepad axis n.
func PadAxis(n int) Source { return Source{KindPadAxis, "axis" + strconv.Itoa(n)} }

// TouchButton returns the source for the on-screen button called name.
func TouchButton(name string) Source { return Source{KindTouchButton, name} }

// TouchAxis returns the source for one axis of the on-screen stick called
// stick: the horizontal one, or the vertical one, which grows downward.
func TouchAxis(stick string, vertical bool) Source {
	if vertical {
		return Source{KindTouchAxis, stick + ".y"}
	}
	return Source{KindTouchAxis, stick + ".x"}
}

// Zero reports whether s is the empty (unbound) source.
func (s Source) Zero() bool { return s.Kind == 0 || s.Code == "" }

// Class groups Kinds that share a physical device family.
type Class uint8

const (
	// ClassKeyboardMouse covers keys, mouse buttons and mouse motion.
	ClassKeyboardMouse Class = iota + 1
	// ClassGamepad covers gamepad buttons and axes.
	ClassGamepad
	// ClassTouch covers on-screen buttons and sticks.
	ClassTouch
)

// Class returns the device family of s.
func (s Source) Class() Class {
	switch s.Kind {
	case KindPadButton, KindPadAxis:
		return ClassGamepad
	case KindTouchButton, KindTouchAxis:
		return ClassTouch
	}
	return ClassKeyboardMouse
}

func (s Source) latches() bool {
	return s.Kind == KindKey || s.Kind == KindMouseButton || s.Kind == KindPadButton || s.Kind == KindTouchButton
}

var kindNames = map[Kind]string{
	KindKey: "key", KindMouseButton: "mouse", KindMouseMotion: "motion",
	KindPadButton: "padbtn", KindPadAxis: "padaxis",
	KindTouchButton: "touch", KindTouchAxis: "touchaxis",
}

// String returns the stable text form "kind:Code" used in saved bindings.
func (s Source) String() string {
	if s.Zero() {
		return ""
	}
	return kindNames[s.Kind] + ":" + s.Code
}

// ErrBadSource is returned by ParseSource for unknown text.
var ErrBadSource = errors.New("actions: bad source")

// ParseSource reverses Source.String. The empty string parses to the zero Source.
func ParseSource(text string) (Source, error) {
	if text == "" {
		return Source{}, nil
	}
	name, code, ok := strings.Cut(text, ":")
	if !ok || code == "" {
		return Source{}, ErrBadSource
	}
	for kind, n := range kindNames {
		if n == name {
			return Source{kind, code}, nil
		}
	}
	return Source{}, ErrBadSource
}

// Binding connects a Source to an action. Scale multiplies the source value;
// zero means 1. Use -1 on one key of a pair (for example S) or on a gamepad
// axis to point the action the other way.
type Binding struct {
	Source Source
	Scale  float64
}

func (b Binding) scale() float64 {
	if b.Scale == 0 {
		return 1
	}
	return b.Scale
}

// Event is one raw input change. Value is 1 or 0 for keys and buttons, the
// analog value for gamepad buttons and axes, and the delta for motion.
type Event struct {
	Device string
	Source Source
	Value  float64
	// Canceled marks a release (Value 0) that the player did not make: the
	// browser took the touch away, or the finger's pointer was lost. The
	// action then reads Released and Canceled together (State.Canceled).
	Canceled bool
}

// Sink receives raw input events. Map and Router implement it.
type Sink interface {
	Feed(Event)
	// ReleaseAll treats every held input as released. Call it on window blur.
	ReleaseAll()
}
