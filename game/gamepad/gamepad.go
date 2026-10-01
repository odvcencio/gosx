package gamepad

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/game"
)

// changeEpsilon is the minimum value delta that counts as a change worth
// reporting. Real hardware reports tiny jitter even at rest; without a
// threshold Poll would call Input.Apply for every axis on every poll.
const changeEpsilon = 1e-3

// Source abstracts navigator.getGamepads(). NavigatorSource is the real
// browser implementation; tests and native code supply a fake.
type Source interface {
	// Gamepads returns the currently connected pads. An empty or disconnected
	// slot is simply omitted, matching how the browser leaves gaps in
	// navigator.getGamepads() when a pad in the middle of the array
	// disconnects.
	Gamepads() []State
}

// State is one gamepad's snapshot for a single poll.
type State struct {
	// Index is the browser-assigned gamepad slot. It is stable for a given
	// physical pad for the life of the connection.
	Index int
	// ID is the browser's device string, e.g.
	// "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)".
	ID string
	// Mapping is "standard" for a pad the browser normalizes to the W3C
	// standard gamepad layout, or "" otherwise.
	Mapping string
	// Buttons holds each button's analog value in [0,1]. A purely digital
	// button reports 0 or 1.
	Buttons []float64
	// Axes holds each axis value in [-1,1]. The standard mapping places the
	// left stick at axes 0 (x) and 1 (y), and the right stick at 2 (x) and 3
	// (y).
	Axes []float64
}

// Config controls how Poller translates gamepad state into game.InputEvent
// calls.
type Config struct {
	// Deadzone is the radial deadzone applied to each stick (an axis pair),
	// in [0,1). Zero selects 0.15.
	Deadzone float64
	// PlayerID is stamped on every emitted InputEvent. Leave it empty for
	// the common single-local-player case, where the server assigns player
	// identity from the connection rather than the input payload.
	PlayerID string
	// AllowNonStandard includes gamepads whose Mapping is not "standard".
	// Button and axis indices are not portable across hardware for such a
	// pad, so this defaults to false (excluded).
	AllowNonStandard bool
}

func (c Config) normalized() Config {
	if c.Deadzone <= 0 {
		c.Deadzone = 0.15
	}
	if c.Deadzone >= 1 {
		c.Deadzone = 0.99
	}
	return c
}

// Poller tracks per-gamepad edge state across polls so Poll only reports
// values that changed. The zero value is not usable; construct one with
// NewPoller.
type Poller struct {
	cfg         Config
	connectedID map[int]string
	buttons     map[int][]float64
	axes        map[int][]float64
}

// NewPoller creates a Poller with cfg. cfg's zero value is a single local
// player, a 0.15 deadzone, and standard-mapping-only pads.
func NewPoller(cfg Config) *Poller {
	return &Poller{
		cfg:         cfg.normalized(),
		connectedID: make(map[int]string),
		buttons:     make(map[int][]float64),
		axes:        make(map[int][]float64),
	}
}

// Poll reads source and applies every changed button and axis value to
// input as a game.EventGamepad InputEvent with code "buttonN" or "axisN".
func (p *Poller) Poll(source Source, input *game.Input) {
	if p == nil || source == nil || input == nil {
		return
	}
	seen := make(map[int]bool)
	for _, pad := range source.Gamepads() {
		if !p.cfg.AllowNonStandard && pad.Mapping != "standard" {
			continue
		}
		seen[pad.Index] = true
		if p.connectedID[pad.Index] != pad.ID {
			// A different physical pad took this slot (or it just connected).
			// Drop stale edge state so held buttons on the old pad cannot
			// report a spurious release, and so the new pad's resting state
			// is not compared against garbage.
			delete(p.buttons, pad.Index)
			delete(p.axes, pad.Index)
			p.connectedID[pad.Index] = pad.ID
		}
		p.pollButtons(pad, input)
		p.pollAxes(pad, input)
	}
	for index := range p.connectedID {
		if seen[index] {
			continue
		}
		delete(p.connectedID, index)
		delete(p.buttons, index)
		delete(p.axes, index)
	}
}

func (p *Poller) pollButtons(pad State, input *game.Input) {
	prev := p.buttons[pad.Index]
	next := make([]float64, len(pad.Buttons))
	copy(next, pad.Buttons)
	for i, value := range pad.Buttons {
		old := 0.0
		if i < len(prev) {
			old = prev[i]
		}
		if math.Abs(value-old) < changeEpsilon {
			continue
		}
		input.Apply(game.InputEvent{
			Kind:     game.EventGamepad,
			Code:     fmt.Sprintf("button%d", i),
			Value:    value,
			PlayerID: p.cfg.PlayerID,
			DeviceID: pad.ID,
		})
	}
	p.buttons[pad.Index] = next
}

func (p *Poller) pollAxes(pad State, input *game.Input) {
	adjusted := make([]float64, len(pad.Axes))
	copy(adjusted, pad.Axes)
	for i := 0; i+1 < len(adjusted); i += 2 {
		adjusted[i], adjusted[i+1] = applyRadialDeadzone(adjusted[i], adjusted[i+1], p.cfg.Deadzone)
	}
	if len(adjusted)%2 == 1 {
		last := len(adjusted) - 1
		adjusted[last] = applyLinearDeadzone(adjusted[last], p.cfg.Deadzone)
	}

	prev := p.axes[pad.Index]
	for i, value := range adjusted {
		old := 0.0
		if i < len(prev) {
			old = prev[i]
		}
		if math.Abs(value-old) < changeEpsilon {
			continue
		}
		input.Apply(game.InputEvent{
			Kind:     game.EventGamepad,
			Code:     fmt.Sprintf("axis%d", i),
			Value:    value,
			PlayerID: p.cfg.PlayerID,
			DeviceID: pad.ID,
		})
	}
	p.axes[pad.Index] = adjusted
}

// applyRadialDeadzone rescales a stick's (x,y) pair so the dead zone reads as
// exactly 0 and the response ramps smoothly from there to full deflection at
// the edge, instead of snapping straight from 0 to deadzone.
func applyRadialDeadzone(x, y, deadzone float64) (float64, float64) {
	magnitude := math.Hypot(x, y)
	if magnitude < deadzone {
		return 0, 0
	}
	if magnitude > 1 {
		magnitude = 1
	}
	scale := (magnitude - deadzone) / (1 - deadzone) / magnitude
	return x * scale, y * scale
}

// applyLinearDeadzone is applyRadialDeadzone for a lone, unpaired axis (a
// trigger reported on the axes array rather than the buttons array, on some
// non-conforming hardware).
func applyLinearDeadzone(v, deadzone float64) float64 {
	sign := 1.0
	if v < 0 {
		sign = -1
		v = -v
	}
	if v < deadzone {
		return 0
	}
	if v > 1 {
		v = 1
	}
	return sign * (v - deadzone) / (1 - deadzone)
}
