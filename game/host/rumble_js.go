//go:build js && wasm

package host

import (
	"sync"
	"syscall/js"
)

var (
	rumbleOnce   sync.Once
	rumbleIgnore js.Func
)

// ignoreRejection keeps a refused effect from logging an unhandled promise
// rejection: a pad that is unplugged while it shakes rejects its promise.
func ignoreRejection(result js.Value) {
	if result.Type() != js.TypeObject || result.Get("catch").Type() != js.TypeFunction {
		return
	}
	rumbleOnce.Do(func() {
		rumbleIgnore = js.FuncOf(func(js.Value, []js.Value) any { return nil })
	})
	result.Call("catch", rumbleIgnore)
}

// gamepadAt returns the connected pad in the browser's slot index.
func gamepadAt(index int) (pad js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	nav := js.Global().Get("navigator")
	if index < 0 || !nav.Truthy() || nav.Get("getGamepads").Type() != js.TypeFunction {
		return js.Value{}, false
	}
	pads := nav.Call("getGamepads")
	if !pads.Truthy() || index >= pads.Length() {
		return js.Value{}, false
	}
	pad = pads.Index(index)
	if !pad.Truthy() {
		return js.Value{}, false
	}
	if connected := pad.Get("connected"); connected.Type() == js.TypeBoolean && !connected.Bool() {
		return js.Value{}, false
	}
	return pad, true
}

// RumbleGamepad shakes the gamepad in the browser's slot index and reports
// whether the pad has motors to shake. It returns at once and is safe to call
// from a frame callback. A new effect replaces the one that is playing.
//
// Browsers play an effect only after the player has pressed a button on the
// pad or the page. A browser with no vibration support, an empty slot and a
// pad with no motors all report false.
func RumbleGamepad(index int, effect Rumble) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	pad, found := gamepadAt(index)
	if !found {
		return false
	}
	effect = effect.clamped()
	if actuator := pad.Get("vibrationActuator"); actuator.Truthy() && actuator.Get("playEffect").Type() == js.TypeFunction {
		ignoreRejection(actuator.Call("playEffect", "dual-rumble", map[string]any{
			"startDelay":      float64(effect.Delay.Milliseconds()),
			"duration":        float64(effect.Duration.Milliseconds()),
			"strongMagnitude": effect.Strong,
			"weakMagnitude":   effect.Weak,
		}))
		return true
	}
	// Firefox has one motor per actuator and no start delay.
	if actuators := pad.Get("hapticActuators"); actuators.Truthy() && actuators.Length() > 0 {
		if first := actuators.Index(0); first.Truthy() && first.Get("pulse").Type() == js.TypeFunction {
			level := effect.Strong
			if effect.Weak > level {
				level = effect.Weak
			}
			ignoreRejection(first.Call("pulse", level, float64(effect.Duration.Milliseconds())))
			return true
		}
	}
	return false
}

// StopRumble stops the gamepad's motors and reports whether the pad has any.
func StopRumble(index int) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	pad, found := gamepadAt(index)
	if !found {
		return false
	}
	if actuator := pad.Get("vibrationActuator"); actuator.Truthy() && actuator.Get("reset").Type() == js.TypeFunction {
		ignoreRejection(actuator.Call("reset"))
		return true
	}
	return RumbleGamepad(index, Rumble{})
}
