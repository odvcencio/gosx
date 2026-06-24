package vm

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
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

// TestTranslatePointTrigBitIdentical proves the trig-hoisted static translate
// path is bit-identical to the motion translatePoint path for a no-spin/no-clip
// object (i.e., an object eligible for the world-bake cache).
//
// The motion path: translatePoint(p, obj, identityQuat, emptyClip, t)
// The trig path:   translatePointTrig(p, obj, t, sceneObjectRotTrig(obj, t))
//
// For a no-spin object, sceneObjectRotTrig reduces to the base rotation only,
// and motion.RotateVec3(identityQuat, …) is identity — so the two paths are
// algebraically identical. This test locks that bit-identity end-to-end.
func TestTranslatePointTrigBitIdentical(t *testing.T) {
	// No-spin, no-drift object: eligible for the bake cache and the trig path.
	obj := sceneObject{
		X: 2, Y: -3, Z: 5,
		Width: 1, Height: 1, Depth: 1,
		RotationX: 0.3, RotationY: 0.6, RotationZ: 0.1,
		// SpinX/Y/Z == 0: static, no drift.
		ShiftX: 0, ShiftY: 0, ShiftZ: 0,
	}
	identityQ := motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	emptyClip := clipTRS{}
	for _, ts := range []float64{0, 0.5, 1.0, 1.9, 7.3} {
		trig := sceneObjectRotTrig(obj, ts)
		for _, p := range trigHoistSamplePoints() {
			want := translatePoint(p, obj, identityQ, emptyClip, ts)
			got := translatePointTrig(p, obj, ts, trig)
			if got != want {
				t.Fatalf("translatePointTrig mismatch p=%#v t=%v: got %#v want %#v", p, ts, got, want)
			}
		}
	}
}

// TestSceneObjectWorldNormalTrigBitIdentical proves the trig-hoisted normal path
// (used by the static/bake cache) is bit-identical to the motion normal path for
// a no-spin/no-clip object. For a no-spin object the identity quat leaves normals
// unchanged (motion.RotateVec3(identityQuat, …) is identity), so the trig path
// and the motion path must agree exactly.
func TestSceneObjectWorldNormalTrigBitIdentical(t *testing.T) {
	// No-spin object: base rotation only, eligible for bake cache and the trig path.
	obj := sceneObject{
		Kind:  "box",
		Width: 1.5, Height: 2.0, Depth: 0.5,
		RotationX: 0.3, RotationY: 0.6, RotationZ: 0.1,
		// SpinX/Y/Z == 0: static.
	}
	identityQ := motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	emptyClip := clipTRS{}
	for _, ts := range []float64{0, 0.5, 1.0, 1.9} {
		trig := sceneObjectRotTrig(obj, ts)
		for _, p := range trigHoistSamplePoints() {
			want := sceneObjectWorldNormal(obj, p, identityQ, emptyClip)
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
