package teamai

import "m31labs.dev/gosx/det"

// The steering functions are pure: they read their arguments, return a value,
// and never allocate. Velocities are in units per second, accelerations in
// units per second squared, and times in seconds.

// Seek returns the velocity that moves from pos straight at target at speed.
// It returns zero when pos is on target.
func Seek(pos, target Vec2, speed float64) Vec2 {
	return target.Sub(pos).Normalize().Scale(speed)
}

// Flee returns the velocity that moves from pos straight away from threat at
// speed.
func Flee(pos, threat Vec2, speed float64) Vec2 {
	return pos.Sub(threat).Normalize().Scale(speed)
}

// Arrive returns the velocity that moves from pos toward target at up to
// maxSpeed and slows so it can stop on target with deceleration decel. Zero or
// negative decel means the agent can stop at once. Within tol of target it
// returns zero.
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

// LimitAccel returns the velocity reached from cur toward desired in one step of
// dt seconds with acceleration at most maxAccel. Zero or negative maxAccel
// returns desired.
func LimitAccel(cur, desired Vec2, maxAccel, dt float64) Vec2 {
	if maxAccel <= 0 {
		return desired
	}
	return cur.Add(desired.Sub(cur).ClampLen(float64(maxAccel * dt)))
}

// Integrate moves b one step of dt seconds toward the commanded velocity cmd,
// limited by b.MaxSpeed and b.MaxAccel. Hosts that move agents with physics
// apply the command through their character controller instead.
func Integrate(b *Body, cmd Vec2, dt float64) {
	b.Vel = LimitAccel(b.Vel, cmd.ClampLen(b.MaxSpeed), b.MaxAccel, dt)
	b.Pos = b.Pos.AddScaled(b.Vel, dt)
}

// Reach returns how far a mover can travel in t seconds when it starts at
// speed v0 and accelerates at accel up to maxSpeed. It assumes the mover can
// turn freely. Zero or negative accel means it is at maxSpeed at once.
func Reach(v0, maxSpeed, accel, t float64) float64 {
	if t <= 0 {
		return 0
	}
	if accel <= 0 || v0 >= maxSpeed {
		return float64(maxSpeed * t)
	}
	if v0 < 0 {
		v0 = 0
	}
	ramp := (maxSpeed - v0) / accel
	if t <= ramp {
		return float64(v0*t) + float64(0.5*float64(float64(accel*t)*t))
	}
	ramped := float64(v0*ramp) + float64(0.5*float64(float64(accel*ramp)*ramp))
	return ramped + float64(maxSpeed*(t-ramp))
}

// Interception is the result of Intercept and Cutoff.
type Interception struct {
	// Point is where the pursuer should run. When OK is false it is the best
	// fallback: the target's predicted position at Time.
	Point Vec2
	// Time is when the pursuer gets to Point, in seconds from now.
	Time float64
	// Dir is the unit direction from the pursuer to Point: the pursuit or
	// cutoff angle.
	Dir Vec2
	// OK reports that the pursuer can reach the target within the horizon.
	OK bool
}

// interceptScan and interceptBisect fix the search cost of an accelerating
// interception: interceptScan samples bracket the first reachable time and
// interceptBisect halvings refine it, about 56 square roots in all.
const (
	interceptScan   = 32
	interceptBisect = 24
)

// Intercept finds the earliest time within horizon seconds at which a pursuer
// at pos, moving at speed v0 now, with top speed maxSpeed and acceleration
// accel, can be where a target at tpos moving at constant velocity tvel will
// be. It leads the target: Point is the predicted meeting point.
func Intercept(pos Vec2, v0, maxSpeed, accel float64, tpos, tvel Vec2, horizon float64) Interception {
	return searchIntercept(pos, v0, maxSpeed, accel, tpos, tvel, 0, horizon, horizon)
}

// Cutoff is Intercept against a target that stops at the edge of bounds: the
// target runs in a straight line until it leaves bounds, and the pursuer must
// arrive margin seconds before the target. When the pursuer cannot cut the
// target off inside bounds, Point is where the target leaves bounds (or where
// it is at the horizon) and OK is false: running there is the angle that
// keeps the pursuer between the target and the boundary for longest.
func Cutoff(pos Vec2, v0, maxSpeed, accel float64, tpos, tvel Vec2, bounds Rect, margin, horizon float64) Interception {
	end := horizon
	if speed := tvel.Len(); speed > epsilon && bounds.Valid() {
		if exit := bounds.Exit(tpos, tvel.Scale(1/speed)) / speed; exit < end {
			end = exit
		}
	}
	if margin < 0 {
		margin = 0
	}
	return searchIntercept(pos, v0, maxSpeed, accel, tpos, tvel, margin, end, end)
}

// searchIntercept finds the smallest t in [margin, end] with
// Reach(t-margin) >= |tpos + tvel*t - pos|. fallback is the time whose target
// position is returned when there is none.
func searchIntercept(pos Vec2, v0, maxSpeed, accel float64, tpos, tvel Vec2, margin, end, fallback float64) Interception {
	gap := func(t float64) float64 {
		return Reach(v0, maxSpeed, accel, t-margin) - tpos.AddScaled(tvel, t).Dist(pos)
	}
	if end < margin || maxSpeed <= 0 {
		return miss(pos, tpos, tvel, det.Max(fallback, 0))
	}
	if margin == 0 && (accel <= 0 || v0 >= maxSpeed) {
		if t, ok := quadraticIntercept(tpos.Sub(pos), tvel, maxSpeed); ok && t <= end {
			return hit(pos, tpos.AddScaled(tvel, t), t)
		}
		return miss(pos, tpos, tvel, fallback)
	}
	if gap(margin) >= 0 {
		return hit(pos, tpos.AddScaled(tvel, margin), margin)
	}
	lo := margin
	step := (end - margin) / interceptScan
	found := false
	hi := end
	for i := 1; i <= interceptScan; i++ {
		t := margin + float64(step*float64(i))
		if gap(t) >= 0 {
			hi = t
			found = true
			break
		}
		lo = t
	}
	if !found {
		return miss(pos, tpos, tvel, fallback)
	}
	for i := 0; i < interceptBisect; i++ {
		mid := float64(0.5 * (lo + hi))
		if gap(mid) >= 0 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hit(pos, tpos.AddScaled(tvel, hi), hi)
}

// quadraticIntercept solves |d + v t| = s t for the smallest t >= 0.
func quadraticIntercept(d, v Vec2, s float64) (float64, bool) {
	c := d.Len2()
	if c <= epsilon {
		return 0, true
	}
	a := v.Len2() - float64(s*s)
	b := float64(2 * d.Dot(v))
	if a > -epsilon && a < epsilon {
		// Target as fast as the pursuer: the linear case b t + c = 0.
		if b >= 0 {
			return 0, false
		}
		return -c / b, true
	}
	disc := float64(b*b) - float64(4*float64(a*c))
	if disc < 0 {
		return 0, false
	}
	sq := det.Sqrt(disc)
	t1 := (-b - sq) / float64(2*a)
	t2 := (-b + sq) / float64(2*a)
	if t1 > t2 {
		t1, t2 = t2, t1
	}
	switch {
	case t1 >= 0:
		return t1, true
	case t2 >= 0:
		return t2, true
	}
	return 0, false
}

func hit(pos, point Vec2, t float64) Interception {
	return Interception{Point: point, Time: t, Dir: point.Sub(pos).Normalize(), OK: true}
}

func miss(pos, tpos, tvel Vec2, t float64) Interception {
	p := tpos.AddScaled(tvel, t)
	return Interception{Point: p, Time: t, Dir: p.Sub(pos).Normalize()}
}

// Pursue returns the velocity that runs at maxSpeed toward the interception
// point of a target, or straight at the target when no interception exists
// within horizon.
func Pursue(pos, vel Vec2, maxSpeed, accel float64, tpos, tvel Vec2, horizon float64) Vec2 {
	in := Intercept(pos, vel.Len(), maxSpeed, accel, tpos, tvel, horizon)
	if !in.OK {
		return Seek(pos, tpos, maxSpeed)
	}
	return in.Dir.Scale(maxSpeed)
}

// Separation returns the push away from the active bodies in others that are
// within radius of pos, skipping index self and, when team >= 0, bodies of
// other teams. Each neighbour adds a unit vector away from it scaled by
// (radius-d)/radius, so the push is 0 at the radius and 1 per neighbour at
// contact. Coincident bodies push apart along +X or -X by index, so the
// result does not depend on evaluation order.
func Separation(pos Vec2, self int, others []Body, radius float64, team int) Vec2 {
	if radius <= 0 {
		return Vec2{}
	}
	var push Vec2
	r2 := float64(radius * radius)
	for j := range others {
		o := &others[j]
		if j == self || !o.Active() || (team >= 0 && o.Team != team) {
			continue
		}
		d := pos.Sub(o.Pos)
		l2 := d.Len2()
		if l2 >= r2 {
			continue
		}
		if l2 <= epsilon {
			if self < j {
				push.X--
			} else {
				push.X++
			}
			continue
		}
		l := det.Sqrt(l2)
		push = push.AddScaled(d, (radius-l)/float64(radius*l))
	}
	return push
}

// GuardPoint returns where a guard of the zone centred at zone with the given
// radius should stand. While threat is inside the zone the guard stands gap
// units from the threat toward protect (the point the zone protects), and
// engaged is true; otherwise it stands at zone.
func GuardPoint(zone Vec2, radius float64, threat, protect Vec2, gap float64) (Vec2, bool) {
	if threat.Sub(zone).Len2() > float64(radius*radius) {
		return zone, false
	}
	dir := protect.Sub(threat).Normalize()
	if dir.IsZero() {
		return threat, true
	}
	return threat.AddScaled(dir, gap), true
}

// Shadow returns the velocity that holds pos at offset from a moving target:
// the target's velocity plus gain times the remaining error, limited to
// maxSpeed. gain is in 1/seconds; 3 closes about 95% of an error in one
// second.
func Shadow(pos, tpos, tvel, offset Vec2, maxSpeed, gain float64) Vec2 {
	want := tpos.Add(offset)
	return tvel.AddScaled(want.Sub(pos), gain).ClampLen(maxSpeed)
}

// ApproachAngle returns the angle in radians, from 0 to Pi, between an
// approach direction and the ideal direction of approach. Contest models use
// it: 0 is a perfect approach and Pi the worst. It returns 0 when either
// vector is zero.
func ApproachAngle(dir, ideal Vec2) float64 {
	a, b := dir.Normalize(), ideal.Normalize()
	if a.IsZero() || b.IsZero() {
		return 0
	}
	return det.Abs(det.Atan2(a.Cross(b), a.Dot(b)))
}
