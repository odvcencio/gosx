//go:build js && wasm

package hand

import (
	"math"
	"syscall/js"
)

// AttachPointers reports the page's pointers to t: every pointerdown, by
// its pointer type, and the mouse's pointermove distance. It listens on the
// document in the capture phase, so a control that stops the event still
// counts as a use. Compatibility mouse events a browser makes after a tap
// are not pointer events of the mouse type, so a tap never reads as the
// mouse. It returns a function that removes the listeners.
func AttachPointers(t *Tracker) (detach func()) {
	doc := js.Global().Get("document")
	opts := js.Global().Get("Object").New()
	opts.Set("capture", true)
	opts.Set("passive", true)
	down := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		switch args[0].Get("pointerType").String() {
		case "touch":
			t.Pointer(PointerTouch)
		case "pen":
			t.Pointer(PointerPen)
		case "mouse":
			t.Pointer(PointerMouse)
		}
		return nil
	})
	move := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Get("pointerType").String() != "mouse" {
			return nil
		}
		ev := args[0]
		t.MouseMoved(math.Hypot(ev.Get("movementX").Float(), ev.Get("movementY").Float()))
		return nil
	})
	doc.Call("addEventListener", "pointerdown", down, opts)
	doc.Call("addEventListener", "pointermove", move, opts)
	return func() {
		doc.Call("removeEventListener", "pointerdown", down, opts)
		doc.Call("removeEventListener", "pointermove", move, opts)
		down.Release()
		move.Release()
	}
}

// TouchOnly reports whether a touch screen is the page's only pointer: a
// phone or a tablet with no mouse or trackpad. Start takes it.
func TouchOnly() bool {
	mm := js.Global().Get("matchMedia")
	if mm.Type() != js.TypeFunction {
		return false
	}
	coarse := js.Global().Call("matchMedia", "(any-pointer: coarse)").Get("matches").Truthy()
	fine := js.Global().Call("matchMedia", "(any-pointer: fine)").Get("matches").Truthy()
	return coarse && !fine
}
