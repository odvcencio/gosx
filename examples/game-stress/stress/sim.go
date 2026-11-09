//gosx:deterministic

// Package stress is a synthetic, deterministic game workload for measuring
// whether a GoSX game fits a 60 fps frame. It is generic on purpose: agents on
// a flat arena with static obstacles, two teams, a moving objective, steering,
// collision queries through package physics, and a table of rules each agent
// evaluates every tick. Nothing here models a particular sport or game.
//
// The workload is deterministic: the same Config and the same inputs give the
// same bits on js/wasm, amd64 and arm64. It follows the rules in
// docs/deterministic-simulation.md: every product that feeds an addition is
// rounded with an explicit float64 conversion, math comes from package det,
// no map is ranged over, and the step reads no clock. Hash reports a digest of
// the state, so two runs (for example a Web Worker and the main thread) can be
// compared tick by tick.
package stress

import (
	"encoding/binary"
	"errors"
	"fmt"

	"m31labs.dev/gosx/det"
	"m31labs.dev/gosx/physics"
)

// Agent states. A rule picks one each tick.
const (
	StateIdle uint8 = iota
	StateSeek
	StatePursue
	StateFlee
	StateWander
	StateRegroup
	StateRest
	stateCount
)

// Input bits for a controlled agent (a seat).
const (
	InputUp uint8 = 1 << iota
	InputDown
	InputLeft
	InputRight
	InputSprint
	// InputMarker raises the controlled agent by MarkerLift while held. The
	// browser test uses it to measure input-to-display latency.
	InputMarker
)

// MarkerLift is the height a held InputMarker adds to a seat's agent.
const MarkerLift = 1.5

// Config sizes the workload. The zero value of a field selects the default
// in DefaultConfig.
type Config struct {
	// Agents is the number of simulated agents, split into two teams.
	Agents int
	// Seats is how many agents (the first of each team, in turn) take input.
	Seats int
	// Seed seeds the deterministic random source.
	Seed uint64
	// HalfX and HalfZ are the arena's half extents in metres.
	HalfX, HalfZ float64
	// Obstacles is the number of static boxes on the arena.
	Obstacles int
	// Rules is the number of rules each agent evaluates per tick.
	Rules int
	// Feelers is the number of obstacle-avoidance rays each agent casts per
	// tick, on top of its two line-of-sight rays.
	Feelers int
	// InterceptIterations bounds the iterative intercept solve each agent runs
	// against the objective per tick.
	InterceptIterations int
	// TickHz is the fixed simulation rate.
	TickHz int
}

// DefaultConfig is the workload the jam performance budget names: 22 agents
// at 30 Hz.
func DefaultConfig() Config {
	return Config{
		Agents: 22, Seats: 2, Seed: 0x5eed, HalfX: 50, HalfZ: 30,
		Obstacles: 12, Rules: 24, Feelers: 3, InterceptIterations: 8, TickHz: 30,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.Agents <= 0 {
		c.Agents = d.Agents
	}
	if c.Seats < 0 {
		c.Seats = 0
	}
	if c.Seats > c.Agents {
		c.Seats = c.Agents
	}
	if c.Seed == 0 {
		c.Seed = d.Seed
	}
	if c.HalfX <= 0 {
		c.HalfX = d.HalfX
	}
	if c.HalfZ <= 0 {
		c.HalfZ = d.HalfZ
	}
	if c.Obstacles < 0 {
		c.Obstacles = 0
	}
	if c.Rules <= 0 {
		c.Rules = d.Rules
	}
	if c.Feelers < 0 {
		c.Feelers = 0
	}
	if c.InterceptIterations <= 0 {
		c.InterceptIterations = d.InterceptIterations
	}
	if c.TickHz <= 0 {
		c.TickHz = d.TickHz
	}
	return c
}

// Agent is one simulated agent. PX, PZ and PHeading hold the pose before the
// last tick, so a renderer can interpolate between the two.
type Agent struct {
	X, Z, VX, VZ, Heading float64
	PX, PZ, PHeading      float64
	Lift, PLift           float64
	Stamina               float64
	GoalX, GoalZ          float64
	Team, State           uint8
	StateTicks            uint16
	Target                int16
}

// Obstacle is a static box on the arena, centred at X, Z.
type Obstacle struct {
	X, Z, HalfX, HalfZ float64
}

// rule scores one action for one agent: weights over the agent's features
// plus a bias. The rule with the highest score picks the agent's state.
type rule struct {
	state uint8
	bias  float64
	w     [featureCount]float64
}

const (
	featObjectiveDist = iota
	featObjectiveVisible
	featOpponentDist
	featOpponentVisible
	featTeammateDist
	featStamina
	featCrowding
	featInterceptTime
	featScoreDiff
	featStateTicks
	featBias
	featureCount
)

// Sim is the deterministic workload. Create it with New.
type Sim struct {
	cfg       Config
	dt        float64
	tick      uint64
	rng       uint64
	objX      float64
	objZ      float64
	objVX     float64
	objVZ     float64
	score     [2]int32
	agents    []Agent
	obstacles []Obstacle
	rules     []rule
	world     *physics.World

	// Scratch reused every tick; not part of the state.
	grid     grid
	features [featureCount]float64
	nearIdx  []int16

	// Timing is optional instrumentation. It never changes the state.
	Timing Timing
}

// Timing accumulates the wall time a Sim spends in its AI step and in the rest
// of its step. Set Clock to a monotonic millisecond clock to enable it; the
// zero value measures nothing.
type Timing struct {
	Clock  func() float64
	AIMS   float64
	StepMS float64
	Ticks  int
}

// Reset clears the accumulated times and keeps Clock.
func (t *Timing) Reset() { t.AIMS, t.StepMS, t.Ticks = 0, 0, 0 }

// New builds a workload from cfg.
func New(cfg Config) *Sim {
	cfg = cfg.withDefaults()
	s := &Sim{cfg: cfg, dt: 1 / float64(cfg.TickHz), rng: cfg.Seed}
	s.world = physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{}, FixedTimestep: s.dt})
	s.obstacles = make([]Obstacle, cfg.Obstacles)
	for i := range s.obstacles {
		o := Obstacle{
			X:     s.randRange(-cfg.HalfX+8, cfg.HalfX-8),
			Z:     s.randRange(-cfg.HalfZ+6, cfg.HalfZ-6),
			HalfX: s.randRange(0.6, 2.2),
			HalfZ: s.randRange(0.6, 2.2),
		}
		s.obstacles[i] = o
		s.world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Offset: physics.Vec3{X: o.X, Y: 1, Z: o.Z},
			Width: 2 * o.HalfX, Height: 2, Depth: 2 * o.HalfZ})
	}
	// The arena walls are colliders too, so feeler rays see them.
	walls := [4][4]float64{
		{0, -cfg.HalfZ - 0.5, cfg.HalfX + 1, 0.5}, {0, cfg.HalfZ + 0.5, cfg.HalfX + 1, 0.5},
		{-cfg.HalfX - 0.5, 0, 0.5, cfg.HalfZ + 1}, {cfg.HalfX + 0.5, 0, 0.5, cfg.HalfZ + 1},
	}
	for _, w := range walls {
		s.world.AddCollider(physics.ColliderConfig{Shape: physics.ShapeBox, Offset: physics.Vec3{X: w[0], Y: 1, Z: w[1]},
			Width: 2 * w[2], Height: 2, Depth: 2 * w[3]})
	}
	s.agents = make([]Agent, cfg.Agents)
	for i := range s.agents {
		a := &s.agents[i]
		a.Team = uint8(i % 2)
		side := -1.0
		if a.Team == 1 {
			side = 1
		}
		a.X = float64(side * s.randRange(5, cfg.HalfX-5))
		a.Z = s.randRange(-cfg.HalfZ+3, cfg.HalfZ-3)
		a.PX, a.PZ = a.X, a.Z
		a.Heading = s.randRange(-3.14159, 3.14159)
		a.PHeading = a.Heading
		a.Stamina = 1
		a.Target = -1
		a.State = StateIdle
	}
	s.rules = make([]rule, cfg.Rules)
	for i := range s.rules {
		r := &s.rules[i]
		r.state = uint8(i % int(stateCount))
		r.bias = s.randRange(-0.2, 0.2)
		for f := range r.w {
			r.w[f] = s.randRange(-1, 1)
		}
	}
	s.objX, s.objZ = 0, 0
	s.objVX, s.objVZ = s.randRange(-4, 4), s.randRange(-3, 3)
	s.grid = newGrid(cfg.HalfX, cfg.HalfZ, 4, cfg.Agents)
	s.nearIdx = make([]int16, 0, cfg.Agents)
	return s
}

// Config returns the effective configuration.
func (s *Sim) Config() Config { return s.cfg }

// Tick returns the number of steps run since New (or the restored tick).
func (s *Sim) Tick() uint64 { return s.tick }

// Agents returns the live agents. Do not keep the slice across Step.
func (s *Sim) Agents() []Agent { return s.agents }

// Obstacles returns the static boxes.
func (s *Sim) Obstacles() []Obstacle { return s.obstacles }

// Objective returns the objective's position.
func (s *Sim) Objective() (x, z float64) { return s.objX, s.objZ }

// Score returns each team's points.
func (s *Sim) Score() [2]int32 { return s.score }

// xorshift64* random source; the state is part of the snapshot.
func (s *Sim) rand() uint64 {
	x := s.rng
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	s.rng = x
	return x * 0x2545F4914F6CDD1D
}

func (s *Sim) randFloat() float64 { return float64(s.rand()>>11) / (1 << 53) }

func (s *Sim) randRange(lo, hi float64) float64 {
	return lo + float64((hi-lo)*s.randFloat())
}

// Step advances one fixed tick. inputs holds one byte per seat (missing seats
// read as zero).
func (s *Sim) Step(inputs []byte) {
	var t0 float64
	clock := s.Timing.Clock
	if clock != nil {
		t0 = clock()
	}
	for i := range s.agents {
		a := &s.agents[i]
		a.PX, a.PZ, a.PHeading, a.PLift = a.X, a.Z, a.Heading, a.Lift
	}
	s.moveObjective()
	s.grid.build(s.agents)
	var aiStart float64
	if clock != nil {
		aiStart = clock()
	}
	for i := range s.agents {
		s.think(i)
	}
	if clock != nil {
		s.Timing.AIMS += clock() - aiStart
	}
	for i := range s.agents {
		var in byte
		if seat := s.seatOf(i); seat >= 0 && seat < len(inputs) {
			in = inputs[seat]
		}
		s.steer(i, in)
	}
	s.resolveCollisions()
	s.applyRules()
	s.tick++
	if clock != nil {
		s.Timing.StepMS += clock() - t0
		s.Timing.Ticks++
	}
}

// seatOf returns the seat that controls agent i, or -1.
func (s *Sim) seatOf(i int) int {
	if i < s.cfg.Seats {
		return i
	}
	return -1
}

func (s *Sim) moveObjective() {
	t := float64(float64(s.tick) * s.dt)
	// A slow drift plus a deterministic wobble, bouncing off the walls.
	wobbleX := float64(0.8 * det.Sin(float64(t*0.7)))
	wobbleZ := float64(0.6 * det.Cos(float64(t*0.53)))
	s.objX += float64((s.objVX + wobbleX) * s.dt)
	s.objZ += float64((s.objVZ + wobbleZ) * s.dt)
	limX, limZ := s.cfg.HalfX-2, s.cfg.HalfZ-2
	if s.objX > limX || s.objX < -limX {
		s.objVX = -s.objVX
		s.objX = det.Max(-limX, det.Min(limX, s.objX))
	}
	if s.objZ > limZ || s.objZ < -limZ {
		s.objVZ = -s.objVZ
		s.objZ = det.Max(-limZ, det.Min(limZ, s.objZ))
	}
}

// think computes agent i's features and lets the rule table pick its state
// and goal. This is the "AI and rules" part of the budget.
func (s *Sim) think(i int) {
	a := &s.agents[i]
	f := &s.features
	odx, odz := s.objX-a.X, s.objZ-a.Z
	objDist := hypot(odx, odz)
	f[featObjectiveDist] = objDist / (2 * s.cfg.HalfX)
	f[featObjectiveVisible] = s.visible(a.X, a.Z, s.objX, s.objZ)

	// Nearest opponent and teammate through the spatial grid.
	s.nearIdx = s.grid.near(s.agents, a.X, a.Z, 12, s.nearIdx[:0])
	oppIdx, mateIdx := int16(-1), int16(-1)
	oppDist, mateDist := 1e9, 1e9
	crowding := 0.0
	for _, j := range s.nearIdx {
		if int(j) == i {
			continue
		}
		b := &s.agents[j]
		d := hypot(b.X-a.X, b.Z-a.Z)
		if d < 4 {
			crowding += float64((4 - d) * 0.25)
		}
		if b.Team != a.Team {
			if d < oppDist {
				oppDist, oppIdx = d, j
			}
		} else if d < mateDist {
			mateDist, mateIdx = d, j
		}
	}
	_ = mateIdx
	f[featOpponentDist] = det.Min(oppDist, 50) / 50
	if oppIdx >= 0 {
		b := &s.agents[oppIdx]
		f[featOpponentVisible] = s.visible(a.X, a.Z, b.X, b.Z)
	} else {
		f[featOpponentVisible] = 0
	}
	f[featTeammateDist] = det.Min(mateDist, 50) / 50
	f[featStamina] = a.Stamina
	f[featCrowding] = det.Min(crowding, 4) / 4
	ix, iz, itime := s.intercept(a)
	f[featInterceptTime] = det.Min(itime, 10) / 10
	diff := float64(s.score[a.Team] - s.score[1-a.Team])
	f[featScoreDiff] = det.Max(-1, det.Min(1, diff/5))
	f[featStateTicks] = det.Min(float64(a.StateTicks), 90) / 90
	f[featBias] = 1

	best, bestScore := uint8(StateIdle), -1e18
	for r := range s.rules {
		rl := &s.rules[r]
		score := rl.bias
		for k := 0; k < featureCount; k++ {
			score += float64(rl.w[k] * f[k])
		}
		// Hysteresis: the current state is sticky for a few ticks.
		if rl.state == a.State && a.StateTicks < 15 {
			score += 0.15
		}
		if score > bestScore {
			best, bestScore = rl.state, score
		}
	}
	// Stamina gates: an exhausted agent rests.
	if a.Stamina < 0.1 {
		best = StateRest
	}
	if best != a.State {
		a.State, a.StateTicks = best, 0
	} else if a.StateTicks < 65535 {
		a.StateTicks++
	}
	switch a.State {
	case StateSeek:
		a.GoalX, a.GoalZ, a.Target = ix, iz, -1
	case StatePursue, StateFlee:
		a.Target = oppIdx
		if oppIdx < 0 {
			a.GoalX, a.GoalZ = ix, iz
		}
	case StateWander:
		if a.StateTicks%30 == 0 {
			a.GoalX = det.Max(-s.cfg.HalfX+2, det.Min(s.cfg.HalfX-2, a.X+s.randRange(-10, 10)))
			a.GoalZ = det.Max(-s.cfg.HalfZ+2, det.Min(s.cfg.HalfZ-2, a.Z+s.randRange(-10, 10)))
		}
		a.Target = -1
	case StateRegroup:
		home := -s.cfg.HalfX * 0.5
		if a.Team == 1 {
			home = s.cfg.HalfX * 0.5
		}
		a.GoalX, a.GoalZ, a.Target = home, float64(0.2*a.Z), -1
	default:
		a.GoalX, a.GoalZ, a.Target = a.X, a.Z, -1
	}
}

// intercept solves for the point where agent a meets the moving objective at
// its top speed, by fixed-point iteration.
func (s *Sim) intercept(a *Agent) (x, z, t float64) {
	speed := 7.0
	t = hypot(s.objX-a.X, s.objZ-a.Z) / speed
	for k := 0; k < s.cfg.InterceptIterations; k++ {
		px := s.objX + float64(s.objVX*t)
		pz := s.objZ + float64(s.objVZ*t)
		t = hypot(px-a.X, pz-a.Z) / speed
	}
	x = det.Max(-s.cfg.HalfX+1, det.Min(s.cfg.HalfX-1, s.objX+float64(s.objVX*t)))
	z = det.Max(-s.cfg.HalfZ+1, det.Min(s.cfg.HalfZ-1, s.objZ+float64(s.objVZ*t)))
	return x, z, t
}

// visible casts a ray through the physics world and reports 1 when nothing
// blocks the line between the two points.
func (s *Sim) visible(ax, az, bx, bz float64) float64 {
	dx, dz := bx-ax, bz-az
	d := hypot(dx, dz)
	if d < 1e-6 {
		return 1
	}
	hit, ok := s.world.Raycast(physics.Ray{Origin: physics.Vec3{X: ax, Y: 1, Z: az}, Direction: physics.Vec3{X: dx / d, Z: dz / d}}, d)
	if ok && hit.Distance < d {
		return 0
	}
	return 1
}

// steer turns the agent's state into a velocity: seek, pursue or flee, plus
// separation from neighbours and feeler-ray obstacle avoidance. A seat's input
// overrides the AI's direction.
func (s *Sim) steer(i int, in byte) {
	a := &s.agents[i]
	maxSpeed := 6.0
	tx, tz := a.GoalX, a.GoalZ
	switch a.State {
	case StatePursue:
		if a.Target >= 0 {
			b := &s.agents[a.Target]
			tx, tz = b.X+float64(b.VX*0.5), b.Z+float64(b.VZ*0.5)
		}
		maxSpeed = 7
	case StateFlee:
		if a.Target >= 0 {
			b := &s.agents[a.Target]
			tx, tz = a.X-(b.X-a.X), a.Z-(b.Z-a.Z)
		}
		maxSpeed = 6.5
	case StateRest, StateIdle:
		maxSpeed = 1.2
	case StateWander:
		maxSpeed = 3
	}
	dx, dz := tx-a.X, tz-a.Z
	d := hypot(dx, dz)
	var wx, wz float64
	if d > 0.05 {
		// Arrive: slow down inside 3 m.
		speed := maxSpeed
		if d < 3 {
			speed = float64(maxSpeed * d / 3)
		}
		wx, wz = float64(dx/d*speed), float64(dz/d*speed)
	}
	if in != 0 {
		ix, iz := 0.0, 0.0
		if in&InputUp != 0 {
			iz--
		}
		if in&InputDown != 0 {
			iz++
		}
		if in&InputLeft != 0 {
			ix--
		}
		if in&InputRight != 0 {
			ix++
		}
		if n := hypot(ix, iz); n > 0 {
			sp := 6.0
			if in&InputSprint != 0 {
				sp = 8.5
			}
			wx, wz = float64(ix/n*sp), float64(iz/n*sp)
		}
	}
	if in&InputMarker != 0 {
		a.Lift = MarkerLift
	} else {
		a.Lift = 0
	}
	// Separation from neighbours.
	s.nearIdx = s.grid.near(s.agents, a.X, a.Z, 3, s.nearIdx[:0])
	for _, j := range s.nearIdx {
		if int(j) == i {
			continue
		}
		b := &s.agents[j]
		ox, oz := a.X-b.X, a.Z-b.Z
		od := hypot(ox, oz)
		if od > 1e-6 && od < 3 {
			k := (3 - od) / od
			wx += float64(ox * k)
			wz += float64(oz * k)
		}
	}
	// Feelers: rays ahead and to each side of the desired direction.
	if wl := hypot(wx, wz); wl > 0.1 && s.cfg.Feelers > 0 {
		hx, hz := wx/wl, wz/wl
		for k := 0; k < s.cfg.Feelers; k++ {
			angle := 0.0
			if s.cfg.Feelers > 1 {
				angle = float64(float64(float64(k)/float64(s.cfg.Feelers-1)-0.5) * 1.2)
			}
			c, sn := det.Cos(angle), det.Sin(angle)
			fx := float64(hx*c) - float64(hz*sn)
			fz := float64(hx*sn) + float64(hz*c)
			hit, ok := s.world.Raycast(physics.Ray{Origin: physics.Vec3{X: a.X, Y: 1, Z: a.Z}, Direction: physics.Vec3{X: fx, Z: fz}}, 4)
			if ok {
				push := float64((4 - hit.Distance) * 1.5)
				wx += float64(hit.Normal.X * push)
				wz += float64(hit.Normal.Z * push)
			}
		}
	}
	// Accelerate toward the wanted velocity, limited by stamina.
	accel := float64(12 * (0.4 + float64(0.6*a.Stamina)))
	ax, az := wx-a.VX, wz-a.VZ
	if al := hypot(ax, az); al > float64(accel*s.dt) {
		k := float64(accel*s.dt) / al
		ax, az = float64(ax*k), float64(az*k)
	}
	a.VX += ax
	a.VZ += az
	if sp := hypot(a.VX, a.VZ); sp > 8.5 {
		a.VX, a.VZ = float64(a.VX/sp*8.5), float64(a.VZ/sp*8.5)
	}
	a.X += float64(a.VX * s.dt)
	a.Z += float64(a.VZ * s.dt)
	if sp := hypot(a.VX, a.VZ); sp > 0.3 {
		target := det.Atan2(a.VX, a.VZ)
		a.Heading = turnToward(a.Heading, target, float64(10*s.dt))
		a.Stamina = det.Max(0, a.Stamina-float64(sp*0.0008))
	} else {
		a.Stamina = det.Min(1, a.Stamina+0.01)
	}
}

// resolveCollisions pushes overlapping agents apart and keeps every agent out
// of the obstacles and inside the arena.
func (s *Sim) resolveCollisions() {
	const radius = 0.45
	s.grid.build(s.agents)
	for i := range s.agents {
		a := &s.agents[i]
		s.nearIdx = s.grid.near(s.agents, a.X, a.Z, 2*radius, s.nearIdx[:0])
		for _, j := range s.nearIdx {
			if int(j) <= i {
				continue
			}
			b := &s.agents[j]
			dx, dz := b.X-a.X, b.Z-a.Z
			d := hypot(dx, dz)
			if d < 2*radius && d > 1e-9 {
				push := float64((2*radius - d) * 0.5 / d)
				a.X -= float64(dx * push)
				a.Z -= float64(dz * push)
				b.X += float64(dx * push)
				b.Z += float64(dz * push)
			}
		}
		for n := range s.obstacles {
			o := &s.obstacles[n]
			cx := det.Max(o.X-o.HalfX, det.Min(o.X+o.HalfX, a.X))
			cz := det.Max(o.Z-o.HalfZ, det.Min(o.Z+o.HalfZ, a.Z))
			dx, dz := a.X-cx, a.Z-cz
			d := hypot(dx, dz)
			if d < radius {
				if d > 1e-9 {
					k := (radius - d) / d
					a.X += float64(dx * k)
					a.Z += float64(dz * k)
				} else {
					a.X = o.X + o.HalfX + radius
				}
			}
		}
		a.X = det.Max(-s.cfg.HalfX+radius, det.Min(s.cfg.HalfX-radius, a.X))
		a.Z = det.Max(-s.cfg.HalfZ+radius, det.Min(s.cfg.HalfZ-radius, a.Z))
	}
}

// applyRules runs the game-level rules: an agent that reaches the objective
// scores for its team and the objective moves; opposing agents in contact
// drain each other's stamina.
func (s *Sim) applyRules() {
	for i := range s.agents {
		a := &s.agents[i]
		if hypot(s.objX-a.X, s.objZ-a.Z) < 1.2 {
			s.score[a.Team]++
			s.objX = s.randRange(-s.cfg.HalfX+5, s.cfg.HalfX-5)
			s.objZ = s.randRange(-s.cfg.HalfZ+5, s.cfg.HalfZ-5)
			s.objVX, s.objVZ = s.randRange(-4, 4), s.randRange(-3, 3)
			break
		}
	}
	for i := range s.agents {
		a := &s.agents[i]
		s.nearIdx = s.grid.near(s.agents, a.X, a.Z, 1.2, s.nearIdx[:0])
		for _, j := range s.nearIdx {
			b := &s.agents[j]
			if b.Team != a.Team {
				a.Stamina = det.Max(0, a.Stamina-0.004)
			}
		}
	}
}

func hypot(x, z float64) float64 { return det.Sqrt(float64(x*x) + float64(z*z)) }

// turnToward rotates angle toward target by at most step radians.
func turnToward(angle, target, step float64) float64 {
	const pi = 3.141592653589793
	d := target - angle
	for d > pi {
		d -= 2 * pi
	}
	for d < -pi {
		d += 2 * pi
	}
	if d > step {
		d = step
	} else if d < -step {
		d = -step
	}
	angle += d
	for angle > pi {
		angle -= 2 * pi
	}
	for angle < -pi {
		angle += 2 * pi
	}
	return angle
}

// grid is a uniform spatial hash of agent indices, rebuilt in place.
type grid struct {
	cell         float64
	minX, minZ   float64
	cols, rows   int
	heads, nexts []int16
}

func newGrid(halfX, halfZ, cell float64, agents int) grid {
	cols := int(det.Ceil(2*halfX/cell)) + 1
	rows := int(det.Ceil(2*halfZ/cell)) + 1
	return grid{cell: cell, minX: -halfX, minZ: -halfZ, cols: cols, rows: rows,
		heads: make([]int16, cols*rows), nexts: make([]int16, agents)}
}

func (g *grid) cellOf(x, z float64) (int, int) {
	cx := int(det.Floor((x - g.minX) / g.cell))
	cz := int(det.Floor((z - g.minZ) / g.cell))
	return min(max(cx, 0), g.cols-1), min(max(cz, 0), g.rows-1)
}

func (g *grid) build(agents []Agent) {
	for i := range g.heads {
		g.heads[i] = -1
	}
	for i := range agents {
		cx, cz := g.cellOf(agents[i].X, agents[i].Z)
		k := cz*g.cols + cx
		g.nexts[i] = g.heads[k]
		g.heads[k] = int16(i)
	}
}

// near appends the agents within radius of (x, z), in a fixed order.
func (g *grid) near(agents []Agent, x, z, radius float64, dst []int16) []int16 {
	x0, z0 := g.cellOf(x-radius, z-radius)
	x1, z1 := g.cellOf(x+radius, z+radius)
	r2 := float64(radius * radius)
	for cz := z0; cz <= z1; cz++ {
		for cx := x0; cx <= x1; cx++ {
			for j := g.heads[cz*g.cols+cx]; j >= 0; j = g.nexts[j] {
				dx, dz := agents[j].X-x, agents[j].Z-z
				if float64(dx*dx)+float64(dz*dz) <= r2 {
					dst = append(dst, j)
				}
			}
		}
	}
	return dst
}

// Snapshot format: a fixed header, then one fixed-size record per agent.
const (
	snapshotMagic     = "GSS1"
	snapshotHeader    = 4 + 2 + 8 + 8 + 4*8 + 2*4
	agentRecordFloats = 15
	agentRecordBytes  = agentRecordFloats*8 + 1 + 1 + 2 + 2
)

// SnapshotSize is the length of a Snapshot for this Sim.
func (s *Sim) SnapshotSize() int { return snapshotHeader + len(s.agents)*agentRecordBytes }

// Snapshot appends the full state to dst and returns it. It allocates only
// when dst is too small.
func (s *Sim) Snapshot(dst []byte) []byte {
	dst = append(dst, snapshotMagic...)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(s.agents)))
	dst = binary.LittleEndian.AppendUint64(dst, s.tick)
	dst = binary.LittleEndian.AppendUint64(dst, s.rng)
	for _, v := range [4]float64{s.objX, s.objZ, s.objVX, s.objVZ} {
		dst = binary.LittleEndian.AppendUint64(dst, det.Float64bits(v))
	}
	dst = binary.LittleEndian.AppendUint32(dst, uint32(s.score[0]))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(s.score[1]))
	for i := range s.agents {
		a := &s.agents[i]
		for _, v := range [agentRecordFloats]float64{a.X, a.Z, a.VX, a.VZ, a.Heading, a.PX, a.PZ, a.PHeading, a.Lift, a.PLift, a.Stamina, a.GoalX, a.GoalZ, 0, 0} {
			dst = binary.LittleEndian.AppendUint64(dst, det.Float64bits(v))
		}
		dst = append(dst, a.Team, a.State)
		dst = binary.LittleEndian.AppendUint16(dst, a.StateTicks)
		dst = binary.LittleEndian.AppendUint16(dst, uint16(a.Target))
	}
	return dst
}

// ErrSnapshot reports a snapshot that does not fit this Sim.
var ErrSnapshot = errors.New("stress: snapshot does not fit this simulation")

// Restore loads a Snapshot. The Sim is unchanged when it returns an error.
func (s *Sim) Restore(b []byte) error {
	if len(b) != s.SnapshotSize() || string(b[:4]) != snapshotMagic || int(binary.LittleEndian.Uint16(b[4:])) != len(s.agents) {
		return fmt.Errorf("%w: %d bytes", ErrSnapshot, len(b))
	}
	off := 6
	u64 := func() uint64 { v := binary.LittleEndian.Uint64(b[off:]); off += 8; return v }
	f64 := func() float64 { return det.Float64frombits(u64()) }
	s.tick = u64()
	s.rng = u64()
	s.objX, s.objZ, s.objVX, s.objVZ = f64(), f64(), f64(), f64()
	s.score[0] = int32(binary.LittleEndian.Uint32(b[off:]))
	s.score[1] = int32(binary.LittleEndian.Uint32(b[off+4:]))
	off += 8
	for i := range s.agents {
		a := &s.agents[i]
		a.X, a.Z, a.VX, a.VZ, a.Heading = f64(), f64(), f64(), f64(), f64()
		a.PX, a.PZ, a.PHeading, a.Lift, a.PLift = f64(), f64(), f64(), f64(), f64()
		a.Stamina, a.GoalX, a.GoalZ = f64(), f64(), f64()
		off += 16
		a.Team, a.State = b[off], b[off+1]
		a.StateTicks = binary.LittleEndian.Uint16(b[off+2:])
		a.Target = int16(binary.LittleEndian.Uint16(b[off+4:]))
		off += 6
	}
	return nil
}

// Hash returns an FNV-1a digest of the state. Equal hashes at equal ticks mean
// two runs agree bit for bit.
func (s *Sim) Hash() uint64 {
	var buf [snapshotHeader + 64*agentRecordBytes]byte
	snap := s.Snapshot(buf[:0])
	h := uint64(14695981039346656037)
	for _, c := range snap {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}
