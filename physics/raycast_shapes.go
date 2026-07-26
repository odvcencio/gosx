package physics

import "math"

// Raycast routines for capsule, cylinder, cone and triangle mesh colliders.
// Every routine works in collider-local space, where the shape axis is local Y,
// and reports distances that are already world distances because the transform
// is a rotation plus a translation.

// localRay converts a world ray into the collider's local frame.
func localRay(c *Collider, ray Ray) (Vec3, Vec3, mat3) {
	basis := mat3FromQuat(c.WorldRotation())
	origin := basis.mulT(ray.Origin.Sub(c.WorldCenter()))
	direction := basis.mulT(ray.Direction)
	return origin, direction, basis
}

func insideHit(ray Ray) (RaycastHit, bool) {
	return RaycastHit{Point: ray.Origin, Normal: ray.Direction.Neg(), Distance: 0}, true
}

func raycastCapsule(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	radius := math.Abs(c.Radius)
	half := math.Abs(c.Height) * 0.5
	if radius <= 0 {
		return RaycastHit{}, false
	}
	origin, direction, basis := localRay(c, ray)

	// A ray that starts inside reports zero distance, which matches the box
	// and sphere routines.
	axisPoint := Vec3{Y: clampFloat(origin.Y, -half, half)}
	if origin.Sub(axisPoint).Len2() <= radius*radius {
		return insideHit(ray)
	}

	best := math.Inf(1)
	var bestNormal Vec3

	// Lateral tube of the capsule.
	a := direction.X*direction.X + direction.Z*direction.Z
	b := origin.X*direction.X + origin.Z*direction.Z
	cc := origin.X*origin.X + origin.Z*origin.Z - radius*radius
	if a > epsilon {
		discriminant := b*b - a*cc
		if discriminant >= 0 {
			root := math.Sqrt(discriminant)
			t := (-b - root) / a
			if t >= 0 && t <= maxDistance {
				y := origin.Y + t*direction.Y
				if math.Abs(y) <= half {
					best = t
					bestNormal = Vec3{X: origin.X + t*direction.X, Z: origin.Z + t*direction.Z}.Normalize()
				}
			}
		}
	}

	// Hemisphere caps.
	for _, capY := range [2]float64{-half, half} {
		center := Vec3{Y: capY}
		m := origin.Sub(center)
		mb := m.Dot(direction)
		mc := m.Dot(m) - radius*radius
		discriminant := mb*mb - mc
		if discriminant < 0 {
			continue
		}
		t := -mb - math.Sqrt(discriminant)
		if t < 0 || t > maxDistance || t >= best {
			continue
		}
		point := origin.Add(direction.Mul(t))
		if capY > 0 && point.Y < half {
			continue
		}
		if capY < 0 && point.Y > -half {
			continue
		}
		best = t
		bestNormal = point.Sub(center).Normalize()
	}

	if math.IsInf(best, 1) {
		return RaycastHit{}, false
	}
	return finishLocalHit(c, ray, basis, bestNormal, best)
}

func raycastCylinder(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	radius := math.Abs(c.Radius)
	half := math.Abs(c.Height) * 0.5
	if radius <= 0 || half <= 0 {
		return RaycastHit{}, false
	}
	origin, direction, basis := localRay(c, ray)

	if math.Abs(origin.Y) <= half && origin.X*origin.X+origin.Z*origin.Z <= radius*radius {
		return insideHit(ray)
	}

	best := math.Inf(1)
	var bestNormal Vec3

	a := direction.X*direction.X + direction.Z*direction.Z
	b := origin.X*direction.X + origin.Z*direction.Z
	cc := origin.X*origin.X + origin.Z*origin.Z - radius*radius
	if a > epsilon {
		discriminant := b*b - a*cc
		if discriminant >= 0 {
			root := math.Sqrt(discriminant)
			for _, t := range [2]float64{(-b - root) / a, (-b + root) / a} {
				if t < 0 || t > maxDistance || t >= best {
					continue
				}
				y := origin.Y + t*direction.Y
				if math.Abs(y) > half {
					continue
				}
				best = t
				bestNormal = Vec3{X: origin.X + t*direction.X, Z: origin.Z + t*direction.Z}.Normalize()
			}
		}
	}

	if math.Abs(direction.Y) > epsilon {
		for _, capY := range [2]float64{-half, half} {
			t := (capY - origin.Y) / direction.Y
			if t < 0 || t > maxDistance || t >= best {
				continue
			}
			x := origin.X + t*direction.X
			z := origin.Z + t*direction.Z
			if x*x+z*z > radius*radius {
				continue
			}
			best = t
			bestNormal = Vec3{Y: math.Copysign(1, capY)}
		}
	}

	if math.IsInf(best, 1) {
		return RaycastHit{}, false
	}
	return finishLocalHit(c, ray, basis, bestNormal, best)
}

func raycastCone(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	radius := math.Abs(c.Radius)
	half := math.Abs(c.Height) * 0.5
	if radius <= 0 || half <= 0 {
		return RaycastHit{}, false
	}
	origin, direction, basis := localRay(c, ray)

	// slope is the cone radius gained per unit of drop from the apex.
	slope := radius / (2 * half)
	slope2 := slope * slope

	if origin.Y >= -half && origin.Y <= half {
		limit := slope * (half - origin.Y)
		if origin.X*origin.X+origin.Z*origin.Z <= limit*limit {
			return insideHit(ray)
		}
	}

	best := math.Inf(1)
	var bestNormal Vec3

	drop := half - origin.Y
	a := direction.X*direction.X + direction.Z*direction.Z - slope2*direction.Y*direction.Y
	b := origin.X*direction.X + origin.Z*direction.Z + slope2*drop*direction.Y
	cc := origin.X*origin.X + origin.Z*origin.Z - slope2*drop*drop

	accept := func(t float64) {
		if t < 0 || t > maxDistance || t >= best {
			return
		}
		y := origin.Y + t*direction.Y
		if y < -half || y > half {
			return
		}
		x := origin.X + t*direction.X
		z := origin.Z + t*direction.Z
		normal := Vec3{X: x, Y: slope2 * (half - y), Z: z}.Normalize()
		if normal.Len2() <= epsilon {
			// The ray landed on the apex, where the surface has no single
			// normal. Report the cone axis, which is the usual convention.
			normal = Vec3{Y: 1}
		}
		best = t
		bestNormal = normal
	}

	if math.Abs(a) > epsilon {
		discriminant := b*b - a*cc
		if discriminant >= 0 {
			root := math.Sqrt(discriminant)
			accept((-b - root) / a)
			accept((-b + root) / a)
		}
	} else if math.Abs(b) > epsilon {
		accept(-cc / (2 * b))
	}

	if math.Abs(direction.Y) > epsilon {
		t := (-half - origin.Y) / direction.Y
		if t >= 0 && t <= maxDistance && t < best {
			x := origin.X + t*direction.X
			z := origin.Z + t*direction.Z
			if x*x+z*z <= radius*radius {
				best = t
				bestNormal = Vec3{Y: -1}
			}
		}
	}

	if math.IsInf(best, 1) {
		return RaycastHit{}, false
	}
	return finishLocalHit(c, ray, basis, bestNormal, best)
}

func raycastMesh(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	if c.mesh == nil {
		return RaycastHit{}, false
	}
	origin, direction, basis := localRay(c, ray)

	best := math.Inf(1)
	var bestNormal Vec3
	c.mesh.queryRay(origin, direction, maxDistance, func(index int, limit float64) float64 {
		tri := c.mesh.tris[index]
		t, normal, ok := rayTriangle(origin, direction, tri, limit)
		if !ok || t >= best {
			return limit
		}
		best = t
		bestNormal = normal
		return t
	})
	if math.IsInf(best, 1) {
		return RaycastHit{}, false
	}
	if bestNormal.Dot(direction) > 0 {
		bestNormal = bestNormal.Neg()
	}
	return finishLocalHit(c, ray, basis, bestNormal, best)
}

// rayTriangle runs the Moller-Trumbore intersection test and returns the
// distance plus the geometric normal.
func rayTriangle(origin, direction Vec3, tri meshTriangle, maxDistance float64) (float64, Vec3, bool) {
	edge1 := tri.b.Sub(tri.a)
	edge2 := tri.c.Sub(tri.a)
	pvec := direction.Cross(edge2)
	det := edge1.Dot(pvec)
	if math.Abs(det) <= 1e-15 {
		return 0, Vec3{}, false
	}
	invDet := 1 / det
	tvec := origin.Sub(tri.a)
	u := tvec.Dot(pvec) * invDet
	if u < 0 || u > 1 {
		return 0, Vec3{}, false
	}
	qvec := tvec.Cross(edge1)
	v := direction.Dot(qvec) * invDet
	if v < 0 || u+v > 1 {
		return 0, Vec3{}, false
	}
	t := edge2.Dot(qvec) * invDet
	if t < 0 || t > maxDistance {
		return 0, Vec3{}, false
	}
	return t, edge1.Cross(edge2).Normalize(), true
}

// finishLocalHit turns a local-space normal plus distance into a world hit.
func finishLocalHit(c *Collider, ray Ray, basis mat3, localNormal Vec3, distance float64) (RaycastHit, bool) {
	normal := basis.mul(localNormal).Normalize()
	if normal.Len2() <= epsilon {
		normal = ray.Direction.Neg()
	}
	return RaycastHit{
		Point:    ray.At(distance),
		Normal:   normal,
		Distance: distance,
	}, true
}
