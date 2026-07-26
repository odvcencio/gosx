package physics

import "math"

// Inertia tensors derived from collider geometry.
//
// Every body carries one body-local inverse inertia tensor. The world inverse
// tensor is rebuilt each step from the body rotation, and the contact solver
// uses it to turn an impulse at a contact point into an angular velocity change.
//
// A body with several colliders splits its mass between them in proportion to
// volume, then sums the shifted tensors with the parallel axis theorem. Convex
// hulls and triangle meshes use the inertia of their local bounding box, which
// overstates the true tensor but never reports a body that cannot rotate.

// defaultInertiaRadius sizes the fallback tensor of a body that carries no
// collider. Such a body still answers ApplyTorque, which several callers rely
// on for simple spin control.
const defaultInertiaRadius = 0.5

// inertiaTensorFromColliders returns the body-local inertia tensor for the
// given mass. It returns a zero matrix when no collider encloses a volume.
func inertiaTensorFromColliders(mass float64, colliders []*Collider) mat3 {
	if mass <= 0 {
		return mat3{}
	}

	total := 0.0
	for _, c := range colliders {
		total += colliderVolume(c)
	}
	if total <= 0 {
		// No collider with volume. Use a solid sphere so torque still works.
		i := 0.4 * mass * defaultInertiaRadius * defaultInertiaRadius
		return mat3Diagonal(Vec3{X: i, Y: i, Z: i})
	}

	tensor := mat3{}
	for _, c := range colliders {
		volume := colliderVolume(c)
		if volume <= 0 {
			continue
		}
		share := mass * volume / total
		local := mat3Diagonal(colliderPrincipalInertia(c, share))
		// Turn the collider frame into the body frame, then shift the tensor
		// from the collider centre to the body origin.
		basis := mat3FromQuat(c.Rotation)
		tensor = tensor.add(rotateTensor(basis, local))
		tensor = tensor.add(parallelAxisTerm(share, c.Offset))
	}
	return tensor
}

// parallelAxisTerm returns m * (|d|^2 * E - d dᵀ), the tensor a point mass adds
// when it sits at offset d from the origin.
func parallelAxisTerm(mass float64, d Vec3) mat3 {
	d2 := d.Len2()
	if mass <= 0 || d2 <= 0 {
		return mat3{}
	}
	return mat3Identity().scale(mass * d2).add(outerProduct(d).scale(-mass))
}

// colliderVolume returns the enclosed volume of one collider. A plane, a
// trigger and an unusable collider all report zero, so none of them can move
// the mass distribution.
func colliderVolume(c *Collider) float64 {
	if c == nil || c.invalid != nil || c.IsTrigger {
		return 0
	}
	switch c.Shape {
	case ShapeBox:
		half := c.halfExtents()
		return 8 * half.X * half.Y * half.Z
	case ShapeSphere:
		r := math.Abs(c.Radius)
		return 4.0 / 3.0 * math.Pi * r * r * r
	case ShapeCapsule:
		r := math.Abs(c.Radius)
		h := math.Abs(c.Height)
		return math.Pi*r*r*h + 4.0/3.0*math.Pi*r*r*r
	case ShapeCylinder:
		r := math.Abs(c.Radius)
		return math.Pi * r * r * math.Abs(c.Height)
	case ShapeCone:
		r := math.Abs(c.Radius)
		return math.Pi * r * r * math.Abs(c.Height) / 3
	case ShapeConvexHull, ShapeTriangleMesh:
		size := c.localBounds.Size()
		return math.Abs(size.X * size.Y * size.Z)
	default:
		return 0
	}
}

// colliderPrincipalInertia returns the diagonal inertia of one collider about
// its own centre, in its own frame, for the given mass. Capsule, cylinder and
// cone run along local Y, which matches the support functions.
func colliderPrincipalInertia(c *Collider, mass float64) Vec3 {
	if c == nil || mass <= 0 {
		return Vec3{}
	}
	switch c.Shape {
	case ShapeBox:
		half := c.halfExtents()
		x2 := 4 * half.X * half.X
		y2 := 4 * half.Y * half.Y
		z2 := 4 * half.Z * half.Z
		return Vec3{
			X: mass * (y2 + z2) / 12,
			Y: mass * (x2 + z2) / 12,
			Z: mass * (x2 + y2) / 12,
		}
	case ShapeSphere:
		r := math.Abs(c.Radius)
		i := 0.4 * mass * r * r
		return Vec3{X: i, Y: i, Z: i}
	case ShapeCylinder:
		r := math.Abs(c.Radius)
		h := math.Abs(c.Height)
		axial := 0.5 * mass * r * r
		radial := mass * (3*r*r + h*h) / 12
		return Vec3{X: radial, Y: axial, Z: radial}
	case ShapeCone:
		// A solid cone with the body origin at mid height. The centroid sits a
		// quarter height above the base, so the transverse term carries a
		// parallel axis shift of h/4.
		r := math.Abs(c.Radius)
		h := math.Abs(c.Height)
		axial := 0.3 * mass * r * r
		radial := 0.15*mass*r*r + mass*h*h/10
		return Vec3{X: radial, Y: axial, Z: radial}
	case ShapeCapsule:
		return capsuleInertia(mass, math.Abs(c.Radius), math.Abs(c.Height))
	case ShapeConvexHull, ShapeTriangleMesh:
		// Use the local bounding box. The result overstates a thin hull but it
		// is never zero and never negative, so the solver stays stable.
		size := c.localBounds.Size()
		box := Vec3{
			X: mass * (size.Y*size.Y + size.Z*size.Z) / 12,
			Y: mass * (size.X*size.X + size.Z*size.Z) / 12,
			Z: mass * (size.X*size.X + size.Y*size.Y) / 12,
		}
		return box
	default:
		return Vec3{}
	}
}

// capsuleInertia returns the diagonal inertia of a capsule along local Y. The
// capsule is one cylinder of height h plus two hemisphere caps of radius r.
func capsuleInertia(mass, r, h float64) Vec3 {
	cylinderVolume := math.Pi * r * r * h
	capVolume := 2.0 / 3.0 * math.Pi * r * r * r // one hemisphere
	total := cylinderVolume + 2*capVolume
	if total <= 0 {
		i := 0.4 * mass * r * r
		return Vec3{X: i, Y: i, Z: i}
	}
	cylinderMass := mass * cylinderVolume / total
	capMass := mass * capVolume / total

	axial := 0.5*cylinderMass*r*r + 2*(0.4*capMass*r*r)

	// A hemisphere has 2/5 m r^2 about a transverse axis through the centre of
	// its flat face, and its centroid sits 3r/8 from that face.
	capAboutOwnCentre := capMass*r*r*0.4 - capMass*(3*r/8)*(3*r/8)
	capOffset := h/2 + 3*r/8
	radial := cylinderMass*(h*h/12+r*r/4) +
		2*(capAboutOwnCentre+capMass*capOffset*capOffset)
	return Vec3{X: radial, Y: axial, Z: radial}
}
