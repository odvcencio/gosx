package physics

import (
	"errors"
	"fmt"
	"strings"
)

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

	// Diagnostics carries problems that the spec producer already found, such
	// as a collider the author declared with a shape name this engine cannot
	// build. BuildWorldChecked returns them together with the world's own
	// diagnostics, so a mistake made upstream still fails loudly.
	Diagnostics []string
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

// ConstraintSpec declares one constraint between two bodies.
//
// Kind names the joint: "distance", "point" (ball and socket), "hinge"
// (revolute) or "fixed" (weld). An empty Kind means "distance", which keeps
// older specs working.
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

	// AxisA and AxisB name the hinge axis in each body's local frame. They are
	// ignored by every other kind. Leave them zero to hinge about local +Y.
	AxisA Vec3
	AxisB Vec3

	// MotorEnabled drives a hinge at MotorSpeed radians per second, with at
	// most MaxMotorTorque newton metres. A zero MaxMotorTorque is unlimited.
	MotorEnabled   bool
	MotorSpeed     float64
	MaxMotorTorque float64

	// LimitEnabled stops a hinge between LowerLimit and UpperLimit radians,
	// measured from the pose the joint held when it first ran.
	LimitEnabled bool
	LowerLimit   float64
	UpperLimit   float64
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

// BuildWorldChecked builds the world and returns an error when any declared
// collider cannot collide. Prefer it over BuildWorld: a shape that the engine
// silently drops is a bug in the scene, not a shape that simply misses.
//
// The world is returned even when the error is non-nil, so a caller may choose
// to log the problem and keep simulating the rest of the scene.
func BuildWorldChecked(spec WorldSpec) (*World, error) {
	world := BuildWorld(spec)
	var errs []error
	for _, message := range spec.Diagnostics {
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		errs = append(errs, fmt.Errorf("physics: %s", message))
	}
	if err := world.Err(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return world, nil
	}
	return world, errors.Join(errs...)
}

func addSpecConstraint(world *World, spec ConstraintSpec, byID map[string]*RigidBody, byIndex map[int]*RigidBody) {
	if world == nil {
		return
	}
	bodyA := specBody(spec.BodyAID, spec.BodyAIndex, byID, byIndex)
	bodyB := specBody(spec.BodyBID, spec.BodyBIndex, byID, byIndex)
	if bodyA == nil || bodyB == nil {
		return
	}

	switch strings.ToLower(strings.TrimSpace(spec.Kind)) {
	case "", "distance":
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
	case "point", "ballsocket", "ball-socket", "balljoint":
		world.AddConstraint(&PointConstraint{
			BodyA:   bodyA,
			BodyB:   bodyB,
			AttachA: spec.AttachA,
			AttachB: spec.AttachB,
		})
	case "hinge", "revolute":
		axisA := spec.AxisA
		if axisA.Len2() <= epsilon {
			axisA = Vec3{Y: 1}
		}
		axisB := spec.AxisB
		if axisB.Len2() <= epsilon {
			axisB = axisA
		}
		world.AddConstraint(&HingeConstraint{
			BodyA:          bodyA,
			BodyB:          bodyB,
			AttachA:        spec.AttachA,
			AttachB:        spec.AttachB,
			AxisA:          axisA,
			AxisB:          axisB,
			MotorEnabled:   spec.MotorEnabled,
			MotorSpeed:     spec.MotorSpeed,
			MaxMotorTorque: spec.MaxMotorTorque,
			LimitEnabled:   spec.LimitEnabled,
			LowerLimit:     spec.LowerLimit,
			UpperLimit:     spec.UpperLimit,
		})
	case "fixed", "weld", "lock":
		world.AddConstraint(&FixedConstraint{
			BodyA:   bodyA,
			BodyB:   bodyB,
			AttachA: spec.AttachA,
			AttachB: spec.AttachB,
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
