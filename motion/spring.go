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
	if s.Damping <= 0 {
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

// Duration returns the analytic settle-time upper bound: the earliest time at
// which the spring's response is guaranteed to be within settleTol of `to`,
// capped at 10 s.
//
// This is O(1) — no integration loop.
//
// Physics parameters (after applying defaults):
//
//	omega0 = sqrt(k/m)          (natural frequency)
//	zeta   = c / (2*sqrt(k*m))  (damping ratio)
//
// The characteristic decay rate is:
//
//   - Underdamped (zeta < 1):            rate = zeta * omega0
//   - Critically/overdamped (zeta >= 1): rate = omega0 * (zeta - sqrt(zeta²-1))
//     (the slower of the two real poles; at zeta=1 exactly, sqrt(…)=0 and rate=omega0)
//
// For UNDERDAMPED springs, the envelope is purely exponential exp(-rate*t), so:
//
//	t_s = safety * (-ln(tol) / rate)
//
// For CRITICALLY/OVERDAMPED springs, the solution has a polynomial prefactor
// (e.g. (A + B*t)*exp(-rate*t) at zeta=1), which slows the effective decay.
// One Newton refinement corrects for this:
//
//	t0 = -ln(tol) / rate
//	t_s = safety * (-ln(tol) + ln(1 + rate*t0)) / rate
//
// A safety margin of 1.4 is applied in both regimes so that Value never
// early-outs before the trajectory has genuinely settled. The amplitude
// |to-from| is not folded in; the 1e-3 threshold is absolute, and 1.4×
// headroom covers typical unit-scale animations.
//
// Guard: if rate ≤ 0 (unreachable with Damping > 0 after defaults), the
// 10 s cap is returned.
func (s Spring) Duration(from, to float64) float64 {
	const maxTime = 10.0
	const settleTol = 1e-3
	const safety = 1.4

	p := s.defaults()
	m := p.Mass
	k := p.Stiffness
	c := p.Damping

	omega0 := math.Sqrt(k / m)
	zeta := c / (2 * math.Sqrt(k*m))

	var rate float64
	if zeta >= 1 {
		// Critically damped or overdamped: slower pole dominates.
		// At zeta == 1, sqrt(zeta²-1) == 0 so rate = omega0 (correct limit).
		rate = omega0 * (zeta - math.Sqrt(zeta*zeta-1))
	} else {
		// Underdamped: envelope decays as exp(-zeta*omega0*t).
		rate = zeta * omega0
	}

	if rate <= 0 {
		return maxTime
	}

	var ts float64
	if zeta >= 1 {
		// Polynomial prefactor correction (one Newton step from the pure-exponential estimate).
		t0 := -math.Log(settleTol) / rate
		ts = safety * ((-math.Log(settleTol) + math.Log(1+rate*t0)) / rate)
	} else {
		ts = safety * (-math.Log(settleTol) / rate)
	}

	if ts > maxTime {
		return maxTime
	}
	return ts
}
