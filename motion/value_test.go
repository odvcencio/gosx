package motion

import (
	"math"
	"testing"
)

// TestWidthArity verifies Width() for all arities.
func TestWidthArity(t *testing.T) {
	cases := []struct {
		a    ValueArity
		want int
	}{
		{ArityScalar, 1},
		{ArityVec2, 2},
		{ArityVec3, 3},
		{ArityVec4, 4},
		{ArityQuat, 4},
		{ArityColor, 4},
	}
	for _, c := range cases {
		if got := c.a.Width(); got != c.want {
			t.Errorf("arity %d Width() = %d, want %d", c.a, got, c.want)
		}
	}
}

// TestLerpValueVec3 verifies component-wise lerp for a Vec3.
func TestLerpValueVec3(t *testing.T) {
	a := Value{ArityVec3, []float64{0, 0, 0}}
	b := Value{ArityVec3, []float64{2, 4, 6}}
	got := LerpValue(a, b, 0.5)
	want := []float64{1, 2, 3}
	for i, w := range want {
		if got.F[i] != w {
			t.Errorf("F[%d] = %v, want %v", i, got.F[i], w)
		}
	}
	if got.Arity != ArityVec3 {
		t.Errorf("Arity = %v, want ArityVec3", got.Arity)
	}
}

// TestLerpValueQuat verifies that Quat arity routes through Slerp, not component lerp.
func TestLerpValueQuat(t *testing.T) {
	// a = identity, b = 90-degree rotation about Z (approx)
	a := Value{ArityQuat, []float64{0, 0, 0, 1}}
	b := Value{ArityQuat, []float64{0, 0, 0.7071068, 0.7071068}}
	got := LerpValue(a, b, 0.5)

	// Expected: Slerp of the same quaternions at t=0.5 ≈ [0, 0, 0.3826834, 0.9238795]
	qa := Quat{X: a.F[0], Y: a.F[1], Z: a.F[2], W: a.F[3]}
	qb := Quat{X: b.F[0], Y: b.F[1], Z: b.F[2], W: b.F[3]}
	expected := Slerp(qa, qb, 0.5)
	want := []float64{expected.X, expected.Y, expected.Z, expected.W}

	const eps = 1e-6
	for i, w := range want {
		if math.Abs(got.F[i]-w) > eps {
			t.Errorf("F[%d] = %v, want %v (eps %g)", i, got.F[i], w, eps)
		}
	}
	if got.Arity != ArityQuat {
		t.Errorf("Arity = %v, want ArityQuat", got.Arity)
	}
}

// TestLerpValueIntoNoAlloc verifies that LerpValueInto produces correct values and zero allocs.
func TestLerpValueIntoNoAlloc(t *testing.T) {
	a := Value{ArityVec3, []float64{0, 0, 0}}
	b := Value{ArityVec3, []float64{2, 4, 6}}
	dst := make([]float64, 3)

	allocs := testing.AllocsPerRun(100, func() {
		LerpValueInto(dst, a, b, 0.5)
	})
	if allocs != 0 {
		t.Errorf("LerpValueInto allocated %v times, want 0", allocs)
	}

	want := []float64{1, 2, 3}
	for i, w := range want {
		if dst[i] != w {
			t.Errorf("dst[%d] = %v, want %v", i, dst[i], w)
		}
	}
}

// TestStepInto verifies StepInto copies a's components into dst.
func TestStepInto(t *testing.T) {
	a := Value{ArityVec3, []float64{7, 8, 9}}
	dst := make([]float64, 3)
	StepInto(dst, a)
	want := []float64{7, 8, 9}
	for i, w := range want {
		if dst[i] != w {
			t.Errorf("dst[%d] = %v, want %v", i, dst[i], w)
		}
	}
}

// TestStepIntoNoAlloc verifies StepInto allocates nothing.
func TestStepIntoNoAlloc(t *testing.T) {
	a := Value{ArityVec3, []float64{1, 2, 3}}
	dst := make([]float64, 3)
	allocs := testing.AllocsPerRun(100, func() {
		StepInto(dst, a)
	})
	if allocs != 0 {
		t.Errorf("StepInto allocated %v times, want 0", allocs)
	}
}
