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

const (
	// constraintBaumgarte is the share of a constraint's position error that
	// one step feeds back as a velocity bias.
	constraintBaumgarte = 0.2
	// constraintSlop is the position error a constraint leaves alone.
	constraintSlop = 0.005
	// constraintMaxBias caps the bias velocity in metres or radians per second.
	constraintMaxBias = 4.0
)

// jointAnchor holds the per-step world state that every anchored joint needs.
type jointAnchor struct {
	worldA Vec3
	worldB Vec3
	rA     Vec3
	rB     Vec3
	invIA  mat3
	invIB  mat3
	invMA  float64
	invMB  float64
}

// prepareAnchor rebuilds the world attach points and the cached inverse mass
// terms of a two body joint.
func prepareAnchor(bodyA, bodyB *RigidBody, attachA, attachB Vec3) jointAnchor {
	var anchor jointAnchor
	anchor.worldA = attachA
	anchor.worldB = attachB
	if bodyA != nil {
		anchor.worldA = bodyA.Position.Add(bodyA.Rotation.Rotate(attachA))
		anchor.rA = anchor.worldA.Sub(bodyA.Position)
	}
	if bodyB != nil {
		anchor.worldB = bodyB.Position.Add(bodyB.Rotation.Rotate(attachB))
		anchor.rB = anchor.worldB.Sub(bodyB.Position)
	}
	anchor.invMA = inverseMass(bodyA)
	anchor.invMB = inverseMass(bodyB)
	anchor.invIA = inverseInertiaWorld(bodyA)
	anchor.invIB = inverseInertiaWorld(bodyB)
	return anchor
}

// linearEffectiveMassMatrix returns the 3x3 effective mass of a point to point
// constraint. It is (1/mA + 1/mB) * E - skew(rA) * IA * skew(rA) - the same for
// B, written without an explicit skew matrix.
func linearEffectiveMassMatrix(a jointAnchor) mat3 {
	k := mat3Identity().scale(a.invMA + a.invMB)
	k = k.add(skewInertiaTerm(a.invIA, a.rA))
	k = k.add(skewInertiaTerm(a.invIB, a.rB))
	return k
}

// skewInertiaTerm returns -skew(r) * invI * skew(r), the angular share of a
// point constraint's effective mass.
func skewInertiaTerm(invI mat3, r Vec3) mat3 {
	if invI.isZero() {
		return mat3{}
	}
	s := skewMatrix(r)
	// -S * I * S is symmetric positive semi definite for a positive definite I.
	return s.mulMat(invI).mulMat(s).scale(-1)
}

// skewMatrix returns the matrix S with S * v = r x v.
func skewMatrix(r Vec3) mat3 {
	return mat3{
		x: Vec3{Y: r.Z, Z: -r.Y},
		y: Vec3{X: -r.Z, Z: r.X},
		z: Vec3{X: r.Y, Y: -r.X},
	}
}

// applyJointImpulse pushes a world impulse through a two body joint.
func applyJointImpulse(bodyA, bodyB *RigidBody, impulse, rA, rB Vec3) {
	applyImpulseAtOffset(bodyA, impulse.Neg(), rA)
	applyImpulseAtOffset(bodyB, impulse, rB)
}

// applyJointAngularImpulse pushes an angular impulse through a two body joint.
func applyJointAngularImpulse(bodyA, bodyB *RigidBody, impulse Vec3) {
	if bodyA != nil && bodyA.IsDynamic() && !bodyA.invInertiaWorld.isZero() {
		bodyA.AngularVelocity = bodyA.AngularVelocity.Sub(bodyA.invInertiaWorld.mul(impulse))
	}
	if bodyB != nil && bodyB.IsDynamic() && !bodyB.invInertiaWorld.isZero() {
		bodyB.AngularVelocity = bodyB.AngularVelocity.Add(bodyB.invInertiaWorld.mul(impulse))
	}
}

func jointAngularVelocity(body *RigidBody) Vec3 {
	if body == nil || body.IsSleeping() {
		return Vec3{}
	}
	return body.AngularVelocity
}

func biasFromError(err, dt float64) float64 {
	if dt <= 0 {
		return 0
	}
	bias := 0.0
	if err > constraintSlop {
		bias = constraintBaumgarte * (err - constraintSlop) / dt
	} else if err < -constraintSlop {
		bias = constraintBaumgarte * (err + constraintSlop) / dt
	}
	return clampFloat(bias, -constraintMaxBias, constraintMaxBias)
}

// DistanceConstraint keeps two bodies' attach points a fixed distance apart.
// The attach points are given in body-local coordinates; world-space attach
// points are recomputed every step from the current body transforms.
//
// Softness > 0 acts as a spring-like compliance: zero gives a rigid rod,
// larger values allow the constraint to "give" under load.
//
// The effective mass carries the angular term of both bodies, so a rod attached
// away from the centre of mass now spins the body it pulls.
type DistanceConstraint struct {
	BodyA, BodyB   *RigidBody
	AttachA        Vec3    // body-local on A
	AttachB        Vec3    // body-local on B
	TargetDistance float64 // rest length
	Softness       float64 // >= 0; 0 = rigid

	// Cached per-step state.
	anchor   jointAnchor
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

	c.anchor = prepareAnchor(c.BodyA, c.BodyB, c.AttachA, c.AttachB)

	delta := c.anchor.worldB.Sub(c.anchor.worldA)
	c.distance = delta.Len()
	if c.distance > epsilon {
		c.axis = delta.Div(c.distance)
	} else {
		c.axis = Vec3{Y: 1}
	}

	k := effectiveMassAlong(c.axis, c.anchor.invMA, c.anchor.invMB,
		c.anchor.invIA, c.anchor.invIB, c.anchor.rA, c.anchor.rB) +
		math.Max(0, c.Softness)
	if k > epsilon {
		c.effMass = 1 / k
	} else {
		c.effMass = 0
	}

	// Baumgarte bias: fraction of positional error fed back as velocity.
	// The bias sign matches the position error (drift) so that
	// lambda = -(vRel + bias) * effMass
	// drives bodies together when stretched and apart when compressed. The
	// clamp stops an explosive impulse when the rod is far out of line.
	if c.effMass > 0 {
		c.bias = biasFromError(c.distance-c.TargetDistance, dt)
	} else {
		c.bias = 0
	}

	// Warm-start: apply the cached impulse along the axis.
	if c.accumImpulse != 0 && c.effMass > 0 {
		impulse := c.axis.Mul(c.accumImpulse)
		applyJointImpulse(c.BodyA, c.BodyB, impulse, c.anchor.rA, c.anchor.rB)
	}
}

func (c *DistanceConstraint) SolveVelocity() {
	if c == nil || c.effMass == 0 {
		return
	}
	vRel := relativeVelocityAt(c.BodyA, c.BodyB, c.anchor.rA, c.anchor.rB).Dot(c.axis)
	lambda := -(vRel + c.bias) * c.effMass

	c.accumImpulse += lambda
	applyJointImpulse(c.BodyA, c.BodyB, c.axis.Mul(lambda), c.anchor.rA, c.anchor.rB)
}

func (c *DistanceConstraint) SolvePosition() {
	// The velocity Baumgarte term above handles the drift of a rod. Position
	// solvers matter far more for the joints below, which lock more axes.
}

// PointConstraint is a ball and socket joint. It holds one point of body A on
// one point of body B and leaves all three rotation axes free.
//
// Use it for a pendulum, a chain link, or a ragdoll shoulder.
type PointConstraint struct {
	BodyA, BodyB *RigidBody
	AttachA      Vec3 // body-local on A
	AttachB      Vec3 // body-local on B

	anchor   jointAnchor
	effMass  mat3
	solvable bool
	bias     Vec3

	accumImpulse Vec3
}

func (c *PointConstraint) Prepare(dt float64) {
	if c == nil || c.BodyA == nil || c.BodyB == nil {
		return
	}
	c.anchor = prepareAnchor(c.BodyA, c.BodyB, c.AttachA, c.AttachB)
	k := linearEffectiveMassMatrix(c.anchor)
	inverse, ok := k.inverse()
	c.solvable = ok && (c.anchor.invMA+c.anchor.invMB) > 0
	if !c.solvable {
		c.bias = Vec3{}
		return
	}
	c.effMass = inverse

	drift := c.anchor.worldB.Sub(c.anchor.worldA)
	c.bias = Vec3{
		X: biasFromError(drift.X, dt),
		Y: biasFromError(drift.Y, dt),
		Z: biasFromError(drift.Z, dt),
	}

	if c.accumImpulse.Len2() > 0 {
		applyJointImpulse(c.BodyA, c.BodyB, c.accumImpulse, c.anchor.rA, c.anchor.rB)
	}
}

func (c *PointConstraint) SolveVelocity() {
	if c == nil || !c.solvable {
		return
	}
	vRel := relativeVelocityAt(c.BodyA, c.BodyB, c.anchor.rA, c.anchor.rB)
	lambda := c.effMass.mul(vRel.Add(c.bias).Neg())
	c.accumImpulse = c.accumImpulse.Add(lambda)
	applyJointImpulse(c.BodyA, c.BodyB, lambda, c.anchor.rA, c.anchor.rB)
}

func (c *PointConstraint) SolvePosition() {}

// HingeConstraint is a revolute joint. It holds one point of body A on one
// point of body B and allows rotation about one axis only.
//
// The axis is given in each body's local frame, so the joint knows the pose the
// two bodies must keep. Set MotorEnabled to drive the joint at MotorSpeed with
// at most MaxMotorTorque. Set LimitEnabled to stop the joint between LowerLimit
// and UpperLimit, both in radians measured from the reference frame.
type HingeConstraint struct {
	BodyA, BodyB *RigidBody
	AttachA      Vec3 // body-local on A
	AttachB      Vec3 // body-local on B
	AxisA        Vec3 // body-local hinge axis on A
	AxisB        Vec3 // body-local hinge axis on B

	// RefA and RefB are body-local reference directions perpendicular to the
	// axis. They define the zero angle. Leave them zero to let the joint pick a
	// perpendicular pair, which makes the zero angle the pose at first use.
	RefA Vec3
	RefB Vec3

	MotorEnabled   bool
	MotorSpeed     float64 // radians per second about the hinge axis
	MaxMotorTorque float64 // newton metres; zero means unlimited

	LimitEnabled bool
	LowerLimit   float64 // radians
	UpperLimit   float64 // radians

	anchor      jointAnchor
	pointMass   mat3
	pointBias   Vec3
	worldAxis   Vec3
	perp        [2]Vec3
	angularMass [2]float64
	angularBias [2]float64
	motorMass   float64
	limitMass   float64
	limitBias   float64
	limitSign   float64
	solvable    bool
	refReady    bool

	accumPoint   Vec3
	accumAngular [2]float64
	accumMotor   float64
	accumLimit   float64
}

// Angle returns the current hinge angle in radians, measured from the reference
// frame. It returns zero when the joint is not usable.
func (c *HingeConstraint) Angle() float64 {
	if c == nil || c.BodyA == nil || c.BodyB == nil {
		return 0
	}
	c.ensureReference()
	axis := c.BodyA.Rotation.Rotate(c.AxisA).Normalize()
	refA := c.BodyA.Rotation.Rotate(c.RefA)
	refB := c.BodyB.Rotation.Rotate(c.RefB)
	refA = refA.Sub(axis.Mul(axis.Dot(refA)))
	refB = refB.Sub(axis.Mul(axis.Dot(refB)))
	if refA.Len2() <= epsilon || refB.Len2() <= epsilon {
		return 0
	}
	refA = refA.Normalize()
	refB = refB.Normalize()
	cross := axis.Cross(refA)
	return math.Atan2(cross.Dot(refB), refA.Dot(refB))
}

// ensureReference fills in the reference directions the first time the joint
// runs. The pose at that moment becomes the zero angle.
func (c *HingeConstraint) ensureReference() {
	if c.refReady {
		return
	}
	if c.AxisA.Len2() <= epsilon {
		c.AxisA = Vec3{Y: 1}
	}
	if c.AxisB.Len2() <= epsilon {
		c.AxisB = c.AxisA
	}
	c.AxisA = c.AxisA.Normalize()
	c.AxisB = c.AxisB.Normalize()
	if c.RefA.Len2() <= epsilon || c.RefB.Len2() <= epsilon {
		refA, _ := orthonormalBasis(c.AxisA)
		c.RefA = refA
		// Express the same world direction in B's frame, so the joint starts at
		// angle zero whatever pose the bodies hold now.
		world := refA
		if c.BodyA != nil {
			world = c.BodyA.Rotation.Rotate(refA)
		}
		if c.BodyB != nil {
			c.RefB = c.BodyB.Rotation.Inverse().Rotate(world)
		} else {
			c.RefB = world
		}
	}
	c.RefA = c.RefA.Normalize()
	c.RefB = c.RefB.Normalize()
	c.refReady = true
}

func (c *HingeConstraint) Prepare(dt float64) {
	if c == nil || c.BodyA == nil || c.BodyB == nil {
		return
	}
	c.ensureReference()
	c.anchor = prepareAnchor(c.BodyA, c.BodyB, c.AttachA, c.AttachB)
	c.solvable = (c.anchor.invMA + c.anchor.invMB) > 0
	if !c.solvable {
		return
	}

	pointMass, ok := linearEffectiveMassMatrix(c.anchor).inverse()
	if !ok {
		c.solvable = false
		return
	}
	c.pointMass = pointMass
	drift := c.anchor.worldB.Sub(c.anchor.worldA)
	c.pointBias = Vec3{
		X: biasFromError(drift.X, dt),
		Y: biasFromError(drift.Y, dt),
		Z: biasFromError(drift.Z, dt),
	}

	// Angular rows: lock the two axes perpendicular to the hinge. The error is
	// the cross product of the two world axes, which is zero when they align.
	axisA := c.BodyA.Rotation.Rotate(c.AxisA).Normalize()
	axisB := c.BodyB.Rotation.Rotate(c.AxisB).Normalize()
	c.worldAxis = axisA
	p0, p1 := orthonormalBasis(axisA)
	c.perp = [2]Vec3{p0, p1}
	alignment := axisA.Cross(axisB)
	for i := 0; i < 2; i++ {
		k := angularEffectiveMass(c.perp[i], c.anchor.invIA, c.anchor.invIB)
		if k > epsilon {
			c.angularMass[i] = 1 / k
		} else {
			c.angularMass[i] = 0
		}
		c.angularBias[i] = biasFromError(-alignment.Dot(c.perp[i]), dt)
	}

	axialMass := angularEffectiveMass(axisA, c.anchor.invIA, c.anchor.invIB)
	if axialMass > epsilon {
		c.motorMass = 1 / axialMass
		c.limitMass = c.motorMass
	} else {
		c.motorMass = 0
		c.limitMass = 0
	}

	c.limitSign = 0
	c.limitBias = 0
	if c.LimitEnabled && c.limitMass > 0 {
		angle := c.Angle()
		switch {
		case angle < c.LowerLimit:
			c.limitSign = 1
			c.limitBias = biasFromError(c.LowerLimit-angle, dt)
		case angle > c.UpperLimit:
			c.limitSign = -1
			c.limitBias = biasFromError(angle-c.UpperLimit, dt)
		}
	}
	if c.limitSign == 0 {
		c.accumLimit = 0
	}
	if !c.MotorEnabled {
		c.accumMotor = 0
	}

	// Warm start every row.
	if c.accumPoint.Len2() > 0 {
		applyJointImpulse(c.BodyA, c.BodyB, c.accumPoint, c.anchor.rA, c.anchor.rB)
	}
	angular := c.perp[0].Mul(c.accumAngular[0]).Add(c.perp[1].Mul(c.accumAngular[1]))
	angular = angular.Add(axisA.Mul(c.accumMotor + c.accumLimit))
	if angular.Len2() > 0 {
		applyJointAngularImpulse(c.BodyA, c.BodyB, angular)
	}
}

func (c *HingeConstraint) SolveVelocity() {
	if c == nil || !c.solvable {
		return
	}
	relativeAngular := jointAngularVelocity(c.BodyB).Sub(jointAngularVelocity(c.BodyA))

	// Motor row first: it is bounded, so later rows correct any overshoot.
	if c.MotorEnabled && c.motorMass > 0 {
		error := relativeAngular.Dot(c.worldAxis) - c.MotorSpeed
		lambda := -error * c.motorMass
		old := c.accumMotor
		next := old + lambda
		if c.MaxMotorTorque > 0 {
			limit := c.MaxMotorTorque
			next = clampFloat(next, -limit, limit)
		}
		delta := next - old
		c.accumMotor = next
		if delta != 0 {
			applyJointAngularImpulse(c.BodyA, c.BodyB, c.worldAxis.Mul(delta))
		}
		relativeAngular = jointAngularVelocity(c.BodyB).Sub(jointAngularVelocity(c.BodyA))
	}

	if c.limitSign != 0 && c.limitMass > 0 {
		speed := relativeAngular.Dot(c.worldAxis) * c.limitSign
		lambda := -(speed - c.limitBias) * c.limitMass
		old := c.accumLimit * c.limitSign
		next := old + lambda
		if next < 0 {
			next = 0
		}
		delta := next - old
		c.accumLimit = next * c.limitSign
		if delta != 0 {
			applyJointAngularImpulse(c.BodyA, c.BodyB, c.worldAxis.Mul(delta*c.limitSign))
		}
		relativeAngular = jointAngularVelocity(c.BodyB).Sub(jointAngularVelocity(c.BodyA))
	}

	for i := 0; i < 2; i++ {
		if c.angularMass[i] <= 0 {
			continue
		}
		speed := relativeAngular.Dot(c.perp[i])
		lambda := -(speed + c.angularBias[i]) * c.angularMass[i]
		c.accumAngular[i] += lambda
		applyJointAngularImpulse(c.BodyA, c.BodyB, c.perp[i].Mul(lambda))
		relativeAngular = jointAngularVelocity(c.BodyB).Sub(jointAngularVelocity(c.BodyA))
	}

	vRel := relativeVelocityAt(c.BodyA, c.BodyB, c.anchor.rA, c.anchor.rB)
	lambda := c.pointMass.mul(vRel.Add(c.pointBias).Neg())
	c.accumPoint = c.accumPoint.Add(lambda)
	applyJointImpulse(c.BodyA, c.BodyB, lambda, c.anchor.rA, c.anchor.rB)
}

func (c *HingeConstraint) SolvePosition() {}

// FixedConstraint locks two bodies together. It holds one point of each body on
// the other and keeps their relative rotation at the value the joint recorded
// when it first ran.
//
// Use it to weld two parts into one rigid assembly that a large enough impulse
// can still bend, or to attach a prop to a moving platform.
type FixedConstraint struct {
	BodyA, BodyB *RigidBody
	AttachA      Vec3 // body-local on A
	AttachB      Vec3 // body-local on B

	// RelativeRotation is the rotation of B in A's frame that the joint holds.
	// Leave it zero to record the pose at first use.
	RelativeRotation Quat

	anchor      jointAnchor
	pointMass   mat3
	pointBias   Vec3
	angularMass mat3
	angularBias Vec3
	solvable    bool
	poseReady   bool

	accumPoint   Vec3
	accumAngular Vec3
}

func (c *FixedConstraint) ensurePose() {
	if c.poseReady {
		return
	}
	zero := c.RelativeRotation
	if zero.X == 0 && zero.Y == 0 && zero.Z == 0 && zero.W == 0 {
		if c.BodyA != nil && c.BodyB != nil {
			c.RelativeRotation = c.BodyA.Rotation.Inverse().Mul(c.BodyB.Rotation).Normalize()
		} else {
			c.RelativeRotation = IdentityQuat()
		}
	} else {
		c.RelativeRotation = zero.Normalize()
	}
	c.poseReady = true
}

func (c *FixedConstraint) Prepare(dt float64) {
	if c == nil || c.BodyA == nil || c.BodyB == nil {
		return
	}
	c.ensurePose()
	c.anchor = prepareAnchor(c.BodyA, c.BodyB, c.AttachA, c.AttachB)
	c.solvable = (c.anchor.invMA + c.anchor.invMB) > 0
	if !c.solvable {
		return
	}

	pointMass, ok := linearEffectiveMassMatrix(c.anchor).inverse()
	if !ok {
		c.solvable = false
		return
	}
	c.pointMass = pointMass
	drift := c.anchor.worldB.Sub(c.anchor.worldA)
	c.pointBias = Vec3{
		X: biasFromError(drift.X, dt),
		Y: biasFromError(drift.Y, dt),
		Z: biasFromError(drift.Z, dt),
	}

	angularMass, ok := c.anchor.invIA.add(c.anchor.invIB).inverse()
	if !ok {
		// Neither body can rotate. Keep the point rows and drop the angular
		// rows instead of failing the whole joint.
		c.angularMass = mat3{}
		c.angularBias = Vec3{}
		return
	}
	c.angularMass = angularMass

	// Rotation error as a small rotation vector. q maps the pose the joint wants
	// onto the pose it has; the vector part of q is half the error axis angle.
	want := c.BodyA.Rotation.Mul(c.RelativeRotation)
	error := c.BodyB.Rotation.Mul(want.Inverse()).Normalize()
	if error.W < 0 {
		error = Quat{X: -error.X, Y: -error.Y, Z: -error.Z, W: -error.W}
	}
	axis := Vec3{X: error.X, Y: error.Y, Z: error.Z}.Mul(2)
	c.angularBias = Vec3{
		X: biasFromError(axis.X, dt),
		Y: biasFromError(axis.Y, dt),
		Z: biasFromError(axis.Z, dt),
	}

	if c.accumPoint.Len2() > 0 {
		applyJointImpulse(c.BodyA, c.BodyB, c.accumPoint, c.anchor.rA, c.anchor.rB)
	}
	if c.accumAngular.Len2() > 0 {
		applyJointAngularImpulse(c.BodyA, c.BodyB, c.accumAngular)
	}
}

func (c *FixedConstraint) SolveVelocity() {
	if c == nil || !c.solvable {
		return
	}
	if !c.angularMass.isZero() {
		relative := jointAngularVelocity(c.BodyB).Sub(jointAngularVelocity(c.BodyA))
		lambda := c.angularMass.mul(relative.Add(c.angularBias).Neg())
		c.accumAngular = c.accumAngular.Add(lambda)
		applyJointAngularImpulse(c.BodyA, c.BodyB, lambda)
	}

	vRel := relativeVelocityAt(c.BodyA, c.BodyB, c.anchor.rA, c.anchor.rB)
	lambda := c.pointMass.mul(vRel.Add(c.pointBias).Neg())
	c.accumPoint = c.accumPoint.Add(lambda)
	applyJointImpulse(c.BodyA, c.BodyB, lambda, c.anchor.rA, c.anchor.rB)
}

func (c *FixedConstraint) SolvePosition() {}

// angularEffectiveMass returns the inverse effective mass of an angular
// constraint row about a unit axis.
func angularEffectiveMass(axis Vec3, invIA, invIB mat3) float64 {
	k := 0.0
	if !invIA.isZero() {
		k += invIA.mul(axis).Dot(axis)
	}
	if !invIB.isZero() {
		k += invIB.mul(axis).Dot(axis)
	}
	return k
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
