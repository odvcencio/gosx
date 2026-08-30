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
	xx := rotation.X * rotation.X
	yy := rotation.Y * rotation.Y
	zz := rotation.Z * rotation.Z
	xy := rotation.X * rotation.Y
	xz := rotation.X * rotation.Z
	yz := rotation.Y * rotation.Z
	wx := rotation.W * rotation.X
	wy := rotation.W * rotation.Y
	wz := rotation.W * rotation.Z
	return affineMatrix{
		(1 - 2*(yy+zz)) * scale.X,
		(2 * (xy + wz)) * scale.X,
		(2 * (xz - wy)) * scale.X,
		0,
		(2 * (xy - wz)) * scale.Y,
		(1 - 2*(xx+zz)) * scale.Y,
		(2 * (yz + wx)) * scale.Y,
		0,
		(2 * (xz + wy)) * scale.Z,
		(2 * (yz - wx)) * scale.Z,
		(1 - 2*(xx+yy)) * scale.Z,
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
			out[column*4+row] = left[row]*right[column*4] +
				left[4+row]*right[column*4+1] +
				left[8+row]*right[column*4+2] +
				left[12+row]*right[column*4+3]
		}
	}
	return out
}

func affinePoint(matrix affineMatrix, point Vector3) Vector3 {
	return Vector3{
		X: matrix[0]*point.X + matrix[4]*point.Y + matrix[8]*point.Z + matrix[12],
		Y: matrix[1]*point.X + matrix[5]*point.Y + matrix[9]*point.Z + matrix[13],
		Z: matrix[2]*point.X + matrix[6]*point.Y + matrix[10]*point.Z + matrix[14],
	}
}

func affineVector(matrix affineMatrix, vector Vector3) Vector3 {
	return Vector3{
		X: matrix[0]*vector.X + matrix[4]*vector.Y + matrix[8]*vector.Z,
		Y: matrix[1]*vector.X + matrix[5]*vector.Y + matrix[9]*vector.Z,
		Z: matrix[2]*vector.X + matrix[6]*vector.Y + matrix[10]*vector.Z,
	}
}

func inverseAffine(matrix affineMatrix) (affineMatrix, bool) {
	if !validAffineMatrix(matrix) {
		return affineMatrix{}, false
	}
	scale := affineLinearMaxAbs(matrix)
	a, b, c := matrix[0]/scale, matrix[4]/scale, matrix[8]/scale
	d, e, f := matrix[1]/scale, matrix[5]/scale, matrix[9]/scale
	g, h, i := matrix[2]/scale, matrix[6]/scale, matrix[10]/scale
	c00, c01, c02 := e*i-f*h, f*g-d*i, d*h-e*g
	c10, c11, c12 := c*h-b*i, a*i-c*g, b*g-a*h
	c20, c21, c22 := b*f-c*e, c*d-a*f, a*e-b*d
	determinant := a*c00 + b*c01 + c*c02
	// Divide in this order so a finite scale near MaxFloat does not overflow
	// the otherwise representable determinant*scale product.
	invDet := 1 / determinant / scale
	if invDet == 0 || math.IsNaN(invDet) || math.IsInf(invDet, 0) {
		return affineMatrix{}, false
	}
	out := affineMatrix{
		c00 * invDet, c01 * invDet, c02 * invDet, 0,
		c10 * invDet, c11 * invDet, c12 * invDet, 0,
		c20 * invDet, c21 * invDet, c22 * invDet, 0,
		0, 0, 0, 1,
	}
	translation := affineVector(out, Vector3{X: matrix[12], Y: matrix[13], Z: matrix[14]})
	out[12], out[13], out[14] = -translation.X, -translation.Y, -translation.Z
	linearNonZero := false
	for index, value := range out {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return affineMatrix{}, false
		}
		if index < 12 && index%4 != 3 && value != 0 {
			linearNonZero = true
		}
	}
	if !linearNonZero {
		return affineMatrix{}, false
	}
	return out, true
}

func affineNormal(matrix affineMatrix, normal Vector3) Vector3 {
	inverse, ok := inverseAffine(matrix)
	if !ok {
		return Vector3{}
	}
	return normalizeVector(Vector3{
		X: inverse[0]*normal.X + inverse[1]*normal.Y + inverse[2]*normal.Z,
		Y: inverse[4]*normal.X + inverse[5]*normal.Y + inverse[6]*normal.Z,
		Z: inverse[8]*normal.X + inverse[9]*normal.Y + inverse[10]*normal.Z,
	})
}

// affineLinearBound is a conservative upper bound on the largest singular
// value of the linear 3x3 block. It keeps sphere broadphases sound under shear.
func affineLinearBound(matrix affineMatrix) float64 {
	squared := matrix[0]*matrix[0] + matrix[1]*matrix[1] + matrix[2]*matrix[2] +
		matrix[4]*matrix[4] + matrix[5]*matrix[5] + matrix[6]*matrix[6] +
		matrix[8]*matrix[8] + matrix[9]*matrix[9] + matrix[10]*matrix[10]
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
