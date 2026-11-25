package animgraph

import "math"

// EndAimLimits bounds how far AimEnd may turn an end bone from its rest pose
// and how much twist it may hand to the mid bone. All three are radians; zero
// allows none.
type EndAimLimits struct {
	// MaxSwing is the largest turn of the end bone away from the chain axis.
	MaxSwing float64
	// MaxTwist is the largest turn of the end bone about the chain axis.
	MaxTwist float64
	// ForearmTwist is the largest extra twist the mid bone takes about the
	// chain axis when the palm needs more than MaxTwist.
	ForearmTwist float64
}

// AimEnd is the end-bone aim step for a two-bone chain, the same math the
// browser runs after the reach. Rotations are unit quaternions [x, y, z, w].
//
// endWorld is the end bone's world rotation after the reach, parentWorld its
// parent's (the mid bone's), and restLocal its local rotation at rest.
// palmLocal is the palm axis in the end bone's frame. twistAxis is the unit
// direction of the end bone in the mid bone's frame (the forearm axis).
// normal is the world direction the palm should face; a zero-length normal
// or a weight of zero or less is no target. weight in [0, 1] scales the
// turn.
//
// It returns the end bone's new local rotation and the twist, in radians
// about twistAxis, to add to the mid bone (Lmid = Lmid * R(twistAxis, twist)).
// The mid twist turns the end bone about an axis through the wrist, so the
// solved end position does not move.
//
// Without a target the result is the input local rotation, parentWorld^-1 *
// endWorld. With one, a clip pose already outside the limits is pulled inside
// them as soon as the weight is above zero, because the limits are measured
// from the rest pose, not from the clip.
func AimEnd(endWorld, parentWorld, restLocal [4]float64, palmLocal, twistAxis, normal [3]float64, weight float64, limits EndAimLimits) (local [4]float64, midTwist float64) {
	parentInv := quatConj(parentWorld)
	local = quatMul(parentInv, endWorld)
	nl := math.Sqrt(normal[0]*normal[0] + normal[1]*normal[1] + normal[2]*normal[2])
	if !(weight > 0) || !(nl >= 1e-6) {
		return local, 0
	}
	if weight > 1 {
		weight = 1
	}
	axis := vecUnit(twistAxis)
	palm := quatRotate(endWorld, palmLocal)
	turn := quatFromTo(palm, normal, weight)
	// L* = P^-1 D P L = P^-1 D H.
	wanted := quatMul(parentInv, quatMul(turn, endWorld))
	swing, twistAngle := splitSwingTwist(quatMul(wanted, quatConj(restLocal)), axis)
	if quatAngle(swing) > limits.MaxSwing {
		swing = quatScaleAngle(swing, limits.MaxSwing)
	}
	kept := math.Max(-limits.MaxTwist, math.Min(limits.MaxTwist, twistAngle))
	midTwist = math.Max(-limits.ForearmTwist, math.Min(limits.ForearmTwist, twistAngle-kept))
	// The wanted end rotation is P * R(kept + midTwist) * swing * rest. The
	// mid bone's twist already supplies R(midTwist) to the end bone's world
	// rotation, so the end bone's own local rotation is R(kept) * swing * rest.
	local = quatNormalize(quatMul(quatAxisAngle(axis, kept), quatMul(swing, restLocal)))
	return local, midTwist
}

var quatIdentity = [4]float64{0, 0, 0, 1}

func quatMul(a, b [4]float64) [4]float64 {
	return [4]float64{
		a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
		a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
		a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
		a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
	}
}

func quatConj(q [4]float64) [4]float64 { return [4]float64{-q[0], -q[1], -q[2], q[3]} }

func quatNormalize(q [4]float64) [4]float64 {
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if n < 1e-12 {
		return quatIdentity
	}
	return [4]float64{q[0] / n, q[1] / n, q[2] / n, q[3] / n}
}

func quatRotate(q [4]float64, v [3]float64) [3]float64 {
	tx, ty, tz := 2*(q[1]*v[2]-q[2]*v[1]), 2*(q[2]*v[0]-q[0]*v[2]), 2*(q[0]*v[1]-q[1]*v[0])
	return [3]float64{
		v[0] + q[3]*tx + (q[1]*tz - q[2]*ty),
		v[1] + q[3]*ty + (q[2]*tx - q[0]*tz),
		v[2] + q[3]*tz + (q[0]*ty - q[1]*tx),
	}
}

func quatAxisAngle(axis [3]float64, angle float64) [4]float64 {
	s, c := math.Sincos(angle / 2)
	return [4]float64{axis[0] * s, axis[1] * s, axis[2] * s, c}
}

// quatAngle is the rotation angle of q in [0, pi].
func quatAngle(q [4]float64) float64 {
	return 2 * math.Atan2(math.Sqrt(q[0]*q[0]+q[1]*q[1]+q[2]*q[2]), math.Abs(q[3]))
}

// quatScaleAngle keeps q's axis and sets its angle to angle (at most pi).
func quatScaleAngle(q [4]float64, angle float64) [4]float64 {
	n := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2])
	if n < 1e-12 {
		return quatIdentity
	}
	sign := 1.0
	if q[3] < 0 {
		sign = -1
	}
	s, c := math.Sincos(angle / 2)
	k := sign * s / n
	return [4]float64{q[0] * k, q[1] * k, q[2] * k, c}
}

func vecUnit(v [3]float64) [3]float64 {
	n := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if n < 1e-12 {
		return [3]float64{}
	}
	return [3]float64{v[0] / n, v[1] / n, v[2] / n}
}

// quatFromTo is the rotation that takes direction a toward direction b by
// weight times the angle between them (exact, not a blend).
func quatFromTo(a, b [3]float64, weight float64) [4]float64 {
	la := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
	lb := math.Sqrt(b[0]*b[0] + b[1]*b[1] + b[2]*b[2])
	if la < 1e-6 || lb < 1e-6 || !(weight > 0) {
		return quatIdentity
	}
	ax, ay, az := a[0]/la, a[1]/la, a[2]/la
	bx, by, bz := b[0]/lb, b[1]/lb, b[2]/lb
	x, y, z := ay*bz-az*by, az*bx-ax*bz, ax*by-ay*bx
	d := ax*bx + ay*by + az*bz
	cross := math.Sqrt(x*x + y*y + z*z)
	s := cross
	if s < 1e-9 {
		if d > 0 {
			return quatIdentity
		}
		// Opposite directions: any axis perpendicular to a.
		x, y, z = 0, -az, ay
		if math.Sqrt(y*y+z*z) < 1e-6 {
			x, y, z = az, 0, -ax
		}
		s = math.Sqrt(x*x + y*y + z*z)
	}
	half := 0.5 * weight * math.Atan2(cross, d)
	k := math.Sin(half) / s
	return [4]float64{x * k, y * k, z * k, math.Cos(half)}
}

// splitSwingTwist writes q as twist * swing, where twist turns about the unit
// axis. It returns the swing (a turn about an axis perpendicular to axis) and
// the twist angle in [-pi, pi].
func splitSwingTwist(q [4]float64, axis [3]float64) (swing [4]float64, twist float64) {
	q = quatNormalize(q)
	if q[3] < 0 {
		q = [4]float64{-q[0], -q[1], -q[2], -q[3]}
	}
	s := q[0]*axis[0] + q[1]*axis[1] + q[2]*axis[2]
	twist = 2 * math.Atan2(s, q[3])
	tq := quatAxisAngle(axis, twist)
	swing = quatMul(quatConj(tq), q)
	if swing[3] < 0 {
		swing = [4]float64{-swing[0], -swing[1], -swing[2], -swing[3]}
	}
	return swing, twist
}

// swingTwistAngles reports a rotation's swing angle and twist angle about
// axis, for tests and callers that check limits.
func swingTwistAngles(q [4]float64, axis [3]float64) (swing, twist float64) {
	s, twist := splitSwingTwist(q, vecUnit(axis))
	return quatAngle(s), twist
}
