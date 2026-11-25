// Package focus moves the keyboard focus over a page's controls with a
// gamepad, the way a console menu does: the D-pad walks the buttons and
// menus on screen, A presses the one with the focus (or steps a menu to its
// next choice, or a slider one step), Left and Right step a focused menu or
// slider, a stick scrolls a long dialog (Scope.Scroll), and B goes back.
//
// The rules are plain functions over indexes (Walk, InRow, StepChoice,
// StepRange) with native tests. Scope applies them to the page through
// package client/browser: it finds the controls in the open modal dialog,
// or in its root when none is open, in document order, leaving out the
// hidden, the disabled and those taken out of the Tab order
// (tabindex="-1"). A roving row (a radio-like group whose one member holds
// tabindex 0) is one stop in that walk, and Left and Right move inside it.
//
// Two helpers keep a keyboard or a controller from being left with the
// focus on nothing. When asks for the focus on a control an island has not
// drawn yet and gets it once the control is drawn and enabled (the engine
// calls Step once a frame). Keep rewrites a panel's markup and puts the focus
// back on the same control, or on the panel's main button.
package focus

import "math"

// Dir is a direction the focus moves in.
type Dir uint8

const (
	Prev Dir = iota + 1
	Next
	Up
	Down
	Left
	Right
)

// forward reports whether d moves on through the document order.
func (d Dir) forward() bool { return d == Next || d == Down || d == Right }

// Walk is the index the focus moves to over n stops in document order, from
// at (-1 when the focus is on none of them). Next, Down and Right go on,
// Prev, Up and Left go back. From none, going on takes the first stop and
// going back the last. Past an end the walk wraps unless noWrap is set, when
// it stays. It returns -1 when there are no stops.
func Walk(n, at int, d Dir, noWrap bool) int {
	if n <= 0 {
		return -1
	}
	if at < 0 || at >= n {
		if d.forward() {
			return 0
		}
		return n - 1
	}
	step := -1
	if d.forward() {
		step = 1
	}
	next := at + step
	switch {
	case next >= 0 && next < n:
		return next
	case noWrap:
		return at
	}
	return (next + n) % n
}

// InRow moves along a roving row of n members from at, for Left and Right:
// one member at a time, stopping at the ends. It reports whether the
// direction belongs to the row (Left or Right from a member), so the press
// is used even at an end, and the walk does not leave the row sideways.
func InRow(n, at int, d Dir) (int, bool) {
	if at < 0 || at >= n || (d != Left && d != Right) {
		return at, false
	}
	if d == Left {
		return max(0, at-1), true
	}
	return min(n-1, at+1), true
}

// StepChoice is the next choice of a menu of n options from the selected
// one, delta steps on (a negative delta goes back), wrapping round and
// passing over the options for which enabled is false. It returns
// selected, unchanged, when no other option is enabled, and -1 when none is.
// A selected index outside the menu counts as before the first option.
func StepChoice(n, selected, delta int, enabled func(int) bool) int {
	if n <= 0 || delta == 0 {
		if n > 0 && selected >= 0 && selected < n {
			return selected
		}
		return -1
	}
	at := selected
	if at < 0 || at >= n {
		at = -1
		if delta < 0 {
			at = n
		}
	}
	dir := 1
	if delta < 0 {
		dir, delta = -1, -delta
	}
	found := -1
	for i := 1; i <= n; i++ {
		c := ((at+dir*i)%n + n) % n
		if !enabled(c) {
			continue
		}
		found = c
		if delta--; delta == 0 {
			return c
		}
	}
	if found < 0 && selected >= 0 && selected < n && enabled(selected) {
		return selected
	}
	return found
}

// Range is a slider's bounds and step. Step 0 (no step, or step="any")
// moves one hundredth of the span.
type Range struct {
	Min, Max, Step float64
}

// StepRange is a slider's value after delta steps from v, on the step grid
// that starts at Min. Past Max it wraps to Min when wrap is set (a
// controller's A walks a slider round, as a menu's choices do), else it
// stays at Max; below Min it wraps to Max or stays at Min.
func StepRange(r Range, v float64, delta int, wrap bool) float64 {
	if r.Max < r.Min {
		r.Max = r.Min
	}
	step := r.Step
	if step <= 0 {
		step = (r.Max - r.Min) / 100
		if step <= 0 {
			return r.Min
		}
	}
	// Snap to the grid first, then step, with a tolerance for the binary
	// fractions a step like 0.1 leaves.
	k := math.Round((v - r.Min) / step)
	next := r.Min + (k+float64(delta))*step
	const eps = 1e-9
	switch {
	case next > r.Max+eps:
		if wrap && v >= r.Max-eps {
			return r.Min
		}
		return r.Max
	case next < r.Min-eps:
		if wrap && v <= r.Min+eps {
			return r.Max
		}
		return r.Min
	}
	return math.Min(r.Max, math.Max(r.Min, next))
}
