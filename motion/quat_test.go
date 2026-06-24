package motion

import (
	"math"
	"testing"
)

func quatClose(a, b Quat, eps float64) bool {
	return math.Abs(a.X-b.X) < eps && math.Abs(a.Y-b.Y) < eps &&
		math.Abs(a.Z-b.Z) < eps && math.Abs(a.W-b.W) < eps
}

func TestSlerpShortestPath(t *testing.T) {
	a := Quat{0, 0, 0, 1}
	b := Quat{0, 0, 0.7071068, 0.7071068} // 90° about Z
	got := Slerp(a, b, 0.5)
	want := Quat{0, 0, 0.3826834, 0.9238795} // 45° about Z
	if !quatClose(got, want, 1e-6) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestSlerpFlipsToShorterArc(t *testing.T) {
	a := Quat{0, 0, 0, 1}
	b := Quat{0, 0, 0, -1} // same orientation, opposite sign
	got := Slerp(a, b, 0.5)
	if got.W < 0.999 {
		t.Fatalf("expected near-identity, got %+v", got)
	}
}

func TestSlerpEndpoints(t *testing.T) {
	a := Quat{0, 0, 0, 1}
	b := Quat{0, 0, 0.7071068, 0.7071068}
	if g := Slerp(a, b, 0); !quatClose(g, a, 1e-9) {
		t.Fatalf("t=0 got %+v want %+v", g, a)
	}
	if g := Slerp(a, b, 1); !quatClose(g, b, 1e-9) {
		t.Fatalf("t=1 got %+v want %+v", g, b)
	}
}

func TestSlerpNlerpFastPath(t *testing.T) {
	// Two nearly-parallel unit quats: dot > 0.9995 forces the nlerp fast-path.
	a := Quat{0, 0, 0, 1}
	b := Quat{0.001, 0, 0, 0.9999995} // dot ≈ 0.9999995, well above the 0.9995 threshold
	got := Slerp(a, b, 0.5)
	// Result must be unit-length (fast path normalizes) and roughly the midpoint in X.
	mag := math.Sqrt(got.X*got.X + got.Y*got.Y + got.Z*got.Z + got.W*got.W)
	if math.Abs(mag-1) > 1e-9 {
		t.Fatalf("fast-path result not unit length: |q|=%v (%+v)", mag, got)
	}
	if got.X < 0.0004 || got.X > 0.0006 {
		t.Fatalf("fast-path X not ~midpoint: %+v", got)
	}
}

// TestQuatFromEulerIdentity: zero angles → identity quaternion {0,0,0,1}.
func TestQuatFromEulerIdentity(t *testing.T) {
	got := QuatFromEuler(0, 0, 0)
	want := Quat{0, 0, 0, 1}
	if !quatClose(got, want, 1e-9) {
		t.Fatalf("identity: got %+v want %+v", got, want)
	}
}

// TestRotateVec3IdentityNoOp: the identity quaternion leaves the vector unchanged.
func TestRotateVec3Identity(t *testing.T) {
	q := Quat{0, 0, 0, 1}
	x, y, z := RotateVec3(q, 1, 2, 3)
	if math.Abs(x-1) > 1e-12 || math.Abs(y-2) > 1e-12 || math.Abs(z-3) > 1e-12 {
		t.Fatalf("identity rotation changed vector: got (%v,%v,%v) want (1,2,3)", x, y, z)
	}
}

// TestRotateVec3YawNinety: 90° about Y maps +X → -Z.
// With QuatFromEuler(0, pi/2, 0) the +X axis rotates onto -Z.
func TestRotateVec3YawNinety(t *testing.T) {
	q := QuatFromEuler(0, math.Pi/2, 0)
	x, y, z := RotateVec3(q, 1, 0, 0)
	if math.Abs(x-0) > 1e-9 || math.Abs(y-0) > 1e-9 || math.Abs(z-(-1)) > 1e-9 {
		t.Fatalf("90° about Y: got (%v,%v,%v) want (0,0,-1)", x, y, z)
	}
}

// TestRotateVec3PreservesLength: rotation by a unit quat preserves vector length.
func TestRotateVec3PreservesLength(t *testing.T) {
	q := QuatFromEuler(0.3, -0.7, 1.1)
	x, y, z := RotateVec3(q, 1.5, -2.0, 0.5)
	gotLen := math.Sqrt(x*x + y*y + z*z)
	wantLen := math.Sqrt(1.5*1.5 + 2.0*2.0 + 0.5*0.5)
	if math.Abs(gotLen-wantLen) > 1e-9 {
		t.Fatalf("length not preserved: got %v want %v", gotLen, wantLen)
	}
}

// TestQuatFromEulerYOnly: single Y-axis rotation must equal axis-angle about Y.
// QuatFromEuler(0, 1.2, 0) == {0, sin(0.6), 0, cos(0.6)}.
func TestQuatFromEulerYOnly(t *testing.T) {
	const angle = 1.2
	got := QuatFromEuler(0, angle, 0)
	want := Quat{0, math.Sin(angle / 2), 0, math.Cos(angle / 2)}
	if !quatClose(got, want, 1e-9) {
		t.Fatalf("Y-only: got %+v want %+v", got, want)
	}
}
