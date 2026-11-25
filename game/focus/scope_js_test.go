//go:build js && wasm

package focus

import (
	"strconv"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/browser"
)

func (d dom) root() (js.Value, Scope) {
	r := d.el(none, "div", map[string]any{"id": "root"}, 800)
	return r, Scope{Root: browser.CurrentDocument().ByID("root")}
}

func (d dom) menu(parent js.Value, choices, selected int) js.Value {
	sel := d.el(parent, "select", map[string]any{"id": "menu"}, 100)
	for i := 0; i < choices; i++ {
		d.el(sel, "option", map[string]any{"value": strconv.Itoa(i)}, 100)
	}
	sel.Set("selectedIndex", selected)
	return sel
}

func (d dom) slider(parent js.Value, min, max, step, value string) js.Value {
	r := d.el(parent, "input", map[string]any{"type": "range", "min": min, "max": max, "step": step}, 100)
	r.Set("value", value)
	return r
}

func events(el js.Value) string { return el.Get("events").Call("join", ",").String() }

func TestLeftAndRightStepAFocusedMenuAndLeaveTheFocus(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	menu := d.menu(root, 3, 1)
	next := d.el(root, "button", map[string]any{"id": "next"}, 40)
	menu.Call("focus")

	if !scope.Move(Right) || menu.Get("selectedIndex").Int() != 2 {
		t.Fatalf("Right: selectedIndex = %v", menu.Get("selectedIndex"))
	}
	if got := events(menu); got != "input,change" {
		t.Fatalf("one step fires %q, want input then change", got)
	}
	d.focused(menu, "after Right")
	if !scope.Move(Right) || menu.Get("selectedIndex").Int() != 2 {
		t.Fatal("Right past the last choice must stay on it and still use the press")
	}
	d.focused(menu, "after Right at the end")
	for _, want := range []int{1, 0, 0} {
		if !scope.Move(Left) || menu.Get("selectedIndex").Int() != want {
			t.Fatalf("Left: selectedIndex = %v, want %d", menu.Get("selectedIndex"), want)
		}
	}
	d.focused(menu, "after Left")
	if !scope.Move(Down) {
		t.Fatal("Down did not move")
	}
	d.focused(next, "Down moves the focus")
	if menu.Get("selectedIndex").Int() != 0 {
		t.Fatal("Down changed the menu")
	}
}

func TestLeftAndRightMoveTheFocusWhenStepsAreOff(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	scope.NoStep = true
	menu := d.menu(root, 3, 1)
	next := d.el(root, "button", map[string]any{"id": "next"}, 40)
	menu.Call("focus")
	scope.Move(Right)
	d.focused(next, "Right with NoStep")
	if menu.Get("selectedIndex").Int() != 1 || events(menu) != "" {
		t.Fatal("a menu changed with NoStep set")
	}
}

func TestLeftAndRightStepASliderByItsStep(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	r := d.slider(root, "0", "10", "5", "5")
	r.Call("focus")
	for _, c := range []struct {
		d    Dir
		want string
	}{{Right, "10"}, {Right, "10"}, {Left, "5"}, {Left, "0"}, {Left, "0"}} {
		if !scope.Move(c.d) || r.Get("value").String() != c.want {
			t.Fatalf("Move(%d): value = %v, want %s", c.d, r.Get("value"), c.want)
		}
	}
	d.focused(r, "after the slider steps")
	if got := events(r); got != "input,change,input,change,input,change" {
		t.Fatalf("slider events = %q", got)
	}
}

func TestScrollMovesTheNearestScrollerAndReportsTheEnd(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	root.Get("attrs").Set("overflow", "auto")
	root.Set("scrollHeight", 1000)
	root.Set("clientHeight", 400)
	root.Set("scrollTop", 0)
	inner := d.el(root, "div", map[string]any{"overflow": "auto"}, 700)
	inner.Set("scrollHeight", 500)
	inner.Set("clientHeight", 300)
	inner.Set("scrollTop", 0)
	plain := d.el(inner, "div", map[string]any{}, 700) // draws, does not scroll
	btn := d.el(plain, "button", map[string]any{"id": "in"}, 40)

	// Nothing is focused: the scope's root scrolls, clamped to its range.
	d.focused(d.doc.Get("body"), "start")
	root.Set("scrollTop", 0)
	if !scope.Scroll(450) || root.Get("scrollTop").Float() != 450 {
		t.Fatalf("root scrollTop = %v", root.Get("scrollTop"))
	}
	if !scope.Scroll(450) || root.Get("scrollTop").Float() != 600 {
		t.Fatalf("a scroll past the end must clamp: scrollTop = %v", root.Get("scrollTop"))
	}
	if scope.Scroll(10) {
		t.Fatal("Scroll at the end reported a move")
	}
	if !scope.Scroll(-100) || root.Get("scrollTop").Float() != 500 {
		t.Fatalf("scrolling back: scrollTop = %v", root.Get("scrollTop"))
	}
	// A focus inside scrolls the nearest scroller, not the root.
	btn.Call("focus")
	if !scope.Scroll(120) || inner.Get("scrollTop").Float() != 120 || root.Get("scrollTop").Float() != 500 {
		t.Fatalf("inner = %v, root = %v", inner.Get("scrollTop"), root.Get("scrollTop"))
	}
	if !scope.Scroll(1000) || inner.Get("scrollTop").Float() != 200 || scope.Scroll(1) {
		t.Fatalf("inner must stop at its range: scrollTop = %v", inner.Get("scrollTop"))
	}
}

func TestASummaryIsAStopAndConfirmOpensItsDetails(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	row := d.el(root, "details", map[string]any{}, 300)
	sum := d.el(row, "summary", map[string]any{}, 300)
	inside := d.el(row, "button", map[string]any{"id": "inside"}, 0) // not drawn while shut
	d.el(root, "button", map[string]any{"id": "after"}, 40)
	inside.Set("drawnWidth", 80)

	stops := scope.Stops()
	if len(stops) != 2 || stops[0].TagName() != "SUMMARY" {
		t.Fatalf("stops = %d, want the summary and the button after the row", len(stops))
	}
	scope.Move(Down)
	d.focused(sum, "the walk reaches the summary")
	if !scope.Accept() || !row.Call("hasAttribute", "open").Bool() {
		t.Fatal("confirm on a summary did not open its details")
	}
	if got := len(scope.Stops()); got != 3 {
		t.Fatalf("stops after opening = %d, want 3", got)
	}
	scope.Move(Down)
	d.focused(inside, "the walk reaches what the row hid")
}

func TestConfirmWithNothingFocusedTakesTheFocusFirst(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	first := d.el(root, "button", map[string]any{"id": "first"}, 40)
	d.el(root, "button", map[string]any{"id": "second"}, 40)
	if !scope.Accept() {
		t.Fatal("Accept with nothing focused reported it did nothing")
	}
	d.focused(first, "first confirm")
	if events(first) != "" {
		t.Fatalf("first confirm clicked the control: %q", events(first))
	}
	if !scope.Accept() || events(first) != "click" {
		t.Fatalf("second confirm: events = %q, want one click", events(first))
	}
}

// scroller makes a div that scrolls: overflow auto, content scrollHeight tall in a
// clientHeight box.
func (d dom) scroller(parent js.Value, scrollHeight, clientHeight int) js.Value {
	e := d.el(parent, "div", map[string]any{"overflow": "auto"}, 700)
	e.Set("scrollHeight", scrollHeight)
	e.Set("clientHeight", clientHeight)
	e.Set("scrollTop", 0)
	return e
}

func TestScrollTakesTheBodyBesideTheFocusedFooterButton(t *testing.T) {
	d := newDOM(t)
	dlg := d.el(none, "dialog", map[string]any{"open": ""}, 600)
	d.el(dlg, "div", map[string]any{}, 600) // a head that does not scroll
	body := d.scroller(dlg, 900, 300)
	second := d.scroller(dlg, 700, 300)
	foot := d.el(dlg, "div", map[string]any{"class": "foot"}, 600)
	btn := d.el(foot, "button", map[string]any{"id": "close", "class": "primary"}, 40)
	btn.Call("focus")
	scope := Scope{} // a dialog is open, so it is the area
	if !scope.Scroll(200) || body.Get("scrollTop").Float() != 200 {
		t.Fatalf("body scrollTop = %v", body.Get("scrollTop"))
	}
	if !scope.Scroll(1000) || body.Get("scrollTop").Float() != 600 {
		t.Fatalf("body must stop at its range: scrollTop = %v", body.Get("scrollTop"))
	}
	if scope.Scroll(10) {
		t.Fatal("Scroll at the end of the first scroller reported a move")
	}
	if second.Get("scrollTop").Float() != 0 {
		t.Fatal("a second scroller moved: the first in document order is taken")
	}
}

func TestAScrollingAncestorIsStillPreferredToADescendant(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	early := d.scroller(root, 800, 300) // first in document order, not an ancestor
	panel := d.scroller(root, 800, 300)
	btn := d.el(panel, "button", map[string]any{"id": "in"}, 40)
	btn.Call("focus")
	if !scope.Scroll(100) || panel.Get("scrollTop").Float() != 100 {
		t.Fatalf("the ancestor did not scroll: %v", panel.Get("scrollTop"))
	}
	if early.Get("scrollTop").Float() != 0 {
		t.Fatal("an earlier scroller moved though the focus has a scrolling ancestor")
	}
}

func TestScrollWithNothingScrollableMovesNothing(t *testing.T) {
	d := newDOM(t)
	root, scope := d.root()
	plain := d.el(root, "div", map[string]any{}, 600)
	plain.Set("scrollTop", 0)
	btn := d.el(plain, "button", map[string]any{"id": "b"}, 40)
	btn.Call("focus")
	if scope.Scroll(100) || plain.Get("scrollTop").Float() != 0 {
		t.Fatal("Scroll moved or reported a move with nothing scrollable")
	}
}
