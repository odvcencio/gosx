package motion

import "math"

// EaseKind identifies the easing function.
type EaseKind uint8

const (
	EaseLinear      EaseKind = iota
	EaseInPow                // Args[0] = exponent (default 2)
	EaseOutPow               // Args[0] = exponent (default 2)
	EaseInOutPow             // Args[0] = exponent (default 2)
	EaseCubicBezier          // Args = [x1, y1, x2, y2]
	EaseSteps                // Args[0] = number of steps n (>=1)
	EaseInBack
	EaseOutBack
	EaseInOutBack // Args[0] = overshoot s (default 1.70158)
)

// Ease describes a single easing function.
// Apply is allocation-free on the hot path.
type Ease struct {
	Kind EaseKind
	Args []float64
}

// Apply maps t ∈ [0,1] to the eased value. t is clamped to [0,1].
func (e Ease) Apply(t float64) float64 {
	// clamp
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}

	switch e.Kind {
	case EaseLinear:
		return t

	case EaseInPow:
		p := powArg(e.Args)
		return math.Pow(t, p)

	case EaseOutPow:
		p := powArg(e.Args)
		return 1 - math.Pow(1-t, p)

	case EaseInOutPow:
		p := powArg(e.Args)
		if t < 0.5 {
			return 0.5 * math.Pow(2*t, p)
		}
		return 1 - 0.5*math.Pow(2-2*t, p)

	case EaseCubicBezier:
		x1, y1, x2, y2 := 0.0, 0.0, 0.0, 0.0
		if len(e.Args) >= 4 {
			x1, y1, x2, y2 = e.Args[0], e.Args[1], e.Args[2], e.Args[3]
		}
		u := solveCubicBezierX(t, x1, x2)
		return cubicBezierValue(u, y1, y2)

	case EaseSteps:
		n := 4.0
		if len(e.Args) >= 1 && e.Args[0] >= 1 {
			n = e.Args[0]
		}
		return math.Floor(t*n) / n

	case EaseInBack:
		s := backArg(e.Args)
		c3 := s + 1
		return c3*t*t*t - s*t*t

	case EaseOutBack:
		s := backArg(e.Args)
		c3 := s + 1
		t1 := t - 1
		return 1 + c3*t1*t1*t1 + s*t1*t1

	case EaseInOutBack:
		s := backArg(e.Args)
		s2 := s * 1.525
		c3 := s2 + 1
		if t < 0.5 {
			tt := 2 * t
			return 0.5 * (c3*tt*tt*tt - s2*tt*tt)
		}
		tt := 2*t - 2
		return 0.5 * (c3*tt*tt*tt + s2*tt*tt) + 1

	default:
		return t
	}
}

// powArg returns the exponent from Args, defaulting to 2.
func powArg(args []float64) float64 {
	if len(args) >= 1 {
		return args[0]
	}
	return 2
}

// backArg returns the overshoot from Args, defaulting to 1.70158.
func backArg(args []float64) float64 {
	if len(args) >= 1 {
		return args[0]
	}
	return 1.70158
}

// cubicBezierValue evaluates the Y (or X) component of the cubic bezier
// curve with control points P0=(0,0), P1=(c1,c1), P2=(c2,c2), P3=(1,1)
// at parameter u.
//
// bezierValue(u) = 3*(1-u)^2*u*c1 + 3*(1-u)*u^2*c2 + u^3
func cubicBezierValue(u, c1, c2 float64) float64 {
	v := 1 - u
	return 3*v*v*u*c1 + 3*v*u*u*c2 + u*u*u
}

// solveCubicBezierX finds the bezier parameter u such that bezierX(u) ≈ x,
// using Newton-Raphson with bisection fallback.
func solveCubicBezierX(x, x1, x2 float64) float64 {
	// Initial guess: linear
	u := x
	// Newton-Raphson iterations
	for i := 0; i < 8; i++ {
		bx := cubicBezierValue(u, x1, x2) - x
		// derivative of bezierX at u
		deriv := cubicBezierDerivX2(u, x1, x2)
		if math.Abs(deriv) < 1e-10 {
			break
		}
		u -= bx / deriv
		if u < 0 {
			u = 0
		} else if u > 1 {
			u = 1
		}
		if math.Abs(bx) < 1e-7 {
			break
		}
	}
	// Bisection fallback to ensure accuracy
	if math.Abs(cubicBezierValue(u, x1, x2)-x) > 1e-5 {
		lo, hi := 0.0, 1.0
		for i := 0; i < 30; i++ {
			mid := (lo + hi) * 0.5
			bx := cubicBezierValue(mid, x1, x2)
			if bx < x {
				lo = mid
			} else {
				hi = mid
			}
			if hi-lo < 1e-7 {
				break
			}
		}
		u = (lo + hi) * 0.5
	}
	return u
}

// cubicBezierDerivX2 returns d/du of cubicBezierValue(u, x1, x2).
// bezierX(u) = 3*(1-u)^2*u*x1 + 3*(1-u)*u^2*x2 + u^3
// d/du = 3*x1*(1-u)*(1-3u) + 3*x2*u*(2-3u) + 3*u^2
func cubicBezierDerivX2(u, x1, x2 float64) float64 {
	return 3*x1*(1-u)*(1-3*u) + 3*x2*u*(2-3*u) + 3*u*u
}
