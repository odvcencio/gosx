package physics

import "math"

// mat3 stores a rotation as three world-space column axes. GJK and EPA call
// the support function many times per pair, and a matrix multiply costs far
// less than Quat.Rotate, which normalizes the quaternion on every call.
type mat3 struct {
	x Vec3
	y Vec3
	z Vec3
}

func mat3FromQuat(q Quat) mat3 {
	return mat3{
		x: q.Rotate(Vec3{X: 1}),
		y: q.Rotate(Vec3{Y: 1}),
		z: q.Rotate(Vec3{Z: 1}),
	}
}

// mul rotates a local vector into world space.
func (m mat3) mul(v Vec3) Vec3 {
	return Vec3{
		X: m.x.X*v.X + m.y.X*v.Y + m.z.X*v.Z,
		Y: m.x.Y*v.X + m.y.Y*v.Y + m.z.Y*v.Z,
		Z: m.x.Z*v.X + m.y.Z*v.Y + m.z.Z*v.Z,
	}
}

// mulT rotates a world vector into local space.
func (m mat3) mulT(v Vec3) Vec3 {
	return Vec3{X: m.x.Dot(v), Y: m.y.Dot(v), Z: m.z.Dot(v)}
}

// convexShape is a support-mapped convex volume placed in world space. One
// description covers boxes, spheres, capsules, cylinders, cones, convex hulls
// and single triangles, so GJK and EPA need no per-shape code.
//
// margin is a rounding radius. A sphere is a point with margin = radius; a
// capsule is a segment with margin = radius. Cylinder and cone carry their
// radius in the profile instead, because their rim is a sharp edge.
type convexShape struct {
	kind   ColliderShape
	center Vec3
	basis  mat3
	half   Vec3
	radius float64
	halfH  float64
	verts  []Vec3
	margin float64
	// bound is the radius of a sphere that encloses the shape. EPA uses it to
	// pick a convergence tolerance that matches the shape size.
	bound float64
	// tri holds a single world-space triangle. Set only by triangleShape.
	tri    [3]Vec3
	useTri bool
}

// newConvexShape builds the support description of one collider. It reports
// false for shapes that no support function can describe, which today is only
// the infinite plane and any collider that failed validation.
func newConvexShape(c *Collider) (convexShape, bool) {
	if c == nil || c.invalid != nil {
		return convexShape{}, false
	}
	shape := convexShape{
		kind:   c.Shape,
		center: c.WorldCenter(),
		basis:  mat3FromQuat(c.WorldRotation()),
		bound:  c.BoundingRadius(),
	}
	switch c.Shape {
	case ShapeBox:
		shape.half = c.halfExtents()
	case ShapeSphere:
		shape.margin = math.Abs(c.Radius)
	case ShapeCapsule:
		shape.margin = math.Abs(c.Radius)
		shape.halfH = math.Abs(c.Height) * 0.5
	case ShapeCylinder, ShapeCone:
		shape.radius = math.Abs(c.Radius)
		shape.halfH = math.Abs(c.Height) * 0.5
	case ShapeConvexHull:
		shape.verts = c.hull
	default:
		return convexShape{}, false
	}
	return shape, true
}

// triangleShape wraps three world-space points as a convex shape so mesh
// triangles reuse the same narrowphase as every other convex pair.
func triangleShape(a, b, c Vec3) convexShape {
	centroid := a.Add(b).Add(c).Div(3)
	bound := a.Sub(centroid).Len()
	bound = math.Max(bound, b.Sub(centroid).Len())
	bound = math.Max(bound, c.Sub(centroid).Len())
	return convexShape{
		kind:   ShapeConvexHull,
		center: centroid,
		bound:  bound,
		useTri: true,
		tri:    [3]Vec3{a, b, c},
	}
}

// support returns the point of the shape that is farthest along dir. dir need
// not be normalized.
func (s convexShape) support(dir Vec3) Vec3 {
	if s.useTri {
		best := s.tri[0]
		bestDot := dir.Dot(best)
		for i := 1; i < 3; i++ {
			if d := dir.Dot(s.tri[i]); d > bestDot {
				bestDot = d
				best = s.tri[i]
			}
		}
		return best
	}

	local := s.basis.mulT(dir)
	var p Vec3
	switch s.kind {
	case ShapeBox:
		p = Vec3{
			X: signedExtent(local.X, s.half.X),
			Y: signedExtent(local.Y, s.half.Y),
			Z: signedExtent(local.Z, s.half.Z),
		}
	case ShapeSphere:
		// A sphere is the origin plus a margin.
	case ShapeCapsule:
		if local.Y > 0 {
			p.Y = s.halfH
		} else if local.Y < 0 {
			p.Y = -s.halfH
		}
	case ShapeCylinder:
		p = cylinderLocalSupport(local, s.radius, s.halfH)
	case ShapeCone:
		p = coneLocalSupport(local, s.radius, s.halfH)
	case ShapeConvexHull:
		p = hullLocalSupport(s.verts, local)
	}

	world := s.center.Add(s.basis.mul(p))
	if s.margin > 0 {
		length := dir.Len()
		if length > epsilon {
			world = world.Add(dir.Mul(s.margin / length))
		}
	}
	return world
}

func signedExtent(component, half float64) float64 {
	if component >= 0 {
		return half
	}
	return -half
}

// cylinderLocalSupport returns the farthest cylinder point along a local
// direction. The cylinder runs along local Y from -halfH to +halfH.
func cylinderLocalSupport(dir Vec3, radius, halfH float64) Vec3 {
	p := Vec3{}
	if dir.Y > 0 {
		p.Y = halfH
	} else if dir.Y < 0 {
		p.Y = -halfH
	}
	radial2 := dir.X*dir.X + dir.Z*dir.Z
	if radial2 > epsilon {
		scale := radius / math.Sqrt(radial2)
		p.X = dir.X * scale
		p.Z = dir.Z * scale
	}
	return p
}

// coneLocalSupport returns the farthest cone point along a local direction.
// The apex sits at +halfH on local Y and the base disc at -halfH.
func coneLocalSupport(dir Vec3, radius, halfH float64) Vec3 {
	radial := math.Sqrt(dir.X*dir.X + dir.Z*dir.Z)
	apexDot := dir.Y * halfH
	baseDot := -dir.Y*halfH + radius*radial
	if apexDot >= baseDot {
		return Vec3{Y: halfH}
	}
	p := Vec3{Y: -halfH}
	if radial > epsilon {
		scale := radius / radial
		p.X = dir.X * scale
		p.Z = dir.Z * scale
	}
	return p
}

func hullLocalSupport(verts []Vec3, dir Vec3) Vec3 {
	if len(verts) == 0 {
		return Vec3{}
	}
	best := verts[0]
	bestDot := dir.Dot(best)
	for i := 1; i < len(verts); i++ {
		if d := dir.Dot(verts[i]); d > bestDot {
			bestDot = d
			best = verts[i]
		}
	}
	return best
}
