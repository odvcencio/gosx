package scene

// PhysicsWorld declares scene-wide physics configuration. A zero value means
// physics is disabled for this scene; set any field (or attach a RigidBody
// to a Mesh) to enable it.
//
// The scene IR carries the config alongside declared bodies so server-side
// wiring can rebuild an authoritative physics.World from the IR and run it
// behind a sim.Runner + hub for broadcast.
type PhysicsWorld struct {
	// Gravity is the constant acceleration (world units per second²).
	// Defaults to (0, -9.81, 0) when any other physics field is set.
	Gravity Vector3

	// FixedTimestep is the deterministic solver tick length in seconds.
	// Defaults to 1/60.
	FixedTimestep float64

	// SolverIterations is the sequential-impulse iteration count. Higher
	// values improve stacking stability at a proportional CPU cost.
	// Defaults to 8.
	SolverIterations int

	// BroadphaseCell is the spatial-hash cell size. Tune to roughly 2× the
	// typical body diameter for best broadphase culling.
	BroadphaseCell float64

	// DisableWarmStart turns off cross-frame impulse caching. Leave false
	// for normal scenes; set true only for deterministic replay tests.
	DisableWarmStart bool

	// Topic is the hub channel name that the server-side physics runner
	// publishes WorldState broadcasts on. If empty, defaults to the
	// mount-scoped topic "scene3d:physics:{programRef}".
	Topic string

	// Colliders holds static (bodyless) colliders, typically the scene
	// ground plane or fixed obstacles. They participate in collision
	// detection but are never integrated.
	Colliders []Collider3D

	// Constraints are scene-level joints between rigid bodies. They
	// reference bodies by the stable Mesh.ID used by RigidBody3D.
	Constraints []Constraint3D
}

// RigidBody3D attaches a physics body to a Mesh. The body's world-space
// initial pose is taken from the Mesh's Position/Rotation; subsequent
// transforms are driven by the physics simulation.
type RigidBody3D struct {
	// Mass in kilograms. A value <= 0 makes the body static/kinematic
	// (infinite mass) — use Static=true for explicit semantics.
	Mass float64

	// Static makes the body non-dynamic (never integrated). Equivalent to
	// Mass <= 0 but preserves a declared Mass for future kinematic work.
	Static bool

	// Restitution is the bounciness [0,1].
	Restitution float64

	// Friction is the Coulomb friction coefficient [0,∞).
	Friction float64

	// LinearDamping reduces linear velocity each tick (0 = no damping).
	LinearDamping float64

	// AngularDamping reduces angular velocity each tick.
	AngularDamping float64

	// Velocity and AngularVelocity seed the solver on body creation.
	Velocity        Vector3
	AngularVelocity Vector3

	// Colliders attached to this body. At least one collider is required
	// for the body to participate in collisions.
	Colliders []Collider3D
}

// Collider3D describes a collision shape attached either to a RigidBody3D
// or to the scene's static geometry via PhysicsWorld.Colliders.
type Collider3D struct {
	// Shape is one of: "box", "sphere", "capsule", "plane".
	// (Capsule is narrowphase-supported in the physics engine; convex and
	// triangle meshes are forthcoming.)
	Shape string

	// Offset applies a local translation from the body's center.
	Offset Vector3

	// Rotation applies a local rotation (Euler) relative to the body.
	Rotation Euler

	// IsTrigger disables contact response; only collision events are emitted.
	IsTrigger bool

	// Box dimensions (Shape == "box").
	Width  float64
	Height float64
	Depth  float64

	// Sphere / Capsule radius (Shape == "sphere" | "capsule").
	Radius float64

	// Plane normal + distance from origin along that normal (Shape == "plane").
	Normal   Vector3
	Distance float64
}

// Constraint3D describes a physics joint between two scene rigid bodies.
// The first supported kind is "distance", a rod/spring between body-local
// attach points.
type Constraint3D struct {
	Kind     string
	BodyA    string
	BodyB    string
	AttachA  Vector3
	AttachB  Vector3
	Distance float64
	Softness float64
}
