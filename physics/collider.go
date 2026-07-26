package physics

import (
	"fmt"
	"math"
)

type ColliderShape int

const (
	ShapeBox ColliderShape = iota
	ShapeSphere
	ShapeCapsule
	ShapePlane
	ShapeCylinder
	ShapeCone
	ShapeConvexHull
	ShapeTriangleMesh
)

// String names the shape for diagnostics.
func (s ColliderShape) String() string {
	switch s {
	case ShapeBox:
		return "box"
	case ShapeSphere:
		return "sphere"
	case ShapeCapsule:
		return "capsule"
	case ShapePlane:
		return "plane"
	case ShapeCylinder:
		return "cylinder"
	case ShapeCone:
		return "cone"
	case ShapeConvexHull:
		return "convexhull"
	case ShapeTriangleMesh:
		return "trianglemesh"
	default:
		return fmt.Sprintf("shape(%d)", int(s))
	}
}

type ColliderConfig struct {
	Shape     ColliderShape
	Offset    Vec3
	Rotation  Quat
	IsTrigger bool
	Width     float64
	Height    float64
	Depth     float64
	Radius    float64
	Normal    Vec3
	Distance  float64

	// Vertices supplies collider-local geometry. ShapeConvexHull reads it as
	// the hull point cloud. ShapeTriangleMesh reads it as the vertex buffer
	// that Indices refers to. Every other shape ignores it.
	Vertices []Vec3

	// Indices lists triangle corners for ShapeTriangleMesh, three per
	// triangle. Every other shape ignores it.
	Indices []int
}

type Collider struct {
	Shape     ColliderShape
	Body      *RigidBody
	Offset    Vec3
	Rotation  Quat
	IsTrigger bool
	Width     float64
	Height    float64
	Depth     float64
	Radius    float64
	Normal    Vec3
	Distance  float64

	index int

	// hull holds the local-space convex point cloud for ShapeConvexHull.
	hull []Vec3
	// mesh holds the local-space triangle soup plus its BVH for
	// ShapeTriangleMesh.
	mesh *triangleMesh
	// localBounds is the collider-local bounding box of hull or mesh
	// geometry. AABB projects it into world space.
	localBounds AABB
	// invalid records why the collider cannot collide. A non-nil value makes
	// every narrowphase and raycast call reject the collider, and makes
	// World.Diagnostics report it.
	invalid error
}

func NewCollider(config ColliderConfig) *Collider {
	return newCollider(nil, config)
}

func newCollider(body *RigidBody, config ColliderConfig) *Collider {
	rotation := config.Rotation
	if rotation.X == 0 && rotation.Y == 0 && rotation.Z == 0 && rotation.W == 0 {
		rotation = IdentityQuat()
	}

	normal := config.Normal
	if normal.Len2() <= epsilon {
		normal = Vec3{Y: 1}
	}

	c := &Collider{
		Shape:     config.Shape,
		Body:      body,
		Offset:    config.Offset,
		Rotation:  rotation.Normalize(),
		IsTrigger: config.IsTrigger,
		Width:     config.Width,
		Height:    config.Height,
		Depth:     config.Depth,
		Radius:    config.Radius,
		Normal:    normal.Normalize(),
		Distance:  config.Distance,
	}
	c.buildGeometry(config)
	c.invalid = c.validate()
	return c
}

// buildGeometry copies the caller's vertex data and precomputes the local
// bounds plus the triangle BVH. The collider never aliases caller slices, so
// later edits by the caller cannot corrupt the simulation.
func (c *Collider) buildGeometry(config ColliderConfig) {
	switch c.Shape {
	case ShapeConvexHull:
		if len(config.Vertices) == 0 {
			return
		}
		c.hull = make([]Vec3, len(config.Vertices))
		copy(c.hull, config.Vertices)
		c.localBounds = boundsOfPoints(c.hull)
		// Indices are optional for a hull. When present they describe the
		// triangulated surface, which is what a raycast needs.
		if len(config.Indices) >= 3 {
			c.mesh = newTriangleMesh(c.hull, config.Indices)
		}
	case ShapeTriangleMesh:
		c.mesh = newTriangleMesh(config.Vertices, config.Indices)
		if c.mesh != nil {
			c.localBounds = c.mesh.bounds
		}
	}
}

// Err reports why the collider cannot take part in collision, or nil when the
// collider is usable. A collider with a non-nil Err never generates contacts,
// so callers must treat it as a configuration mistake and not as a shape that
// simply misses.
func (c *Collider) Err() error {
	if c == nil {
		return nil
	}
	return c.invalid
}

// validate rejects shapes whose parameters cannot describe a volume. Returning
// an error here is what stops a mis-declared collider from passing silently
// through every other body in the world.
func (c *Collider) validate() error {
	switch c.Shape {
	case ShapeBox:
		half := c.halfExtents()
		if half.X <= 0 || half.Y <= 0 || half.Z <= 0 {
			return fmt.Errorf("physics: box collider needs Width, Height and Depth > 0, got %v x %v x %v",
				c.Width, c.Height, c.Depth)
		}
	case ShapeSphere:
		if math.Abs(c.Radius) <= 0 {
			return fmt.Errorf("physics: sphere collider needs Radius > 0")
		}
	case ShapeCapsule:
		if math.Abs(c.Radius) <= 0 {
			return fmt.Errorf("physics: capsule collider needs Radius > 0")
		}
	case ShapePlane:
		if c.Normal.Len2() <= epsilon {
			return fmt.Errorf("physics: plane collider needs a nonzero Normal")
		}
	case ShapeCylinder:
		if math.Abs(c.Radius) <= 0 || math.Abs(c.Height) <= 0 {
			return fmt.Errorf("physics: cylinder collider needs Radius > 0 and Height > 0, got radius %v height %v",
				c.Radius, c.Height)
		}
	case ShapeCone:
		if math.Abs(c.Radius) <= 0 || math.Abs(c.Height) <= 0 {
			return fmt.Errorf("physics: cone collider needs Radius > 0 and Height > 0, got radius %v height %v",
				c.Radius, c.Height)
		}
	case ShapeConvexHull:
		if len(c.hull) < 4 {
			return fmt.Errorf("physics: convex hull collider needs at least 4 Vertices, got %d", len(c.hull))
		}
		if !hullHasVolume(c.hull) {
			return fmt.Errorf("physics: convex hull collider Vertices are coplanar, which has no volume")
		}
	case ShapeTriangleMesh:
		if c.mesh == nil || len(c.mesh.tris) == 0 {
			return fmt.Errorf("physics: triangle mesh collider needs Vertices plus Indices in multiples of 3 that name valid corners")
		}
	default:
		return fmt.Errorf("physics: unknown collider shape %d", int(c.Shape))
	}
	return nil
}

func (c *Collider) WorldCenter() Vec3 {
	if c == nil {
		return Vec3{}
	}
	if c.Body == nil {
		return c.Offset
	}
	return c.Body.Position.Add(c.Body.Rotation.Rotate(c.Offset))
}

func (c *Collider) WorldRotation() Quat {
	if c == nil {
		return IdentityQuat()
	}
	if c.Body == nil {
		return c.Rotation.Normalize()
	}
	return c.Body.Rotation.Mul(c.Rotation).Normalize()
}

func (c *Collider) Plane() (Vec3, float64) {
	normal := c.Normal
	if normal.Len2() <= epsilon {
		normal = Vec3{Y: 1}
	}
	normal = c.WorldRotation().Rotate(normal).Normalize()
	point := c.WorldCenter().Add(normal.Mul(c.Distance))
	return normal, normal.Dot(point)
}

func (c *Collider) AABB() AABB {
	if c == nil {
		return EmptyAABB()
	}
	// An unusable collider still needs a finite, tiny box. An infinite box
	// would put it in the broadphase's unbounded list, where it pairs against
	// every other collider every step while never producing a contact.
	if c.invalid != nil {
		center := c.WorldCenter()
		return AABB{Min: center, Max: center}
	}

	switch c.Shape {
	case ShapeSphere:
		radius := math.Abs(c.Radius)
		return AABBFromCenterHalfExtents(c.WorldCenter(), Vec3{X: radius, Y: radius, Z: radius})
	case ShapeBox:
		return obbAABB(c.WorldCenter(), c.WorldRotation(), c.halfExtents())
	case ShapePlane:
		return AABB{
			Min: Vec3{X: math.Inf(-1), Y: math.Inf(-1), Z: math.Inf(-1)},
			Max: Vec3{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)},
		}
	case ShapeCapsule:
		// Capsule is a cylinder of Height + 2*Radius total length along the
		// collider's local Y axis, with hemisphere caps. World-space AABB is
		// the enclosing ball-sweep.
		radius := math.Abs(c.Radius)
		halfAxis := math.Abs(c.Height) * 0.5
		axisWorld := c.WorldRotation().Rotate(Vec3{Y: halfAxis})
		center := c.WorldCenter()
		halfExt := Vec3{
			X: math.Abs(axisWorld.X) + radius,
			Y: math.Abs(axisWorld.Y) + radius,
			Z: math.Abs(axisWorld.Z) + radius,
		}
		return AABBFromCenterHalfExtents(center, halfExt)
	case ShapeCylinder:
		// A cylinder of half height h and radius r around unit axis u spans
		// h*|u.e| + r*sqrt(1 - (u.e)^2) along each world axis e. That is the
		// tight bound, not the bound of the enclosing box.
		radius := math.Abs(c.Radius)
		half := math.Abs(c.Height) * 0.5
		axis := c.WorldAxis()
		return AABBFromCenterHalfExtents(c.WorldCenter(), Vec3{
			X: half*math.Abs(axis.X) + radius*discExtent(axis.X),
			Y: half*math.Abs(axis.Y) + radius*discExtent(axis.Y),
			Z: half*math.Abs(axis.Z) + radius*discExtent(axis.Z),
		})
	case ShapeCone:
		// The cone is the convex hull of its apex point and its base disc, so
		// the bound is the union of the apex and the base disc bound.
		radius := math.Abs(c.Radius)
		half := math.Abs(c.Height) * 0.5
		axis := c.WorldAxis()
		center := c.WorldCenter()
		apex := center.Add(axis.Mul(half))
		base := center.Sub(axis.Mul(half))
		discHalf := Vec3{
			X: radius * discExtent(axis.X),
			Y: radius * discExtent(axis.Y),
			Z: radius * discExtent(axis.Z),
		}
		return AABB{Min: apex, Max: apex}.Union(AABBFromCenterHalfExtents(base, discHalf))
	case ShapeConvexHull, ShapeTriangleMesh:
		// Project the local bounds through the world rotation. Slightly loose
		// for a rotated hull, and O(1) instead of O(vertices).
		center := c.WorldCenter()
		rotation := c.WorldRotation()
		localCenter := c.localBounds.Center()
		localHalf := c.localBounds.Size().Mul(0.5)
		box := obbAABB(center.Add(rotation.Rotate(localCenter)), rotation, localHalf)
		return box
	default:
		center := c.WorldCenter()
		return AABB{Min: center, Max: center}
	}
}

// WorldAxis returns the collider's local +Y axis in world space. Capsule,
// cylinder and cone are all built around that axis.
func (c *Collider) WorldAxis() Vec3 {
	if c == nil {
		return Vec3{Y: 1}
	}
	axis := c.WorldRotation().Rotate(Vec3{Y: 1})
	if axis.Len2() <= epsilon {
		return Vec3{Y: 1}
	}
	return axis.Normalize()
}

// CapsuleAxisEndpoints returns the two hemisphere-center endpoints of a
// capsule collider in world space. For non-capsule shapes it returns both
// endpoints equal to WorldCenter.
func (c *Collider) CapsuleAxisEndpoints() (Vec3, Vec3) {
	if c == nil || c.Shape != ShapeCapsule {
		center := c.WorldCenter()
		return center, center
	}
	halfAxis := math.Abs(c.Height) * 0.5
	axisWorld := c.WorldRotation().Rotate(Vec3{Y: halfAxis})
	center := c.WorldCenter()
	return center.Sub(axisWorld), center.Add(axisWorld)
}

// CylinderCapCenters returns the two cap-disc centers of a cylinder collider
// in world space, bottom first.
func (c *Collider) CylinderCapCenters() (Vec3, Vec3) {
	center := c.WorldCenter()
	if c == nil || c.Shape != ShapeCylinder {
		return center, center
	}
	offset := c.WorldAxis().Mul(math.Abs(c.Height) * 0.5)
	return center.Sub(offset), center.Add(offset)
}

// ConeApexAndBase returns the world-space apex point and base-disc center of a
// cone collider. The apex sits on the collider's local +Y side, which matches
// the renderer's cone orientation.
func (c *Collider) ConeApexAndBase() (Vec3, Vec3) {
	center := c.WorldCenter()
	if c == nil || c.Shape != ShapeCone {
		return center, center
	}
	offset := c.WorldAxis().Mul(math.Abs(c.Height) * 0.5)
	return center.Add(offset), center.Sub(offset)
}

// BoundingRadius returns the radius of a sphere centred on WorldCenter that
// encloses the collider. Planes have no finite bound and report 0.
func (c *Collider) BoundingRadius() float64 {
	if c == nil || c.invalid != nil {
		return 0
	}
	switch c.Shape {
	case ShapeSphere:
		return math.Abs(c.Radius)
	case ShapeCapsule:
		return math.Abs(c.Radius) + math.Abs(c.Height)*0.5
	case ShapeBox:
		return c.halfExtents().Len()
	case ShapeCylinder, ShapeCone:
		half := math.Abs(c.Height) * 0.5
		return math.Sqrt(half*half + c.Radius*c.Radius)
	case ShapeConvexHull, ShapeTriangleMesh:
		// The local bounds are not centred on the collider origin, so measure
		// to the farthest corner rather than to the box centre.
		return math.Max(c.localBounds.Min.Len(), c.localBounds.Max.Len())
	default:
		return 0
	}
}

func (c *Collider) halfExtents() Vec3 {
	return Vec3{
		X: math.Abs(c.Width) * 0.5,
		Y: math.Abs(c.Height) * 0.5,
		Z: math.Abs(c.Depth) * 0.5,
	}
}

// discExtent returns sqrt(1 - component^2), the half-width of a unit disc
// whose normal has the given component along a world axis.
func discExtent(component float64) float64 {
	rest := 1 - component*component
	if rest <= 0 {
		return 0
	}
	return math.Sqrt(rest)
}

// obbAABB returns the world AABB of an oriented box.
func obbAABB(center Vec3, rotation Quat, half Vec3) AABB {
	axisX := rotation.Rotate(Vec3{X: 1}).Abs()
	axisY := rotation.Rotate(Vec3{Y: 1}).Abs()
	axisZ := rotation.Rotate(Vec3{Z: 1}).Abs()
	extents := Vec3{
		X: axisX.X*half.X + axisY.X*half.Y + axisZ.X*half.Z,
		Y: axisX.Y*half.X + axisY.Y*half.Y + axisZ.Y*half.Z,
		Z: axisX.Z*half.X + axisY.Z*half.Y + axisZ.Z*half.Z,
	}
	return AABBFromCenterHalfExtents(center, extents)
}

func boundsOfPoints(points []Vec3) AABB {
	if len(points) == 0 {
		return AABB{}
	}
	bounds := AABB{Min: points[0], Max: points[0]}
	for _, p := range points[1:] {
		bounds.Min = bounds.Min.Min(p)
		bounds.Max = bounds.Max.Max(p)
	}
	return bounds
}

// hullHasVolume reports whether the point cloud spans three dimensions. A flat
// cloud cannot seed the EPA tetrahedron, so it is rejected at construction.
func hullHasVolume(points []Vec3) bool {
	if len(points) < 4 {
		return false
	}
	const areaTolerance = 1e-12
	const volumeTolerance = 1e-9
	base := points[0]
	var edge1 Vec3
	found1 := false
	for _, p := range points[1:] {
		d := p.Sub(base)
		if d.Len2() > areaTolerance {
			edge1 = d
			found1 = true
			break
		}
	}
	if !found1 {
		return false
	}
	var normal Vec3
	found2 := false
	for _, p := range points[1:] {
		n := edge1.Cross(p.Sub(base))
		if n.Len2() > areaTolerance {
			normal = n.Normalize()
			found2 = true
			break
		}
	}
	if !found2 {
		return false
	}
	for _, p := range points[1:] {
		if math.Abs(normal.Dot(p.Sub(base))) > volumeTolerance {
			return true
		}
	}
	return false
}
