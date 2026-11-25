package actions

import "slices"

// PressReader is what a Prefix reads: a Map in a drawn frame, or Ticks in a
// simulation step.
type PressReader interface {
	Pressed(name string) bool
	Down(name string) bool
}

// Prefix is a button that changes what the next press means, such as a
// shoulder button that turns the D-pad into four quick choices. It works
// either held with the next press or pressed and let go just before it: "LT,
// Left" and "LT+Left" are the same input. There is no long-press threshold
// and no time window: a pressed prefix waits until a press ends it, or the
// game clears it when it leaves the screen the prefix belongs to.
//
// Use it once per read, in this order: On says whether the prefix applies to
// this read's presses; the game acts on them; After updates the latch.
type Prefix struct {
	// Keys are the actions that are the prefix: a gamepad button and a key
	// often both are.
	Keys []string
	// Ends are the actions whose press ends a waiting prefix: the presses it
	// gives a meaning to (its directions), and a cancel that drops it doing
	// nothing (B, Backspace).
	Ends []string
	// latched is a prefix pressed and let go, waiting for its next press.
	latched bool
}

// On reports whether the prefix applies to r's presses: one of its keys is
// held or pressed, or it was pressed before and still waits.
func (p *Prefix) On(r PressReader) bool {
	if p.latched {
		return true
	}
	for _, k := range p.Keys {
		if r.Down(k) || r.Pressed(k) {
			return true
		}
	}
	return false
}

// Latched reports whether a pressed prefix waits for its next press.
func (p *Prefix) Latched() bool { return p.latched }

// After updates the latch after a read: a press of one of Ends lets the
// prefix go (it was used, held or waiting, or canceled), and a press of the
// prefix with no End in the same read makes it wait for the next one.
func (p *Prefix) After(r PressReader) {
	if slices.ContainsFunc(p.Ends, r.Pressed) {
		p.latched = false
		return
	}
	if slices.ContainsFunc(p.Keys, r.Pressed) {
		p.latched = true
	}
}

// Clear drops a waiting prefix: the game left the screen it belongs to.
func (p *Prefix) Clear() { p.latched = false }
