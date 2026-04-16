package physics

import "math"

// RaycastHit is the closest intersection between a ray and a collider.
type RaycastHit struct {
	Collider *Collider
	Body     *RigidBody
	Point    Vec3
	Normal   Vec3
	Distance float64
}

// Raycast returns the closest hit against all world colliders. maxDistance <= 0
// means unbounded.
func (w *World) Raycast(ray Ray, maxDistance float64) (RaycastHit, bool) {
	if w == nil {
		return RaycastHit{}, false
	}
	limit := raycastMaxDistance(maxDistance)
	best := RaycastHit{Distance: math.Inf(1)}
	found := false
	for _, collider := range w.colliders {
		hit, ok := collider.Raycast(ray, limit)
		if !ok {
			continue
		}
		if !found || hit.Distance < best.Distance-epsilon ||
			(math.Abs(hit.Distance-best.Distance) <= epsilon && colliderIndex(hit.Collider) < colliderIndex(best.Collider)) {
			best = hit
			found = true
		}
	}
	return best, found
}

// Raycast returns the closest hit against this collider. maxDistance <= 0
// means unbounded.
func (c *Collider) Raycast(ray Ray, maxDistance float64) (RaycastHit, bool) {
	if c == nil {
		return RaycastHit{}, false
	}
	direction := ray.Direction.Normalize()
	if direction.Len2() <= epsilon {
		return RaycastHit{}, false
	}
	r := Ray{Origin: ray.Origin, Direction: direction}
	limit := raycastMaxDistance(maxDistance)

	var hit RaycastHit
	var ok bool
	switch c.Shape {
	case ShapeSphere:
		hit, ok = raycastSphere(c, r, limit)
	case ShapePlane:
		hit, ok = raycastPlane(c, r, limit)
	case ShapeBox:
		hit, ok = raycastBox(c, r, limit)
	default:
		return RaycastHit{}, false
	}
	if !ok {
		return RaycastHit{}, false
	}
	hit.Collider = c
	hit.Body = c.Body
	return hit, true
}

func raycastSphere(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	center := c.WorldCenter()
	radius := math.Abs(c.Radius)
	if radius <= 0 {
		return RaycastHit{}, false
	}
	m := ray.Origin.Sub(center)
	b := m.Dot(ray.Direction)
	cc := m.Dot(m) - radius*radius
	if cc > 0 && b > 0 {
		return RaycastHit{}, false
	}
	discriminant := b*b - cc
	if discriminant < 0 {
		return RaycastHit{}, false
	}
	distance := -b - math.Sqrt(discriminant)
	if distance < 0 {
		distance = 0
	}
	if distance > maxDistance {
		return RaycastHit{}, false
	}
	point := ray.At(distance)
	normal := point.Sub(center).Normalize()
	if normal.Len2() <= epsilon {
		normal = ray.Direction.Neg()
	}
	return RaycastHit{Point: point, Normal: normal, Distance: distance}, true
}

func raycastPlane(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	normal, planeDistance := c.Plane()
	denom := normal.Dot(ray.Direction)
	if math.Abs(denom) <= epsilon {
		return RaycastHit{}, false
	}
	distance := (planeDistance - normal.Dot(ray.Origin)) / denom
	if distance < 0 || distance > maxDistance {
		return RaycastHit{}, false
	}
	return RaycastHit{
		Point:    ray.At(distance),
		Normal:   normal,
		Distance: distance,
	}, true
}

func raycastBox(c *Collider, ray Ray, maxDistance float64) (RaycastHit, bool) {
	half := c.halfExtents()
	if half.X <= 0 || half.Y <= 0 || half.Z <= 0 {
		return RaycastHit{}, false
	}
	rotation := c.WorldRotation()
	inv := rotation.Inverse()
	origin := inv.Rotate(ray.Origin.Sub(c.WorldCenter()))
	direction := inv.Rotate(ray.Direction)

	tMin := 0.0
	tMax := maxDistance
	enterNormal := Vec3{}
	axes := [3]Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	mins := [3]float64{-half.X, -half.Y, -half.Z}
	maxs := [3]float64{half.X, half.Y, half.Z}
	origins := [3]float64{origin.X, origin.Y, origin.Z}
	dirs := [3]float64{direction.X, direction.Y, direction.Z}

	for i := 0; i < 3; i++ {
		if math.Abs(dirs[i]) <= epsilon {
			if origins[i] < mins[i] || origins[i] > maxs[i] {
				return RaycastHit{}, false
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
			return RaycastHit{}, false
		}
	}
	if tMin < 0 || tMin > maxDistance {
		return RaycastHit{}, false
	}
	normal := rotation.Rotate(enterNormal).Normalize()
	if normal.Len2() <= epsilon {
		normal = ray.Direction.Neg()
	}
	return RaycastHit{
		Point:    ray.At(tMin),
		Normal:   normal,
		Distance: tMin,
	}, true
}

func raycastMaxDistance(maxDistance float64) float64 {
	if maxDistance <= 0 || math.IsInf(maxDistance, 1) {
		return math.Inf(1)
	}
	return maxDistance
}

func colliderIndex(c *Collider) int {
	if c == nil {
		return 0
	}
	return c.index
}
