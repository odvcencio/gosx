package physics

import "strings"

// WorldSpec is a declarative, purely-data description of a physics world
// that can be constructed all at once via BuildWorld. It mirrors the shape
// of the scene IR's physics section so the scene package can hand us a
// spec without exposing scene types as physics dependencies.
//
// Every field is copy-by-value; BuildWorld does not retain the spec.
type WorldSpec struct {
	Config      WorldConfig
	Bodies      []BodySpec
	Constraints []ConstraintSpec
	// Static colliders that are not attached to a body (typical: ground
	// plane or fixed obstacles). They participate in collision detection
	// but are never integrated.
	Static []ColliderConfig
}

// BodySpec declares one rigid body plus its attached colliders.
type BodySpec struct {
	// Body is the physics BodyConfig used by World.AddBody. Position and
	// Rotation here are the initial world-space pose; subsequent frames
	// are driven by the solver.
	Body BodyConfig
	// Colliders attached to the body. A body with zero colliders still
	// integrates under gravity but will never generate contacts.
	Colliders []ColliderConfig
}

// ConstraintSpec declares one constraint between two bodies. The first
// shipped declarative constraint is "distance"; future constraint kinds can
// extend this struct without changing BuildWorld callers.
type ConstraintSpec struct {
	Kind       string
	BodyAID    string
	BodyBID    string
	BodyAIndex int
	BodyBIndex int
	AttachA    Vec3
	AttachB    Vec3
	Distance   float64
	Softness   float64
}

// BuildWorld constructs a fully-populated physics.World from a WorldSpec.
// Bodies and their colliders are added in declaration order; static
// colliders are added afterwards (scene geometry usually wants to be in
// the broadphase ahead of the dynamic bodies, but ordering is immaterial
// for the spatial hash — this is a readability convention only).
func BuildWorld(spec WorldSpec) *World {
	world := NewWorld(spec.Config)
	byID := make(map[string]*RigidBody, len(spec.Bodies))
	byIndex := make(map[int]*RigidBody, len(spec.Bodies))
	for _, bodySpec := range spec.Bodies {
		body := world.AddBody(bodySpec.Body)
		if body.ID != "" {
			byID[body.ID] = body
		}
		byIndex[body.index] = body
		for _, collider := range bodySpec.Colliders {
			body.AddCollider(collider)
		}
	}
	for _, constraint := range spec.Constraints {
		addSpecConstraint(world, constraint, byID, byIndex)
	}
	for _, static := range spec.Static {
		world.AddCollider(static)
	}
	return world
}

func addSpecConstraint(world *World, spec ConstraintSpec, byID map[string]*RigidBody, byIndex map[int]*RigidBody) {
	if world == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(spec.Kind)) {
	case "", "distance":
		bodyA := specBody(spec.BodyAID, spec.BodyAIndex, byID, byIndex)
		bodyB := specBody(spec.BodyBID, spec.BodyBIndex, byID, byIndex)
		if bodyA == nil || bodyB == nil {
			return
		}
		distance := spec.Distance
		if distance <= 0 {
			distance = bodyA.Position.Add(bodyA.Rotation.Rotate(spec.AttachA)).
				Distance(bodyB.Position.Add(bodyB.Rotation.Rotate(spec.AttachB)))
		}
		world.AddConstraint(&DistanceConstraint{
			BodyA:          bodyA,
			BodyB:          bodyB,
			AttachA:        spec.AttachA,
			AttachB:        spec.AttachB,
			TargetDistance: distance,
			Softness:       spec.Softness,
		})
	}
}

func specBody(id string, index int, byID map[string]*RigidBody, byIndex map[int]*RigidBody) *RigidBody {
	if id != "" {
		if body := byID[id]; body != nil {
			return body
		}
	}
	if index > 0 {
		return byIndex[index]
	}
	return nil
}
