//gosx:deterministic

// Package motion provides a unified animation evaluator for GoSX.
// It is designed to be TinyGo-clean: no reflect, no encoding/json, no
// per-frame heap allocation on hot paths.
package motion

import (
	"math"

	"m31labs.dev/gosx/det"
)

// Quat is a unit quaternion representing a 3D rotation.
type Quat struct {
	X, Y, Z, W float64
}

// Normalize returns a unit-length copy of q.
// If the magnitude is effectively zero the identity quaternion {0,0,0,1}
// is returned to avoid a divide-by-zero NaN.
func (q Quat) Normalize() Quat {
	m2 := float64(q.X*q.X) + float64(q.Y*q.Y) + float64(q.Z*q.Z) + float64(q.W*q.W)
	// NOTE: guards squared-magnitude < 1e-30 (i.e. |q| < 1e-15); the JS reference
	// (19a-scene-animation.js:125) guards length < 1e-10 — a less strict threshold.
	if m2 < 1e-30 {
		return Quat{0, 0, 0, 1}
	}
	inv := 1.0 / math.Sqrt(m2)
	return Quat{float64(q.X * inv), float64(q.Y * inv), float64(q.Z * inv), float64(q.W * inv)}
}

// quatMul returns the Hamilton product of two quaternions a * b.
// Result represents the combined rotation: first apply b, then a.
func quatMul(a, b Quat) Quat {
	return Quat{
		X: float64(a.W*b.X) + float64(a.X*b.W) + float64(a.Y*b.Z) - float64(a.Z*b.Y),
		Y: float64(a.W*b.Y) - float64(a.X*b.Z) + float64(a.Y*b.W) + float64(a.Z*b.X),
		Z: float64(a.W*b.Z) + float64(a.X*b.Y) - float64(a.Y*b.X) + float64(a.Z*b.W),
		W: float64(a.W*b.W) - float64(a.X*b.X) - float64(a.Y*b.Y) - float64(a.Z*b.Z),
	}
}

// QuatFromEuler constructs a unit quaternion from intrinsic XYZ Euler angles
// (in radians). Composition order is qx * qy * qz.
// For a single axis, the result equals the axis-angle quaternion for that axis:
//
//	QuatFromEuler(0, y, 0) == {0, sin(y/2), 0, cos(y/2)}
func QuatFromEuler(x, y, z float64) Quat {
	hx, hy, hz := x*0.5, y*0.5, z*0.5
	qx := Quat{X: det.Sin(hx), W: det.Cos(hx)}
	qy := Quat{Y: det.Sin(hy), W: det.Cos(hy)}
	qz := Quat{Z: det.Sin(hz), W: det.Cos(hz)}
	return quatMul(quatMul(qx, qy), qz)
}

// RotateVec3 applies the (assumed unit) quaternion q to the vector (x, y, z)
// and returns the rotated components. Uses the branch-free form
//
//	v' = v + 2 * cross(q.xyz, cross(q.xyz, v) + q.w*v)
//
// which avoids constructing a quaternion product. For a non-unit q the result
// is scaled by |q|^2; callers should pass unit quaternions (QuatFromEuler /
// Slerp outputs are unit).
func RotateVec3(q Quat, x, y, z float64) (float64, float64, float64) {
	// t = cross(q.xyz, v) + q.w*v
	tx := float64(q.Y*z) - float64(q.Z*y) + float64(q.W*x)
	ty := float64(q.Z*x) - float64(q.X*z) + float64(q.W*y)
	tz := float64(q.X*y) - float64(q.Y*x) + float64(q.W*z)
	// v' = v + 2 * cross(q.xyz, t)
	return x + 2*(float64(q.Y*tz)-float64(q.Z*ty)),
		y + 2*(float64(q.Z*tx)-float64(q.X*tz)),
		z + 2*(float64(q.X*ty)-float64(q.Y*tx))
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
	dot := float64(a.X*b.X) + float64(a.Y*b.Y) + float64(a.Z*b.Z) + float64(a.W*b.W)

	// Shortest-arc: flip b so we travel the short way around.
	if dot < 0 {
		b = Quat{-b.X, -b.Y, -b.Z, -b.W}
		dot = -dot
	}

	// Nlerp fast-path for nearly-parallel quaternions.
	if dot > 0.9995 {
		r := Quat{
			a.X + float64(t*(b.X-a.X)),
			a.Y + float64(t*(b.Y-a.Y)),
			a.Z + float64(t*(b.Z-a.Z)),
			a.W + float64(t*(b.W-a.W)),
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

	theta := det.Acos(dot)
	sinTheta := det.Sin(theta)
	wa := det.Sin(float64((1-t)*theta)) / sinTheta
	wb := det.Sin(float64(t*theta)) / sinTheta

	return Quat{
		float64(wa*a.X) + float64(wb*b.X),
		float64(wa*a.Y) + float64(wb*b.Y),
		float64(wa*a.Z) + float64(wb*b.Z),
		float64(wa*a.W) + float64(wb*b.W),
	}
}
