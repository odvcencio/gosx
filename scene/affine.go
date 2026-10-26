//gosx:deterministic

package scene

import (
	"math"

	"m31labs.dev/gosx/internal/sceneaffine"
)

// affineMatrix is a column-major 4x4 affine transform. Group hierarchy scale
// uses the exact matrix form because non-uniform scale followed by rotation can
// contain shear and cannot be decomposed back into position/quaternion/scale.
type affineMatrix [16]float64

func resolvedSceneScale(scale Vector3) Vector3 {
	if scale.X == 0 {
		scale.X = 1
	}
	if scale.Y == 0 {
		scale.Y = 1
	}
	if scale.Z == 0 {
		scale.Z = 1
	}
	return scale
}

func isUnitSceneScale(scale Vector3) bool {
	return resolvedSceneScale(scale) == (Vector3{X: 1, Y: 1, Z: 1})
}

func affineFromTRS(translation Vector3, rotation quaternion, scale Vector3) affineMatrix {
	rotation = rotation.normalized()
	scale = resolvedSceneScale(scale)
	xx := float64(rotation.X * rotation.X)
	yy := float64(rotation.Y * rotation.Y)
	zz := float64(rotation.Z * rotation.Z)
	xy := float64(rotation.X * rotation.Y)
	xz := float64(rotation.X * rotation.Z)
	yz := float64(rotation.Y * rotation.Z)
	wx := float64(rotation.W * rotation.X)
	wy := float64(rotation.W * rotation.Y)
	wz := float64(rotation.W * rotation.Z)
	return affineMatrix{
		float64((1 - 2*(yy+zz)) * scale.X),
		float64((2 * (xy + wz)) * scale.X),
		float64((2 * (xz - wy)) * scale.X),
		0,
		float64((2 * (xy - wz)) * scale.Y),
		float64((1 - 2*(xx+zz)) * scale.Y),
		float64((2 * (yz + wx)) * scale.Y),
		0,
		float64((2 * (xz + wy)) * scale.Z),
		float64((2 * (yz - wx)) * scale.Z),
		float64((1 - 2*(xx+yy)) * scale.Z),
		0,
		translation.X,
		translation.Y,
		translation.Z,
		1,
	}
}

func multiplyAffine(left, right affineMatrix) affineMatrix {
	var out affineMatrix
	for column := 0; column < 4; column++ {
		for row := 0; row < 4; row++ {
			out[column*4+row] = float64(left[row]*right[column*4]) +
				float64(left[4+row]*right[column*4+1]) +
				float64(left[8+row]*right[column*4+2]) +
				float64(left[12+row]*right[column*4+3])
		}
	}
	return out
}

func affinePoint(matrix affineMatrix, point Vector3) Vector3 {
	return Vector3{
		X: float64(matrix[0]*point.X) + float64(matrix[4]*point.Y) + float64(matrix[8]*point.Z) + matrix[12],
		Y: float64(matrix[1]*point.X) + float64(matrix[5]*point.Y) + float64(matrix[9]*point.Z) + matrix[13],
		Z: float64(matrix[2]*point.X) + float64(matrix[6]*point.Y) + float64(matrix[10]*point.Z) + matrix[14],
	}
}

func affineVector(matrix affineMatrix, vector Vector3) Vector3 {
	return Vector3{
		X: float64(matrix[0]*vector.X) + float64(matrix[4]*vector.Y) + float64(matrix[8]*vector.Z),
		Y: float64(matrix[1]*vector.X) + float64(matrix[5]*vector.Y) + float64(matrix[9]*vector.Z),
		Z: float64(matrix[2]*vector.X) + float64(matrix[6]*vector.Y) + float64(matrix[10]*vector.Z),
	}
}

func inverseAffine(matrix affineMatrix) (affineMatrix, bool) {
	linear, ok := sceneaffine.InverseLinear(matrix[:])
	if !ok {
		return affineMatrix{}, false
	}
	out := affineMatrix{
		linear[0], linear[1], linear[2], 0,
		linear[3], linear[4], linear[5], 0,
		linear[6], linear[7], linear[8], 0,
		0, 0, 0, 1,
	}
	translation := affineVector(out, Vector3{X: matrix[12], Y: matrix[13], Z: matrix[14]})
	out[12], out[13], out[14] = -translation.X, -translation.Y, -translation.Z
	for _, value := range out {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return affineMatrix{}, false
		}
	}
	return out, true
}

func affineNormal(matrix affineMatrix, normal Vector3) Vector3 {
	inverse, ok := inverseAffine(matrix)
	if !ok {
		return Vector3{}
	}
	return normalizeVector(Vector3{
		X: float64(inverse[0]*normal.X) + float64(inverse[1]*normal.Y) + float64(inverse[2]*normal.Z),
		Y: float64(inverse[4]*normal.X) + float64(inverse[5]*normal.Y) + float64(inverse[6]*normal.Z),
		Z: float64(inverse[8]*normal.X) + float64(inverse[9]*normal.Y) + float64(inverse[10]*normal.Z),
	})
}

// affineLinearBound is a conservative upper bound on the largest singular
// value of the linear 3x3 block. It keeps sphere broadphases sound under shear.
func affineLinearBound(matrix affineMatrix) float64 {
	squared := float64(matrix[0]*matrix[0]) + float64(matrix[1]*matrix[1]) + float64(matrix[2]*matrix[2]) +
		float64(matrix[4]*matrix[4]) + float64(matrix[5]*matrix[5]) + float64(matrix[6]*matrix[6]) +
		float64(matrix[8]*matrix[8]) + float64(matrix[9]*matrix[9]) + float64(matrix[10]*matrix[10])
	if squared <= 0 {
		return 1
	}
	return math.Sqrt(squared)
}

func worldAffine(world worldTransform) affineMatrix {
	if world.HasMatrix {
		return world.Matrix
	}
	return affineFromTRS(world.Position, world.Rotation, sceneUnitScale())
}

func worldAffineWithScale(world worldTransform, scale Vector3) affineMatrix {
	return multiplyAffine(worldAffine(world), affineFromTRS(Vector3{}, quaternion{W: 1}, scale))
}

func affineSlice(matrix affineMatrix) []float64 {
	out := make([]float64, len(matrix))
	copy(out, matrix[:])
	return out
}

func affineFromValues(values []float64) (affineMatrix, bool) {
	if len(values) != 16 {
		return affineMatrix{}, false
	}
	var out affineMatrix
	copy(out[:], values)
	return out, validAffineMatrix(out)
}

// ValidParentMatrix reports whether values encode the exact affine contract
// accepted by every Scene3D boundary: a finite column-major 4x4 matrix with
// bottom row [0, 0, 0, 1] and a scale-invariant, safely invertible linear part.
func ValidParentMatrix(values []float64) bool {
	return sceneaffine.ValidParentMatrix(values)
}

func validAffineMatrix(matrix affineMatrix) bool {
	return sceneaffine.ValidParentMatrix(matrix[:])
}

func affineLinearMaxAbs(matrix affineMatrix) float64 {
	maximum := 0.0
	for _, index := range [...]int{0, 1, 2, 4, 5, 6, 8, 9, 10} {
		maximum = math.Max(maximum, math.Abs(matrix[index]))
	}
	return maximum
}
