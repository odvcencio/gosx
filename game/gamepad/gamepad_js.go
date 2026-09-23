//go:build js && wasm

package gamepad

import "syscall/js"

// NavigatorSource reads gamepad state from navigator.getGamepads(). The zero
// value is ready to use.
type NavigatorSource struct{}

// Gamepads implements Source.
func (NavigatorSource) Gamepads() []State {
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
	out := make([]State, 0, count)
	for i := 0; i < count; i++ {
		pad := pads.Index(i)
		if !pad.Truthy() {
			continue
		}
		state := State{
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
