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
	a := Vec3V(0, 0, 0)
	b := Vec3V(2, 4, 6)
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
	a := Value{ArityQuat, [4]float64{0, 0, 0, 1}}
	b := Value{ArityQuat, [4]float64{0, 0, 0.7071068, 0.7071068}}
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
	a := Vec3V(0, 0, 0)
	b := Vec3V(2, 4, 6)
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
	a := Vec3V(7, 8, 9)
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
	a := Vec3V(1, 2, 3)
	dst := make([]float64, 3)
	allocs := testing.AllocsPerRun(100, func() {
		StepInto(dst, a)
	})
	if allocs != 0 {
		t.Errorf("StepInto allocated %v times, want 0", allocs)
	}
}

// hermiteOracle is a plain reimplementation of the glTF CUBICSPLINE basis, used
// to cross-check CubicHermiteInto and the evaluator.
func hermiteOracle(vK, bK, vK1, aK1, delta, s float64) float64 {
	s2 := s * s
	s3 := s2 * s
	return (2*s3-3*s2+1)*vK + delta*(s3-2*s2+s)*bK + (-2*s3+3*s2)*vK1 + delta*(s3-s2)*aK1
}

// TestCubicHermiteEndpoints: at s=0 the spline returns v_k exactly; at s=1 it
// returns v_{k+1} exactly, independent of the tangents.
func TestCubicHermiteEndpoints(t *testing.T) {
	vK := Vec3V(1, 2, 3)
	vK1 := Vec3V(7, 8, 9)
	bK := Vec3V(5, -5, 100)  // arbitrary out-tangent
	aK1 := Vec3V(-3, 2, -50) // arbitrary in-tangent
	delta := 2.0
	dst := make([]float64, 3)

	CubicHermiteInto(dst, vK, bK, vK1, aK1, delta, 0)
	for i := 0; i < 3; i++ {
		if math.Abs(dst[i]-vK.F[i]) > 1e-12 {
			t.Errorf("s=0 F[%d] = %v, want %v (v_k)", i, dst[i], vK.F[i])
		}
	}

	CubicHermiteInto(dst, vK, bK, vK1, aK1, delta, 1)
	for i := 0; i < 3; i++ {
		if math.Abs(dst[i]-vK1.F[i]) > 1e-12 {
			t.Errorf("s=1 F[%d] = %v, want %v (v_{k+1})", i, dst[i], vK1.F[i])
		}
	}
}

// TestCubicHermiteKnownCase verifies a mid-segment value against the hand-rolled
// oracle, and confirms it differs from a plain linear lerp for non-zero tangents.
func TestCubicHermiteKnownCase(t *testing.T) {
	vK := ScalarV(0)
	vK1 := ScalarV(10)
	bK := ScalarV(4)  // out-tangent at left key
	aK1 := ScalarV(2) // in-tangent at right key
	delta := 2.0
	s := 0.5
	dst := make([]float64, 1)

	CubicHermiteInto(dst, vK, bK, vK1, aK1, delta, s)
	want := hermiteOracle(0, 4, 10, 2, delta, s)
	if math.Abs(dst[0]-want) > 1e-12 {
		t.Errorf("CubicHermiteInto = %v, want oracle %v", dst[0], want)
	}

	// Must differ from a linear lerp (which would be 5.0 at s=0.5).
	lin := 0 + s*(10-0)
	if math.Abs(dst[0]-lin) < 1e-9 {
		t.Errorf("expected cubicspline to differ from linear (%v), got %v", lin, dst[0])
	}
}

// TestCubicHermiteQuatNormalized verifies the ArityQuat path normalizes the
// component-wise interpolated result to unit length.
func TestCubicHermiteQuatNormalized(t *testing.T) {
	vK := Value{ArityQuat, [4]float64{0, 0, 0, 1}}
	vK1 := Value{ArityQuat, [4]float64{0, 0, 0.7071068, 0.7071068}}
	bK := Value{ArityQuat, [4]float64{0, 0, 1, 0}}   // arbitrary tangent
	aK1 := Value{ArityQuat, [4]float64{0, 0, -1, 0}} // arbitrary tangent
	delta := 1.0
	dst := make([]float64, 4)

	CubicHermiteInto(dst, vK, bK, vK1, aK1, delta, 0.5)
	mag := math.Sqrt(dst[0]*dst[0] + dst[1]*dst[1] + dst[2]*dst[2] + dst[3]*dst[3])
	if math.Abs(mag-1.0) > 1e-9 {
		t.Errorf("quat result not unit length: |q| = %v", mag)
	}
}

// TestCubicHermiteIntoNoAlloc verifies CubicHermiteInto allocates nothing.
func TestCubicHermiteIntoNoAlloc(t *testing.T) {
	vK := Vec3V(0, 0, 0)
	vK1 := Vec3V(10, 10, 10)
	bK := Vec3V(1, 1, 1)
	aK1 := Vec3V(2, 2, 2)
	dst := make([]float64, 3)
	allocs := testing.AllocsPerRun(100, func() {
		CubicHermiteInto(dst, vK, bK, vK1, aK1, 1.0, 0.5)
	})
	if allocs != 0 {
		t.Errorf("CubicHermiteInto allocated %v times, want 0", allocs)
	}
}
