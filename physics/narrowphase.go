package physics

import "math"

const contactTolerance = 1e-6

func Collide(a, b *Collider) (ContactManifold, bool) {
	if a == nil || b == nil {
		return ContactManifold{}, false
	}

	switch {
	case a.Shape == ShapeSphere && b.Shape == ShapeSphere:
		return collideSphereSphere(a, b)
	case a.Shape == ShapeSphere && b.Shape == ShapePlane:
		return collideSpherePlane(a, b)
	case a.Shape == ShapePlane && b.Shape == ShapeSphere:
		manifold, ok := collideSpherePlane(b, a)
		if !ok {
			return ContactManifold{}, false
		}
		return flipManifold(manifold), true
	case a.Shape == ShapeSphere && b.Shape == ShapeBox:
		return collideSphereBox(a, b)
	case a.Shape == ShapeBox && b.Shape == ShapeSphere:
		manifold, ok := collideSphereBox(b, a)
		if !ok {
			return ContactManifold{}, false
		}
		return flipManifold(manifold), true
	case a.Shape == ShapeBox && b.Shape == ShapePlane:
		return collideBoxPlane(a, b)
	case a.Shape == ShapePlane && b.Shape == ShapeBox:
		manifold, ok := collideBoxPlane(b, a)
		if !ok {
			return ContactManifold{}, false
		}
		return flipManifold(manifold), true
	case a.Shape == ShapeBox && b.Shape == ShapeBox:
		return collideBoxBox(a, b)
	case a.Shape == ShapeSphere && b.Shape == ShapeCapsule:
		return collideSphereCapsule(a, b)
	case a.Shape == ShapeCapsule && b.Shape == ShapeSphere:
		manifold, ok := collideSphereCapsule(b, a)
		if !ok {
			return ContactManifold{}, false
		}
		return flipManifold(manifold), true
	case a.Shape == ShapeCapsule && b.Shape == ShapePlane:
		return collideCapsulePlane(a, b)
	case a.Shape == ShapePlane && b.Shape == ShapeCapsule:
		manifold, ok := collideCapsulePlane(b, a)
		if !ok {
			return ContactManifold{}, false
		}
		return flipManifold(manifold), true
	case a.Shape == ShapeCapsule && b.Shape == ShapeCapsule:
		return collideCapsuleCapsule(a, b)
	default:
		return ContactManifold{}, false
	}
}

// closestPointOnSegment returns the point on segment [A,B] nearest to p,
// along with the parameter t in [0,1] where the closest point is A + t*(B-A).
func closestPointOnSegment(a, b, p Vec3) (Vec3, float64) {
	ab := b.Sub(a)
	lenSq := ab.Len2()
	if lenSq <= epsilon {
		return a, 0
	}
	t := p.Sub(a).Dot(ab) / lenSq
	t = clampFloat(t, 0, 1)
	return a.Add(ab.Mul(t)), t
}

// closestPointsBetweenSegments returns the closest points on segments
// [A1,A2] and [B1,B2]. Useful for capsule-capsule contact generation.
func closestPointsBetweenSegments(a1, a2, b1, b2 Vec3) (Vec3, Vec3) {
	d1 := a2.Sub(a1)
	d2 := b2.Sub(b1)
	r := a1.Sub(b1)
	a := d1.Dot(d1)
	e := d2.Dot(d2)
	f := d2.Dot(r)

	var s, t float64
	if a <= epsilon && e <= epsilon {
		return a1, b1
	}
	if a <= epsilon {
		s = 0
		t = clampFloat(f/e, 0, 1)
	} else {
		c := d1.Dot(r)
		if e <= epsilon {
			t = 0
			s = clampFloat(-c/a, 0, 1)
		} else {
			b := d1.Dot(d2)
			denom := a*e - b*b
			if denom > epsilon {
				s = clampFloat((b*f-c*e)/denom, 0, 1)
			}
			t = (b*s + f) / e
			if t < 0 {
				t = 0
				s = clampFloat(-c/a, 0, 1)
			} else if t > 1 {
				t = 1
				s = clampFloat((b-c)/a, 0, 1)
			}
		}
	}
	return a1.Add(d1.Mul(s)), b1.Add(d2.Mul(t))
}

func collideSphereCapsule(sphere, capsule *Collider) (ContactManifold, bool) {
	center := sphere.WorldCenter()
	p0, p1 := capsule.CapsuleAxisEndpoints()
	closest, _ := closestPointOnSegment(p0, p1, center)

	radii := math.Abs(sphere.Radius) + math.Abs(capsule.Radius)
	delta := center.Sub(closest)
	distance2 := delta.Len2()
	if distance2 > (radii+contactTolerance)*(radii+contactTolerance) {
		return ContactManifold{}, false
	}

	distance := math.Sqrt(distance2)
	normal := Vec3{Y: 1}
	if distance > epsilon {
		normal = delta.Div(distance).Neg() // points sphere → capsule
	}
	penetration := maxFloat(radii-distance, 0)
	contactPoint := center.Sub(normal.Mul(math.Abs(sphere.Radius) - penetration*0.5))
	return makeContactManifold(sphere, capsule, normal, []ContactPoint{
		makeContactPoint(sphere, capsule, contactPoint, penetration),
	}), true
}

func collideCapsulePlane(capsule, plane *Collider) (ContactManifold, bool) {
	planeNormal, planeDistance := plane.Plane()
	p0, p1 := capsule.CapsuleAxisEndpoints()
	radius := math.Abs(capsule.Radius)

	// Test both endpoints. The capsule collides with the plane when either
	// hemisphere cap dips below planeNormal.dot(point) = planeDistance.
	points := make([]ContactPoint, 0, 2)
	for _, point := range []Vec3{p0, p1} {
		signed := planeNormal.Dot(point) - planeDistance
		if signed > radius+contactTolerance {
			continue
		}
		penetration := maxFloat(radius-signed, 0)
		contact := point.Sub(planeNormal.Mul(signed))
		points = append(points, makeContactPoint(capsule, plane, contact, penetration))
	}
	if len(points) == 0 {
		return ContactManifold{}, false
	}
	// Normal points from capsule → plane, which is -planeNormal (plane normal
	// points away from the supporting half-space).
	return makeContactManifold(capsule, plane, planeNormal.Neg(), points), true
}

func collideCapsuleCapsule(a, b *Collider) (ContactManifold, bool) {
	a0, a1 := a.CapsuleAxisEndpoints()
	b0, b1 := b.CapsuleAxisEndpoints()
	pa, pb := closestPointsBetweenSegments(a0, a1, b0, b1)

	radii := math.Abs(a.Radius) + math.Abs(b.Radius)
	delta := pb.Sub(pa)
	distance2 := delta.Len2()
	if distance2 > (radii+contactTolerance)*(radii+contactTolerance) {
		return ContactManifold{}, false
	}

	distance := math.Sqrt(distance2)
	normal := Vec3{Y: 1}
	if distance > epsilon {
		normal = delta.Div(distance)
	}
	penetration := maxFloat(radii-distance, 0)
	contactPoint := pa.Add(pb).Mul(0.5)
	return makeContactManifold(a, b, normal, []ContactPoint{
		makeContactPoint(a, b, contactPoint, penetration),
	}), true
}

func collideSphereSphere(a, b *Collider) (ContactManifold, bool) {
	centerA := a.WorldCenter()
	centerB := b.WorldCenter()
	radius := math.Abs(a.Radius) + math.Abs(b.Radius)
	delta := centerB.Sub(centerA)
	distance2 := delta.Len2()
	if distance2 > (radius+contactTolerance)*(radius+contactTolerance) {
		return ContactManifold{}, false
	}

	distance := math.Sqrt(distance2)
	normal := Vec3{X: 1}
	if distance > epsilon {
		normal = delta.Div(distance)
	}
	penetration := maxFloat(radius-distance, 0)
	point := centerA.Add(normal.Mul(math.Abs(a.Radius) - penetration*0.5))
	return makeContactManifold(a, b, normal, []ContactPoint{
		makeContactPoint(a, b, point, penetration),
	}), true
}

func collideSpherePlane(sphere, plane *Collider) (ContactManifold, bool) {
	normal, distance := plane.Plane()
	center := sphere.WorldCenter()
	signedDistance := normal.Dot(center) - distance
	sideNormal := normal
	separation := signedDistance
	if signedDistance < 0 {
		sideNormal = normal.Neg()
		separation = -signedDistance
	}

	radius := math.Abs(sphere.Radius)
	if separation > radius+contactTolerance {
		return ContactManifold{}, false
	}

	penetration := maxFloat(radius-separation, 0)
	contactPoint := center.Sub(sideNormal.Mul(separation))
	return makeContactManifold(sphere, plane, sideNormal.Neg(), []ContactPoint{
		makeContactPoint(sphere, plane, contactPoint, penetration),
	}), true
}

func collideSphereBox(sphere, box *Collider) (ContactManifold, bool) {
	center := sphere.WorldCenter()
	boxCenter := box.WorldCenter()
	rotation := box.WorldRotation()
	inverse := rotation.Inverse()
	localCenter := inverse.Rotate(center.Sub(boxCenter))
	half := box.halfExtents()

	clamped := Vec3{
		X: clampFloat(localCenter.X, -half.X, half.X),
		Y: clampFloat(localCenter.Y, -half.Y, half.Y),
		Z: clampFloat(localCenter.Z, -half.Z, half.Z),
	}
	delta := clamped.Sub(localCenter)
	radius := math.Abs(sphere.Radius)

	inside := localCenter.X >= -half.X && localCenter.X <= half.X &&
		localCenter.Y >= -half.Y && localCenter.Y <= half.Y &&
		localCenter.Z >= -half.Z && localCenter.Z <= half.Z

	var normalLocal Vec3
	var penetration float64
	var contactLocal Vec3

	if inside {
		normalToSurface, distanceToSurface := nearestBoxSurface(localCenter, half)
		if distanceToSurface < 0 {
			return ContactManifold{}, false
		}
		normalLocal = normalToSurface.Neg()
		penetration = radius + distanceToSurface
		contactLocal = localCenter.Add(normalToSurface.Mul(distanceToSurface))
	} else {
		distance := delta.Len()
		if distance > radius+contactTolerance {
			return ContactManifold{}, false
		}
		if distance <= epsilon {
			normalLocal = Vec3{Y: -1}
		} else {
			normalLocal = delta.Div(distance)
		}
		penetration = maxFloat(radius-distance, 0)
		contactLocal = clamped
	}

	normal := rotation.Rotate(normalLocal).Normalize()
	contactPoint := boxCenter.Add(rotation.Rotate(contactLocal))
	return makeContactManifold(sphere, box, normal, []ContactPoint{
		makeContactPoint(sphere, box, contactPoint, penetration),
	}), true
}

func nearestBoxSurface(point, half Vec3) (Vec3, float64) {
	dx := half.X - math.Abs(point.X)
	dy := half.Y - math.Abs(point.Y)
	dz := half.Z - math.Abs(point.Z)

	axis := Vec3{X: 1}
	distance := dx
	if point.X < 0 {
		axis = axis.Neg()
	}
	if dy < distance {
		axis = Vec3{Y: 1}
		if point.Y < 0 {
			axis = axis.Neg()
		}
		distance = dy
	}
	if dz < distance {
		axis = Vec3{Z: 1}
		if point.Z < 0 {
			axis = axis.Neg()
		}
		distance = dz
	}
	return axis, distance
}

func collideBoxPlane(box, plane *Collider) (ContactManifold, bool) {
	planeNormal, planeDistance := plane.Plane()
	rotation := box.WorldRotation()
	center := box.WorldCenter()
	half := box.halfExtents()
	vertices := [8]Vec3{
		{X: -half.X, Y: -half.Y, Z: -half.Z},
		{X: half.X, Y: -half.Y, Z: -half.Z},
		{X: -half.X, Y: half.Y, Z: -half.Z},
		{X: half.X, Y: half.Y, Z: -half.Z},
		{X: -half.X, Y: -half.Y, Z: half.Z},
		{X: half.X, Y: -half.Y, Z: half.Z},
		{X: -half.X, Y: half.Y, Z: half.Z},
		{X: half.X, Y: half.Y, Z: half.Z},
	}

	var points [4]ContactPoint
	count := 0
	for _, vertex := range vertices {
		worldVertex := center.Add(rotation.Rotate(vertex))
		signedDistance := planeNormal.Dot(worldVertex) - planeDistance
		if signedDistance > contactTolerance {
			continue
		}
		penetration := maxFloat(-signedDistance, 0)
		point := makeContactPoint(box, plane, worldVertex, penetration)
		if count < len(points) {
			points[count] = point
			count++
			continue
		}
		shallowest := 0
		for i := 1; i < len(points); i++ {
			if points[i].Penetration < points[shallowest].Penetration {
				shallowest = i
			}
		}
		if point.Penetration > points[shallowest].Penetration {
			points[shallowest] = point
		}
	}
	if count == 0 {
		return ContactManifold{}, false
	}

	contacts := make([]ContactPoint, count)
	for i := 0; i < count; i++ {
		contacts[i] = points[i]
	}
	return makeContactManifold(box, plane, planeNormal.Neg(), contacts), true
}
