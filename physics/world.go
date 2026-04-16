package physics

type WorldConfig struct {
	Gravity          Vec3
	FixedTimestep    float64
	SolverIter       int
	SolverIterations int
	BroadPhaseCell   float64
	// DisableWarmStart turns off contact impulse caching across frames. Warm
	// starting is on by default; disabling is only useful for tests or
	// deterministic replay scenarios that must reproduce a cold solver.
	DisableWarmStart bool
}

type contactCacheKey struct {
	a int
	b int
}

type cachedContactPoint struct {
	LocalA        Vec3
	LocalB        Vec3
	NormalImpulse float64
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

	warmStart    bool
	contactCache map[contactCacheKey]cachedManifold
	constraints  []Constraint
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
	w := &World{
		gravity:          config.Gravity,
		fixedTimestep:    config.FixedTimestep,
		solverIterations: iterations,
		broadphase:       NewSpatialHash(config.BroadPhaseCell),
		warmStart:        !config.DisableWarmStart,
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

func (w *World) CandidatePairs() []ColliderPair {
	return w.broadphase.CandidatePairs(w.colliders)
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

func (w *World) SetGravity(gravity Vec3) {
	w.gravity = gravity
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
	w.integrateForces(dt)
	w.generateContacts()
	if w.warmStart {
		w.warmStartContacts()
	}
	w.prepareConstraints(dt)
	w.solveContactsAndConstraints(dt)
	if w.warmStart {
		w.cacheContactImpulses()
	}
	w.integrateVelocities(dt)
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
			solveContactVelocity(&w.contacts[i])
		}
		for _, c := range w.constraints {
			if c != nil {
				c.SolveVelocity()
			}
		}
	}
	for i := range w.contacts {
		solveContactPosition(&w.contacts[i], dt)
	}
	for _, c := range w.constraints {
		if c != nil {
			c.SolvePosition()
		}
	}
}

// warmStartContacts seeds newly-generated contact manifolds with cached
// normal impulses from the previous frame and applies those impulses to body
// velocities. This accelerates convergence on stable stacks where the solver
// otherwise has to rebuild equilibrium from zero each tick.
//
// Matching: for each new contact point, find the cached point with the closest
// (LocalA, LocalB) pair within a tolerance. Matched points inherit the cached
// NormalImpulse; unmatched points start from zero. Tangent impulses always
// start from zero because the tangent basis is recomputed each iteration from
// the current relative velocity.
func (w *World) warmStartContacts() {
	if w.contactCache == nil {
		return
	}
	const matchToleranceSq = 0.04 * 0.04 // 4cm in combined-local distance

	for mi := range w.contacts {
		m := &w.contacts[mi]
		if m.IsTrigger() || m.PointCount == 0 {
			continue
		}
		key := manifoldCacheKey(m)
		cached, ok := w.contactCache[key]
		if !ok {
			continue
		}
		normal := m.Normal.Normalize()
		for pi := 0; pi < m.PointCount; pi++ {
			p := &m.Points[pi]
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
			seeded := cached.Points[best].NormalImpulse
			if seeded <= 0 {
				continue
			}
			p.NormalImpulse = seeded
			impulse := normal.Mul(seeded)
			applyLinearImpulse(m.BodyA, impulse.Neg())
			applyLinearImpulse(m.BodyB, impulse)
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
				LocalA:        m.Points[pi].LocalA,
				LocalB:        m.Points[pi].LocalB,
				NormalImpulse: m.Points[pi].NormalImpulse,
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
		body.AngularVelocity = body.AngularVelocity.Add(body.torque.Mul(body.InvMass * dt))
		body.Velocity = body.Velocity.Mul(dampingFactor(body.LinearDamping, dt))
		body.AngularVelocity = body.AngularVelocity.Mul(dampingFactor(body.AngularDamping, dt))
		body.clearForces()
	}
}

func (w *World) generateContacts() {
	pairs := w.broadphase.CandidatePairs(w.colliders)
	w.contacts = w.contacts[:0]
	for _, pair := range pairs {
		contact, ok := Collide(pair.A, pair.B)
		if ok {
			w.contacts = append(w.contacts, contact)
		}
	}
}

func (w *World) integrateVelocities(dt float64) {
	for _, body := range w.bodies {
		if !body.IsDynamic() || body.IsSleeping() {
			continue
		}
		body.Position = body.Position.Add(body.Velocity.Mul(dt))
		angularSpeed := body.AngularVelocity.Len()
		if angularSpeed > epsilon {
			delta := QuatFromAxisAngle(body.AngularVelocity.Div(angularSpeed), angularSpeed*dt)
			body.Rotation = delta.Mul(body.Rotation).Normalize()
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
