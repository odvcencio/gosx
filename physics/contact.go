package physics

type ContactPoint struct {
	Point          Vec3
	LocalA         Vec3
	LocalB         Vec3
	Penetration    float64
	NormalImpulse  float64
	TangentImpulse [2]float64
}

type ContactManifold struct {
	BodyA       *RigidBody
	BodyB       *RigidBody
	ColliderA   *Collider
	ColliderB   *Collider
	Normal      Vec3
	Points      [4]ContactPoint
	PointCount  int
	Friction    float64
	Restitution float64
}

func (m ContactManifold) IsTrigger() bool {
	return (m.ColliderA != nil && m.ColliderA.IsTrigger) ||
		(m.ColliderB != nil && m.ColliderB.IsTrigger)
}

func makeContactManifold(a, b *Collider, normal Vec3, points []ContactPoint) ContactManifold {
	manifold := ContactManifold{
		ColliderA:   a,
		ColliderB:   b,
		Normal:      normal.Normalize(),
		Friction:    combinedFriction(a, b),
		Restitution: combinedRestitution(a, b),
	}
	if a != nil {
		manifold.BodyA = a.Body
	}
	if b != nil {
		manifold.BodyB = b.Body
	}
	for i := 0; i < len(points) && i < len(manifold.Points); i++ {
		manifold.Points[i] = points[i]
		manifold.PointCount++
	}
	return manifold
}

func makeContactPoint(a, b *Collider, point Vec3, penetration float64) ContactPoint {
	return ContactPoint{
		Point:       point,
		LocalA:      localPoint(a, point),
		LocalB:      localPoint(b, point),
		Penetration: penetration,
	}
}

func localPoint(c *Collider, point Vec3) Vec3 {
	if c == nil || c.Body == nil {
		return point
	}
	return c.Body.Rotation.Inverse().Rotate(point.Sub(c.Body.Position))
}

func flipManifold(m ContactManifold) ContactManifold {
	m.BodyA, m.BodyB = m.BodyB, m.BodyA
	m.ColliderA, m.ColliderB = m.ColliderB, m.ColliderA
	m.Normal = m.Normal.Neg()
	for i := 0; i < m.PointCount; i++ {
		m.Points[i].LocalA, m.Points[i].LocalB = m.Points[i].LocalB, m.Points[i].LocalA
	}
	return m
}

func combinedRestitution(a, b *Collider) float64 {
	return maxFloat(bodyRestitution(a), bodyRestitution(b))
}

func combinedFriction(a, b *Collider) float64 {
	af := bodyFriction(a)
	bf := bodyFriction(b)
	if af == 0 {
		return bf
	}
	if bf == 0 {
		return af
	}
	if af < bf {
		return af
	}
	return bf
}

func bodyRestitution(c *Collider) float64 {
	if c == nil || c.Body == nil {
		return 0
	}
	return c.Body.Restitution
}

func bodyFriction(c *Collider) float64 {
	if c == nil || c.Body == nil {
		return 0
	}
	return c.Body.Friction
}
