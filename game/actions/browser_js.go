//go:build js && wasm

package actions

import (
	"strconv"
	"strings"
	"syscall/js"
)

// BrowserConfig tunes Attach.
type BrowserConfig struct {
	// Target receives keyboard, mouse and wheel listeners. The zero value
	// selects the window for keys and the document for the rest.
	Target js.Value
	// PreventDefault, if set, is asked for each key code; returning true calls
	// preventDefault so the browser does not scroll on Space or arrows.
	PreventDefault func(code string) bool
	// A key press aimed at a form control (an input, textarea, select or
	// contenteditable element) is skipped, so typing in a field or stepping
	// through a menu does not move the player. KeepEditable turns that off
	// for every key. A key release is never skipped: a key that went down
	// before the focus moved into a field must not stay down.
	KeepEditable bool
	// KeepKey, if set, is asked for each key press aimed at a form control,
	// with the key's code and the control's kind (ControlText, ControlSelect,
	// ControlRange, ControlToggle or ControlButton). Returning true sends the
	// key to the game as well; the control still acts on it unless
	// PreventDefault says otherwise. KeepUnusedKeys keeps every key the
	// control does not use itself. It is not asked when KeepEditable is set.
	KeepKey func(code, kind string) bool
	// PointerLockOnly makes MouseDX and MouseDY report only while the pointer
	// is locked (the default for game cameras). Wheel and buttons always report.
	PointerLockOnly bool
}

// Attach registers keyboard, mouse, wheel, blur and tab-hide listeners that
// feed sink. It returns a function that removes them. Keys arrive on device
// DeviceKeyboard and the mouse on DeviceMouse.
//
// The per-frame work (Map.Update and the query methods) does not allocate.
// The listeners allocate only the Go strings that syscall/js makes for
// event.code; they run per input event, not per frame.
func Attach(sink Sink, cfg BrowserConfig) (detach func()) {
	global := js.Global()
	win := global
	doc := global.Get("document")
	target := cfg.Target
	if target.IsUndefined() || target.IsNull() {
		target = win
	}
	var funcs []js.Func
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
		funcs = append(funcs, f)
		on.Call("addEventListener", name, f)
		regs = append(regs, reg{on, name, f})
	}
	// control is the kind of form control the event is aimed at, or "".
	control := func(ev js.Value) string {
		t := ev.Get("target")
		if !t.Truthy() {
			return ""
		}
		tag := t.Get("tagName")
		if tag.Type() != js.TypeString {
			return "" // the window or the document
		}
		kind := ""
		if strings.EqualFold(tag.String(), "input") {
			if typ := t.Get("type"); typ.Type() == js.TypeString {
				kind = typ.String()
			}
		}
		return ControlKind(tag.String(), kind, t.Get("isContentEditable").Truthy())
	}
	key := func(down bool) func(ev js.Value) {
		return func(ev js.Value) {
			if ev.Get("repeat").Truthy() {
				return
			}
			code := ev.Get("code").String()
			if code == "" {
				return
			}
			if down && !cfg.KeepEditable {
				if kind := control(ev); kind != "" && (cfg.KeepKey == nil || !cfg.KeepKey(code, kind)) {
					return
				}
			}
			v := 0.0
			if down {
				v = 1
			}
			sink.Feed(Event{Device: DeviceKeyboard, Source: Key(code), Value: v})
			if cfg.PreventDefault != nil && cfg.PreventDefault(code) {
				ev.Call("preventDefault")
			}
		}
	}
	add(target, "keydown", key(true))
	add(target, "keyup", key(false))
	button := func(down bool) func(ev js.Value) {
		return func(ev js.Value) {
			v := 0.0
			if down {
				v = 1
			}
			sink.Feed(Event{Device: DeviceMouse, Source: Source{KindMouseButton, "Mouse" + strconv.Itoa(ev.Get("button").Int())}, Value: v})
		}
	}
	add(target, "mousedown", button(true))
	add(target, "mouseup", button(false))
	add(target, "mousemove", func(ev js.Value) {
		if cfg.PointerLockOnly && !doc.Get("pointerLockElement").Truthy() {
			return
		}
		if dx := ev.Get("movementX").Float(); dx != 0 {
			sink.Feed(Event{Device: DeviceMouse, Source: Motion(CodeMouseDX), Value: dx})
		}
		if dy := ev.Get("movementY").Float(); dy != 0 {
			sink.Feed(Event{Device: DeviceMouse, Source: Motion(CodeMouseDY), Value: dy})
		}
	})
	add(target, "wheel", func(ev js.Value) {
		// deltaY > 0 is a scroll down; report positive as "forward" the way
		// most games expect, which is the negation.
		if dy := ev.Get("deltaY").Float(); dy != 0 {
			sink.Feed(Event{Device: DeviceMouse, Source: Motion(CodeWheelY), Value: -dy})
		}
		if dx := ev.Get("deltaX").Float(); dx != 0 {
			sink.Feed(Event{Device: DeviceMouse, Source: Motion(CodeWheelX), Value: dx})
		}
	})
	add(win, "blur", func(js.Value) { sink.ReleaseAll() })
	add(doc, "visibilitychange", func(js.Value) {
		if doc.Get("hidden").Truthy() {
			sink.ReleaseAll()
		}
	})
	return func() {
		for _, r := range regs {
			r.on.Call("removeEventListener", r.name, r.fn)
		}
		for _, f := range funcs {
			f.Release()
		}
	}
}

// RequestPointerLock asks the browser to lock the pointer to el, which must
// happen inside a user gesture such as a click.
func RequestPointerLock(el js.Value) {
	if el.Truthy() && el.Get("requestPointerLock").Type() == js.TypeFunction {
		el.Call("requestPointerLock")
	}
}

// ExitPointerLock releases the pointer.
func ExitPointerLock() {
	doc := js.Global().Get("document")
	if doc.Get("exitPointerLock").Type() == js.TypeFunction {
		doc.Call("exitPointerLock")
	}
}

// PointerLocked reports whether the pointer is locked.
func PointerLocked() bool {
	return js.Global().Get("document").Get("pointerLockElement").Truthy()
}
