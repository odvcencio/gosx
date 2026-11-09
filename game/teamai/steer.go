package teamai

import "m31labs.dev/gosx/det"

// Seek returns the velocity that moves from pos straight at target at speed.
// It returns zero when pos is on target.
func Seek(pos, target Vec2, speed float64) Vec2 {
	return target.Sub(pos).Normalize().Scale(speed)
}

// Arrive returns the velocity that moves from pos toward target at up to
// maxSpeed and slows so it can stop on target with deceleration decel (units
// per second squared). Zero or negative decel means the agent can stop at
// once. Within tol of target it returns zero.
func Arrive(pos, target Vec2, maxSpeed, decel, tol float64) Vec2 {
	d := target.Sub(pos)
	dist := d.Len()
	if dist <= tol || dist <= epsilon {
		return Vec2{}
	}
	speed := maxSpeed
	if decel > 0 {
		if stop := det.Sqrt(float64(2 * float64(decel*(dist-tol)))); stop < speed {
			speed = stop
		}
	}
	return d.Scale(speed / dist)
}
