package motion

import "math"

// SpringDT is the fixed internal timestep used by the spring integrator (seconds).
// Both native Go and WASM use this constant to guarantee deterministic output.
const SpringDT = 1.0 / 240.0

// Spring drives a scalar from `from` to `to` under physics.
// Zero values are replaced by sane defaults before integration.
//
// TinyGo-clean: no reflect, no encoding/json; only math. No heap allocation on
// the hot path.
type Spring struct {
	Mass      float64 // default 1
	Stiffness float64 // k, default 100
	Damping   float64 // c, default 10
	Velocity  float64 // initial velocity v0, default 0
}

// defaults returns a copy of s with zero/invalid fields replaced by sane values.
func (s Spring) defaults() Spring {
	if s.Mass <= 0 {
		s.Mass = 1
	}
	if s.Stiffness <= 0 {
		s.Stiffness = 100
	}
	if s.Damping < 0 {
		s.Damping = 10
	}
	return s
}

// Value returns the spring position at time t (seconds), animating from → to.
//
// Pure function of t: re-derives state from t0 via fixed-dt semi-implicit Euler.
// No mutable state is used or modified across calls.
//
//   - t <= 0  → returns from exactly.
//   - t >= Duration(from,to)  → returns to exactly (early-out).
//   - otherwise: runs int(t/SpringDT) fixed-dt steps from t0 and returns x.
func (s Spring) Value(from, to, t float64) float64 {
	if t <= 0 {
		return from
	}

	// Early-out: if the spring has settled, return to exactly.
	if t >= s.Duration(from, to) {
		return to
	}

	p := s.defaults()
	steps := int(t / SpringDT) // floor; same convention as Duration

	x := from
	v := p.Velocity
	for i := 0; i < steps; i++ {
		F := -p.Stiffness*(x-to) - p.Damping*v
		v += (F / p.Mass) * SpringDT
		x += v * SpringDT
	}
	return x
}

// Duration returns the settle time: the earliest t at which |x−to| < 1e-3 and
// |v| < 1e-3, capped at 10 s. The result is always a multiple of SpringDT.
func (s Spring) Duration(from, to float64) float64 {
	const maxTime = 10.0
	const maxSteps = int(maxTime / SpringDT) // 2400

	p := s.defaults()
	x := from
	v := p.Velocity

	for i := 0; i < maxSteps; i++ {
		F := -p.Stiffness*(x-to) - p.Damping*v
		v += (F / p.Mass) * SpringDT
		x += v * SpringDT

		if math.Abs(x-to) < 1e-3 && math.Abs(v) < 1e-3 {
			return float64(i+1) * SpringDT
		}
	}
	return maxTime
}
