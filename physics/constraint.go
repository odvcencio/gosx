package physics

import "math"

// Constraint is the polymorphic interface every constraint type implements.
// The world invokes Prepare once per step (to compute effective mass etc.),
// SolveVelocity every solver iteration, and SolvePosition once per step for
// drift correction.
//
// Implementations are expected to store accumulated impulses across frames
// for warm starting — the world does not reset them.
type Constraint interface {
	// Prepare runs once per step before the velocity iterations. Implementations
	// should cache world-space quantities (attach points, axis direction,
	// effective mass) and may apply the warm-start impulse here.
	Prepare(dt float64)
	// SolveVelocity runs per solver iteration.
	SolveVelocity()
	// SolvePosition runs once per step after velocity iterations to correct
	// positional drift (Baumgarte stabilization).
	SolvePosition()
}

// DistanceConstraint keeps two bodies' attach points a fixed distance apart.
// The attach points are given in body-local coordinates; world-space attach
// points are recomputed every step from the current body transforms.
//
// Softness > 0 acts as a spring-like compliance: zero gives a rigid rod,
// larger values allow the constraint to "give" under load. The angular
// contribution to effective mass is approximated — point-mass constraints
// work well for typical rope/chain scenarios.
type DistanceConstraint struct {
	BodyA, BodyB   *RigidBody
	AttachA        Vec3    // body-local on A
	AttachB        Vec3    // body-local on B
	TargetDistance float64 // rest length
	Softness       float64 // >= 0; 0 = rigid

	// Cached per-step state.
	worldA   Vec3
	worldB   Vec3
	axis     Vec3    // unit direction from A to B
	distance float64 // current world-space distance
	effMass  float64 // effective mass along axis
	bias     float64 // Baumgarte-style velocity bias

	// Warm-starting state.
	accumImpulse float64
}

func (c *DistanceConstraint) Prepare(dt float64) {
	if c == nil || c.BodyA == nil || c.BodyB == nil {
		return
	}

	c.worldA = c.BodyA.Position.Add(c.BodyA.Rotation.Rotate(c.AttachA))
	c.worldB = c.BodyB.Position.Add(c.BodyB.Rotation.Rotate(c.AttachB))

	delta := c.worldB.Sub(c.worldA)
	c.distance = delta.Len()
	if c.distance > epsilon {
		c.axis = delta.Div(c.distance)
	} else {
		c.axis = Vec3{Y: 1}
	}

	invMassA := inverseMass(c.BodyA)
	invMassB := inverseMass(c.BodyB)
	invMassSum := invMassA + invMassB + math.Max(0, c.Softness)
	if invMassSum > 0 {
		c.effMass = 1 / invMassSum
	} else {
		c.effMass = 0
	}

	// Baumgarte bias: fraction of positional error fed back as velocity.
	// The bias sign matches the position error (drift) so that
	// lambda = -(vRel + bias) * effMass
	// drives bodies together when stretched (drift > 0 → bias > 0 → lambda
	// more negative → impulse pulls B toward A) and apart when compressed.
	// Clamped to maxBiasVelocity to prevent explosive impulses when the
	// constraint gets far out of line (e.g., a horizontal-rod pendulum
	// without angular coupling drifts past its rest length under gravity).
	drift := c.distance - c.TargetDistance
	const baumgarte = 0.2
	const allowedSlop = 0.005
	const maxBiasVelocity = 2.0
	if dt > 0 && c.effMass > 0 {
		if drift > allowedSlop {
			c.bias = baumgarte * (drift - allowedSlop) / dt
		} else if drift < -allowedSlop {
			c.bias = baumgarte * (drift + allowedSlop) / dt
		} else {
			c.bias = 0
		}
		if c.bias > maxBiasVelocity {
			c.bias = maxBiasVelocity
		} else if c.bias < -maxBiasVelocity {
			c.bias = -maxBiasVelocity
		}
	} else {
		c.bias = 0
	}

	// Warm-start: apply cached impulse along axis.
	if c.accumImpulse != 0 && c.effMass > 0 {
		impulse := c.axis.Mul(c.accumImpulse)
		applyLinearImpulse(c.BodyA, impulse.Neg())
		applyLinearImpulse(c.BodyB, impulse)
	}
}

func (c *DistanceConstraint) SolveVelocity() {
	if c == nil || c.effMass == 0 {
		return
	}
	vA := velocityAt(c.BodyA, c.worldA)
	vB := velocityAt(c.BodyB, c.worldB)
	vRel := vB.Sub(vA).Dot(c.axis)
	lambda := -(vRel + c.bias) * c.effMass

	c.accumImpulse += lambda
	impulse := c.axis.Mul(lambda)
	applyLinearImpulse(c.BodyA, impulse.Neg())
	applyLinearImpulse(c.BodyB, impulse)
}

func (c *DistanceConstraint) SolvePosition() {
	// Velocity Baumgarte above handles most drift; additional explicit
	// position correction is usually unneeded for point-mass rods. Left as
	// a hook for future constraint types that want NGS position solvers.
}

// AddConstraint registers a constraint with the world. The constraint will
// be solved alongside contacts every fixed step.
func (w *World) AddConstraint(c Constraint) {
	if w == nil || c == nil {
		return
	}
	w.constraints = append(w.constraints, c)
}

// RemoveConstraint unregisters a previously added constraint. No-op if the
// constraint is not registered.
func (w *World) RemoveConstraint(c Constraint) {
	if w == nil || c == nil {
		return
	}
	for i, existing := range w.constraints {
		if existing == c {
			w.constraints = append(w.constraints[:i], w.constraints[i+1:]...)
			return
		}
	}
}

// Constraints returns a defensive copy of the world's constraints.
func (w *World) Constraints() []Constraint {
	if w == nil || len(w.constraints) == 0 {
		return nil
	}
	out := make([]Constraint, len(w.constraints))
	copy(out, w.constraints)
	return out
}
