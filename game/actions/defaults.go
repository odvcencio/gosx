package actions

// Standard gamepad button indexes (W3C standard mapping).
const (
	PadA     = 0
	PadB     = 1
	PadStart = 9
)

// PauseBindings returns the usual bindings for a "pause" action in a gameplay
// context: Escape on the keyboard and Start on a gamepad.
func PauseBindings() []Binding {
	return []Binding{{Source: Key("Escape")}, {Source: PadButton(PadStart)}}
}

// BackBindings returns the usual bindings for a "back" or "resume" action in a
// menu context: Escape, and Start or B on a gamepad.
func BackBindings() []Binding {
	return []Binding{{Source: Key("Escape")}, {Source: PadButton(PadStart)}, {Source: PadButton(PadB)}}
}
