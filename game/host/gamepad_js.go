//go:build js && wasm

package host

import "syscall/js"

// NavigatorSource reads gamepad state from navigator.getGamepads(). The zero
// value is ready to use.
type NavigatorSource struct{}

// Gamepads implements Source.
func (NavigatorSource) Gamepads() []GamepadState {
	nav := js.Global().Get("navigator")
	if nav.IsUndefined() || nav.IsNull() {
		return nil
	}
	getGamepads := nav.Get("getGamepads")
	if getGamepads.Type() != js.TypeFunction {
		return nil
	}
	pads := nav.Call("getGamepads")
	count := pads.Length()
	out := make([]GamepadState, 0, count)
	for i := 0; i < count; i++ {
		pad := pads.Index(i)
		if !pad.Truthy() {
			continue
		}
		state := GamepadState{
			Index:   pad.Get("index").Int(),
			ID:      pad.Get("id").String(),
			Mapping: pad.Get("mapping").String(),
		}
		buttons := pad.Get("buttons")
		state.Buttons = make([]float64, buttons.Length())
		for b := range state.Buttons {
			state.Buttons[b] = buttons.Index(b).Get("value").Float()
		}
		axes := pad.Get("axes")
		state.Axes = make([]float64, axes.Length())
		for a := range state.Axes {
			state.Axes[a] = axes.Index(a).Float()
		}
		out = append(out, state)
	}
	return out
}

// GamepadReader reads navigator.getGamepads() into buffers it reuses, for a
// game that polls every frame. It reads a pad's buttons and axes only when the
// pad's timestamp changed, and its id and mapping only when it connects, so
// polling idle pads allocates almost nothing: syscall/js still makes one handle
// for the pad list and one per connected pad.
//
// Unlike NavigatorSource, the slice Gamepads returns and the Buttons and Axes
// in it are the reader's own: they stay valid until the next call. Copy what
// you keep. game/gamepad.Poller copies. A GamepadReader is not safe for
// concurrent use.
type GamepadReader struct {
	resolved bool
	nav      js.Value
	slots    []gamepadSlot
	out      []GamepadState
}

type gamepadSlot struct {
	present   bool
	seen      bool
	timestamp float64
	state     GamepadState
}

// NewGamepadReader creates a GamepadReader.
func NewGamepadReader() *GamepadReader { return &GamepadReader{} }

// Gamepads implements GamepadSource with the reader's reused buffers.
func (r *GamepadReader) Gamepads() []GamepadState {
	r.out = r.out[:0]
	if !r.resolved {
		r.resolved = true
		nav := js.Global().Get("navigator")
		if nav.Truthy() && nav.Get("getGamepads").Type() == js.TypeFunction {
			r.nav = nav
		}
	}
	if !r.nav.Truthy() {
		return r.out
	}
	pads := r.nav.Call("getGamepads")
	if !pads.Truthy() {
		return r.out
	}
	for i := range r.slots {
		r.slots[i].seen = false
	}
	count := pads.Length()
	for i := 0; i < count; i++ {
		pad := pads.Index(i)
		if !pad.Truthy() {
			continue
		}
		if connected := pad.Get("connected"); connected.Type() == js.TypeBoolean && !connected.Bool() {
			continue
		}
		index := pad.Get("index").Int()
		if index < 0 {
			continue
		}
		for index >= len(r.slots) {
			r.slots = append(r.slots, gamepadSlot{})
		}
		slot := &r.slots[index]
		slot.seen = true
		timestamp := 0.0
		if ts := pad.Get("timestamp"); ts.Type() == js.TypeNumber {
			timestamp = ts.Float()
		}
		if !slot.present {
			slot.present = true
			slot.state.Index = index
			slot.state.ID = pad.Get("id").String()
			slot.state.Mapping = pad.Get("mapping").String()
		} else if timestamp != 0 && timestamp == slot.timestamp {
			r.out = append(r.out, slot.state)
			continue
		}
		slot.timestamp = timestamp
		buttons := pad.Get("buttons")
		slot.state.Buttons = resizeFloats(slot.state.Buttons, buttons.Length())
		for b := range slot.state.Buttons {
			button := buttons.Index(b)
			if button.Type() == js.TypeNumber {
				slot.state.Buttons[b] = button.Float()
			} else {
				slot.state.Buttons[b] = button.Get("value").Float()
			}
		}
		axes := pad.Get("axes")
		slot.state.Axes = resizeFloats(slot.state.Axes, axes.Length())
		for a := range slot.state.Axes {
			slot.state.Axes[a] = axes.Index(a).Float()
		}
		r.out = append(r.out, slot.state)
	}
	for i := range r.slots {
		if r.slots[i].present && !r.slots[i].seen {
			// Gone: the next pad in this slot reads its id and mapping again.
			r.slots[i].present = false
			r.slots[i].timestamp = 0
		}
	}
	return r.out
}

// resizeFloats returns buf with length n, reusing its storage.
func resizeFloats(buf []float64, n int) []float64 {
	if cap(buf) < n {
		return make([]float64, n)
	}
	return buf[:n]
}
