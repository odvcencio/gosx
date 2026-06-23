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
