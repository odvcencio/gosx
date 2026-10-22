package physics

import (
	"math"

	"m31labs.dev/gosx/det"
)

const epsilon = 1e-9

// Vec3 is a small value type for 3D simulation math.
type Vec3 struct {
	X float64
	Y float64
	Z float64
}

func (v Vec3) Add(o Vec3) Vec3 {
	return Vec3{X: v.X + o.X, Y: v.Y + o.Y, Z: v.Z + o.Z}
}

func (v Vec3) Sub(o Vec3) Vec3 {
	return Vec3{X: v.X - o.X, Y: v.Y - o.Y, Z: v.Z - o.Z}
}

func (v Vec3) Mul(s float64) Vec3 {
	return Vec3{X: float64(v.X * s), Y: float64(v.Y * s), Z: float64(v.Z * s)}
}

func (v Vec3) Div(s float64) Vec3 {
	if s == 0 {
		return Vec3{}
	}
	return Vec3{X: v.X / s, Y: v.Y / s, Z: v.Z / s}
}

func (v Vec3) Neg() Vec3 {
	return Vec3{X: -v.X, Y: -v.Y, Z: -v.Z}
}

func (v Vec3) Dot(o Vec3) float64 {
	return float64(v.X*o.X) + float64(v.Y*o.Y) + float64(v.Z*o.Z)
}

func (v Vec3) Cross(o Vec3) Vec3 {
	return Vec3{
		X: float64(v.Y*o.Z) - float64(v.Z*o.Y),
		Y: float64(v.Z*o.X) - float64(v.X*o.Z),
		Z: float64(v.X*o.Y) - float64(v.Y*o.X),
	}
}

func (v Vec3) Len2() float64 {
	return v.Dot(v)
}

func (v Vec3) Len() float64 {
	return math.Sqrt(v.Len2())
}

func (v Vec3) Normalize() Vec3 {
	length := v.Len()
	if length <= epsilon {
		return Vec3{}
	}
	return v.Div(length)
}

func (v Vec3) Distance(o Vec3) float64 {
	return v.Sub(o).Len()
}

func (v Vec3) Min(o Vec3) Vec3 {
	return Vec3{
		X: math.Min(v.X, o.X),
		Y: math.Min(v.Y, o.Y),
		Z: math.Min(v.Z, o.Z),
	}
}

func (v Vec3) Max(o Vec3) Vec3 {
	return Vec3{
		X: math.Max(v.X, o.X),
		Y: math.Max(v.Y, o.Y),
		Z: math.Max(v.Z, o.Z),
	}
}

func (v Vec3) Abs() Vec3 {
	return Vec3{X: math.Abs(v.X), Y: math.Abs(v.Y), Z: math.Abs(v.Z)}
}

func (v Vec3) Near(o Vec3, tolerance float64) bool {
	return math.Abs(v.X-o.X) <= tolerance &&
		math.Abs(v.Y-o.Y) <= tolerance &&
		math.Abs(v.Z-o.Z) <= tolerance
}

func Lerp(a, b Vec3, t float64) Vec3 {
	return a.Add(b.Sub(a).Mul(t))
}

// Quat stores a quaternion as x, y, z, w with w as the scalar component.
type Quat struct {
	X float64
	Y float64
	Z float64
	W float64
}

func IdentityQuat() Quat {
	return Quat{W: 1}
}

func QuatFromAxisAngle(axis Vec3, angle float64) Quat {
	normalized := axis.Normalize()
	if normalized.Len2() <= epsilon {
		return IdentityQuat()
	}
	half := angle * 0.5
	s := det.Sin(half)
	return Quat{
		X: float64(normalized.X * s),
		Y: float64(normalized.Y * s),
		Z: float64(normalized.Z * s),
		W: det.Cos(half),
	}.Normalize()
}

func (q Quat) Normalize() Quat {
	length := math.Sqrt(float64(q.X*q.X) + float64(q.Y*q.Y) + float64(q.Z*q.Z) + float64(q.W*q.W))
	if length <= epsilon {
		return IdentityQuat()
	}
	return Quat{X: q.X / length, Y: q.Y / length, Z: q.Z / length, W: q.W / length}
}

func (q Quat) Conjugate() Quat {
	return Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W}
}

func (q Quat) Inverse() Quat {
	length2 := float64(q.X*q.X) + float64(q.Y*q.Y) + float64(q.Z*q.Z) + float64(q.W*q.W)
	if length2 <= epsilon {
		return IdentityQuat()
	}
	return Quat{X: -q.X / length2, Y: -q.Y / length2, Z: -q.Z / length2, W: q.W / length2}
}

func (q Quat) Mul(o Quat) Quat {
	return Quat{
		X: float64(q.W*o.X) + float64(q.X*o.W) + float64(q.Y*o.Z) - float64(q.Z*o.Y),
		Y: float64(q.W*o.Y) - float64(q.X*o.Z) + float64(q.Y*o.W) + float64(q.Z*o.X),
		Z: float64(q.W*o.Z) + float64(q.X*o.Y) - float64(q.Y*o.X) + float64(q.Z*o.W),
		W: float64(q.W*o.W) - float64(q.X*o.X) - float64(q.Y*o.Y) - float64(q.Z*o.Z),
	}
}

func (q Quat) Rotate(v Vec3) Vec3 {
	n := q.Normalize()
	u := Vec3{X: n.X, Y: n.Y, Z: n.Z}
	return u.Mul(2 * u.Dot(v)).
		Add(v.Mul(float64(n.W*n.W) - u.Dot(u))).
		Add(u.Cross(v).Mul(2 * n.W))
}

func (q Quat) Slerp(to Quat, t float64) Quat {
	a := q.Normalize()
	b := to.Normalize()
	dot := float64(a.X*b.X) + float64(a.Y*b.Y) + float64(a.Z*b.Z) + float64(a.W*b.W)
	if dot < 0 {
		b = Quat{X: -b.X, Y: -b.Y, Z: -b.Z, W: -b.W}
		dot = -dot
	}
	if dot > 0.9995 {
		return Quat{
			X: a.X + float64((b.X-a.X)*t),
			Y: a.Y + float64((b.Y-a.Y)*t),
			Z: a.Z + float64((b.Z-a.Z)*t),
			W: a.W + float64((b.W-a.W)*t),
		}.Normalize()
	}
	theta0 := det.Acos(dot)
	theta := float64(theta0 * t)
	sinTheta := det.Sin(theta)
	sinTheta0 := det.Sin(theta0)
	s0 := det.Cos(theta) - dot*sinTheta/sinTheta0
	s1 := sinTheta / sinTheta0
	return Quat{
		X: float64(a.X*s0) + float64(b.X*s1),
		Y: float64(a.Y*s0) + float64(b.Y*s1),
		Z: float64(a.Z*s0) + float64(b.Z*s1),
		W: float64(a.W*s0) + float64(b.W*s1),
	}.Normalize()
}

// AABB is an axis-aligned bounding box in world space.
type AABB struct {
	Min Vec3
	Max Vec3
}

func NewAABB(min, max Vec3) AABB {
	return AABB{Min: min.Min(max), Max: min.Max(max)}
}

func EmptyAABB() AABB {
	return AABB{
		Min: Vec3{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)},
		Max: Vec3{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)},
	}
}

func AABBFromCenterHalfExtents(center, halfExtents Vec3) AABB {
	half := halfExtents.Abs()
	return AABB{Min: center.Sub(half), Max: center.Add(half)}
}

func (a AABB) IsFinite() bool {
	return isFiniteFloat(a.Min.X) && isFiniteFloat(a.Min.Y) && isFiniteFloat(a.Min.Z) &&
		isFiniteFloat(a.Max.X) && isFiniteFloat(a.Max.Y) && isFiniteFloat(a.Max.Z)
}

// isFiniteFloat reports the same boolean AABB.IsFinite always computed, one
// value at a time instead of through a slice literal, which let the compiler
// keep every check on the stack instead of materializing and ranging a
// []float64 for six values on the narrowphase and broadphase hot paths.
func isFiniteFloat(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}

func (a AABB) Overlaps(b AABB) bool {
	return a.Min.X <= b.Max.X && a.Max.X >= b.Min.X &&
		a.Min.Y <= b.Max.Y && a.Max.Y >= b.Min.Y &&
		a.Min.Z <= b.Max.Z && a.Max.Z >= b.Min.Z
}

func (a AABB) Contains(p Vec3) bool {
	return p.X >= a.Min.X && p.X <= a.Max.X &&
		p.Y >= a.Min.Y && p.Y <= a.Max.Y &&
		p.Z >= a.Min.Z && p.Z <= a.Max.Z
}

func (a AABB) Expand(amount float64) AABB {
	delta := Vec3{X: amount, Y: amount, Z: amount}
	return AABB{Min: a.Min.Sub(delta), Max: a.Max.Add(delta)}
}

func (a AABB) Union(b AABB) AABB {
	return AABB{Min: a.Min.Min(b.Min), Max: a.Max.Max(b.Max)}
}

func (a AABB) Center() Vec3 {
	return a.Min.Add(a.Max).Mul(0.5)
}

func (a AABB) Size() Vec3 {
	return a.Max.Sub(a.Min)
}

// Ray is a world-space ray with a normalized direction by convention.
type Ray struct {
	Origin    Vec3
	Direction Vec3
}

func (r Ray) At(distance float64) Vec3 {
	return r.Origin.Add(r.Direction.Mul(distance))
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
