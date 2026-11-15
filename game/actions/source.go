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

// Zero reports whether s is the empty (unbound) source.
func (s Source) Zero() bool { return s.Kind == 0 || s.Code == "" }

// Class groups Kinds that share a physical device family.
type Class uint8

const (
	// ClassKeyboardMouse covers keys, mouse buttons and mouse motion.
	ClassKeyboardMouse Class = iota + 1
	// ClassGamepad covers gamepad buttons and axes.
	ClassGamepad
)

// Class returns the device family of s.
func (s Source) Class() Class {
	if s.Kind == KindPadButton || s.Kind == KindPadAxis {
		return ClassGamepad
	}
	return ClassKeyboardMouse
}

func (s Source) latches() bool {
	return s.Kind == KindKey || s.Kind == KindMouseButton || s.Kind == KindPadButton
}

var kindNames = map[Kind]string{
	KindKey: "key", KindMouseButton: "mouse", KindMouseMotion: "motion",
	KindPadButton: "padbtn", KindPadAxis: "padaxis",
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
}

// Sink receives raw input events. Map and Router implement it.
type Sink interface {
	Feed(Event)
	// ReleaseAll treats every held input as released. Call it on window blur.
	ReleaseAll()
}
