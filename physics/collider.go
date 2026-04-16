package physics

import "math"

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

	return &Collider{
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

	switch c.Shape {
	case ShapeSphere:
		radius := math.Abs(c.Radius)
		return AABBFromCenterHalfExtents(c.WorldCenter(), Vec3{X: radius, Y: radius, Z: radius})
	case ShapeBox:
		center := c.WorldCenter()
		half := c.halfExtents()
		rotation := c.WorldRotation()
		axisX := rotation.Rotate(Vec3{X: 1}).Abs()
		axisY := rotation.Rotate(Vec3{Y: 1}).Abs()
		axisZ := rotation.Rotate(Vec3{Z: 1}).Abs()
		extents := Vec3{
			X: axisX.X*half.X + axisY.X*half.Y + axisZ.X*half.Z,
			Y: axisX.Y*half.X + axisY.Y*half.Y + axisZ.Y*half.Z,
			Z: axisX.Z*half.X + axisY.Z*half.Y + axisZ.Z*half.Z,
		}
		return AABBFromCenterHalfExtents(center, extents)
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
	default:
		return EmptyAABB()
	}
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

func (c *Collider) halfExtents() Vec3 {
	return Vec3{
		X: math.Abs(c.Width) * 0.5,
		Y: math.Abs(c.Height) * 0.5,
		Z: math.Abs(c.Depth) * 0.5,
	}
}
