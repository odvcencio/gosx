package physics

type WorldConfig struct {
	Gravity          Vec3
	FixedTimestep    float64
	SolverIter       int
	SolverIterations int
	BroadPhaseCell   float64
	// DisableCCD turns off conservative swept collision checks for fast
	// dynamic bodies against static colliders. CCD is enabled by default.
	DisableCCD bool
	// DisableWarmStart turns off contact impulse caching across frames. Warm
	// starting is on by default; disabling is only useful for tests or
	// deterministic replay scenarios that must reproduce a cold solver.
	DisableWarmStart bool

	// SleepTime is how long a body must stay slow before it sleeps, in seconds.
	// Sleeping applies only to a body whose BodyConfig sets CanSleep. Leave the
	// field zero to use half a second.
	SleepTime float64
	// SleepLinearSpeed and SleepAngularSpeed are the speeds a sleeping
	// candidate must stay under, in metres and radians per second. Leave them
	// zero to use 0.05 and 0.1.
	SleepLinearSpeed  float64
	SleepAngularSpeed float64
}

type contactCacheKey struct {
	a int
	b int
}

type cachedContactPoint struct {
	LocalA         Vec3
	LocalB         Vec3
	NormalImpulse  float64
	TangentImpulse [2]float64
}

type cachedManifold struct {
	Points [4]cachedContactPoint
	Count  int
}

type World struct {
	gravity           Vec3
	fixedTimestep     float64
	accumulator       float64
	solverIterations  int
	broadphase        *SpatialHash
	bodies            []*RigidBody
	colliders         []*Collider
	contacts          []ContactManifold
	nextBodyIndex     int
	nextColliderIndex int

	continuousCollision bool
	warmStart           bool
	contactCache        map[contactCacheKey]cachedManifold
	constraints         []Constraint

	// solveState mirrors contacts one to one and holds the per-step solver
	// scratch. The backing array is reused, so a step allocates nothing here.
	solveState []contactSolveManifold
	// pseudoBodies lists the bodies the position pass moved this step. Both
	// slices keep their backing array between steps.
	pseudoBodies []*RigidBody

	sleepTime         float64
	sleepLinearSpeed  float64
	sleepAngularSpeed float64

	// steps counts fixed steps. broadphaseStep records the step whose poses
	// the grid holds, so the swept pass never queries a stale grid.
	steps          uint64
	broadphaseStep uint64
	// sweepTargets is the reused result buffer of the swept broadphase query.
	sweepTargets []*Collider
}

func DefaultWorldConfig() WorldConfig {
	return WorldConfig{
		Gravity:          Vec3{Y: -9.81},
		FixedTimestep:    1.0 / 60.0,
		SolverIterations: 8,
		BroadPhaseCell:   2,
	}
}

func NewWorld(config WorldConfig) *World {
	if config == (WorldConfig{}) {
		config = DefaultWorldConfig()
	}
	if config.FixedTimestep <= 0 {
		config.FixedTimestep = 1.0 / 60.0
	}
	iterations := config.SolverIterations
	if iterations <= 0 {
		iterations = config.SolverIter
	}
	if iterations <= 0 {
		iterations = 8
	}
	if config.BroadPhaseCell <= 0 {
		config.BroadPhaseCell = 2
	}
	if config.SleepTime <= 0 {
		config.SleepTime = 0.5
	}
	if config.SleepLinearSpeed <= 0 {
		config.SleepLinearSpeed = 0.05
	}
	if config.SleepAngularSpeed <= 0 {
		config.SleepAngularSpeed = 0.1
	}
	w := &World{
		gravity:             config.Gravity,
		fixedTimestep:       config.FixedTimestep,
		solverIterations:    iterations,
		broadphase:          NewSpatialHash(config.BroadPhaseCell),
		continuousCollision: !config.DisableCCD,
		warmStart:           !config.DisableWarmStart,
		sleepTime:           config.SleepTime,
		sleepLinearSpeed:    config.SleepLinearSpeed,
		sleepAngularSpeed:   config.SleepAngularSpeed,
	}
	if w.warmStart {
		w.contactCache = make(map[contactCacheKey]cachedManifold)
	}
	return w
}

func (w *World) AddBody(config BodyConfig) *RigidBody {
	body := NewRigidBody(config)
	w.nextBodyIndex++
	body.index = w.nextBodyIndex
	body.world = w
	w.bodies = append(w.bodies, body)
	for _, collider := range body.colliders {
		w.registerCollider(collider)
	}
	return body
}

func (w *World) AddCollider(config ColliderConfig) *Collider {
	collider := newCollider(nil, config)
	w.registerCollider(collider)
	return collider
}

func (w *World) Bodies() []*RigidBody {
	bodies := make([]*RigidBody, len(w.bodies))
	copy(bodies, w.bodies)
	return bodies
}

func (w *World) Colliders() []*Collider {
	colliders := make([]*Collider, len(w.colliders))
	copy(colliders, w.colliders)
	return colliders
}

func (w *World) DynamicBodies() []*RigidBody {
	bodies := make([]*RigidBody, 0, len(w.bodies))
	for _, body := range w.bodies {
		if body.IsDynamic() {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

func (w *World) Contacts() []ContactManifold {
	contacts := make([]ContactManifold, len(w.contacts))
	copy(contacts, w.contacts)
	return contacts
}

// CandidatePairs returns the broadphase pairs for the current collider poses.
// The result is a fresh slice, so callers may keep it.
func (w *World) CandidatePairs() []ColliderPair {
	internal := w.broadphase.CandidatePairs(w.colliders)
	pairs := make([]ColliderPair, len(internal))
	copy(pairs, internal)
	return pairs
}

func (w *World) FixedTimestep() float64 {
	return w.fixedTimestep
}

func (w *World) Accumulator() float64 {
	return w.accumulator
}

func (w *World) Gravity() Vec3 {
	return w.gravity
}

// SetGravity replaces the world gravity and wakes every body, because a body
// asleep under the old gravity may have to fall under the new one.
func (w *World) SetGravity(gravity Vec3) {
	w.gravity = gravity
	w.WakeAll()
}

func (w *World) Step(elapsed float64) int {
	if elapsed <= 0 || w.fixedTimestep <= 0 {
		return 0
	}
	w.accumulator += elapsed

	steps := 0
	for w.accumulator+epsilon >= w.fixedTimestep {
		w.stepFixed(w.fixedTimestep)
		w.accumulator -= w.fixedTimestep
		if w.accumulator < epsilon {
			w.accumulator = 0
		}
		steps++
	}
	return steps
}

func (w *World) StepFixed() {
	w.stepFixed(w.fixedTimestep)
}

func (w *World) stepFixed(dt float64) {
	w.steps++
	w.refreshInertia()
	w.integrateForces(dt)
	w.generateContacts()
	w.wakeTouchedBodies()
	w.prepareContacts()
	if w.warmStart {
		w.warmStartContacts()
	}
	w.prepareConstraints(dt)
	w.solveContactsAndConstraints(dt)
	if w.warmStart {
		w.cacheContactImpulses()
	}
	w.integrateVelocities(dt)
	w.updateSleep(dt)
}

// wakeTouchedBodies wakes a sleeping body that a moving body now touches. A
// sleeping body takes no impulse, so it would sink through an arriving body
// without this pass.
func (w *World) wakeTouchedBodies() {
	for i := range w.contacts {
		manifold := &w.contacts[i]
		if manifold.IsTrigger() {
			continue
		}
		a, b := manifold.BodyA, manifold.BodyB
		// Wake only a body that is asleep. Calling Wake on a body that is
		// already awake would restart its countdown, and one restless body in a
		// tower would then stop the whole tower from ever settling.
		if b.IsSleeping() && bodyDisturbs(w, a) {
			b.Wake()
		}
		if a.IsSleeping() && bodyDisturbs(w, b) {
			a.Wake()
		}
	}
}

// bodyDisturbs reports whether a body moves fast enough to wake what it hits.
func bodyDisturbs(w *World, body *RigidBody) bool {
	if body == nil || body.IsSleeping() {
		return false
	}
	return body.Velocity.Len2() > w.sleepLinearSpeed*w.sleepLinearSpeed ||
		body.AngularVelocity.Len2() > w.sleepAngularSpeed*w.sleepAngularSpeed
}

// updateSleep puts a slow body to sleep once it has stayed slow for long
// enough. A sleeping body keeps its pose, takes no impulse and costs the solver
// nothing, which is what stops a settled stack from creeping over thousands of
// steps.
//
// Only a body whose BodyConfig sets CanSleep is eligible. Every other body
// keeps simulating, which preserves the behaviour callers had before.
func (w *World) updateSleep(dt float64) {
	if w.sleepTime <= 0 {
		return
	}

	// Mark the bodies that are slow enough to be sleep candidates.
	for _, body := range w.bodies {
		if body == nil {
			continue
		}
		body.sleepBlocked = false
		body.sleepSlow = body.Velocity.Len2() <= w.sleepLinearSpeed*w.sleepLinearSpeed &&
			body.AngularVelocity.Len2() <= w.sleepAngularSpeed*w.sleepAngularSpeed
	}

	// A body that leans on a moving body cannot sleep. Letting it sleep anyway
	// turns it into an immovable support for one step, which shakes the pile it
	// belongs to and wakes it again. The block travels one contact per step, so
	// a whole tower goes quiet before any part of it sleeps.
	for i := range w.contacts {
		manifold := &w.contacts[i]
		if manifold.IsTrigger() {
			continue
		}
		a, b := manifold.BodyA, manifold.BodyB
		if a != nil && a.IsDynamic() && !a.sleepSlow && b != nil {
			b.sleepBlocked = true
		}
		if b != nil && b.IsDynamic() && !b.sleepSlow && a != nil {
			a.sleepBlocked = true
		}
	}

	for _, body := range w.bodies {
		if body == nil || !body.CanSleep || !body.IsDynamic() || body.sleeping {
			continue
		}
		if !body.sleepSlow || body.sleepBlocked {
			body.sleepTimer = 0
			continue
		}
		body.sleepTimer += dt
		if body.sleepTimer < w.sleepTime {
			continue
		}
		body.sleeping = true
		body.sleepTimer = 0
		body.Velocity = Vec3{}
		body.AngularVelocity = Vec3{}
	}
}

// WakeAll wakes every sleeping body. Call it after changing the world in a way
// the contact pass cannot see, such as teleporting geometry.
func (w *World) WakeAll() {
	if w == nil {
		return
	}
	for _, body := range w.bodies {
		body.Wake()
	}
}

// refreshInertia rebuilds the world inverse inertia tensor of every dynamic
// body from its current rotation. Contacts and constraints both read it, so it
// must run before contact generation.
func (w *World) refreshInertia() {
	for _, body := range w.bodies {
		if body == nil || !body.IsDynamic() {
			continue
		}
		body.refreshWorldInertia()
	}
}

func (w *World) prepareConstraints(dt float64) {
	for _, c := range w.constraints {
		if c != nil {
			c.Prepare(dt)
		}
	}
}

// solveContactsAndConstraints runs the interleaved sequential-impulse solver
// for contacts and constraints. Constraints and contacts share the same
// iteration budget; each iteration runs all contact impulses then all
// constraint impulses, which is the standard Box2D-style ordering.
func (w *World) solveContactsAndConstraints(dt float64) {
	iterations := w.solverIterations
	if iterations <= 0 {
		iterations = 1
	}
	for iter := 0; iter < iterations; iter++ {
		for i := range w.contacts {
			solveContactVelocityState(&w.contacts[i], &w.solveState[i])
		}
		for _, c := range w.constraints {
			if c != nil {
				c.SolveVelocity()
			}
		}
	}
	w.solveContactPositions(dt)
	for _, c := range w.constraints {
		if c != nil {
			c.SolvePosition()
		}
	}
}

// solveContactPositions removes the remaining overlap with a pseudo velocity
// pass, then integrates the pseudo velocities into positions and rotations.
//
// The pass gives a body an angular correction as well as a linear one, so a box
// that lands on one corner rotates down onto its face instead of sinking.
func (w *World) solveContactPositions(dt float64) {
	if dt <= 0 || len(w.contacts) == 0 {
		return
	}
	w.pseudoBodies = w.pseudoBodies[:0]
	for iter := 0; iter < positionIterations; iter++ {
		for i := range w.contacts {
			w.pseudoBodies = solveContactPositionState(&w.contacts[i], &w.solveState[i], dt, w.pseudoBodies)
		}
	}
	for i, body := range w.pseudoBodies {
		if body.pseudoVelocity.Len2() > 0 {
			body.Position = body.Position.Add(body.pseudoVelocity.Mul(dt))
		}
		if speed := body.pseudoAngular.Len(); speed > epsilon {
			delta := QuatFromAxisAngle(body.pseudoAngular.Div(speed), speed*dt)
			body.Rotation = delta.Mul(body.Rotation).Normalize()
			body.refreshWorldInertia()
		}
		body.pseudoVelocity = Vec3{}
		body.pseudoAngular = Vec3{}
		body.pseudoActive = false
		// Drop the reference so a removed body can be collected.
		w.pseudoBodies[i] = nil
	}
	w.pseudoBodies = w.pseudoBodies[:0]
}

// warmStartContacts seeds newly-generated contact manifolds with cached
// normal impulses from the previous frame and applies those impulses to body
// velocities. This accelerates convergence on stable stacks where the solver
// otherwise has to rebuild equilibrium from zero each tick.
//
// Matching: for each new contact point, find the cached point with the closest
// (LocalA, LocalB) pair within a tolerance. Matched points inherit the cached
// normal and friction impulses; unmatched points start from zero.
//
// The friction basis is derived from the contact normal alone, so a cached
// friction impulse still names the same direction on the next step. That is
// what lets a resting stack hold its lateral grip without rebuilding it.
func (w *World) warmStartContacts() {
	if w.contactCache == nil {
		return
	}
	const matchToleranceSq = 0.04 * 0.04 // 4cm in combined-local distance

	for mi := range w.contacts {
		m := &w.contacts[mi]
		state := &w.solveState[mi]
		if !state.active {
			continue
		}
		key := manifoldCacheKey(m)
		cached, ok := w.contactCache[key]
		if !ok {
			continue
		}
		for pi := 0; pi < state.count; pi++ {
			p := &m.Points[pi]
			slot := &state.points[pi]
			best := -1
			bestDist := matchToleranceSq
			for ci := 0; ci < cached.Count; ci++ {
				c := cached.Points[ci]
				d := p.LocalA.Sub(c.LocalA).Len2() + p.LocalB.Sub(c.LocalB).Len2()
				if d < bestDist {
					bestDist = d
					best = ci
				}
			}
			if best < 0 {
				continue
			}
			seededNormal := cached.Points[best].NormalImpulse
			seededTangent := cached.Points[best].TangentImpulse
			if seededNormal <= 0 {
				continue
			}
			p.NormalImpulse = seededNormal
			p.TangentImpulse = seededTangent
			impulse := state.normal.Mul(seededNormal).
				Add(state.tangent[0].Mul(seededTangent[0])).
				Add(state.tangent[1].Mul(seededTangent[1]))
			applyImpulseAtOffset(m.BodyA, impulse.Neg(), slot.rA)
			applyImpulseAtOffset(m.BodyB, impulse, slot.rB)
		}
	}
}

// cacheContactImpulses stores the post-solve normal impulses keyed by
// collider pair for warm-starting the next frame. Manifolds no longer in
// contact are pruned naturally because the cache is rebuilt from this
// frame's manifolds.
func (w *World) cacheContactImpulses() {
	if w.contactCache == nil {
		w.contactCache = make(map[contactCacheKey]cachedManifold)
	}
	next := make(map[contactCacheKey]cachedManifold, len(w.contacts))
	for mi := range w.contacts {
		m := &w.contacts[mi]
		if m.IsTrigger() || m.PointCount == 0 {
			continue
		}
		key := manifoldCacheKey(m)
		var cm cachedManifold
		for pi := 0; pi < m.PointCount; pi++ {
			cm.Points[cm.Count] = cachedContactPoint{
				LocalA:         m.Points[pi].LocalA,
				LocalB:         m.Points[pi].LocalB,
				NormalImpulse:  m.Points[pi].NormalImpulse,
				TangentImpulse: m.Points[pi].TangentImpulse,
			}
			cm.Count++
		}
		next[key] = cm
	}
	w.contactCache = next
}

func manifoldCacheKey(m *ContactManifold) contactCacheKey {
	ai, bi := 0, 0
	if m.ColliderA != nil {
		ai = m.ColliderA.index
	}
	if m.ColliderB != nil {
		bi = m.ColliderB.index
	}
	if bi < ai {
		ai, bi = bi, ai
	}
	return contactCacheKey{a: ai, b: bi}
}

func (w *World) integrateForces(dt float64) {
	for _, body := range w.bodies {
		if !body.IsDynamic() || body.IsSleeping() {
			body.clearForces()
			continue
		}
		acceleration := w.gravity.Add(body.force.Mul(body.InvMass))
		body.Velocity = body.Velocity.Add(acceleration.Mul(dt))
		if !body.invInertiaWorld.isZero() && body.torque.Len2() > 0 {
			// Angular acceleration is the inverse inertia tensor applied to the
			// torque, not the torque divided by mass.
			body.AngularVelocity = body.AngularVelocity.
				Add(body.invInertiaWorld.mul(body.torque).Mul(dt))
		}
		body.Velocity = body.Velocity.Mul(dampingFactor(body.LinearDamping, dt))
		body.AngularVelocity = body.AngularVelocity.Mul(dampingFactor(body.AngularDamping, dt))
		body.clearForces()
	}
}

func (w *World) generateContacts() {
	pairs := w.broadphase.CandidatePairs(w.colliders)
	w.broadphaseStep = w.steps
	w.contacts = w.contacts[:0]
	for _, pair := range pairs {
		w.contacts = CollideAll(pair.A, pair.B, w.contacts)
	}
}

// ensureBroadphase rebuilds the grid when the current step has not filled it
// yet. The swept pass reads the static cell map, and a stale map would let a
// fast body pass through geometry.
func (w *World) ensureBroadphase() {
	if w.broadphaseStep == w.steps && w.broadphase.Built() > 0 {
		return
	}
	w.broadphase.Rebuild(w.colliders)
	w.broadphaseStep = w.steps
}

func (w *World) integrateVelocities(dt float64) {
	for _, body := range w.bodies {
		if !body.IsDynamic() || body.IsSleeping() {
			continue
		}
		displacement := body.Velocity.Mul(dt)
		swept := false
		if w.continuousCollision && bodyNeedsSweep(body, displacement) {
			w.ensureBroadphase()
			if hit, ok := w.sweepBody(body, displacement); ok {
				body.Position = body.Position.Add(displacement.Normalize().Mul(maxFloat(0, hit.Distance-ccdSlop)))
				resolveCCDVelocity(body, hit)
				swept = true
			}
		}
		if !swept {
			body.Position = body.Position.Add(displacement)
		}
		angularSpeed := body.AngularVelocity.Len()
		if angularSpeed > epsilon {
			delta := QuatFromAxisAngle(body.AngularVelocity.Div(angularSpeed), angularSpeed*dt)
			body.Rotation = delta.Mul(body.Rotation).Normalize()
			body.refreshWorldInertia()
		}
	}
}

func (w *World) registerCollider(collider *Collider) {
	if collider == nil {
		return
	}
	if collider.index == 0 {
		w.nextColliderIndex++
		collider.index = w.nextColliderIndex
	}
	w.colliders = append(w.colliders, collider)
}

func dampingFactor(damping, dt float64) float64 {
	if damping <= 0 {
		return 1
	}
	return maxFloat(0, 1-damping*dt)
}
