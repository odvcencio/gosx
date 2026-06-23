package motion

import (
	"math"
	"testing"
)

// TestSpringSettle verifies the spring reaches the target within tolerance at
// Duration, and starts at `from` at t=0.
func TestSpringSettle(t *testing.T) {
	s := Spring{Mass: 1, Stiffness: 100, Damping: 10}
	dur := s.Duration(0, 1)
	if dur <= 0 {
		t.Fatalf("Duration must be positive, got %v", dur)
	}

	atStart := s.Value(0, 1, 0)
	if atStart != 0 {
		t.Errorf("Value(0,1,0) = %v; want 0 (exactly)", atStart)
	}

	atEnd := s.Value(0, 1, dur)
	if math.Abs(atEnd-1.0) > 0.02 {
		t.Errorf("Value(0,1,Duration) = %v; want within 0.02 of 1.0", atEnd)
	}
}

// TestSpringDeterminism verifies that Value re-derives state from t0 via the
// same fixed-dt semi-implicit Euler integrator — bit-identical to a manual loop.
//
// Parameters chosen so the spring has NOT settled by t = 480*SpringDT = 2.0s:
//   - Spring{Mass:1, Stiffness:400, Damping:2} is very underdamped (ζ ≈ 0.05)
//     and will ring for many seconds; Duration(0,1) >> 2.0s.
func TestSpringDeterminism(t *testing.T) {
	s := Spring{Mass: 1, Stiffness: 400, Damping: 2}

	// Sanity: confirm Duration > 2.0s so the early-out won't fire at 480 steps.
	dur := s.Duration(0, 1)
	target := 480 * SpringDT // 2.0 s
	if dur <= target {
		t.Fatalf("Duration(%v) <= target time %v — early-out would fire; choose different params", dur, target)
	}

	// Manual integrator — same convention as Value: steps = int(t/SpringDT).
	const from, to = 0.0, 1.0
	const N = 480
	m := applySpringDefaults(s)
	x := from
	v := m.Velocity
	for i := 0; i < N; i++ {
		F := -m.Stiffness*(x-to) - m.Damping*v
		v += (F / m.Mass) * SpringDT
		x += v * SpringDT
	}
	manual := x

	got := s.Value(from, to, float64(N)*SpringDT)
	if got != manual {
		t.Errorf("Value bit-identity failed: got %.20g, want %.20g", got, manual)
	}
}

// TestSpringPurity verifies that two consecutive calls to Value with the same
// arguments return bit-identical results (pure function, no mutable state).
func TestSpringPurity(t *testing.T) {
	s := Spring{Mass: 1, Stiffness: 100, Damping: 10}
	a := s.Value(0, 1, 0.5)
	b := s.Value(0, 1, 0.5)
	if a != b {
		t.Errorf("Value is not pure: first call %v, second call %v", a, b)
	}
}

// TestSpringEarlyOut verifies that past the settle time Value returns `to` exactly.
func TestSpringEarlyOut(t *testing.T) {
	s := Spring{Mass: 1, Stiffness: 100, Damping: 10}
	dur := s.Duration(0, 1)
	got := s.Value(0, 1, dur+5)
	if got != 1.0 {
		t.Errorf("Value past Duration = %v; want 1.0 exactly", got)
	}
}

// applySpringDefaults mirrors the defaults applied inside Spring so the test
// manual loop uses the same effective parameters.
func applySpringDefaults(s Spring) Spring {
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

// TestSpringZeroValueDamped verifies that a zero-value Spring{} actually settles
// (i.e. Damping=0 is treated as "use default 10", not "undamped").
func TestSpringZeroValueDamped(t *testing.T) {
	var s Spring
	dur := s.Duration(0, 1)
	if dur >= 10 {
		t.Errorf("Duration(0,1) = %v; want < 10 (should settle, not hit cap)", dur)
	}
	got := s.Value(0, 1, dur)
	if math.Abs(got-1.0) > 0.02 {
		t.Errorf("Value(0,1,Duration) = %v; want within 0.02 of 1.0", got)
	}
}
