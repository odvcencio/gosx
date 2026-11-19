//go:build js && wasm

package actions

import (
	"strconv"
	"syscall/js"
)

// The attributes AttachTouch reads from the page, and the ones it writes.
const (
	// AttrStick marks an element as an on-screen stick and names it:
	// data-gosx-stick="steer" feeds TouchAxis("steer", false) and
	// TouchAxis("steer", true). The element's box is the area a finger can
	// land in.
	AttrStick = "data-gosx-stick"
	// AttrStickRadius, AttrStickDeadzone and AttrStickCenter set the stick's
	// shape on the element: the travel in CSS pixels, the dead zone as a
	// share of it, and "fixed", "floating" or "follow" for the centre.
	// Without them TouchConfig.Stick is used.
	AttrStickRadius   = "data-gosx-stick-radius"
	AttrStickDeadzone = "data-gosx-stick-deadzone"
	AttrStickCenter   = "data-gosx-stick-center"
	// AttrHold marks an element as an on-screen button that reads down while
	// a finger is on it, and names it: data-gosx-hold="sprint" feeds
	// TouchButton("sprint").
	AttrHold = "data-gosx-hold"
	// AttrTouch is written on a stick or a button while a finger holds it:
	// data-gosx-touch="on". A stylesheet draws the pressed state from it.
	AttrTouch = "data-gosx-touch"
)

// TouchConfig tunes AttachTouch.
type TouchConfig struct {
	// Root hears the pointer events. The zero value is the document.
	Root js.Value
	// Stick is the shape of a stick whose element does not give its own.
	Stick Stick
	// Allow, if set, is asked when a finger lands on a stick or a button.
	// Returning false leaves the touch to the page: the control is off.
	Allow func(name string, stick bool) bool
	// Ended, if set, is called when a touch ends, at the moment it ends:
	// with the control's name, whether it was a stick, and whether the touch
	// was taken away, not lifted. For a lifted finger this is before the
	// click the browser may make of the tap, so a game that acts on the
	// finger's lift can drop that click.
	Ended func(name string, stick, canceled bool)
}

// AttachTouch reads on-screen sticks and buttons from the page and feeds
// sink, through a Touches it returns. The page marks the elements; nothing
// else is needed from it:
//
//	<div data-gosx-stick="steer"></div>
//	<button data-gosx-hold="sprint">Sprint</button>
//
// A finger, a pen or the primary mouse button that goes down on a marked
// element takes it, and keeps it wherever the finger slides (the pointer is
// captured). Each finger holds one control, so two thumbs hold a stick and a
// button together. Lifting ends the touch; the browser taking the pointer
// away, the window losing the focus or the page being hidden cancels it, and
// a canceled button release reads State.Canceled. A button with the disabled
// attribute or aria-disabled="true" is not taken.
//
// While a control is held its element carries data-gosx-touch="on". A held
// stick also carries four custom properties for a stylesheet to draw it
// with: --stick-cx and --stick-cy, its centre in pixels from the element's
// left and top edges, and --stick-x and --stick-y, its deflection from -1 to
// 1 (y grows downward). They are removed when the finger goes.
//
// Give the elements touch-action: none in the stylesheet, or the browser
// scrolls with the finger and cancels the touch.
//
// Touches.Drop ends the touch on a control that is going away with a finger
// still on it: hiding an element does not take its finger away, and the
// browser goes on sending that finger's moves to it.
//
// The returned function removes the listeners and cancels every touch.
func AttachTouch(sink Sink, cfg TouchConfig) (*Touches, func()) {
	global := js.Global()
	doc := global.Get("document")
	root := cfg.Root
	if root.IsUndefined() || root.IsNull() {
		root = doc
	}
	t := NewTouches(sink)
	// held is the element each finger holds, for the attribute and the
	// custom properties.
	type grip struct {
		el    js.Value
		stick bool
	}
	held := map[int]grip{}
	// centre is where each held stick's centre was last drawn.
	centre := map[int][2]float64{}
	type reg struct {
		on   js.Value
		name string
		fn   js.Func
	}
	var regs []reg
	add := func(on js.Value, name string, fn func(ev js.Value)) {
		f := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				fn(args[0])
			}
			return nil
		})
		on.Call("addEventListener", name, f)
		regs = append(regs, reg{on, name, f})
	}
	px := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) + "px" }
	unit := func(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
	draw := func(el js.Value, st StickState, left, top float64, centre bool) {
		style := el.Get("style")
		if centre {
			style.Call("setProperty", "--stick-cx", px(st.CenterX-left))
			style.Call("setProperty", "--stick-cy", px(st.CenterY-top))
		}
		style.Call("setProperty", "--stick-x", unit(st.X))
		style.Call("setProperty", "--stick-y", unit(st.Y))
	}
	// Whatever ends a touch (a lift, a cancel, Drop, ReleaseAll), the mark
	// and the custom properties come off its element.
	t.OnEnd = func(pointer int, name string, stick, canceled bool) {
		if cfg.Ended != nil {
			defer cfg.Ended(name, stick, canceled)
		}
		g, ok := held[pointer]
		if !ok {
			return
		}
		delete(held, pointer)
		delete(centre, pointer)
		g.el.Call("removeAttribute", AttrTouch)
		if g.stick {
			style := g.el.Get("style")
			for _, p := range [...]string{"--stick-cx", "--stick-cy", "--stick-x", "--stick-y"} {
				style.Call("removeProperty", p)
			}
		}
	}
	number := func(el js.Value, attr string) (float64, bool) {
		v := el.Call("getAttribute", attr)
		if v.Type() != js.TypeString {
			return 0, false
		}
		n, err := strconv.ParseFloat(v.String(), 64)
		return n, err == nil
	}
	add(root, "pointerdown", func(ev js.Value) {
		if ev.Get("pointerType").String() == "mouse" && ev.Get("button").Int() != 0 {
			return
		}
		target := ev.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return
		}
		el := target.Call("closest", "["+AttrStick+"],["+AttrHold+"]")
		if !el.Truthy() {
			return
		}
		pointer := ev.Get("pointerId").Int()
		x, y := ev.Get("clientX").Float(), ev.Get("clientY").Float()
		if name := el.Call("getAttribute", AttrStick); name.Type() == js.TypeString {
			if cfg.Allow != nil && !cfg.Allow(name.String(), true) {
				return
			}
			shape := cfg.Stick
			if r, ok := number(el, AttrStickRadius); ok {
				shape.Radius = r
			}
			if dz, ok := number(el, AttrStickDeadzone); ok {
				shape.Deadzone = dz
			}
			if c := el.Call("getAttribute", AttrStickCenter); c.Type() == js.TypeString {
				shape.Floating = c.String() == "floating" || c.String() == "follow"
				shape.Follow = c.String() == "follow"
			}
			box := el.Call("getBoundingClientRect")
			left, top := box.Get("left").Float(), box.Get("top").Float()
			st, ok := t.StickDown(pointer, name.String(), x, y, left+box.Get("width").Float()/2, top+box.Get("height").Float()/2, shape)
			if !ok {
				return
			}
			held[pointer] = grip{el, true}
			centre[pointer] = [2]float64{st.CenterX, st.CenterY}
			el.Call("setAttribute", AttrTouch, "on")
			draw(el, st, left, top, true)
			capture(el, pointer)
			// A stick is not a control the browser knows: keep the finger
			// from selecting text or starting a drag.
			ev.Call("preventDefault")
			return
		}
		name := el.Call("getAttribute", AttrHold)
		if name.Type() != js.TypeString || name.String() == "" {
			return
		}
		if el.Get("disabled").Truthy() || el.Call("getAttribute", "aria-disabled").String() == "true" {
			return
		}
		if cfg.Allow != nil && !cfg.Allow(name.String(), false) {
			return
		}
		if !t.ButtonDown(pointer, name.String()) {
			return
		}
		held[pointer] = grip{el, false}
		el.Call("setAttribute", AttrTouch, "on")
		capture(el, pointer)
	})
	add(root, "pointermove", func(ev js.Value) {
		pointer := ev.Get("pointerId").Int()
		g, ok := held[pointer]
		if !ok || !g.stick {
			return
		}
		st, ok := t.Move(pointer, ev.Get("clientX").Float(), ev.Get("clientY").Float())
		if !ok {
			return
		}
		// The centre moves only when the stick follows the finger.
		var left, top float64
		follows := st.CenterX != centre[pointer][0] || st.CenterY != centre[pointer][1]
		if follows {
			centre[pointer] = [2]float64{st.CenterX, st.CenterY}
			box := g.el.Call("getBoundingClientRect")
			left, top = box.Get("left").Float(), box.Get("top").Float()
		}
		draw(g.el, st, left, top, follows)
	})
	end := func(canceled bool) func(ev js.Value) {
		return func(ev js.Value) {
			pointer := ev.Get("pointerId").Int()
			if canceled {
				t.Cancel(pointer)
			} else {
				t.Up(pointer)
			}
		}
	}
	add(root, "pointerup", end(false))
	add(root, "pointercancel", end(true))
	// The capture is lost after every pointerup too; by then the finger is
	// already gone from the tracker and this does nothing.
	add(root, "lostpointercapture", end(true))
	add(global, "blur", func(js.Value) { t.ReleaseAll() })
	add(doc, "visibilitychange", func(js.Value) {
		if doc.Get("hidden").Truthy() {
			t.ReleaseAll()
		}
	})
	return t, func() {
		t.ReleaseAll()
		for _, r := range regs {
			r.on.Call("removeEventListener", r.name, r.fn)
			r.fn.Release()
		}
	}
}

// capture keeps the pointer's events coming to el wherever the finger goes.
// The browser refuses a pointer it does not know (an event made by a
// script), and that is no reason to stop.
func capture(el js.Value, pointer int) {
	defer func() { _ = recover() }()
	el.Call("setPointerCapture", pointer)
}
