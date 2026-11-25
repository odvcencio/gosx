//go:build js && wasm

package host

import (
	"syscall/js"
	"testing"
	"time"
)

// fakePads installs navigator.getGamepads returning the given slots.
func fakePads(t *testing.T, slots ...any) {
	t.Helper()
	read := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf(slots) })
	t.Cleanup(read.Release)
	replaceGlobal(t, "navigator", map[string]any{"getGamepads": read})
}

func TestRumbleGamepadPlaysADualRumbleEffect(t *testing.T) {
	var kind string
	var params js.Value
	calls := 0
	play := js.FuncOf(func(_ js.Value, args []js.Value) any {
		calls++
		kind, params = args[0].String(), args[1]
		return js.Global().Get("Promise").Call("reject", "unplugged")
	})
	defer play.Release()
	fakePads(t, nil, map[string]any{"connected": true, "index": 1, "vibrationActuator": map[string]any{"playEffect": play}})

	if !RumbleGamepad(1, Rumble{Duration: 180 * time.Millisecond, Strong: 0.9, Weak: 1.4, Delay: 20 * time.Millisecond}) {
		t.Fatal("a pad with a vibration actuator must report true")
	}
	if calls != 1 || kind != "dual-rumble" {
		t.Fatalf("playEffect ran %d times with %q, want once with dual-rumble", calls, kind)
	}
	got := [4]float64{params.Get("duration").Float(), params.Get("strongMagnitude").Float(), params.Get("weakMagnitude").Float(), params.Get("startDelay").Float()}
	if want := [4]float64{180, 0.9, 1, 20}; got != want {
		t.Fatalf("effect = %v (duration, strong, weak, delay), want %v", got, want)
	}
	// An empty slot, a slot past the end and a negative slot have nothing to shake.
	for _, index := range []int{0, 2, -1} {
		if RumbleGamepad(index, Rumble{Duration: time.Second, Strong: 1}) {
			t.Fatalf("slot %d has no pad and must report false", index)
		}
	}
	if calls != 1 {
		t.Fatalf("a missing pad must not reach another pad's motors; playEffect ran %d times", calls)
	}
}

func TestRumbleGamepadFallsBackToAHapticPulse(t *testing.T) {
	var level, duration float64
	pulses := 0
	pulse := js.FuncOf(func(_ js.Value, args []js.Value) any {
		pulses++
		level, duration = args[0].Float(), args[1].Float()
		return nil
	})
	defer pulse.Release()
	fakePads(t, map[string]any{"connected": true, "index": 0, "hapticActuators": []any{map[string]any{"pulse": pulse}}})
	if !RumbleGamepad(0, Rumble{Duration: 90 * time.Millisecond, Strong: 0.3, Weak: 0.7}) {
		t.Fatal("a pad with a haptic actuator must report true")
	}
	if pulses != 1 || level != 0.7 || duration != 90 {
		t.Fatalf("pulse ran %d times with level %v for %v ms, want once at 0.7 for 90", pulses, level, duration)
	}
}

func TestRumbleGamepadReportsFalseWithNoMotorsOrNoAPI(t *testing.T) {
	fakePads(t, map[string]any{"connected": true, "index": 0})
	if RumbleGamepad(0, Rumble{Duration: time.Second, Strong: 1}) {
		t.Fatal("a pad with no actuator must report false")
	}
	fakePads(t, map[string]any{"connected": false, "index": 0, "vibrationActuator": map[string]any{}})
	if RumbleGamepad(0, Rumble{Duration: time.Second, Strong: 1}) {
		t.Fatal("a disconnected pad must report false")
	}
	replaceGlobal(t, "navigator", map[string]any{})
	if RumbleGamepad(0, Rumble{Duration: time.Second, Strong: 1}) || StopRumble(0) {
		t.Fatal("a browser with no gamepad API must report false")
	}
}

func TestStopRumbleResetsTheActuator(t *testing.T) {
	resets := 0
	reset := js.FuncOf(func(js.Value, []js.Value) any { resets++; return nil })
	defer reset.Release()
	fakePads(t, map[string]any{"connected": true, "index": 0, "vibrationActuator": map[string]any{"reset": reset}})
	if !StopRumble(0) || resets != 1 {
		t.Fatalf("StopRumble must reset the actuator once; reset ran %d times", resets)
	}
}
