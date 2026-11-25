package focus

import (
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/client/browser"
)

// DefaultControls are the controls a Scope walks when it names none:
// enabled buttons and menus, and the summary of a details row. Text fields,
// sliders and links are left to the keyboard and the pointer unless a Scope
// asks for them.
const DefaultControls = "button:not(:disabled), select:not(:disabled), summary"

// Scope is where a gamepad's focus may go on a page.
type Scope struct {
	// Root holds the controls when no modal is open.
	Root browser.Element
	// Modal is a selector for an open modal, looked for in the whole
	// document; while one is open the focus stays inside it. "" selects
	// "dialog[open]".
	Modal string
	// Controls selects the stops of the walk. "" selects DefaultControls.
	Controls string
	// Row selects a roving row's container and Member its members (for
	// example ".kit-chips" and ".kit"). The row is one stop in the walk:
	// the member that holds tabindex 0. Left and Right move inside it.
	// Empty Row means no rows.
	Row, Member string
	// NoWrap stops the walk at its ends instead of wrapping round.
	NoWrap bool
	// NoStep keeps Left and Right moving the focus off a menu or a slider, as
	// they did before menus stepped.
	// By default, on a focused select or range input they step its value
	// (see Move) and leave the focus where it is.
	NoStep bool
}

func (s Scope) modal() string {
	if s.Modal == "" {
		return "dialog[open]"
	}
	return s.Modal
}

// Area is where the focus may go now: the open modal, or Root.
func (s Scope) Area() browser.Element {
	if open := browser.CurrentDocument().Query(s.modal()); open.Valid() {
		return open
	}
	return s.Root
}

// shown reports whether a control can take the focus from the walk: it is
// drawn and in the Tab order.
func shown(el browser.Element) bool {
	return el.OffsetWidth() > 0 && strings.TrimSpace(el.Attr("tabindex")) != "-1"
}

// Stops are the controls the walk visits, in document order.
func (s Scope) Stops() []browser.Element {
	controls := s.Controls
	if controls == "" {
		controls = DefaultControls
	}
	area := s.Area()
	if !area.Valid() {
		return nil
	}
	all := area.QueryAll(controls)
	out := make([]browser.Element, 0, all.Len())
	for i := 0; i < all.Len(); i++ {
		if el := all.At(i); shown(el) {
			out = append(out, el)
		}
	}
	return out
}

func indexOf(els []browser.Element, el browser.Element) int {
	if !el.Valid() {
		return -1
	}
	for i, e := range els {
		if e.Equal(el) {
			return i
		}
	}
	return -1
}

// Move moves the focus one stop in direction d. Left and Right on a member
// of a roving row move along the row, and stop at its ends. Left and Right on
// a focused menu (select) or slider (range input) step its value back or
// forward by one choice or one step and leave the focus where it is, unless
// NoStep is set; the value stops at its first and last choice and the press
// is used there too. It reports whether the press was used: false when
// there is nowhere to go.
func (s Scope) Move(d Dir) bool {
	active := browser.CurrentDocument().ActiveElement()
	if !s.NoStep && (d == Left || d == Right) && s.Area().Contains(active) && !active.Disabled() {
		delta := 1
		if d == Left {
			delta = -1
		}
		switch strings.ToUpper(active.TagName()) {
		case "SELECT":
			stepMenu(active, delta, false, true)
			return true
		case "INPUT":
			if strings.EqualFold(active.Attr("type"), "range") {
				stepSlider(active, delta, false, true)
				return true
			}
		}
	}
	if s.Row != "" && (d == Left || d == Right) && s.Area().Contains(active) {
		if used := s.moveInRow(active, d); used {
			return true
		}
	}
	stops := s.Stops()
	next := Walk(len(stops), indexOf(stops, active), d, s.NoWrap)
	if next < 0 {
		return false
	}
	stops[next].Focus()
	return true
}

func (s Scope) moveInRow(active browser.Element, d Dir) bool {
	member := s.Member
	if member == "" {
		member = s.Controls
		if member == "" {
			member = DefaultControls
		}
	}
	row := active.Closest(s.Row)
	if !row.Valid() || !active.Closest(member).Equal(active) {
		return false
	}
	all := row.QueryAll(member)
	var peers []browser.Element
	for i := 0; i < all.Len(); i++ {
		if el := all.At(i); el.OffsetWidth() > 0 && !el.Disabled() {
			peers = append(peers, el)
		}
	}
	at := indexOf(peers, active)
	next, used := InRow(len(peers), at, d)
	if !used {
		return false
	}
	if next != at {
		active.SetAttr("tabindex", "-1")
		peers[next].SetAttr("tabindex", "0")
		peers[next].Focus()
	}
	return true
}

// Accept presses the control with the focus, as Activate does. When the
// focus is on none of the stops it gives the focus to the first stop and
// presses nothing; the next Accept presses it. It reports whether it used
// the press: false when there are no stops.
func (s Scope) Accept() bool {
	stops := s.Stops()
	if len(stops) == 0 {
		return false
	}
	at := indexOf(stops, browser.CurrentDocument().ActiveElement())
	if at < 0 {
		stops[0].Focus()
		return true
	}
	return Activate(stops[at], true)
}

// Scroll scrolls one scroller by dy CSS pixels, down for a positive dy. The
// scroller is, in this order: the nearest scrolling ancestor of the focused
// element inside the scope's area (the open modal, or Root); else the area
// itself when it scrolls; else the first scrolling descendant of the area in
// document order (a footer button beside a scrolling body is the common
// case). When two descendants scroll, the first in document order is taken;
// a page chooses another by focusing something inside it. The position stops
// at the ends of the scroller's range. It reports whether anything moved, so
// a caller can tell the end of the content, and moves nothing when nothing
// in the area scrolls.
func (s Scope) Scroll(dy float64) bool {
	area := s.Area()
	if !area.Valid() || dy == 0 || math.IsNaN(dy) {
		return false
	}
	target := scroller(area)
	if !target.Valid() {
		target = area
	}
	top := target.ScrollTop()
	next := math.Max(0, math.Min(top+dy, target.ScrollRange()))
	if next == top {
		return false
	}
	target.SetScrollTop(next)
	return true
}

// scroller finds the element Scroll moves, or an invalid element when none
// of the area scrolls.
func scroller(area browser.Element) browser.Element {
	if active := browser.CurrentDocument().ActiveElement(); active.Valid() && area.Contains(active) {
		for el := active.Parent(); el.Valid(); el = el.Parent() {
			if scrolls(el) {
				return el
			}
			if el.Equal(area) {
				break
			}
		}
	}
	if scrolls(area) {
		return area
	}
	all := area.QueryAll("*")
	for i := 0; i < all.Len(); i++ {
		if el := all.At(i); scrolls(el) {
			return el
		}
	}
	return browser.Element{}
}

// scrolls reports whether el is a scroller with content past its box.
func scrolls(el browser.Element) bool {
	switch el.OverflowY() {
	case "auto", "scroll":
		return el.ScrollRange() > 0
	}
	return false
}

// ScrollDelta is the distance in CSS pixels that a stick held at deflection
// (-1 to 1, positive down) scrolls in the given seconds, at speed pixels a
// second when it is fully pushed. Deflections up to deadZone give 0; past
// it the distance grows from 0 to the full speed, so a stick resting off
// centre does not drift. Pass the result to Scope.Scroll.
func ScrollDelta(deflection, seconds, speed, deadZone float64) float64 {
	m := math.Abs(deflection)
	if m <= deadZone || m == 0 || seconds <= 0 || deadZone >= 1 {
		return 0
	}
	m = math.Min(m, 1)
	d := (m - deadZone) / (1 - deadZone) * speed * seconds
	if deflection < 0 {
		return -d
	}
	return d
}

// Activate presses el as a controller's A does: a button is clicked, a menu
// steps to its next enabled choice and a slider one step (round to its
// start past its end when wrap is set), each followed by one bubbling
// change event, so the page's own listener runs. el need not have the
// focus: a shortcut can step a menu the player is not on. It reports
// whether anything happened.
func Activate(el browser.Element, wrap bool) bool {
	if !el.Valid() || el.Disabled() {
		return false
	}
	switch strings.ToUpper(el.TagName()) {
	case "SELECT":
		return StepMenu(el, 1)
	case "INPUT":
		if strings.EqualFold(el.Attr("type"), "range") {
			return StepSlider(el, 1, wrap)
		}
	}
	// A click on a summary opens or shuts its details row, as it does for a
	// pointer.
	el.Click()
	return true
}

// StepMenu moves a select menu delta enabled choices on, wrapping round,
// and dispatches one bubbling change event when the choice changed.
func StepMenu(el browser.Element, delta int) bool {
	return stepMenu(el, delta, true, false)
}

// stepMenu is StepMenu. wrap false stops at the first and last enabled
// choice; input true dispatches an input event before the change event, as
// a person's pick does.
func stepMenu(el browser.Element, delta int, wrap, input bool) bool {
	options := el.QueryAll("option")
	n := options.Len()
	enabled := func(i int) bool {
		o := options.At(i)
		return !o.Disabled() && !o.Closest("optgroup:disabled").Valid()
	}
	at := el.SelectedIndex()
	next := StepChoice(n, at, delta, enabled)
	if next < 0 || next == at || (!wrap && (next-at)*delta < 0) {
		return false
	}
	el.SetSelectedIndex(next)
	if input {
		el.Dispatch("input", true)
	}
	el.Dispatch("change", true)
	return true
}

// StepSlider moves a range input delta steps, on its min, max and step
// attributes (the browser's defaults 0, 100 and 1 when they are missing;
// step="any" moves a hundredth of the span), and dispatches one bubbling
// change event when the value changed.
func StepSlider(el browser.Element, delta int, wrap bool) bool {
	return stepSlider(el, delta, wrap, false)
}

// stepSlider is StepSlider. input true dispatches an input event before the
// change event, as a person's drag does.
func stepSlider(el browser.Element, delta int, wrap, input bool) bool {
	num := func(name string, def float64) float64 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(el.Attr(name)), 64); err == nil {
			return v
		}
		return def
	}
	r := Range{Min: num("min", 0), Max: num("max", 100), Step: num("step", 1)}
	if strings.EqualFold(strings.TrimSpace(el.Attr("step")), "any") {
		r.Step = 0
	}
	v, err := strconv.ParseFloat(el.Value(), 64)
	if err != nil {
		v = r.Min
	}
	next := StepRange(r, v, delta, wrap)
	if next == v {
		return false
	}
	el.SetValue(strconv.FormatFloat(next, 'f', -1, 64))
	if input {
		el.Dispatch("input", true)
	}
	el.Dispatch("change", true)
	return true
}

// FocusFirst gives the focus to the first enabled control in el that
// matches prefer (for example ".primary"), else to its first enabled
// button, menu or input. It reports whether a control took it.
func FocusFirst(el browser.Element, prefer string) bool {
	if !el.Valid() {
		return false
	}
	for _, selector := range []string{prefer, "button:not(:disabled), select:not(:disabled), input:not(:disabled)"} {
		if selector == "" {
			continue
		}
		if first := el.Query(selector); first.Valid() {
			first.Focus()
			return true
		}
	}
	return false
}

// OnPage reports whether the focus is on no control: on the page itself,
// or on a control that is no longer drawn.
func OnPage(active browser.Element) bool {
	return !active.Valid() || strings.EqualFold(active.TagName(), "body") || active.OffsetWidth() == 0
}
