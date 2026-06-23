// Package motion provides a unified animation evaluator for GoSX.
// It is designed to be TinyGo-clean: no reflect, no encoding/json, no
// per-frame heap allocation on hot paths.
package motion

import "math"

// Quat is a unit quaternion representing a 3D rotation.
type Quat struct {
	X, Y, Z, W float64
}

// Normalize returns a unit-length copy of q.
// If the magnitude is effectively zero the identity quaternion {0,0,0,1}
// is returned to avoid a divide-by-zero NaN.
func (q Quat) Normalize() Quat {
	m2 := q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W
	// NOTE: guards squared-magnitude < 1e-30 (i.e. |q| < 1e-15); the JS reference
	// (19a-scene-animation.js:125) guards length < 1e-10 — a less strict threshold.
	if m2 < 1e-30 {
		return Quat{0, 0, 0, 1}
	}
	inv := 1.0 / math.Sqrt(m2)
	return Quat{q.X * inv, q.Y * inv, q.Z * inv, q.W * inv}
}

// quatMul returns the Hamilton product of two quaternions a * b.
// Result represents the combined rotation: first apply b, then a.
func quatMul(a, b Quat) Quat {
	return Quat{
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
	}
}

// QuatFromEuler constructs a unit quaternion from intrinsic XYZ Euler angles
// (in radians). Composition order is qx * qy * qz.
// For a single axis, the result equals the axis-angle quaternion for that axis:
//
//	QuatFromEuler(0, y, 0) == {0, sin(y/2), 0, cos(y/2)}
func QuatFromEuler(x, y, z float64) Quat {
	hx, hy, hz := x*0.5, y*0.5, z*0.5
	qx := Quat{X: math.Sin(hx), W: math.Cos(hx)}
	qy := Quat{Y: math.Sin(hy), W: math.Cos(hy)}
	qz := Quat{Z: math.Sin(hz), W: math.Cos(hz)}
	return quatMul(quatMul(qx, qy), qz)
}

// Slerp performs spherical linear interpolation between unit quaternions a and b
// by parameter t in [0, 1].
//
// Invariants:
//   - Shortest-arc: if the dot product of a and b is negative, b is negated so
//     the interpolation travels the short way around.
//   - Fast-path: when the quaternions are nearly parallel (dot > 0.9995) a
//     normalized linear interpolation (nlerp) is used to avoid numerical
//     instability near sin(0).
//   - NaN-safe: dot is clamped to [-1, 1] before math.Acos.
func Slerp(a, b Quat, t float64) Quat {
	// Four-component dot product.
	dot := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W

	// Shortest-arc: flip b so we travel the short way around.
	if dot < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		dot = -dot
	}

	// Nlerp fast-path for nearly-parallel quaternions.
	if dot > 0.9995 {
		r := Quat{
			a.X + t*(b.X-a.X),
			a.Y + t*(b.Y-a.Y),
			a.Z + t*(b.Z-a.Z),
			a.W + t*(b.W-a.W),
		}
		return r.Normalize()
	}

	// Clamp dot to [-1, 1] to guard math.Acos against NaN on floating-point
	// rounding slightly outside the domain.
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}

	theta := math.Acos(dot)
	sinTheta := math.Sin(theta)
	wa := math.Sin((1-t)*theta) / sinTheta
	wb := math.Sin(t*theta) / sinTheta

	return Quat{
		wa*a.X + wb*b.X,
		wa*a.Y + wb*b.Y,
		wa*a.Z + wb*b.Z,
		wa*a.W + wb*b.W,
	}
}
