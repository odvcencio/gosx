package vm

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
)

// The reference functions retain the pre-hoist motion composition. Check exact
// bits, including signed zero: an algebraically equivalent matrix can change
// winding, normals or golden render streams through different rounding.
func TestSceneFrameTransformPreservesMotionComposition(t *testing.T) {
	objects := []sceneObject{
		{Kind: "sphere"},
		{Kind: "box", X: 2, Y: -3, Z: 5, RotationX: .3, RotationY: -.6, RotationZ: .1,
			ScaleX: -2, ScaleY: .5, ScaleZ: 3, ShiftX: .8, ShiftY: -.4, ShiftZ: 2, DriftPhase: .7, DriftSpeed: .9},
		{Kind: "sphere", X: -1, Y: .5, Z: 4, RotationX: -.7, RotationY: 1.2, RotationZ: .4,
			ScaleX: .3, ScaleY: 2, ShiftX: .2, ShiftZ: .7, DriftSpeed: .3,
			HasParentMatrix: true, ParentMatrix: [16]float64{-2, 0, 0, 0, 1, 3, 0, 0, 0, .5, 4, 0, 5, 6, 7, 1}},
	}
	clips := []clipTRS{
		{},
		{HasT: true, T: [3]float64{.4, -.7, 2}},
		{HasR: true, R: motion.Quat{X: .5, Y: .5, Z: .5, W: .5}},
		{HasS: true, S: [3]float64{-2, .5, 3}},
		{HasT: true, HasR: true, HasS: true, T: [3]float64{1, 2, 3},
			R: motion.Quat{X: .5, Y: -.5, Z: .5, W: .5}, S: [3]float64{0, 2, -3}},
	}
	spins := []motion.Quat{{W: 1}, {X: .5, Y: .5, Z: -.5, W: .5}}
	for _, object := range objects {
		for _, seconds := range []float64{-3, 0, .7, 9.1, 63} {
			frame := prepareSceneObjectFrameTransform(object, seconds)
			for _, clip := range clips {
				for _, spin := range spins {
					for _, point := range trigHoistSamplePoints() {
						assertFramePointBits(t, frame.point(point, object, spin, clip), referenceFramePoint(point, object, spin, clip, seconds))
						normal := sceneObjectLocalNormal(object, point)
						assertFramePointBits(t, frame.normal(object, normal, spin, clip), referenceFrameNormal(object, normal, spin, clip))
					}
				}
			}
		}
	}
}

func assertFramePointBits(t *testing.T, got, want point3) {
	t.Helper()
	if math.Float64bits(got.X) != math.Float64bits(want.X) ||
		math.Float64bits(got.Y) != math.Float64bits(want.Y) ||
		math.Float64bits(got.Z) != math.Float64bits(want.Z) {
		t.Fatalf("frame transform = %#v, want original composition %#v", got, want)
	}
}

func referenceFramePoint(point point3, object sceneObject, spin motion.Quat, clip clipTRS, seconds float64) point3 {
	local := applySceneObjectScale(point, object)
	if clip.HasS {
		local = point3{X: local.X * clip.S[0], Y: local.Y * clip.S[1], Z: local.Z * clip.S[2]}
	}
	rotated := rotatePoint(local, object.RotationX, object.RotationY, object.RotationZ)
	if clip.HasR {
		x, y, z := motion.RotateVec3(clip.R, rotated.X, rotated.Y, rotated.Z)
		rotated = point3{X: x, Y: y, Z: z}
	}
	x, y, z := motion.RotateVec3(spin, rotated.X, rotated.Y, rotated.Z)
	offset := sceneMotionOffset(object, seconds)
	tx, ty, tz := 0.0, 0.0, 0.0
	if clip.HasT {
		tx, ty, tz = clip.T[0], clip.T[1], clip.T[2]
	}
	return applySceneParentPoint(object, point3{
		X: x + object.X + offset.X + tx,
		Y: y + object.Y + offset.Y + ty,
		Z: z + object.Z + offset.Z + tz,
	})
}

func referenceFrameNormal(object sceneObject, normal point3, spin motion.Quat, clip clipTRS) point3 {
	normal = applySceneObjectInverseScaleToNormal(object, normal)
	if clip.HasS {
		if clip.S[0] == 0 || clip.S[1] == 0 || clip.S[2] == 0 {
			normal = normalizePoint3(normal)
		} else {
			normal = normalizePoint3(point3{X: normal.X / clip.S[0], Y: normal.Y / clip.S[1], Z: normal.Z / clip.S[2]})
		}
	}
	rotated := rotatePoint(normal, object.RotationX, object.RotationY, object.RotationZ)
	if clip.HasR {
		x, y, z := motion.RotateVec3(clip.R, rotated.X, rotated.Y, rotated.Z)
		rotated = point3{X: x, Y: y, Z: z}
	}
	x, y, z := motion.RotateVec3(spin, rotated.X, rotated.Y, rotated.Z)
	return applySceneParentNormal(object, point3{X: x, Y: y, Z: z})
}
