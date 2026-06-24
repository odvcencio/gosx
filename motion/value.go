package motion

// Width returns the number of float64 components for an arity.
func (a ValueArity) Width() int {
	switch a {
	case ArityScalar:
		return 1
	case ArityVec2:
		return 2
	case ArityVec3:
		return 3
	case ArityVec4, ArityQuat, ArityColor:
		return 4
	default:
		return 1
	}
}

// Constructors — keep call sites clean and guard unused trailing components at zero.

func ScalarV(x float64) Value {
	return Value{ArityScalar, [4]float64{x}}
}

func Vec2V(x, y float64) Value {
	return Value{ArityVec2, [4]float64{x, y}}
}

func Vec3V(x, y, z float64) Value {
	return Value{ArityVec3, [4]float64{x, y, z}}
}

func Vec4V(x, y, z, w float64) Value {
	return Value{ArityVec4, [4]float64{x, y, z, w}}
}

func QuatV(q Quat) Value {
	return Value{ArityQuat, [4]float64{q.X, q.Y, q.Z, q.W}}
}

func ColorV(r, g, b, a float64) Value {
	return Value{ArityColor, [4]float64{r, g, b, a}}
}

// LerpValueInto writes the interpolation of a→b at t into dst.
// dst must have length >= a.Arity.Width().
// Quat arity routes through Slerp; all others use component-wise linear lerp.
// NO heap allocation.
func LerpValueInto(dst []float64, a, b Value, t float64) {
	if a.Arity == ArityQuat {
		qa := Quat{X: a.F[0], Y: a.F[1], Z: a.F[2], W: a.F[3]}
		qb := Quat{X: b.F[0], Y: b.F[1], Z: b.F[2], W: b.F[3]}
		q := Slerp(qa, qb, t)
		dst[0] = q.X
		dst[1] = q.Y
		dst[2] = q.Z
		dst[3] = q.W
		return
	}
	n := a.Arity.Width()
	for i := 0; i < n; i++ {
		dst[i] = a.F[i] + t*(b.F[i]-a.F[i])
	}
}

// StepInto copies a's components into dst (step interp holds the previous key value).
// NO heap allocation.
func StepInto(dst []float64, a Value) {
	n := a.Arity.Width()
	for i := 0; i < n; i++ {
		dst[i] = a.F[i]
	}
}

// CubicHermiteInto writes the glTF CUBICSPLINE Hermite interpolation into dst.
//
// For a segment between key k (value vK, out-tangent bK) and key k+1 (value
// vK1, in-tangent aK1), with segment duration delta and local parameter
// s = (t - t_k)/delta ∈ [0,1], the per-component spline is:
//
//	p(s) = (2s³-3s²+1)·vK + delta·(s³-2s²+s)·bK
//	     + (-2s³+3s²)·vK1 + delta·(s³-s²)·aK1
//
// dst must have length >= vK.Arity.Width(). For ArityQuat the four components
// are interpolated independently then normalized (per the glTF spec). NO heap
// allocation — all scalars are stack locals.
func CubicHermiteInto(dst []float64, vK, bK, vK1, aK1 Value, delta, s float64) {
	s2 := s * s
	s3 := s2 * s
	// Hermite basis coefficients.
	h00 := 2*s3 - 3*s2 + 1 // for vK
	h10 := s3 - 2*s2 + s   // for delta·bK
	h01 := -2*s3 + 3*s2    // for vK1
	h11 := s3 - s2         // for delta·aK1
	n := vK.Arity.Width()
	for i := 0; i < n; i++ {
		dst[i] = h00*vK.F[i] + delta*h10*bK.F[i] + h01*vK1.F[i] + delta*h11*aK1.F[i]
	}
	if vK.Arity == ArityQuat {
		q := Quat{X: dst[0], Y: dst[1], Z: dst[2], W: dst[3]}.Normalize()
		dst[0] = q.X
		dst[1] = q.Y
		dst[2] = q.Z
		dst[3] = q.W
	}
}

// LerpValue is a convenience wrapper for tests and non-hot paths.
// Returns a Value by value — zero heap allocation (POD array copy).
// Do NOT use on the per-frame interpolation hot path; use LerpValueInto instead.
func LerpValue(a, b Value, t float64) Value {
	var scratch [4]float64
	LerpValueInto(scratch[:], a, b, t)
	return Value{Arity: a.Arity, F: scratch}
}
