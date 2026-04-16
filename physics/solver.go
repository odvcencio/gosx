package physics

const (
	positionSlop          = 0.001
	positionCorrection    = 0.8
	restitutionVelMinimum = 1.0
)

func solveContactVelocity(manifold *ContactManifold) {
	if manifold == nil || manifold.IsTrigger() {
		return
	}
	invMassA := inverseMass(manifold.BodyA)
	invMassB := inverseMass(manifold.BodyB)
	invMassSum := invMassA + invMassB
	if invMassSum <= 0 {
		return
	}

	normal := manifold.Normal.Normalize()
	for i := 0; i < manifold.PointCount; i++ {
		point := manifold.Points[i].Point
		rv := velocityAt(manifold.BodyB, point).Sub(velocityAt(manifold.BodyA, point))
		velAlongNormal := rv.Dot(normal)

		restitution := manifold.Restitution
		if -velAlongNormal < restitutionVelMinimum {
			restitution = 0
		}

		// Incremental-impulse form: delta is what this iteration wants to
		// apply, but the accumulated total is clamped to >= 0 so warm-started
		// seeds are preserved across iterations and stacking stays stable.
		oldTotal := manifold.Points[i].NormalImpulse
		delta := -(1 + restitution) * velAlongNormal / invMassSum
		newTotal := oldTotal + delta
		if newTotal < 0 {
			newTotal = 0
		}
		actualDelta := newTotal - oldTotal
		if actualDelta != 0 {
			impulse := normal.Mul(actualDelta)
			applyLinearImpulse(manifold.BodyA, impulse.Neg())
			applyLinearImpulse(manifold.BodyB, impulse)
		}
		manifold.Points[i].NormalImpulse = newTotal

		if manifold.Friction <= 0 || newTotal <= 0 {
			continue
		}
		rv = velocityAt(manifold.BodyB, point).Sub(velocityAt(manifold.BodyA, point))
		tangent := rv.Sub(normal.Mul(rv.Dot(normal)))
		if tangent.Len2() <= epsilon {
			continue
		}
		tangent = tangent.Normalize()

		oldTangent := manifold.Points[i].TangentImpulse[0]
		tangentDelta := -rv.Dot(tangent) / invMassSum
		newTangent := oldTangent + tangentDelta
		maxFriction := newTotal * manifold.Friction
		if newTangent > maxFriction {
			newTangent = maxFriction
		} else if newTangent < -maxFriction {
			newTangent = -maxFriction
		}
		tangentActual := newTangent - oldTangent
		if tangentActual != 0 {
			frictionImpulse := tangent.Mul(tangentActual)
			applyLinearImpulse(manifold.BodyA, frictionImpulse.Neg())
			applyLinearImpulse(manifold.BodyB, frictionImpulse)
		}
		manifold.Points[i].TangentImpulse[0] = newTangent
	}
}

func solveContactPosition(manifold *ContactManifold, dt float64) {
	if manifold == nil || manifold.IsTrigger() || dt <= 0 {
		return
	}
	invMassA := inverseMass(manifold.BodyA)
	invMassB := inverseMass(manifold.BodyB)
	invMassSum := invMassA + invMassB
	if invMassSum <= 0 {
		return
	}

	maxPenetration := 0.0
	for i := 0; i < manifold.PointCount; i++ {
		maxPenetration = maxFloat(maxPenetration, manifold.Points[i].Penetration)
	}
	correctionMagnitude := maxFloat(maxPenetration-positionSlop, 0) / invMassSum * positionCorrection
	if correctionMagnitude <= 0 {
		return
	}
	correction := manifold.Normal.Normalize().Mul(correctionMagnitude)
	if manifold.BodyA != nil && manifold.BodyA.IsDynamic() {
		manifold.BodyA.Position = manifold.BodyA.Position.Sub(correction.Mul(invMassA))
	}
	if manifold.BodyB != nil && manifold.BodyB.IsDynamic() {
		manifold.BodyB.Position = manifold.BodyB.Position.Add(correction.Mul(invMassB))
	}
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
	r := point.Sub(body.Position)
	return body.Velocity.Add(body.AngularVelocity.Cross(r))
}

func applyLinearImpulse(body *RigidBody, impulse Vec3) {
	if body == nil || !body.IsDynamic() {
		return
	}
	body.Velocity = body.Velocity.Add(impulse.Mul(body.InvMass))
}
