package camera

import "math"

// Vec3 is a world-space vector in scene units, Y up.
type Vec3 struct{ X, Y, Z float64 }

// Add returns a+b.
func (a Vec3) Add(b Vec3) Vec3 { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Sub returns a-b.
func (a Vec3) Sub(b Vec3) Vec3 { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

// Scale returns a*s.
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the dot product.
func (a Vec3) Dot(b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the cross product.
func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// Len returns the length.
func (a Vec3) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Norm returns a with length 1, or the zero vector if a has no length.
func (a Vec3) Norm() Vec3 {
	l := a.Len()
	if l < 1e-12 {
		return Vec3{}
	}
	return a.Scale(1 / l)
}

// Lerp returns a+(b-a)*t.
func Lerp(a, b Vec3, t float64) Vec3 { return a.Add(b.Sub(a).Scale(t)) }

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func finite(v ...float64) bool {
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

func (a Vec3) finite() bool { return finite(a.X, a.Y, a.Z) }

// Ease maps progress in [0,1] to eased progress in [0,1].
type Ease func(t float64) float64

// Built-in easings.
var (
	EaseLinear Ease = func(t float64) float64 { return t }
	// EaseSmooth is smoothstep: zero velocity at both ends.
	EaseSmooth Ease = func(t float64) float64 { return t * t * (3 - 2*t) }
	// EaseInOutCubic accelerates and decelerates more sharply than EaseSmooth.
	EaseInOutCubic Ease = func(t float64) float64 {
		if t < 0.5 {
			return 4 * t * t * t
		}
		u := -2*t + 2
		return 1 - u*u*u/2
	}
)

func (e Ease) at(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	if e == nil {
		return EaseSmooth(t)
	}
	return e(t)
}

// SmoothDamp moves cur toward target like a critically damped spring and
// returns the new value and velocity. smoothTime is roughly the time to reach
// the target. It does not overshoot and stays stable for any dt, including a
// long frame after a tab switch. A smoothTime of zero or less snaps.
func SmoothDamp(cur, target, vel, smoothTime, dt float64) (float64, float64) {
	if dt <= 0 {
		return cur, vel
	}
	if smoothTime <= 1e-6 {
		return target, 0
	}
	omega := 2 / smoothTime
	x := omega * dt
	e := 1 / (1 + x + 0.48*x*x + 0.235*x*x*x)
	change := cur - target
	temp := (vel + omega*change) * dt
	vel = (vel - omega*temp) * e
	out := target + (change+temp)*e
	// Guard against overshoot from the polynomial approximation.
	if (target-cur > 0) == (out > target) && out != target {
		out, vel = target, 0
	}
	return out, vel
}

// SmoothDamp3 is SmoothDamp on each axis of a Vec3.
func SmoothDamp3(cur, target, vel Vec3, smoothTime, dt float64) (Vec3, Vec3) {
	var out, v Vec3
	out.X, v.X = SmoothDamp(cur.X, target.X, vel.X, smoothTime, dt)
	out.Y, v.Y = SmoothDamp(cur.Y, target.Y, vel.Y, smoothTime, dt)
	out.Z, v.Z = SmoothDamp(cur.Z, target.Z, vel.Z, smoothTime, dt)
	return out, v
}

// Direction returns the unit vector from a focus point toward the camera for a
// yaw and pitch in radians. Yaw 0 puts the camera on +Z looking toward -Z;
// positive pitch lifts the camera above the focus.
func Direction(yaw, pitch float64) Vec3 {
	cp := math.Cos(pitch)
	return Vec3{math.Sin(yaw) * cp, math.Sin(pitch), math.Cos(yaw) * cp}
}
