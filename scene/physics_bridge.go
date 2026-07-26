package scene

import (
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/physics"
)

// PhysicsSpec lowers the scene IR's physics declaration into a
// physics.WorldSpec that BuildWorld can consume. Returns an empty spec if
// the scene declares no physics.
//
// The scene IR is authoritative for configuration and bodies; this function
// is a pure translator — no physics.World is instantiated. Callers drive
// simulation lifecycle separately (typically via physics.NewRunner paired
// with a hub).
func (ir IR) PhysicsSpec() physics.WorldSpec {
	if ir.Physics == nil {
		return physics.WorldSpec{}
	}
	p := ir.Physics

	cfg := physics.WorldConfig{
		Gravity: physics.Vec3{
			X: p.Gravity.X,
			Y: p.Gravity.Y,
			Z: p.Gravity.Z,
		},
		FixedTimestep:    p.FixedTimestep,
		SolverIterations: p.SolverIterations,
		BroadPhaseCell:   p.BroadphaseCell,
		DisableWarmStart: p.DisableWarmStart,
	}

	spec := physics.WorldSpec{Config: cfg}
	var diagnostics []string

	for _, body := range p.Bodies {
		mass := body.Mass
		if body.Static {
			mass = 0
		}
		colliders, bodyDiagnostics := collidersToPhysics(body.Colliders, "body "+strings.TrimSpace(body.ID))
		diagnostics = append(diagnostics, bodyDiagnostics...)
		spec.Bodies = append(spec.Bodies, physics.BodySpec{
			Body: physics.BodyConfig{
				ID:              strings.TrimSpace(body.ID),
				Mass:            mass,
				Position:        physics.Vec3{X: body.Position.X, Y: body.Position.Y, Z: body.Position.Z},
				Rotation:        eulerToQuat(body.Rotation),
				Velocity:        physics.Vec3{X: body.Velocity.X, Y: body.Velocity.Y, Z: body.Velocity.Z},
				AngularVelocity: physics.Vec3{X: body.AngularVelocity.X, Y: body.AngularVelocity.Y, Z: body.AngularVelocity.Z},
				Restitution:     body.Restitution,
				Friction:        body.Friction,
				LinearDamping:   body.LinearDamping,
				AngularDamping:  body.AngularDamping,
			},
			Colliders: colliders,
		})
	}

	if len(p.Static) > 0 {
		static, staticDiagnostics := collidersToPhysics(p.Static, "scene static geometry")
		spec.Static = static
		diagnostics = append(diagnostics, staticDiagnostics...)
	}
	spec.Diagnostics = diagnostics
	if len(p.Constraints) > 0 {
		spec.Constraints = make([]physics.ConstraintSpec, 0, len(p.Constraints))
		for _, constraint := range p.Constraints {
			spec.Constraints = append(spec.Constraints, constraintToPhysics(constraint))
		}
	}

	return spec
}

// PhysicsTopic returns the hub topic name for physics state broadcasts,
// respecting an explicit IR override or falling back to the default
// per-mount topic derived from the program reference.
func (ir IR) PhysicsTopic(programRef string) string {
	if ir.Physics == nil {
		return ""
	}
	if topic := strings.TrimSpace(ir.Physics.Topic); topic != "" {
		return topic
	}
	trimmed := strings.TrimSpace(programRef)
	if trimmed == "" {
		return "scene3d:physics"
	}
	return "scene3d:physics:" + trimmed
}

// collidersToPhysics translates scene colliders. owner names the declaring
// body or the scene, so a diagnostic points the author at the right place.
//
// A shape the engine cannot build is reported, never dropped in silence. A
// dropped collider makes its body pass through the world, which is far harder
// to notice than an error.
func collidersToPhysics(src []IRCollider, owner string) ([]physics.ColliderConfig, []string) {
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]physics.ColliderConfig, 0, len(src))
	var diagnostics []string
	for index, c := range src {
		shape, err := shapeNameToKind(c.Shape)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s collider %d: %v", owner, index, err))
			continue
		}
		out = append(out, physics.ColliderConfig{
			Shape:     shape,
			Offset:    physics.Vec3{X: c.Offset.X, Y: c.Offset.Y, Z: c.Offset.Z},
			Rotation:  eulerToQuat(IRVector3{X: c.Rotation.X, Y: c.Rotation.Y, Z: c.Rotation.Z}),
			IsTrigger: c.IsTrigger,
			Width:     c.Width,
			Height:    c.Height,
			Depth:     c.Depth,
			Radius:    c.Radius,
			Normal:    physics.Vec3{X: c.Normal.X, Y: c.Normal.Y, Z: c.Normal.Z},
			Distance:  c.Distance,
		})
	}
	return out, diagnostics
}

// constraintToPhysics translates one scene constraint.
//
// The engine builds four joints: "distance", "point", "hinge" and "fixed". A
// scene names the joint through Kind and gives the two attach points. An empty
// Kind means "distance", which keeps older scenes working.
//
// IRConstraint carries no axis, motor or limit fields, so a scene-declared
// hinge turns about the body local +Y axis with no motor and no limit. Build
// the hinge through physics.HingeConstraint to set an axis, a motor or limits.
func constraintToPhysics(c IRConstraint) physics.ConstraintSpec {
	kind := strings.ToLower(strings.TrimSpace(c.Kind))
	if kind == "" {
		kind = "distance"
	}
	return physics.ConstraintSpec{
		Kind:     kind,
		BodyAID:  strings.TrimSpace(c.BodyA),
		BodyBID:  strings.TrimSpace(c.BodyB),
		AttachA:  physics.Vec3{X: c.AttachA.X, Y: c.AttachA.Y, Z: c.AttachA.Z},
		AttachB:  physics.Vec3{X: c.AttachB.X, Y: c.AttachB.Y, Z: c.AttachB.Z},
		Distance: c.Distance,
		Softness: c.Softness,
	}
}

// shapeNameToKind maps a scene shape name onto a physics collider shape.
//
// "convex" and "mesh" are rejected on purpose. The physics engine supports both
// shapes, but Collider3D carries no vertex or index buffer, so a scene-declared
// hull or mesh would have no geometry and would pass through every body. Build
// such colliders through the physics API, where ColliderConfig.Vertices and
// ColliderConfig.Indices supply the geometry.
func shapeNameToKind(name string) (physics.ColliderShape, error) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	switch trimmed {
	case "box":
		return physics.ShapeBox, nil
	case "sphere":
		return physics.ShapeSphere, nil
	case "capsule":
		return physics.ShapeCapsule, nil
	case "plane":
		return physics.ShapePlane, nil
	case "cylinder":
		return physics.ShapeCylinder, nil
	case "cone":
		return physics.ShapeCone, nil
	case "convex", "convexhull":
		return 0, fmt.Errorf("shape %q needs vertex data that Collider3D cannot carry; declare the hull through physics.ColliderConfig.Vertices instead", trimmed)
	case "mesh", "trianglemesh":
		return 0, fmt.Errorf("shape %q needs vertex and index data that Collider3D cannot carry; declare the mesh through physics.ColliderConfig.Vertices and Indices instead", trimmed)
	case "":
		return 0, fmt.Errorf("collider Shape is empty; use box, sphere, capsule, plane, cylinder or cone")
	}
	return 0, fmt.Errorf("unknown collider shape %q; use box, sphere, capsule, plane, cylinder or cone", trimmed)
}

// eulerToQuat converts XYZ Euler radians to a unit quaternion using the
// ZYX intrinsic / XYZ extrinsic convention that matches the existing
// Scene3D renderer. Fine for static initial poses; runtime updates from
// the solver use quaternions directly.
func eulerToQuat(e IRVector3) physics.Quat {
	cx := math.Cos(e.X * 0.5)
	sx := math.Sin(e.X * 0.5)
	cy := math.Cos(e.Y * 0.5)
	sy := math.Sin(e.Y * 0.5)
	cz := math.Cos(e.Z * 0.5)
	sz := math.Sin(e.Z * 0.5)
	return physics.Quat{
		X: sx*cy*cz - cx*sy*sz,
		Y: cx*sy*cz + sx*cy*sz,
		Z: cx*cy*sz - sx*sy*cz,
		W: cx*cy*cz + sx*sy*sz,
	}.Normalize()
}
