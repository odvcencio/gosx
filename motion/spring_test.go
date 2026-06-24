package motion

import (
	"math"
	"testing"
	"time"
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

// durationIntegrated is the original integration-loop oracle, kept here as a
// test-only reference to compare against the analytic implementation.
func durationIntegrated(s Spring, from, to float64) float64 {
	const maxTime = 10.0
	const maxSteps = int(maxTime / SpringDT)

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

// TestSpringAnalyticDurationIsValidSettleTime verifies that for several spring
// regimes, Value at Duration is within 2e-3 of `to` (the analytic Duration is
// a genuine upper bound — the spring has actually settled by then).
func TestSpringAnalyticDurationIsValidSettleTime(t *testing.T) {
	cases := []struct {
		name string
		s    Spring
	}{
		{"underdamped {1,100,10}", Spring{Mass: 1, Stiffness: 100, Damping: 10}},
		{"near-critical {1,100,20}", Spring{Mass: 1, Stiffness: 100, Damping: 20}},
		{"overdamped {1,100,60}", Spring{Mass: 1, Stiffness: 100, Damping: 60}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dur := tc.s.Duration(0, 1)
			if dur <= 0 {
				t.Fatalf("Duration must be positive, got %v", dur)
			}
			// Value at Duration must be within 2e-3 of target (spring settled).
			got := tc.s.Value(0, 1, dur)
			if math.Abs(got-1.0) > 2e-3 {
				t.Errorf("Value(0,1,Duration)=%v; want within 2e-3 of 1.0 (Duration=%v)", got, dur)
			}
		})
	}
}

// TestSpringAnalyticDurationVsIntegrated asserts the analytic Duration is a
// safe upper bound relative to the integrated oracle:
//
//	analytic >= integrated*0.9  (not wildly shorter than actual settle)
//	analytic <= integrated*3 + 1  (not absurdly loose)
func TestSpringAnalyticDurationVsIntegrated(t *testing.T) {
	cases := []struct {
		name string
		s    Spring
	}{
		{"underdamped {1,100,10}", Spring{Mass: 1, Stiffness: 100, Damping: 10}},
		{"near-critical {1,100,20}", Spring{Mass: 1, Stiffness: 100, Damping: 20}},
		{"overdamped {1,100,60}", Spring{Mass: 1, Stiffness: 100, Damping: 60}},
		{"very-underdamped {1,400,2}", Spring{Mass: 1, Stiffness: 400, Damping: 2}},
		{"zero-value defaults", Spring{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analytic := tc.s.Duration(0, 1)
			integrated := durationIntegrated(tc.s, 0, 1)
			lo := integrated * 0.9
			hi := integrated*3 + 1
			if analytic < lo {
				t.Errorf("analytic Duration %v < integrated*0.9 %v (integrated=%v) — analytic may be too short", analytic, lo, integrated)
			}
			if analytic > hi {
				t.Errorf("analytic Duration %v > integrated*3+1 %v (integrated=%v) — analytic is too loose", analytic, hi, integrated)
			}
		})
	}
}

// TestSpringDurationIsO1 verifies that Duration executes in O(1) time by
// confirming it finishes in under 100µs per call (integration up to 2400 steps
// at this complexity would take multiple milliseconds).
func TestSpringDurationIsO1(t *testing.T) {
	s := Spring{Mass: 1, Stiffness: 100, Damping: 10}

	const iters = 1000
	start := time.Now()
	for i := 0; i < iters; i++ {
		_ = s.Duration(0, 1)
	}
	elapsed := time.Since(start)

	// 1000 iterations of the old integration loop would take ~1-5ms.
	// 1000 analytic calls should easily complete in under 2ms (2µs each).
	if elapsed > 2*time.Millisecond {
		t.Errorf("Duration took %v for %d calls (~%v/call); expected O(1) < 2µs/call", elapsed, iters, elapsed/iters)
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
