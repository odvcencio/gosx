package host

import "time"

// Rumble is one vibration of a gamepad's two motors.
type Rumble struct {
	// Duration is how long the motors run. Values over MaxRumble are cut to it.
	Duration time.Duration
	// Strong is the level of the heavy, low-frequency motor, from 0 to 1.
	Strong float64
	// Weak is the level of the light, high-frequency motor, from 0 to 1.
	Weak float64
	// Delay is how long the pad waits before it starts.
	Delay time.Duration
}

// MaxRumble is the longest single effect a browser plays.
const MaxRumble = 5 * time.Second

// clamped returns the effect with every field in the range a browser accepts.
func (r Rumble) clamped() Rumble {
	level := func(v float64) float64 {
		if !(v > 0) { // also catches NaN
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	span := func(d time.Duration) time.Duration {
		if d < 0 {
			return 0
		}
		if d > MaxRumble {
			return MaxRumble
		}
		return d
	}
	return Rumble{Duration: span(r.Duration), Strong: level(r.Strong), Weak: level(r.Weak), Delay: span(r.Delay)}
}
