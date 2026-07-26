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

	// Inertia overrides the principal moments the collider shapes imply. Leave
	// it zero to derive the tensor from the attached colliders. Give it
	// positive entries to model a mass distribution the shapes do not describe.
	Inertia Vec3

	// LockRotation makes the body refuse every angular impulse. Contacts and
	// constraints still change its linear velocity. Use it for characters and
	// for props that must never tip over.
	LockRotation bool
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
	// LockRotation stops every angular impulse. Set it through BodyConfig or
	// through SetLockRotation, never by hand, so the cached tensors stay right.
	LockRotation bool

	force     Vec3
	torque    Vec3
	world     *World
	index     int
	sleeping  bool
	colliders []*Collider

	// inertiaOverride holds the caller-supplied principal moments. A zero value
	// means the collider shapes decide the tensor.
	inertiaOverride Vec3
	// invInertiaLocal is the inverse inertia tensor in the body frame. It is
	// zero for a static, kinematic or rotation-locked body.
	invInertiaLocal mat3
	// invInertiaWorld is invInertiaLocal rotated into world space. The world
	// refreshes it once per step, before contacts are solved.
	invInertiaWorld mat3
	// invInertiaRotation is the rotation invInertiaWorld was built from. A body
	// that does not turn keeps its tensor, which saves two matrix products per
	// step for every resting body in the scene.
	invInertiaRotation Quat
	// pseudoActive marks a body that received a position pass impulse this
	// step. It keeps the pass proportional to the contacts, not to the world.
	pseudoActive bool
	// sleepTimer counts how long the body has stayed below the sleep speeds.
	sleepTimer float64
	// sleepSlow and sleepBlocked are per-step scratch of the sleep pass.
	sleepSlow    bool
	sleepBlocked bool

	// pseudoVelocity and pseudoAngular carry the position pass. They exist only
	// between the solver and the integrator, and they never leak into the
	// reported body velocity.
	pseudoVelocity Vec3
	pseudoAngular  Vec3
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
		LockRotation:    config.LockRotation,
		inertiaOverride: config.Inertia,
	}
	body.setMass(config.Mass)
	return body
}

func (b *RigidBody) setMass(mass float64) {
	b.Mass = mass
	if mass > 0 && !b.IsKinematic {
		b.InvMass = 1 / mass
	} else {
		b.InvMass = 0
	}
	b.UpdateInertia()
}

// SetMass changes the body mass and rebuilds the inertia tensor.
func (b *RigidBody) SetMass(mass float64) {
	if b == nil {
		return
	}
	b.setMass(mass)
}

// SetInertia replaces the principal moments the collider shapes imply. Pass a
// zero vector to go back to the derived tensor.
func (b *RigidBody) SetInertia(inertia Vec3) {
	if b == nil {
		return
	}
	b.inertiaOverride = inertia
	b.UpdateInertia()
}

// SetLockRotation turns the angular response of the body on or off.
func (b *RigidBody) SetLockRotation(locked bool) {
	if b == nil {
		return
	}
	b.LockRotation = locked
	b.UpdateInertia()
}

// UpdateInertia rebuilds the body-local inverse inertia tensor from the current
// mass and colliders. AddCollider and setMass already call it; call it by hand
// only after changing a collider's size in place.
func (b *RigidBody) UpdateInertia() {
	if b == nil {
		return
	}
	b.invInertiaLocal = mat3{}
	b.invInertiaWorld = mat3{}
	b.invInertiaRotation = Quat{}
	if b.InvMass <= 0 || b.LockRotation {
		return
	}

	var tensor mat3
	if b.inertiaOverride.X > 0 || b.inertiaOverride.Y > 0 || b.inertiaOverride.Z > 0 {
		tensor = mat3Diagonal(b.inertiaOverride)
	} else {
		tensor = inertiaTensorFromColliders(b.Mass, b.colliders)
	}
	inverse, ok := tensor.inverse()
	if !ok {
		return
	}
	b.invInertiaLocal = inverse
	b.refreshWorldInertia()
}

// refreshWorldInertia rebuilds the world-space inverse inertia tensor from the
// current rotation. It returns at once when the rotation has not changed since
// the last rebuild.
func (b *RigidBody) refreshWorldInertia() {
	if b == nil {
		return
	}
	if b.invInertiaLocal.isZero() {
		b.invInertiaWorld = mat3{}
		return
	}
	if b.Rotation == b.invInertiaRotation {
		return
	}
	b.invInertiaWorld = rotateTensor(mat3FromQuat(b.Rotation), b.invInertiaLocal)
	b.invInertiaRotation = b.Rotation
}

// InverseInertiaWorld returns the world-space inverse inertia tensor entries as
// three column vectors. A static, kinematic or rotation-locked body returns
// three zero vectors.
func (b *RigidBody) InverseInertiaWorld() (Vec3, Vec3, Vec3) {
	if b == nil {
		return Vec3{}, Vec3{}, Vec3{}
	}
	return b.invInertiaWorld.x, b.invInertiaWorld.y, b.invInertiaWorld.z
}

// Inertia returns the body-local principal moments the solver uses. It returns
// a zero vector when the body cannot rotate.
func (b *RigidBody) Inertia() Vec3 {
	if b == nil || b.invInertiaLocal.isZero() {
		return Vec3{}
	}
	tensor, ok := b.invInertiaLocal.inverse()
	if !ok {
		return Vec3{}
	}
	return Vec3{X: tensor.x.X, Y: tensor.y.Y, Z: tensor.z.Z}
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

// Wake ends sleep and restarts the countdown that leads back to it.
func (b *RigidBody) Wake() {
	if b != nil {
		b.sleeping = false
		b.sleepTimer = 0
	}
}

func (b *RigidBody) AddCollider(config ColliderConfig) *Collider {
	collider := newCollider(b, config)
	b.colliders = append(b.colliders, collider)
	b.UpdateInertia()
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

// ApplyImpulse adds a linear impulse at a world point. An impulse that does not
// pass through the centre of mass also changes the angular velocity, through
// the world inverse inertia tensor.
func (b *RigidBody) ApplyImpulse(impulse, worldPoint Vec3) {
	if !b.IsDynamic() {
		return
	}
	b.Velocity = b.Velocity.Add(impulse.Mul(b.InvMass))
	r := worldPoint.Sub(b.Position)
	if r.Len2() > epsilon && !b.invInertiaWorld.isZero() {
		b.AngularVelocity = b.AngularVelocity.Add(b.invInertiaWorld.mul(r.Cross(impulse)))
	}
	b.Wake()
}

// ApplyAngularImpulse adds an impulse that changes only the angular velocity.
func (b *RigidBody) ApplyAngularImpulse(impulse Vec3) {
	if !b.IsDynamic() || b.invInertiaWorld.isZero() {
		return
	}
	b.AngularVelocity = b.AngularVelocity.Add(b.invInertiaWorld.mul(impulse))
	b.Wake()
}

func (b *RigidBody) clearForces() {
	b.force = Vec3{}
	b.torque = Vec3{}
}
