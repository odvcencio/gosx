// Package input provides typed browser input snapshots for GoSX clients.
//
// Browser-specific polling lives behind build-tagged implementations so
// packages using input can still build outside js/wasm.
package input

// These bounds keep each poll allocation-free and snapshots cheap to copy.
const (
	MaxGamepads = 4
	MaxButtons  = 32
	MaxAxes     = 8
)

// ButtonState is an owned copy of one GamepadButton state.
type ButtonState struct {
	Pressed bool
	Value   float64
}

// GamepadState is an owned, bounded copy of one browser gamepad.
type GamepadState struct {
	Connected   bool
	Index       int
	ID          string
	Mapping     string
	ButtonCount int
	Buttons     [MaxButtons]ButtonState
	AxisCount   int
	Axes        [MaxAxes]float64
}

// Gamepads is a bounded snapshot of connected browser gamepads. Pads are
// compacted into the first Count slots; Index retains each browser index.
type Gamepads struct {
	Count int
	Pads  [MaxGamepads]GamepadState
}

// FirstStandard returns the first connected gamepad with the standard
// mapping, if one was included in the snapshot.
func (g Gamepads) FirstStandard() (GamepadState, bool) {
	for i := 0; i < g.Count && i < len(g.Pads); i++ {
		pad := g.Pads[i]
		if pad.Connected && pad.Mapping == "standard" {
			return pad, true
		}
	}
	return GamepadState{}, false
}

func boundedCount(length, limit int) int {
	if length < 0 {
		return 0
	}
	if length > limit {
		return limit
	}
	return length
}
