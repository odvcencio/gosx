package camera

import (
	"math"
	"testing"
)

func TestLensPutsTheTargetInTheMiddle(t *testing.T) {
	p := Pose{Position: Vec3{0, 5, 10}, Target: Vec3{0, 0, 0}, FOV: 60}
	l := p.Lens(800, 600)
	x, y, scale, depth := l.Project(p.Target)
	if math.Abs(x-400) > 1e-9 || math.Abs(y-300) > 1e-9 || depth <= 0 || scale <= 0 {
		t.Fatalf("target at %v,%v scale %v depth %v", x, y, scale, depth)
	}
	// A point to the camera's right lands right of centre; one above, higher.
	if x, _, _, _ := l.Project(Vec3{1, 0, 0}); x <= 400 {
		t.Fatalf("+X from a camera looking down -Z landed at x=%v", x)
	}
	if _, y, _, _ := l.Project(Vec3{0, 1, 0}); y >= 300 {
		t.Fatalf("a point above the target landed at y=%v", y)
	}
	if _, _, _, depth := l.Project(Vec3{0, 5, 20}); depth > 0 {
		t.Fatal("a point behind the camera has positive depth")
	}
	if x, side := l.ClampX(-5, 20); x != 20 || side != -1 {
		t.Fatalf("clamp left = %v %d", x, side)
	}
}

func TestFitDistanceBacksAwayOnlyWhenNeeded(t *testing.T) {
	p := Pose{Position: Vec3{0, 0, 10}, Target: Vec3{0, 0, 0}, FOV: 60}
	if d := FitDistance(p, []Vec3{{1, 0, 0}}, 1, .9, .9); d != 10 {
		t.Fatalf("a point already in the picture moved the camera to %v", d)
	}
	far := Vec3{30, 0, 0}
	d := FitDistance(p, []Vec3{far}, 1, 1, 1)
	// At distance d the half width is d*tan(30deg); it must reach 30.
	if half := d * math.Tan(math.Pi/6); math.Abs(half-30) > 1e-9 {
		t.Fatalf("fit distance %v gives half width %v, want 30", d, half)
	}
}
