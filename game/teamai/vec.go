package teamai

import "m31labs.dev/gosx/det"

// epsilon is the length below which a vector counts as zero.
const epsilon = 1e-9

// Vec2 is a point or direction in the ground plane. X is the lateral world
// axis; Y is the second ground axis (world Z in a Y-up scene).
//
// In data files a Vec2 is a two-number array, [x, y].
type Vec2 struct {
	X float64
	Y float64
}

// V2 returns Vec2{x, y}.
func V2(x, y float64) Vec2 { return Vec2{X: x, Y: y} }

// Add returns v + o.
func (v Vec2) Add(o Vec2) Vec2 { return Vec2{X: v.X + o.X, Y: v.Y + o.Y} }

// Sub returns v - o.
func (v Vec2) Sub(o Vec2) Vec2 { return Vec2{X: v.X - o.X, Y: v.Y - o.Y} }

// Scale returns v * s.
func (v Vec2) Scale(s float64) Vec2 { return Vec2{X: float64(v.X * s), Y: float64(v.Y * s)} }

// AddScaled returns v + o*s with the product rounded first.
func (v Vec2) AddScaled(o Vec2, s float64) Vec2 {
	return Vec2{X: v.X + float64(o.X*s), Y: v.Y + float64(o.Y*s)}
}

// Neg returns -v.
func (v Vec2) Neg() Vec2 { return Vec2{X: -v.X, Y: -v.Y} }

// Dot returns the dot product of v and o.
func (v Vec2) Dot(o Vec2) float64 { return float64(v.X*o.X) + float64(v.Y*o.Y) }

// Cross returns the z component of the 3D cross product of v and o: positive
// when o is counter-clockwise from v.
func (v Vec2) Cross(o Vec2) float64 { return float64(v.X*o.Y) - float64(v.Y*o.X) }

// Len2 returns the squared length of v.
func (v Vec2) Len2() float64 { return v.Dot(v) }

// Len returns the length of v.
func (v Vec2) Len() float64 { return det.Sqrt(v.Len2()) }

// Dist returns the distance between v and o.
func (v Vec2) Dist(o Vec2) float64 { return v.Sub(o).Len() }

// Normalize returns v scaled to length 1, or the zero vector when v is
// shorter than 1e-9.
func (v Vec2) Normalize() Vec2 {
	l := v.Len()
	if l <= epsilon {
		return Vec2{}
	}
	return Vec2{X: v.X / l, Y: v.Y / l}
}

// Perp returns v rotated 90 degrees counter-clockwise.
func (v Vec2) Perp() Vec2 { return Vec2{X: -v.Y, Y: v.X} }

// ClampLen returns v shortened to at most max.
func (v Vec2) ClampLen(max float64) Vec2 {
	l2 := v.Len2()
	if max <= 0 {
		return Vec2{}
	}
	if l2 <= float64(max*max) {
		return v
	}
	return v.Scale(max / det.Sqrt(l2))
}

// IsZero reports whether both components are zero.
func (v Vec2) IsZero() bool { return v.X == 0 && v.Y == 0 }

// Lerp returns a + (b-a)*t.
func Lerp(a, b Vec2, t float64) Vec2 { return a.AddScaled(b.Sub(a), t) }

func finite(x float64) bool { return !det.IsNaN(x) && !det.IsInf(x, 0) }

func (v Vec2) finite() bool { return finite(v.X) && finite(v.Y) }

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Rect is an axis-aligned rectangle in the ground plane, such as the playing
// field. A zero Rect means "no bounds".
type Rect struct {
	Min Vec2
	Max Vec2
}

// Valid reports whether r has a positive area.
func (r Rect) Valid() bool { return r.Max.X > r.Min.X && r.Max.Y > r.Min.Y }

// Contains reports whether p lies inside r, edges included.
func (r Rect) Contains(p Vec2) bool {
	return p.X >= r.Min.X && p.X <= r.Max.X && p.Y >= r.Min.Y && p.Y <= r.Max.Y
}

// Clamp returns the point of r closest to p. A zero Rect returns p.
func (r Rect) Clamp(p Vec2) Vec2 {
	if !r.Valid() {
		return p
	}
	return Vec2{X: clamp(p.X, r.Min.X, r.Max.X), Y: clamp(p.Y, r.Min.Y, r.Max.Y)}
}

// Exit returns the distance along the unit direction dir from p, a point inside
// r, to the edge of r. It returns +Inf when r is not Valid or dir is zero, and 0
// when p is outside r.
func (r Rect) Exit(p, dir Vec2) float64 {
	if !r.Valid() || dir.IsZero() {
		return det.Inf(1)
	}
	if !r.Contains(p) {
		return 0
	}
	t := det.Inf(1)
	if dir.X > 0 {
		t = det.Min(t, (r.Max.X-p.X)/dir.X)
	} else if dir.X < 0 {
		t = det.Min(t, (r.Min.X-p.X)/dir.X)
	}
	if dir.Y > 0 {
		t = det.Min(t, (r.Max.Y-p.Y)/dir.Y)
	} else if dir.Y < 0 {
		t = det.Min(t, (r.Min.Y-p.Y)/dir.Y)
	}
	return t
}
