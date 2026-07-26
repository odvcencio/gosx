package physics

import "math"

// General 3x3 matrix helpers. mat3 stores three world-space column vectors, so
// mat3{x, y, z} is the matrix whose first column is x. support.go builds a mat3
// from a quaternion; the operations here let the solver treat the same type as
// an inertia tensor.

func mat3Identity() mat3 {
	return mat3{x: Vec3{X: 1}, y: Vec3{Y: 1}, z: Vec3{Z: 1}}
}

// mat3Diagonal returns the diagonal matrix with the given entries.
func mat3Diagonal(d Vec3) mat3 {
	return mat3{x: Vec3{X: d.X}, y: Vec3{Y: d.Y}, z: Vec3{Z: d.Z}}
}

func (m mat3) add(o mat3) mat3 {
	return mat3{x: m.x.Add(o.x), y: m.y.Add(o.y), z: m.z.Add(o.z)}
}

func (m mat3) scale(s float64) mat3 {
	return mat3{x: m.x.Mul(s), y: m.y.Mul(s), z: m.z.Mul(s)}
}

func (m mat3) transpose() mat3 {
	return mat3{
		x: Vec3{X: m.x.X, Y: m.y.X, Z: m.z.X},
		y: Vec3{X: m.x.Y, Y: m.y.Y, Z: m.z.Y},
		z: Vec3{X: m.x.Z, Y: m.y.Z, Z: m.z.Z},
	}
}

// mulMat returns m * o.
func (m mat3) mulMat(o mat3) mat3 {
	return mat3{x: m.mul(o.x), y: m.mul(o.y), z: m.mul(o.z)}
}

func (m mat3) isZero() bool {
	return m.x.Len2() == 0 && m.y.Len2() == 0 && m.z.Len2() == 0
}

// determinant returns the scalar determinant of the matrix.
func (m mat3) determinant() float64 {
	return m.x.Dot(m.y.Cross(m.z))
}

// inverse returns the matrix inverse. It reports false when the matrix is
// singular, which tells the caller to treat the body as unable to rotate.
func (m mat3) inverse() (mat3, bool) {
	c0 := m.y.Cross(m.z)
	c1 := m.z.Cross(m.x)
	c2 := m.x.Cross(m.y)
	det := m.x.Dot(c0)
	if math.Abs(det) <= 1e-18 {
		return mat3{}, false
	}
	inv := 1 / det
	// The inverse is the transposed cofactor matrix divided by the determinant.
	return mat3{
		x: Vec3{X: c0.X * inv, Y: c1.X * inv, Z: c2.X * inv},
		y: Vec3{X: c0.Y * inv, Y: c1.Y * inv, Z: c2.Y * inv},
		z: Vec3{X: c0.Z * inv, Y: c1.Z * inv, Z: c2.Z * inv},
	}, true
}

// outerProduct returns v * vᵀ, the matrix whose entry (i,j) is v_i * v_j.
func outerProduct(v Vec3) mat3 {
	return mat3{x: v.Mul(v.X), y: v.Mul(v.Y), z: v.Mul(v.Z)}
}

// rotateTensor returns R * t * Rᵀ, which moves a body-local tensor into world
// space. The solver calls this once per dynamic body per step.
func rotateTensor(r mat3, t mat3) mat3 {
	return r.mulMat(t).mulMat(r.transpose())
}

// solveSymmetric3 solves m * x = b for a symmetric positive definite m. It
// reports false when m is singular.
func solveSymmetric3(m mat3, b Vec3) (Vec3, bool) {
	inv, ok := m.inverse()
	if !ok {
		return Vec3{}, false
	}
	return inv.mul(b), true
}
