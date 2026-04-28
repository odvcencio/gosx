package physics

import "math"

const ccdSlop = 1e-4

type ccdHit struct {
	Collider *Collider
	Normal   Vec3
	Distance float64
}

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
		for _, target := range w.colliders {
			if target == nil || target == moving || target.Body == body || target.IsTrigger || !staticCollider(target) {
				continue
			}
			hit, ok := sweepSphereLikeCollider(origin, radius, direction, distance, target)
			if !ok {
				continue
			}
			if !found || hit.Distance < best.Distance {
				best = hit
				found = true
			}
		}
	}
	return best, found
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
		return ccdHit{Collider: target, Normal: normal, Distance: 0}, true
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
		normal := origin.Sub(center).Normalize()
		if normal.Len2() <= epsilon {
			normal = direction.Neg()
		}
		return ccdHit{Collider: target, Normal: normal, Distance: 0}, true
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
	if tMin < 0 || tMin > maxDistance {
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
