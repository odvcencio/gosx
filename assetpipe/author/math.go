// Package author builds rigged, animated glTF models from Go code: a mesh
// builder (ring stacks, ellipsoids, swept tubes, merge, normals, atlas UVs),
// a skeleton with skin weights and joint bands, keyframed clips sampled from
// Go functions, a texture atlas, morph targets, and a writer on top of
// gltfedit. Audit checks a skinned GLB, authored here or anywhere else, for
// the deformation faults that show on screen: waist pinch, stretched or torn
// edges, open joints, weights that cross the hip, inside-out shells and
// joints outside natural bands.
//
// Units are the caller's. The writer assumes +Y up, as glTF does.
package author

import "math"

// Vec3 is a point or direction.
type Vec3 [3]float64

// Add returns a+b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }

// Sub returns a-b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

// Scale returns a*s.
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a[0] * s, a[1] * s, a[2] * s} }

// Dot returns the dot product.
func (a Vec3) Dot(b Vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

// Cross returns the cross product.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

// Len returns the length.
func (a Vec3) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Unit returns a scaled to length 1; a zero vector stays near zero.
func (a Vec3) Unit() Vec3 { return a.Scale(1 / math.Max(1e-10, a.Len())) }

// Quat is a rotation as x, y, z, w.
type Quat [4]float64

// Identity is the rotation that does nothing.
var Identity = Quat{0, 0, 0, 1}

// Euler returns the rotation by x, then y, then z degrees about the local
// axes (q = qx * qy * qz), the order EulerOf undoes.
func Euler(x, y, z float64) Quat {
	const half = math.Pi / 180
	x, y, z = x*half*.5, y*half*.5, z*half*.5
	a, b, c := math.Cos(x), math.Cos(y), math.Cos(z)
	u, v, w := math.Sin(x), math.Sin(y), math.Sin(z)
	return Quat{u*b*c + a*v*w, a*v*c - u*b*w, a*b*w + u*v*c, a*b*c - u*v*w}
}

// EulerOf returns the x, y and z angles in degrees with Euler(x, y, z) = q.
func EulerOf(q Quat) Vec3 {
	m := q.Matrix()
	y := math.Asin(math.Max(-1, math.Min(1, m[0][2])))
	x := math.Atan2(-m[1][2], m[2][2])
	z := math.Atan2(-m[0][1], m[0][0])
	const deg = 180 / math.Pi
	return Vec3{x * deg, y * deg, z * deg}
}

// Dot returns the four-component dot product.
func (q Quat) Dot(r Quat) float64 { return q[0]*r[0] + q[1]*r[1] + q[2]*r[2] + q[3]*r[3] }

// Mul returns q*r: r applied first, then q.
func (q Quat) Mul(r Quat) Quat {
	return Quat{
		q[3]*r[0] + q[0]*r[3] + q[1]*r[2] - q[2]*r[1],
		q[3]*r[1] - q[0]*r[2] + q[1]*r[3] + q[2]*r[0],
		q[3]*r[2] + q[0]*r[1] - q[1]*r[0] + q[2]*r[3],
		q[3]*r[3] - q[0]*r[0] - q[1]*r[1] - q[2]*r[2],
	}
}

// Normalize returns q scaled to unit length.
func (q Quat) Normalize() Quat {
	n := math.Max(1e-10, math.Sqrt(q.Dot(q)))
	return Quat{q[0] / n, q[1] / n, q[2] / n, q[3] / n}
}

// Nlerp blends q toward r by t along the shorter way and normalizes.
func Nlerp(q, r Quat, t float64) Quat {
	if q.Dot(r) < 0 {
		r = Quat{-r[0], -r[1], -r[2], -r[3]}
	}
	var o Quat
	for i := range o {
		o[i] = (1-t)*q[i] + t*r[i]
	}
	return o.Normalize()
}

// Slerp blends q toward r by t at constant angular speed.
func Slerp(q, r Quat, t float64) Quat {
	d := q.Dot(r)
	if d < 0 {
		r, d = Quat{-r[0], -r[1], -r[2], -r[3]}, -d
	}
	if d > .9995 {
		return Nlerp(q, r, t)
	}
	th := math.Acos(d)
	s := math.Sin(th)
	a, b := math.Sin((1-t)*th)/s, math.Sin(t*th)/s
	return Quat{a*q[0] + b*r[0], a*q[1] + b*r[1], a*q[2] + b*r[2], a*q[3] + b*r[3]}
}

// Angle returns the rotation angle of q in degrees, 0 to 180.
func (q Quat) Angle() float64 {
	w := math.Min(1, math.Abs(q.Normalize()[3]))
	return 2 * math.Acos(w) * 180 / math.Pi
}

// Mat4 is a row-major affine transform.
type Mat4 [4][4]float64

// Matrix returns the rotation matrix of q.
func (q Quat) Matrix() Mat4 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	return Mat4{
		{1 - 2*(y*y+z*z), 2 * (x*y - z*w), 2 * (x*z + y*w), 0},
		{2 * (x*y + z*w), 1 - 2*(x*x+z*z), 2 * (y*z - x*w), 0},
		{2 * (x*z - y*w), 2 * (y*z + x*w), 1 - 2*(x*x+y*y), 0},
		{0, 0, 0, 1},
	}
}

// TRS returns the transform that scales by s, rotates by r, then moves by t.
func TRS(t Vec3, r Quat, s Vec3) Mat4 {
	m := r.Matrix()
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			m[i][j] *= s[j]
		}
		m[i][3] = t[i]
	}
	return m
}

// Mul returns a*b.
func (a Mat4) Mul(b Mat4) Mat4 {
	var c Mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			for k := 0; k < 4; k++ {
				c[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return c
}

// Apply transforms a point.
func (a Mat4) Apply(p Vec3) Vec3 {
	var o Vec3
	for i := 0; i < 3; i++ {
		o[i] = a[i][0]*p[0] + a[i][1]*p[1] + a[i][2]*p[2] + a[i][3]
	}
	return o
}

// ApplyDir transforms a direction (no translation).
func (a Mat4) ApplyDir(p Vec3) Vec3 {
	var o Vec3
	for i := 0; i < 3; i++ {
		o[i] = a[i][0]*p[0] + a[i][1]*p[1] + a[i][2]*p[2]
	}
	return o
}

// Origin returns the translation column.
func (a Mat4) Origin() Vec3 { return Vec3{a[0][3], a[1][3], a[2][3]} }

// Linspace matches NumPy's linspace: start + i*step, the last value exactly
// stop. num must be at least 2.
func Linspace(start, stop float64, num int) []float64 {
	out := make([]float64, num)
	step := (stop - start) / float64(num-1)
	for i := range out {
		out[i] = float64(i)*step + start
	}
	out[num-1] = stop
	return out
}

// Smoothstep is 0 below a, 1 above b and eased between.
func Smoothstep(t, a, b float64) float64 {
	f := math.Max(0, math.Min(1, (t-a)/(b-a)))
	return f * f * (3 - 2*f)
}
