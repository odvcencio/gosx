package physics

import "math"

const (
	// positionSlop is the overlap the position pass leaves in place. A resting
	// body settles this far inside the surface it stands on, which keeps the
	// contact alive from step to step.
	positionSlop = 0.0005
	// positionCorrection is the share of the excess overlap that one step
	// removes. Values near 1 recover fast and jitter; values near 0 sink.
	positionCorrection = 0.45
	// maxCorrectionSpeed caps the pseudo velocity of the position pass, in
	// metres per second. Without the cap a deep overlap throws bodies apart.
	maxCorrectionSpeed = 3.0
	// restitutionVelMinimum is the approach speed below which a contact stops
	// bouncing. It stops a resting stack from buzzing.
	restitutionVelMinimum = 1.0
	// positionIterations is the iteration count of the position pass. The pass
	// carries no friction, so it converges faster than the velocity pass.
	positionIterations = 4
)

// contactSolvePoint is the per-point scratch of one step. The world rebuilds it
// in prepareContacts and reuses the backing array across steps.
type contactSolvePoint struct {
	// rA and rB are the contact offsets from each body centre of mass.
	rA Vec3
	rB Vec3
	// normalMass and tangentMass are the inverse effective masses along the
	// contact normal and along the two friction directions.
	normalMass  float64
	tangentMass [2]float64
	// restitutionTarget is the separating speed the velocity pass aims for.
	restitutionTarget float64
	// penetration is the overlap the position pass must remove.
	penetration float64
	// positionImpulse accumulates the pseudo impulse of the position pass.
	positionImpulse float64
}

// contactSolveManifold holds the prepared state of one manifold.
type contactSolveManifold struct {
	normal   Vec3
	tangent  [2]Vec3
	points   [4]contactSolvePoint
	count    int
	friction float64
	active   bool
}

// inverseInertiaWorld returns the world inverse inertia tensor the solver may
// use for a body. A static, kinematic, sleeping or rotation-locked body returns
// the zero matrix, so it absorbs no angular impulse.
func inverseInertiaWorld(body *RigidBody) mat3 {
	if body == nil || !body.IsDynamic() || body.IsSleeping() {
		return mat3{}
	}
	return body.invInertiaWorld
}

// effectiveMassAlong returns the inverse effective mass of a contact pair along
// direction dir, including the angular term of both bodies.
//
// The scalar is invMassA + invMassB + dir . ((IA * (rA x dir)) x rA) + the same
// term for B. It is the denominator of every sequential impulse in this file.
func effectiveMassAlong(dir Vec3, invMassA, invMassB float64, invIA, invIB mat3, rA, rB Vec3) float64 {
	k := invMassA + invMassB
	if !invIA.isZero() {
		ra := rA.Cross(dir)
		k += invIA.mul(ra).Dot(ra)
	}
	if !invIB.isZero() {
		rb := rB.Cross(dir)
		k += invIB.mul(rb).Dot(rb)
	}
	return k
}

// applyImpulseAtOffset adds a world impulse acting at offset r from the body
// centre of mass. It changes both linear and angular velocity.
func applyImpulseAtOffset(body *RigidBody, impulse, r Vec3) {
	if body == nil || !body.IsDynamic() || body.IsSleeping() {
		return
	}
	body.Velocity = body.Velocity.Add(impulse.Mul(body.InvMass))
	if !body.invInertiaWorld.isZero() {
		body.AngularVelocity = body.AngularVelocity.Add(body.invInertiaWorld.mul(r.Cross(impulse)))
	}
}

// applyPseudoImpulseAtOffset is applyImpulseAtOffset for the position pass. It
// moves the pseudo velocities, which never reach the reported body velocity.
//
// A body that receives its first pseudo impulse of the step is appended to
// touched, so the caller integrates only the bodies the pass moved.
func applyPseudoImpulseAtOffset(body *RigidBody, impulse, r Vec3, touched []*RigidBody) []*RigidBody {
	if body == nil || !body.IsDynamic() || body.IsSleeping() {
		return touched
	}
	if !body.pseudoActive {
		body.pseudoActive = true
		body.pseudoVelocity = Vec3{}
		body.pseudoAngular = Vec3{}
		touched = append(touched, body)
	}
	body.pseudoVelocity = body.pseudoVelocity.Add(impulse.Mul(body.InvMass))
	if !body.invInertiaWorld.isZero() {
		body.pseudoAngular = body.pseudoAngular.Add(body.invInertiaWorld.mul(r.Cross(impulse)))
	}
	return touched
}

// relativeVelocityAt returns the velocity of body B's material point minus the
// velocity of body A's material point at the same world location.
func relativeVelocityAt(a, b *RigidBody, rA, rB Vec3) Vec3 {
	return bodyPointVelocity(b, rB).Sub(bodyPointVelocity(a, rA))
}

func bodyPointVelocity(body *RigidBody, r Vec3) Vec3 {
	if body == nil || body.IsSleeping() {
		return Vec3{}
	}
	return body.Velocity.Add(body.AngularVelocity.Cross(r))
}

func relativePseudoVelocityAt(a, b *RigidBody, rA, rB Vec3) Vec3 {
	return bodyPseudoVelocity(b, rB).Sub(bodyPseudoVelocity(a, rA))
}

func bodyPseudoVelocity(body *RigidBody, r Vec3) Vec3 {
	if body == nil || body.IsSleeping() || !body.pseudoActive {
		return Vec3{}
	}
	return body.pseudoVelocity.Add(body.pseudoAngular.Cross(r))
}

// prepareContacts builds the per-step solver state of every manifold, seeds the
// accumulated impulses from the warm-start cache, and applies those impulses.
//
// The tangent basis comes from the contact normal alone, so it is the same in
// every step in which the normal is the same. That is what makes a cached
// friction impulse meaningful across steps.
func (w *World) prepareContacts() {
	if cap(w.solveState) < len(w.contacts) {
		w.solveState = make([]contactSolveManifold, len(w.contacts))
	}
	w.solveState = w.solveState[:len(w.contacts)]

	for i := range w.contacts {
		manifold := &w.contacts[i]
		state := &w.solveState[i]
		*state = contactSolveManifold{}
		if manifold.IsTrigger() || manifold.PointCount == 0 {
			continue
		}

		bodyA, bodyB := manifold.BodyA, manifold.BodyB
		invMassA := inverseMass(bodyA)
		invMassB := inverseMass(bodyB)
		invIA := inverseInertiaWorld(bodyA)
		invIB := inverseInertiaWorld(bodyB)
		if invMassA+invMassB <= 0 {
			continue
		}

		normal := manifold.Normal.Normalize()
		if normal.Len2() <= epsilon {
			continue
		}
		t0, t1 := orthonormalBasis(normal)

		state.normal = normal
		state.tangent = [2]Vec3{t0, t1}
		state.friction = manifold.Friction
		state.count = manifold.PointCount
		state.active = true

		for p := 0; p < manifold.PointCount; p++ {
			point := &manifold.Points[p]
			slot := &state.points[p]

			slot.rA = contactOffset(bodyA, point.Point)
			slot.rB = contactOffset(bodyB, point.Point)
			slot.penetration = point.Penetration
			slot.positionImpulse = 0

			kn := effectiveMassAlong(normal, invMassA, invMassB, invIA, invIB, slot.rA, slot.rB)
			if kn > epsilon {
				slot.normalMass = 1 / kn
			}
			for axis := 0; axis < 2; axis++ {
				kt := effectiveMassAlong(state.tangent[axis], invMassA, invMassB, invIA, invIB, slot.rA, slot.rB)
				if kt > epsilon {
					slot.tangentMass[axis] = 1 / kt
				}
			}

			// Restitution uses the approach speed measured before any impulse
			// of this step, which is what makes a bounce reproducible.
			approach := relativeVelocityAt(bodyA, bodyB, slot.rA, slot.rB).Dot(normal)
			if manifold.Restitution > 0 && -approach > restitutionVelMinimum {
				slot.restitutionTarget = -manifold.Restitution * approach
			}
		}
	}
}

// contactOffset returns the vector from a body centre of mass to a world point.
func contactOffset(body *RigidBody, point Vec3) Vec3 {
	if body == nil {
		return Vec3{}
	}
	return point.Sub(body.Position)
}

// solveContactVelocityState runs one velocity iteration over one manifold.
//
// Normal and friction impulses both act at the contact point, so both change
// angular velocity. Friction is a two axis cone: the pair of tangent impulses
// is clamped by the length of its own vector, not axis by axis, so a sliding
// box slows the same way in every direction.
//
// Friction runs before the normal impulses, bounded by the normal impulse the
// previous iteration produced. That is the Box2D ordering, and it converges far
// better on a stack than solving each point in full one after the other.
func solveContactVelocityState(manifold *ContactManifold, state *contactSolveManifold) {
	if state == nil || !state.active {
		return
	}
	bodyA, bodyB := manifold.BodyA, manifold.BodyB
	normal := state.normal

	if state.friction > 0 {
		for i := 0; i < state.count; i++ {
			point := &manifold.Points[i]
			slot := &state.points[i]
			limit := state.friction * point.NormalImpulse
			if limit <= 0 {
				point.TangentImpulse[0] = 0
				point.TangentImpulse[1] = 0
				continue
			}

			relative := relativeVelocityAt(bodyA, bodyB, slot.rA, slot.rB)
			old0 := point.TangentImpulse[0]
			old1 := point.TangentImpulse[1]
			new0 := old0 - relative.Dot(state.tangent[0])*slot.tangentMass[0]
			new1 := old1 - relative.Dot(state.tangent[1])*slot.tangentMass[1]

			if magnitude := math.Hypot(new0, new1); magnitude > limit && magnitude > epsilon {
				scale := limit / magnitude
				new0 *= scale
				new1 *= scale
			}

			delta0 := new0 - old0
			delta1 := new1 - old1
			if delta0 != 0 || delta1 != 0 {
				impulse := state.tangent[0].Mul(delta0).Add(state.tangent[1].Mul(delta1))
				applyImpulseAtOffset(bodyA, impulse.Neg(), slot.rA)
				applyImpulseAtOffset(bodyB, impulse, slot.rB)
			}
			point.TangentImpulse[0] = new0
			point.TangentImpulse[1] = new1
		}
	}

	for i := 0; i < state.count; i++ {
		point := &manifold.Points[i]
		slot := &state.points[i]
		if slot.normalMass <= 0 {
			continue
		}

		relative := relativeVelocityAt(bodyA, bodyB, slot.rA, slot.rB)
		velAlongNormal := relative.Dot(normal)

		oldTotal := point.NormalImpulse
		newTotal := oldTotal + (slot.restitutionTarget-velAlongNormal)*slot.normalMass
		if newTotal < 0 {
			newTotal = 0
		}
		if delta := newTotal - oldTotal; delta != 0 {
			impulse := normal.Mul(delta)
			applyImpulseAtOffset(bodyA, impulse.Neg(), slot.rA)
			applyImpulseAtOffset(bodyB, impulse, slot.rB)
		}
		point.NormalImpulse = newTotal
	}
}

// solveContactPositionState runs one iteration of the position pass over one
// manifold. The pass moves pseudo velocities only; the caller integrates them
// into positions and rotations after the last iteration.
//
// Working on pseudo velocities keeps the overlap correction out of the reported
// velocity, so a body pushed out of a wall does not gain kinetic energy.
func solveContactPositionState(manifold *ContactManifold, state *contactSolveManifold, dt float64, touched []*RigidBody) []*RigidBody {
	if state == nil || !state.active || dt <= 0 {
		return touched
	}
	bodyA, bodyB := manifold.BodyA, manifold.BodyB
	normal := state.normal

	for i := 0; i < state.count; i++ {
		slot := &state.points[i]
		if slot.normalMass <= 0 {
			continue
		}
		excess := slot.penetration - positionSlop
		if excess <= 0 {
			continue
		}
		bias := positionCorrection * excess / dt
		if bias > maxCorrectionSpeed {
			bias = maxCorrectionSpeed
		}

		relative := relativePseudoVelocityAt(bodyA, bodyB, slot.rA, slot.rB).Dot(normal)
		old := slot.positionImpulse
		next := old + (bias-relative)*slot.normalMass
		if next < 0 {
			next = 0
		}
		if delta := next - old; delta != 0 {
			impulse := normal.Mul(delta)
			touched = applyPseudoImpulseAtOffset(bodyA, impulse.Neg(), slot.rA, touched)
			touched = applyPseudoImpulseAtOffset(bodyB, impulse, slot.rB, touched)
		}
		slot.positionImpulse = next
	}
	return touched
}

func inverseMass(body *RigidBody) float64 {
	if body == nil || !body.IsDynamic() || body.IsSleeping() {
		return 0
	}
	return body.InvMass
}

func velocityAt(body *RigidBody, point Vec3) Vec3 {
	if body == nil || body.IsSleeping() {
		return Vec3{}
	}
	return bodyPointVelocity(body, point.Sub(body.Position))
}

func applyLinearImpulse(body *RigidBody, impulse Vec3) {
	if body == nil || !body.IsDynamic() {
		return
	}
	body.Velocity = body.Velocity.Add(impulse.Mul(body.InvMass))
}
