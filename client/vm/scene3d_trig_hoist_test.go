package vm

import (
	"math"
	"testing"
)

// trigHoistSamplePoints returns a spread of points that exercise sign/magnitude
// combinations through the rotation math.
func trigHoistSamplePoints() []point3 {
	return []point3{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 0, Y: 1, Z: 0},
		{X: 0, Y: 0, Z: 1},
		{X: 1, Y: 2, Z: 3},
		{X: -1, Y: -2, Z: -3},
		{X: 0.5, Y: -0.25, Z: 7.5},
		{X: -123.4, Y: 56.78, Z: -9.01},
		{X: 1e-9, Y: -1e9, Z: 3.14159},
	}
}

func trigHoistSampleAngles() [][3]float64 {
	return [][3]float64{
		{0, 0, 0},
		{0.3, 0.6, 0.1},
		{-0.3, 0.6, -0.1},
		{math.Pi / 4, math.Pi / 3, math.Pi / 6},
		{1.23456, -2.34567, 3.45678},
		{2 * math.Pi, -2 * math.Pi, 7 * math.Pi},
		{0.5, 0.5 + 0.5*0.83, 0.5}, // mimics rotation + spin*time
	}
}

// TestRotatePointTrigIsBitIdenticalToRotatePoint proves the hoisted forward
// rotation produces the EXACT same float bits as the original per-call path.
func TestRotatePointTrigIsBitIdenticalToRotatePoint(t *testing.T) {
	for _, ang := range trigHoistSampleAngles() {
		trig := newRotTrig(ang[0], ang[1], ang[2])
		for _, p := range trigHoistSamplePoints() {
			want := rotatePoint(p, ang[0], ang[1], ang[2])
			got := rotatePointTrig(p, trig)
			if got != want {
				t.Fatalf("rotatePointTrig mismatch for p=%#v ang=%v: got %#v want %#v", p, ang, got, want)
			}
		}
	}
}

// TestInverseRotatePointTrigIsBitIdenticalToInverseRotatePoint proves the
// hoisted inverse rotation is bit-identical to the original per-call path.
func TestInverseRotatePointTrigIsBitIdenticalToInverseRotatePoint(t *testing.T) {
	for _, ang := range trigHoistSampleAngles() {
		trig := newInverseRotTrig(ang[0], ang[1], ang[2])
		for _, p := range trigHoistSamplePoints() {
			want := inverseRotatePoint(p, ang[0], ang[1], ang[2])
			got := inverseRotatePointTrig(p, trig)
			if got != want {
				t.Fatalf("inverseRotatePointTrig mismatch for p=%#v ang=%v: got %#v want %#v", p, ang, got, want)
			}
		}
	}
}

// TestTranslatePointTrigBitIdentical proves the object-level translate wrapper
// and its trig variant agree exactly.
func TestTranslatePointTrigBitIdentical(t *testing.T) {
	obj := sceneObject{
		X: 2, Y: -3, Z: 5,
		Width: 1, Height: 1, Depth: 1,
		RotationX: 0.3, RotationY: 0.6, RotationZ: 0.1,
		SpinX: 0.2, SpinY: 0.5, SpinZ: -0.15,
		ShiftX: 0.4, ShiftY: 0.7, ShiftZ: -0.2,
		DriftPhase: 0.9, DriftSpeed: 1.3,
	}
	for _, ts := range []float64{0, 0.5, 1.0, 1.9, 7.3} {
		trig := sceneObjectRotTrig(obj, ts)
		for _, p := range trigHoistSamplePoints() {
			want := translatePoint(p, obj, ts)
			got := translatePointTrig(p, obj, ts, trig)
			if got != want {
				t.Fatalf("translatePointTrig mismatch p=%#v t=%v: got %#v want %#v", p, ts, got, want)
			}
		}
	}
}

// TestSceneObjectWorldNormalTrigBitIdentical proves the normal wrapper and trig
// variant agree exactly.
func TestSceneObjectWorldNormalTrigBitIdentical(t *testing.T) {
	obj := sceneObject{
		Kind:  "box",
		Width: 1.5, Height: 2.0, Depth: 0.5,
		RotationX: 0.3, RotationY: 0.6, RotationZ: 0.1,
		SpinX: 0.2, SpinY: 0.5, SpinZ: -0.15,
	}
	for _, ts := range []float64{0, 0.5, 1.0, 1.9} {
		trig := sceneObjectRotTrig(obj, ts)
		for _, p := range trigHoistSamplePoints() {
			want := sceneObjectWorldNormal(obj, p, ts)
			got := sceneObjectWorldNormalTrig(obj, p, trig)
			if got != want {
				t.Fatalf("sceneObjectWorldNormalTrig mismatch p=%#v t=%v: got %#v want %#v", p, ts, got, want)
			}
		}
	}
}

// TestCameraLocalPointTrigBitIdentical proves the camera-local wrapper and trig
// variant agree exactly across rotated cameras.
func TestCameraLocalPointTrigBitIdentical(t *testing.T) {
	cameras := []sceneCamera{
		{X: 0, Y: 0, Z: 6, RotationX: 0, RotationY: 0, RotationZ: 0, FOV: 72, Near: 0.05, Far: 128},
		{X: 1.5, Y: -2.5, Z: 8, RotationX: 0.2, RotationY: -0.4, RotationZ: 0.15, FOV: 60, Near: 0.1, Far: 256},
		{X: -3, Y: 4, Z: 12, RotationX: -1.1, RotationY: 0.9, RotationZ: -0.6, FOV: 90, Near: 0.05, Far: 512},
	}
	for _, cam := range cameras {
		trig := cameraInverseRotTrig(cam)
		for _, p := range trigHoistSamplePoints() {
			want := cameraLocalPoint(p, cam)
			got := cameraLocalPointTrig(p, cam, trig)
			if got != want {
				t.Fatalf("cameraLocalPointTrig mismatch p=%#v cam=%#v: got %#v want %#v", p, cam, got, want)
			}
		}
	}
}
