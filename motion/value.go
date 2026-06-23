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

// LerpValue is an allocating convenience wrapper for tests and non-hot paths.
// Do NOT use on the per-frame interpolation hot path; use LerpValueInto instead.
func LerpValue(a, b Value, t float64) Value {
	n := a.Arity.Width()
	f := make([]float64, n)
	LerpValueInto(f, a, b, t)
	return Value{Arity: a.Arity, F: f}
}
