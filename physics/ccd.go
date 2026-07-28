package physics

import "math"

const ccdSlop = 1e-4

// ccdMotionRatio is the share of a body's own radius that one step of motion
// must exceed before the swept pass runs.
//
// A body that moves less than half its own radius cannot pass through anything,
// so the discrete contacts already hold it. Skipping the sweep for such a body
// does two things: it removes the cost of the sweep from every resting body,
// and it stops the sweep from clamping the lateral motion of a body that leans
// on a static box.
const ccdMotionRatio = 0.5

type ccdHit struct {
	Collider *Collider
	Normal   Vec3
	Distance float64
}

// bodyNeedsSweep reports whether one step of the given displacement is long
// enough to justify a swept test.
func bodyNeedsSweep(body *RigidBody, displacement Vec3) bool {
	if body == nil {
		return false
	}
	travel2 := displacement.Len2()
	if travel2 <= epsilon {
		return false
	}
	largest := 0.0
	for _, collider := range body.colliders {
		radius, ok := movingSweepRadius(collider)
		if !ok || collider.IsTrigger {
			continue
		}
		if radius > largest {
			largest = radius
		}
	}
	if largest <= 0 {
		return false
	}
	threshold := ccdMotionRatio * largest
	return travel2 > threshold*threshold
}

// sweepBody finds the nearest static collider that the body's swept volume
// reaches this step.
//
// Candidates come from the broadphase's static cell map, which holds only
// immovable, non-trigger colliders. That keeps the per-body cost proportional
// to the swept volume instead of to the whole collider list. The caller must
// have rebuilt the broadphase for the current step; ensureBroadphase does that.
func (w *World) sweepBody(body *RigidBody, displacement Vec3) (ccdHit, bool) {
	if w == nil || body == nil || displacement.Len2() <= epsilon {
		return ccdHit{}, false
	}
	distance := displacement.Len()
	direction := displacement.Div(distance)
	best := ccdHit{Distance: math.Inf(1)}
	found := false
	for _, moving := range body.colliders {
		if moving == nil || moving.IsTrigger {
			continue
		}
		origin := moving.WorldCenter()
		radius, ok := movingSweepRadius(moving)
		if !ok {
			continue
		}
		w.sweepTargets = w.broadphase.QueryStaticAABB(
			sweptSphereBounds(origin, radius, direction, distance),
			w.sweepTargets[:0],
		)
		for _, target := range w.sweepTargets {
			if target == moving || target.Body == body {
				continue
			}
			hit, ok := sweepSphereLikeCollider(origin, radius, direction, distance, target)
			if !ok {
				continue
			}
			if !found || closerSweepHit(hit, best) {
				best = hit
				found = true
			}
		}
	}
	return best, found
}

// closerSweepHit decides which of two swept hits wins. Ties break on the lower
// collider index so the result does not depend on the order the broadphase
// happens to report candidates in. Replay determinism needs that.
func closerSweepHit(candidate, best ccdHit) bool {
	if candidate.Distance < best.Distance-epsilon {
		return true
	}
	if candidate.Distance > best.Distance+epsilon {
		return false
	}
	return colliderIndex(candidate.Collider) < colliderIndex(best.Collider)
}

// sweptSphereBounds returns the world box that encloses a sphere swept from
// origin along direction for the given distance. Every point the sweep can
// touch lies inside this box, so it is safe to reject anything outside it.
func sweptSphereBounds(origin Vec3, radius float64, direction Vec3, distance float64) AABB {
	end := origin.Add(direction.Mul(distance))
	box := AABB{Min: origin.Min(end), Max: origin.Max(end)}
	pad := math.Abs(radius)
	return box.Expand(pad)
}

func staticCollider(c *Collider) bool {
	return c.Body == nil || !c.Body.IsDynamic()
}

func movingSweepRadius(c *Collider) (float64, bool) {
	if c == nil {
		return 0, false
	}
	switch c.Shape {
	case ShapeSphere:
		return math.Abs(c.Radius), true
	case ShapeCapsule:
		return math.Abs(c.Radius) + math.Abs(c.Height)*0.5, true
	default:
		return 0, false
	}
}

func sweepSphereLikeCollider(origin Vec3, radius float64, direction Vec3, maxDistance float64, target *Collider) (ccdHit, bool) {
	if target == nil || direction.Len2() <= epsilon {
		return ccdHit{}, false
	}
	switch target.Shape {
	case ShapePlane:
		return sweepSpherePlane(origin, radius, direction, maxDistance, target)
	case ShapeSphere:
		return sweepSphereSphere(origin, radius, direction, maxDistance, target)
	case ShapeBox:
		return sweepSphereBox(origin, radius, direction, maxDistance, target)
	default:
		return ccdHit{}, false
	}
}

func sweepSpherePlane(origin Vec3, radius float64, direction Vec3, maxDistance float64, target *Collider) (ccdHit, bool) {
	normal, planeDistance := target.Plane()
	signed := normal.Dot(origin) - planeDistance
	if signed <= radius {
		// The sphere already touches the plane. Report no swept hit and leave
		// the case to the discrete contact solver. A zero-distance hit here
		// would clamp the whole step, which freezes a resting body sideways.
		return ccdHit{}, false
	}
	denom := normal.Dot(direction)
	if denom >= -epsilon {
		return ccdHit{}, false
	}
	distance := (radius - signed) / denom
	if distance < 0 || distance > maxDistance {
		return ccdHit{}, false
	}
	return ccdHit{Collider: target, Normal: normal, Distance: distance}, true
}

func sweepSphereSphere(origin Vec3, radius float64, direction Vec3, maxDistance float64, target *Collider) (ccdHit, bool) {
	center := target.WorldCenter()
	combined := radius + math.Abs(target.Radius)
	m := origin.Sub(center)
	b := m.Dot(direction)
	c := m.Dot(m) - combined*combined
	if c <= 0 {
		// Already overlapping. The discrete solver owns this case.
		return ccdHit{}, false
	}
	if b > 0 {
		return ccdHit{}, false
	}
	discriminant := b*b - c
	if discriminant < 0 {
		return ccdHit{}, false
	}
	distance := -b - math.Sqrt(discriminant)
	if distance < 0 || distance > maxDistance {
		return ccdHit{}, false
	}
	point := origin.Add(direction.Mul(distance))
	normal := point.Sub(center).Normalize()
	if normal.Len2() <= epsilon {
		normal = direction.Neg()
	}
	return ccdHit{Collider: target, Normal: normal, Distance: distance}, true
}

func sweepSphereBox(origin Vec3, radius float64, direction Vec3, maxDistance float64, target *Collider) (ccdHit, bool) {
	half := target.halfExtents()
	if half.X <= 0 || half.Y <= 0 || half.Z <= 0 {
		return ccdHit{}, false
	}
	half = half.Add(Vec3{X: radius, Y: radius, Z: radius})
	rotation := target.WorldRotation()
	inv := rotation.Inverse()
	localOrigin := inv.Rotate(origin.Sub(target.WorldCenter()))
	localDirection := inv.Rotate(direction)

	tMin := 0.0
	tMax := maxDistance
	enterNormal := Vec3{}
	axes := [3]Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	mins := [3]float64{-half.X, -half.Y, -half.Z}
	maxs := [3]float64{half.X, half.Y, half.Z}
	origins := [3]float64{localOrigin.X, localOrigin.Y, localOrigin.Z}
	dirs := [3]float64{localDirection.X, localDirection.Y, localDirection.Z}

	for i := 0; i < 3; i++ {
		if math.Abs(dirs[i]) <= epsilon {
			if origins[i] < mins[i] || origins[i] > maxs[i] {
				return ccdHit{}, false
			}
			continue
		}
		invD := 1 / dirs[i]
		t1 := (mins[i] - origins[i]) * invD
		t2 := (maxs[i] - origins[i]) * invD
		n1 := axes[i].Neg()
		n2 := axes[i]
		if t1 > t2 {
			t1, t2 = t2, t1
			n1, n2 = n2, n1
		}
		if t1 > tMin {
			tMin = t1
			enterNormal = n1
		}
		if t2 < tMax {
			tMax = t2
		}
		if tMin > tMax {
			return ccdHit{}, false
		}
	}
	if tMin <= 0 || tMin > maxDistance {
		// tMin stays at zero when the swept sphere starts inside the expanded
		// box. Report no hit there: a zero-distance hit clamps the whole step
		// and freezes a body that rests on the box.
		return ccdHit{}, false
	}
	normal := rotation.Rotate(enterNormal).Normalize()
	if normal.Len2() <= epsilon {
		normal = direction.Neg()
	}
	return ccdHit{Collider: target, Normal: normal, Distance: tMin}, true
}

func resolveCCDVelocity(body *RigidBody, hit ccdHit) {
	if body == nil {
		return
	}
	normal := hit.Normal.Normalize()
	if normal.Len2() <= epsilon {
		return
	}
	vn := body.Velocity.Dot(normal)
	if vn >= 0 {
		return
	}
	restitution := body.Restitution
	if hit.Collider != nil && hit.Collider.Body != nil && hit.Collider.Body.Restitution > restitution {
		restitution = hit.Collider.Body.Restitution
	}
	body.Velocity = body.Velocity.Sub(normal.Mul((1 + restitution) * vn))
}
