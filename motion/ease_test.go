package motion

import (
	"math"
	"testing"
)

// approx returns true if got and want differ by at most eps.
func approx(got, want, eps float64) bool {
	return math.Abs(got-want) <= eps
}

func TestEaseLinear(t *testing.T) {
	e := Ease{Kind: EaseLinear}
	got := e.Apply(0.5)
	if got != 0.5 {
		t.Errorf("EaseLinear.Apply(0.5) = %v, want 0.5", got)
	}
	if e.Apply(0) != 0 {
		t.Errorf("EaseLinear.Apply(0) = %v, want 0", e.Apply(0))
	}
	if e.Apply(1) != 1 {
		t.Errorf("EaseLinear.Apply(1) = %v, want 1", e.Apply(1))
	}
}

func TestEaseInOutPow(t *testing.T) {
	e := Ease{Kind: EaseInOutPow, Args: []float64{3}}
	const eps = 1e-9
	if !approx(e.Apply(0), 0, eps) {
		t.Errorf("EaseInOutPow(3).Apply(0) = %v, want ≈0", e.Apply(0))
	}
	if !approx(e.Apply(1), 1, eps) {
		t.Errorf("EaseInOutPow(3).Apply(1) = %v, want ≈1", e.Apply(1))
	}
	if !approx(e.Apply(0.5), 0.5, eps) {
		t.Errorf("EaseInOutPow(3).Apply(0.5) = %v, want ≈0.5", e.Apply(0.5))
	}
	// eased-in at start: should be slower (less than linear)
	v25 := e.Apply(0.25)
	if v25 >= 0.25 {
		t.Errorf("EaseInOutPow(3).Apply(0.25) = %v, want < 0.25 (slow start)", v25)
	}
}

func TestEaseCubicBezier(t *testing.T) {
	e := Ease{Kind: EaseCubicBezier, Args: []float64{0.16, 1, 0.3, 1}}
	const eps = 1e-3
	if !approx(e.Apply(0), 0, eps) {
		t.Errorf("CubicBezier.Apply(0) = %v, want ≈0", e.Apply(0))
	}
	if !approx(e.Apply(1), 1, eps) {
		t.Errorf("CubicBezier.Apply(1) = %v, want ≈1", e.Apply(1))
	}
	// monotonic non-decreasing
	pts := []float64{0, 0.25, 0.5, 0.75, 1}
	prev := e.Apply(pts[0])
	for _, pt := range pts[1:] {
		cur := e.Apply(pt)
		if cur < prev-eps {
			t.Errorf("CubicBezier not monotone: Apply(%v)=%v < Apply(prev)=%v", pt, cur, prev)
		}
		prev = cur
	}
}

func TestEaseSteps(t *testing.T) {
	e := Ease{Kind: EaseSteps, Args: []float64{4}}
	// t=0.1 and t=0.2 are both in step 0 → 0.0
	v01 := e.Apply(0.1)
	v02 := e.Apply(0.2)
	if v01 != 0.0 {
		t.Errorf("EaseSteps(4).Apply(0.1) = %v, want 0.0", v01)
	}
	if v02 != 0.0 {
		t.Errorf("EaseSteps(4).Apply(0.2) = %v, want 0.0", v02)
	}
	// t=0.3 is in step 1 → 0.25
	v03 := e.Apply(0.3)
	if v03 != 0.25 {
		t.Errorf("EaseSteps(4).Apply(0.3) = %v, want 0.25", v03)
	}
	// t=1 must return exactly 1
	v1 := e.Apply(1)
	if v1 != 1.0 {
		t.Errorf("EaseSteps(4).Apply(1) = %v, want 1.0", v1)
	}
	// check 4 distinct levels exist across [0,1)
	seen := map[float64]bool{}
	for i := 0; i < 100; i++ {
		tt := float64(i) / 100.0
		seen[e.Apply(tt)] = true
	}
	if len(seen) != 4 {
		t.Errorf("EaseSteps(4) should produce 4 distinct levels in [0,1), got %d: %v", len(seen), seen)
	}
}

func TestEaseOutBack(t *testing.T) {
	e := Ease{Kind: EaseOutBack}
	const eps = 1e-6
	if !approx(e.Apply(0), 0, eps) {
		t.Errorf("EaseOutBack.Apply(0) = %v, want ≈0", e.Apply(0))
	}
	if !approx(e.Apply(1), 1, eps) {
		t.Errorf("EaseOutBack.Apply(1) = %v, want ≈1", e.Apply(1))
	}
	// overshoot: at some t (e.g. 0.8), output > 1
	v08 := e.Apply(0.8)
	if v08 <= 1.0 {
		t.Errorf("EaseOutBack.Apply(0.8) = %v, want > 1.0 (overshoot)", v08)
	}
}
