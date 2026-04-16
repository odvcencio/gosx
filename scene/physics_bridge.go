package scene

import (
	"math"
	"strings"

	"github.com/odvcencio/gosx/physics"
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

	for _, body := range p.Bodies {
		mass := body.Mass
		if body.Static {
			mass = 0
		}
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
			Colliders: collidersToPhysics(body.Colliders),
		})
	}

	if len(p.Static) > 0 {
		spec.Static = collidersToPhysics(p.Static)
	}
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

func collidersToPhysics(src []IRCollider) []physics.ColliderConfig {
	if len(src) == 0 {
		return nil
	}
	out := make([]physics.ColliderConfig, 0, len(src))
	for _, c := range src {
		shape, ok := shapeNameToKind(c.Shape)
		if !ok {
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
	return out
}

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

func shapeNameToKind(name string) (physics.ColliderShape, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "box":
		return physics.ShapeBox, true
	case "sphere":
		return physics.ShapeSphere, true
	case "capsule":
		return physics.ShapeCapsule, true
	case "plane":
		return physics.ShapePlane, true
	case "cylinder":
		return physics.ShapeCylinder, true
	case "cone":
		return physics.ShapeCone, true
	case "convex", "convexhull":
		return physics.ShapeConvexHull, true
	case "mesh", "trianglemesh":
		return physics.ShapeTriangleMesh, true
	}
	return 0, false
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
