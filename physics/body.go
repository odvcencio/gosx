package physics

// BodyConfig configures a rigid body at creation time.
type BodyConfig struct {
	ID              string
	Mass            float64
	Position        Vec3
	Rotation        Quat
	Velocity        Vec3
	AngularVelocity Vec3
	Restitution     float64
	Friction        float64
	LinearDamping   float64
	AngularDamping  float64
	IsKinematic     bool
	CanSleep        bool
}

// RigidBody is a simulation body with linear and angular state.
type RigidBody struct {
	ID              string
	Mass            float64
	InvMass         float64
	Position        Vec3
	Rotation        Quat
	Velocity        Vec3
	AngularVelocity Vec3
	Restitution     float64
	Friction        float64
	LinearDamping   float64
	AngularDamping  float64
	IsKinematic     bool
	CanSleep        bool

	force     Vec3
	torque    Vec3
	world     *World
	index     int
	sleeping  bool
	colliders []*Collider
}

func NewRigidBody(config BodyConfig) *RigidBody {
	rotation := config.Rotation
	if rotation.X == 0 && rotation.Y == 0 && rotation.Z == 0 && rotation.W == 0 {
		rotation = IdentityQuat()
	}

	body := &RigidBody{
		ID:              config.ID,
		Mass:            config.Mass,
		Position:        config.Position,
		Rotation:        rotation.Normalize(),
		Velocity:        config.Velocity,
		AngularVelocity: config.AngularVelocity,
		Restitution:     config.Restitution,
		Friction:        config.Friction,
		LinearDamping:   config.LinearDamping,
		AngularDamping:  config.AngularDamping,
		IsKinematic:     config.IsKinematic,
		CanSleep:        config.CanSleep,
	}
	body.setMass(config.Mass)
	return body
}

func (b *RigidBody) setMass(mass float64) {
	b.Mass = mass
	if mass > 0 && !b.IsKinematic {
		b.InvMass = 1 / mass
		return
	}
	b.InvMass = 0
}

func (b *RigidBody) IsStatic() bool {
	return b == nil || (b.InvMass == 0 && !b.IsKinematic)
}

func (b *RigidBody) IsDynamic() bool {
	return b != nil && b.InvMass > 0 && !b.IsKinematic
}

func (b *RigidBody) IsSleeping() bool {
	return b != nil && b.sleeping
}

func (b *RigidBody) Wake() {
	if b != nil {
		b.sleeping = false
	}
}

func (b *RigidBody) AddCollider(config ColliderConfig) *Collider {
	collider := newCollider(b, config)
	b.colliders = append(b.colliders, collider)
	if b.world != nil {
		b.world.registerCollider(collider)
	}
	return collider
}

func (b *RigidBody) Colliders() []*Collider {
	if b == nil || len(b.colliders) == 0 {
		return nil
	}
	colliders := make([]*Collider, len(b.colliders))
	copy(colliders, b.colliders)
	return colliders
}

func (b *RigidBody) ApplyForce(force Vec3) {
	if !b.IsDynamic() {
		return
	}
	b.force = b.force.Add(force)
	b.Wake()
}

func (b *RigidBody) ApplyTorque(torque Vec3) {
	if !b.IsDynamic() {
		return
	}
	b.torque = b.torque.Add(torque)
	b.Wake()
}

func (b *RigidBody) ApplyImpulse(impulse, worldPoint Vec3) {
	if !b.IsDynamic() {
		return
	}
	b.Velocity = b.Velocity.Add(impulse.Mul(b.InvMass))
	r := worldPoint.Sub(b.Position)
	if r.Len2() > epsilon {
		b.AngularVelocity = b.AngularVelocity.Add(r.Cross(impulse).Mul(b.InvMass))
	}
	b.Wake()
}

func (b *RigidBody) clearForces() {
	b.force = Vec3{}
	b.torque = Vec3{}
}
