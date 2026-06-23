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
