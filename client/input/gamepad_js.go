//go:build js && wasm

package input

import "syscall/js"

// PollGamepads reads navigator.getGamepads once and returns an owned snapshot.
// The returned value contains no js.Value references and performs no heap
// allocation for its bounded arrays.
func PollGamepads() Gamepads {
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() || navigator.Get("getGamepads").Type() != js.TypeFunction {
		return Gamepads{}
	}
	values := navigator.Call("getGamepads")
	if !values.Truthy() {
		return Gamepads{}
	}
	padLength := values.Get("length")
	if padLength.Type() != js.TypeNumber {
		return Gamepads{}
	}

	var result Gamepads
	length := boundedCount(padLength.Int(), MaxGamepads)
	for index := 0; index < length && result.Count < len(result.Pads); index++ {
		value := values.Index(index)
		if !value.Truthy() || value.Get("connected").Type() != js.TypeBoolean || !value.Get("connected").Bool() {
			continue
		}
		pad := GamepadState{Connected: true, Index: index}
		if id := value.Get("id"); id.Type() == js.TypeString {
			pad.ID = id.String()
		}
		if mapping := value.Get("mapping"); mapping.Type() == js.TypeString {
			pad.Mapping = mapping.String()
		}
		buttons := value.Get("buttons")
		if buttons.Truthy() {
			buttonLength := buttons.Get("length")
			if buttonLength.Type() == js.TypeNumber {
				pad.ButtonCount = boundedCount(buttonLength.Int(), len(pad.Buttons))
				for i := 0; i < pad.ButtonCount; i++ {
					button := buttons.Index(i)
					if !button.Truthy() {
						continue
					}
					pressed := button.Get("pressed")
					if pressed.Type() == js.TypeBoolean {
						pad.Buttons[i].Pressed = pressed.Bool()
					}
					if amount := button.Get("value"); amount.Type() == js.TypeNumber {
						pad.Buttons[i].Value = amount.Float()
					}
				}
			}
		}
		axes := value.Get("axes")
		if axes.Truthy() {
			axisLength := axes.Get("length")
			if axisLength.Type() == js.TypeNumber {
				pad.AxisCount = boundedCount(axisLength.Int(), len(pad.Axes))
				for i := 0; i < pad.AxisCount; i++ {
					axis := axes.Index(i)
					if axis.Type() == js.TypeNumber {
						pad.Axes[i] = axis.Float()
					}
				}
			}
		}
		result.Pads[result.Count] = pad
		result.Count++
	}
	return result
}
