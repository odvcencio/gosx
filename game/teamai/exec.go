package teamai

import (
	"errors"
	"fmt"
)

// Options configures an Executor.
type Options struct {
	// Step is the fixed simulation step in seconds, such as 1.0/30. Required.
	Step float64
	// ArriveRadius is the distance at which an agent counts as arrived at a
	// slot, point or path segment end. Zero means 0.25.
	ArriveRadius float64
	// Horizon is how far ahead, in seconds, pursuit and cutoff look for an
	// interception. Zero means 4.
	Horizon float64
	// Separation is the radius within which teammates push each other apart.
	// Zero turns separation off.
	Separation float64
	// SeparationWeight scales the push, as a fraction of the agent's top speed
	// per neighbour at contact. Zero means 0.5.
	SeparationWeight float64
	// ShadowGain is the error gain, in 1/seconds, of mirror and engage steps.
	// Zero means 3.
	ShadowGain float64
}

// Command is the executor's output for one agent for one tick.
type Command struct {
	// Vel is the desired velocity. The host moves the agent with it, through
	// its character controller or with Integrate.
	Vel Vec2
	// Face is the unit direction the agent should face. It is the target's
	// direction for mirror, guard and engage steps, else the direction of
	// Vel, else the team's forward.
	Face Vec2
}

// EventKind is the kind of an Event.
type EventKind uint8

const (
	// EventStep: the agent started step Value of its assignment.
	EventStep EventKind = iota + 1
	// EventMark: the agent reached mark Value (see Book.MarkName).
	EventMark
	// EventBranch: a decision took branch Value.
	EventBranch
	// EventInterrupt: interrupt Value of the assignment fired.
	EventInterrupt
	// EventContact: an engage step made contact with agent or object Value
	// (Target says which kind).
	EventContact
	// EventDone: the assignment ran past its last step. The agent now has no
	// assignment.
	EventDone
)

func (k EventKind) String() string {
	switch k {
	case EventStep:
		return "step"
	case EventMark:
		return "mark"
	case EventBranch:
		return "branch"
	case EventInterrupt:
		return "interrupt"
	case EventContact:
		return "contact"
	case EventDone:
		return "done"
	}
	return "unknown"
}

// Event reports something that happened to an agent during Step, in agent
// order. Hosts use them to trigger animation, audio and contests.
type Event struct {
	Agent  int
	Kind   EventKind
	Value  int
	Target Ref
}

// agentState is everything the executor keeps per agent. It is fixed-size and
// part of the snapshot.
type agentState struct {
	assignment   int16
	step         int16
	slot         int16 // slot index in the team formation, or -1
	begun        bool
	contact      bool
	pending      int8 // interrupt waiting out the reaction time, or -1
	sign         float64
	stepTicks    uint32
	assignTicks  uint32
	pendingTicks uint16
	fired        uint16
	marks        uint32
	target       Ref // latched target
	assigned     Ref // host-set target
	point        Vec2
	mem          [MemorySlots]float64
	path         pathState
}

type teamState struct {
	formation FormationID
	slots     []Vec2 // resolved this tick
	slotsTick uint64
	resolved  bool
}

// Executor runs one assignment per agent. It is deterministic: the same Book,
// Options, executor state and World give the same Commands and Events on every
// platform. It is not safe for concurrent use.
type Executor struct {
	book   *Book
	opt    Options
	dt     float64
	tol    float64
	agents []agentState
	teams  []teamState
	cmds   []Command
	events []Event
	q      Query
	used   []bool
	slotOf []int
	buf    []byte
}

// NewExecutor returns an executor for book. Call Reset before the first Step.
func NewExecutor(book *Book, opt Options) (*Executor, error) {
	if book == nil {
		return nil, errors.New("teamai: NewExecutor needs a Book")
	}
	if !finite(opt.Step) || opt.Step <= 0 {
		return nil, fmt.Errorf("teamai: Options.Step %v: want a positive number of seconds", opt.Step)
	}
	if opt.ArriveRadius <= 0 {
		opt.ArriveRadius = 0.25
	}
	if opt.Horizon <= 0 {
		opt.Horizon = 4
	}
	if opt.SeparationWeight <= 0 {
		opt.SeparationWeight = 0.5
	}
	if opt.ShadowGain <= 0 {
		opt.ShadowGain = 3
	}
	return &Executor{book: book, opt: opt, dt: opt.Step, tol: opt.ArriveRadius}, nil
}

// Book returns the executor's Book.
func (e *Executor) Book() *Book { return e.book }

// Reset sizes the executor for agents agents and teams teams and clears every
// assignment. It allocates; Step does not.
func (e *Executor) Reset(agents, teams int) {
	e.agents = make([]agentState, agents)
	for i := range e.agents {
		e.agents[i] = idleState()
	}
	e.teams = make([]teamState, teams)
	maxSlots := 0
	for i := range e.book.formations {
		if n := len(e.book.formations[i].slots); n > maxSlots {
			maxSlots = n
		}
	}
	for i := range e.teams {
		e.teams[i] = teamState{formation: NoFormation, slots: make([]Vec2, 0, maxSlots)}
	}
	e.cmds = make([]Command, agents)
	e.events = make([]Event, 0, 4*agents+16)
	e.used = make([]bool, agents)
	e.slotOf = make([]int, maxSlots)
	e.buf = nil
}

func idleState() agentState {
	return agentState{assignment: -1, step: -1, slot: -1, pending: -1, sign: 1, path: pathState{seg: -1}}
}

// NumAgents returns the number of agents Reset sized the executor for.
func (e *Executor) NumAgents() int { return len(e.agents) }

// SetFormation sets team's formation for slot steps without starting a plan.
func (e *Executor) SetFormation(team int, f FormationID) {
	e.teams[team].formation = f
}

// Formation returns team's formation.
func (e *Executor) Formation(team int) FormationID { return e.teams[team].formation }

// StartPlan puts a team into plan p: it sets the plan's formation, gives each
// slot an agent of the team by role (AssignByRole), signs each agent by its
// slot's side in the team's current frame, and starts each agent's slot
// assignment. Team agents without a slot get the plan's extra assignment. It
// returns the number of slots no agent could fill. StartPlan does not
// allocate.
func (e *Executor) StartPlan(w *World, team int, p PlanID) (int, error) {
	if p < 0 || int(p) >= len(e.book.plans) {
		return 0, fmt.Errorf("teamai: plan %d out of range", p)
	}
	if team < 0 || team >= len(e.teams) || team >= len(w.Teams) {
		return 0, fmt.Errorf("teamai: team %d out of range", team)
	}
	if len(w.Agents) != len(e.agents) {
		return 0, fmt.Errorf("teamai: world has %d agents, executor %d", len(w.Agents), len(e.agents))
	}
	plan := &e.book.plans[p]
	f := &e.book.formations[plan.formation]
	e.teams[team].formation = plan.formation
	e.teams[team].resolved = false
	fr := &w.Teams[team].Frame
	slotOf := e.slotOf[:f.NumSlots()]
	empty := AssignByRole(slotOf, f, w.Agents, team, e.used)
	for i := range w.Agents {
		if w.Agents[i].Team == team {
			e.clear(i)
		}
	}
	for s, a := range slotOf {
		if a < 0 {
			continue
		}
		st := &e.agents[a]
		st.slot = int16(s)
		st.sign = f.SlotSign(fr, s)
		e.start(a, plan.slotAssn[s])
	}
	for i := range w.Agents {
		if w.Agents[i].Team == team && e.agents[i].slot < 0 && w.Agents[i].Active() {
			e.start(i, plan.extra)
		}
	}
	return empty, nil
}

// Assign starts assignment a for agent i, keeping its slot and side. Pass
// NoAssignment to stop it.
func (e *Executor) Assign(i int, a AssignmentID) {
	slot, sign, assigned, mem := e.agents[i].slot, e.agents[i].sign, e.agents[i].assigned, e.agents[i].mem
	e.agents[i] = idleState()
	e.agents[i].slot, e.agents[i].sign, e.agents[i].assigned, e.agents[i].mem = slot, sign, assigned, mem
	e.start(i, a)
}

// SetSlot gives agent i slot s of its team's formation, signed by side
// (+1 or -1), for slot steps. Use it when the host assigns slots itself.
func (e *Executor) SetSlot(i, s int, side float64) {
	e.agents[i].slot = int16(s)
	if side < 0 {
		e.agents[i].sign = -1
	} else {
		e.agents[i].sign = 1
	}
}

func (e *Executor) clear(i int) {
	assigned, mem := e.agents[i].assigned, e.agents[i].mem
	e.agents[i] = idleState()
	e.agents[i].assigned, e.agents[i].mem = assigned, mem
}

func (e *Executor) start(i int, a AssignmentID) {
	st := &e.agents[i]
	if a < 0 || int(a) >= len(e.book.assignments) {
		st.assignment, st.step = -1, -1
		return
	}
	st.assignment = int16(a)
	st.step = 0
	st.begun = false
	st.stepTicks, st.assignTicks = 0, 0
	st.marks, st.fired = 0, 0
	st.pending, st.pendingTicks = -1, 0
	st.contact = false
}

// SetTarget sets the target that the "assigned" selector returns for agent i,
// such as an opponent picked by the host.
func (e *Executor) SetTarget(i int, r Ref) { e.agents[i].assigned = r }

// SetMemory sets agent memory slot k of agent i. Rules read it as mem.<k>.
func (e *Executor) SetMemory(i, k int, v float64) { e.agents[i].mem[k] = v }

// AgentInfo describes what an agent is doing.
type AgentInfo struct {
	Assignment AssignmentID
	Step       int
	Kind       StepKind
	Slot       int
	Side       float64
	Target     Ref
	Contact    bool
	Marks      uint32
}

// Agent describes agent i.
func (e *Executor) Agent(i int) AgentInfo {
	st := &e.agents[i]
	info := AgentInfo{Assignment: AssignmentID(st.assignment), Step: int(st.step), Slot: int(st.slot),
		Side: st.sign, Target: st.target, Contact: st.contact, Marks: st.marks}
	if st.assignment >= 0 && st.step >= 0 {
		info.Kind = e.book.assignments[st.assignment].steps[st.step].kind
	}
	return info
}

// Events returns the events of the last Step. The slice is reused by the
// next Step.
func (e *Executor) Events() []Event { return e.events }

func (e *Executor) emit(agent int, kind EventKind, value int, target Ref) {
	e.events = append(e.events, Event{Agent: agent, Kind: kind, Value: value, Target: target})
}

// frameOf returns the frame of team t, or a default frame when t is out of
// range.
func (e *Executor) frameOf(w *World, t int) *Frame {
	if t >= 0 && t < len(w.Teams) {
		return &w.Teams[t].Frame
	}
	return &defaultFrame
}

var defaultFrame = Frame{Band: -1}

// slotPos returns the world position of slot s of team t's formation this
// tick, resolving the formation at most once per tick.
func (e *Executor) slotPos(w *World, t int, s int16) (Vec2, bool) {
	if t < 0 || t >= len(e.teams) || s < 0 {
		return Vec2{}, false
	}
	ts := &e.teams[t]
	f := e.book.Formation(ts.formation)
	if f == nil || int(s) >= f.NumSlots() {
		return Vec2{}, false
	}
	if !ts.resolved || ts.slotsTick != w.Tick {
		ts.slots = f.Resolve(e.frameOf(w, t), w.Field, ts.slots[:0])
		ts.slotsTick = w.Tick
		ts.resolved = true
	}
	return ts.slots[s], true
}

// Step runs one tick for every agent and returns their commands, indexed like
// w.Agents. The slice is reused by the next Step. Step does not allocate once
// the event buffer has grown to the busiest tick.
func (e *Executor) Step(w *World) []Command {
	e.events = e.events[:0]
	if len(w.Agents) != len(e.agents) {
		panic(fmt.Sprintf("teamai: world has %d agents, executor was Reset for %d", len(w.Agents), len(e.agents)))
	}
	for t := range e.teams {
		e.teams[t].resolved = false
	}
	for i := range e.agents {
		e.cmds[i] = e.stepAgent(w, i)
	}
	return e.cmds
}

func (e *Executor) stepAgent(w *World, i int) Command {
	st := &e.agents[i]
	b := &w.Agents[i]
	q := &e.q
	q.reset(e, w, i)
	if st.assignment < 0 || !b.Active() {
		return Command{Face: q.frame.Fwd()}
	}
	a := &e.book.assignments[st.assignment]
	e.interrupts(q, a)
	cmd := Command{}
	for n := 0; ; n++ {
		if st.step < 0 || int(st.step) >= len(a.steps) {
			e.emit(i, EventDone, int(st.assignment), Ref{})
			st.assignment, st.step = -1, -1
			return Command{Face: q.frame.Fwd()}
		}
		if n == maxTransitions {
			break
		}
		s := &a.steps[st.step]
		if !st.begun {
			e.beginStep(q, s)
		}
		if !s.latch {
			q.target = e.selectTarget(q, s.target)
			st.target = q.target
		} else {
			q.target = st.target
		}
		if s.until >= 0 && e.book.preds[s.until].Test(q) {
			e.finishStep(q, s, s.next)
			continue
		}
		if s.duration > 0 && elapsed(st.stepTicks, e.dt) >= s.duration {
			e.finishStep(q, s, s.next)
			continue
		}
		var natural bool
		var jump int16
		cmd, natural, jump = e.act(q, s)
		if jump != noJump {
			e.finishStep(q, s, jump)
			continue
		}
		if natural && s.until < 0 && s.kind != StepHold {
			e.finishStep(q, s, s.next)
			continue
		}
		break
	}
	st.stepTicks++
	st.assignTicks++
	if s := &a.steps[st.step]; s.kind == StepPath {
		st.path.ticks++
		st.path.segTicks++
	}
	return e.finishCommand(q, cmd)
}

const noJump int16 = -2

func (e *Executor) beginStep(q *Query, s *step) {
	st := q.st
	st.begun = true
	st.stepTicks = 0
	st.point = q.body.Pos
	if s.latch {
		st.target = e.selectTarget(q, s.target)
	}
	if s.kind == StepPath {
		st.path.beginPath(q.body.Pos, q.frame, st.sign)
	}
	e.emit(q.self, EventStep, int(st.step), st.target)
}

func (e *Executor) finishStep(q *Query, s *step, next int16) {
	st := q.st
	if s.mark >= 0 {
		q.marked(s.mark)
	}
	st.step = next
	st.begun = false
	st.stepTicks = 0
	st.contact = false
}

// interrupts tests the assignment's interrupts and jumps when one fires.
func (e *Executor) interrupts(q *Query, a *Assignment) {
	st := q.st
	for k := range a.interrupts {
		in := &a.interrupts[k]
		bit := uint16(1) << uint(k)
		if in.rule < 0 || in.target < 0 {
			continue
		}
		if !e.book.preds[in.rule].Test(q) {
			st.fired &^= bit
			if int(st.pending) == k {
				st.pending, st.pendingTicks = -1, 0
			}
			continue
		}
		if st.fired&bit != 0 {
			continue
		}
		if st.pending < 0 {
			st.pending, st.pendingTicks = int8(k), 0
		}
		if int(st.pending) != k {
			continue
		}
		if st.pendingTicks < q.body.Reaction {
			st.pendingTicks++
			continue
		}
		st.fired |= bit
		st.pending, st.pendingTicks = -1, 0
		e.emit(q.self, EventInterrupt, k, Ref{})
		st.step = in.target
		st.begun = false
		st.stepTicks = 0
		st.contact = false
		return
	}
}

func (e *Executor) selectTarget(q *Query, sel selector) Ref {
	switch sel.kind {
	case selNearestOpp:
		if j := q.NearestOpponent(); j >= 0 {
			return Agent(j)
		}
	case selNearestMate:
		if j := q.NearestTeammate(); j >= 0 {
			return Agent(j)
		}
	case selOppRole:
		if j := q.nearest(RoleID(sel.index), false); j >= 0 {
			return Agent(j)
		}
	case selMateRole:
		if j := q.nearest(RoleID(sel.index), true); j >= 0 {
			return Agent(j)
		}
	case selObject:
		return Object(int(sel.index))
	case selLandmark:
		return Ref{Kind: RefLandmark, Index: sel.index}
	case selAssigned:
		return q.st.assigned
	}
	return Ref{}
}

// act runs step s for one tick. natural reports the step's natural end; jump
// is a step to jump to (decide), or noJump.
func (e *Executor) act(q *Query, s *step) (cmd Command, natural bool, jump int16) {
	b := q.body
	st := q.st
	speed := float64(s.speed * b.MaxSpeed)
	jump = noJump
	switch s.kind {
	case StepSlot, StepMove:
		var p Vec2
		ok := true
		switch {
		case s.kind == StepSlot:
			p, ok = e.slotPos(q.w, b.Team, st.slot)
		case s.slot >= 0:
			f := e.book.Formation(e.teams[b.Team].formation)
			ok = f != nil
			if ok {
				p, ok = e.slotPos(q.w, b.Team, f.nameSlot[s.slot])
			}
		case s.landmark >= 0:
			p = q.landmarkPos(s.landmark)
		default:
			p = st.point
		}
		if !ok {
			// No slot this tick: hold where the step started.
			p = st.point
		}
		p = p.Add(q.frame.Rotate(Vec2{X: float64(s.offset.X * st.sign), Y: s.offset.Y}))
		cmd.Vel = Arrive(b.Pos, p, speed, b.MaxAccel, e.tol)
		natural = b.Pos.Dist(p) <= e.tol
	case StepPath:
		out := e.book.paths[s.path].step(&st.path, b, q, e.dt, e.tol)
		cmd.Vel = out.vel.ClampLen(speed)
		natural = out.done
	case StepHold:
		cmd.Vel = Arrive(b.Pos, st.point, b.MaxSpeed, b.MaxAccel, e.tol)
	case StepMirror:
		tpos, tvel, ok := q.RefBody(q.target)
		if !ok {
			cmd.Vel = Arrive(b.Pos, st.point, speed, b.MaxAccel, e.tol)
			break
		}
		off := q.frame.Rotate(Vec2{X: float64(s.offset.X * st.sign), Y: s.offset.Y})
		cmd.Vel = Shadow(b.Pos, tpos, tvel, off, speed, e.opt.ShadowGain)
		cmd.Face = tpos.Sub(b.Pos).Normalize()
		st.point = b.Pos
	case StepGuard:
		zone := q.landmarkPos(s.landmark).Add(q.frame.Rotate(Vec2{X: float64(s.offset.X * st.sign), Y: s.offset.Y}))
		protect := zone
		if s.protect >= 0 {
			protect = q.landmarkPos(s.protect)
		}
		p := zone
		if tpos, _, ok := q.RefBody(q.target); ok {
			var engaged bool
			if p, engaged = GuardPoint(zone, s.radius, tpos, protect, s.gap); engaged {
				cmd.Face = tpos.Sub(b.Pos).Normalize()
			}
		}
		cmd.Vel = Arrive(b.Pos, p, speed, b.MaxAccel, e.tol)
	case StepPursue, StepCutoff, StepEngage:
		tpos, tvel, ok := q.RefBody(q.target)
		if !ok {
			cmd.Vel = Arrive(b.Pos, st.point, speed, b.MaxAccel, e.tol)
			break
		}
		st.point = b.Pos
		dist := tpos.Dist(b.Pos)
		if s.radius > 0 && dist <= s.radius {
			if s.kind != StepEngage {
				natural = true
				break
			}
			if !st.contact {
				st.contact = true
				e.emit(q.self, EventContact, int(q.target.Index), q.target)
			}
		}
		if s.kind == StepEngage && st.contact {
			// Stay at the contact distance on the side the agent is on.
			away := b.Pos.Sub(tpos).Normalize().Scale(float64(s.radius * 0.9))
			cmd.Vel = Shadow(b.Pos, tpos, tvel, away, speed, e.opt.ShadowGain)
			cmd.Face = tpos.Sub(b.Pos).Normalize()
			break
		}
		var in Interception
		if s.kind == StepCutoff {
			in = Cutoff(b.Pos, b.Vel.Len(), speed, b.MaxAccel, tpos, tvel, q.w.Field, s.margin, e.opt.Horizon)
			cmd.Vel = in.Dir.Scale(speed)
			if in.Dir.IsZero() {
				cmd.Vel = Seek(b.Pos, tpos, speed)
			}
		} else {
			in = Intercept(b.Pos, b.Vel.Len(), speed, b.MaxAccel, tpos, tvel, e.opt.Horizon)
			if in.OK && !in.Dir.IsZero() {
				cmd.Vel = in.Dir.Scale(speed)
			} else {
				cmd.Vel = Seek(b.Pos, tpos, speed)
			}
		}
	case StepDecide:
		switch k := q.decide(&s.decision, st.stepTicks); {
		case k >= 0:
			return cmd, false, s.decision.branches[k].target
		case k == decideExpired:
			return cmd, false, s.decision.otherwise
		}
		cmd.Vel = Arrive(b.Pos, st.point, b.MaxSpeed, b.MaxAccel, e.tol)
	}
	return cmd, natural, jump
}

// finishCommand adds separation, limits the speed and sets the facing.
func (e *Executor) finishCommand(q *Query, cmd Command) Command {
	b := q.body
	if e.opt.Separation > 0 {
		push := Separation(b.Pos, q.self, q.w.Agents, e.opt.Separation, b.Team)
		cmd.Vel = cmd.Vel.AddScaled(push, float64(e.opt.SeparationWeight*b.MaxSpeed))
	}
	cmd.Vel = cmd.Vel.ClampLen(b.MaxSpeed)
	if cmd.Face.IsZero() {
		cmd.Face = cmd.Vel.Normalize()
	}
	if cmd.Face.IsZero() {
		cmd.Face = q.frame.Fwd()
	}
	return cmd
}

// Results of Query.decide besides a branch index.
const (
	decideWait    = -1
	decideExpired = -2
)

// decide picks a branch of d, decideWait, or decideExpired. The agent first
// tests the decision Reaction ticks after reaching it, then every tick for
// Window seconds; a decision with no window is tested once. It is the pathEnv
// method too, so path decisions and decide steps share it.
func (q *Query) decide(d *Decision, waited uint32) int {
	reaction := uint32(q.body.Reaction)
	if waited < reaction {
		return decideWait
	}
	k := decideWait
	switch {
	case d.chooser >= 0:
		k = q.ex.book.choosers[d.chooser].Choose(q, d)
		if k < 0 || k >= len(d.branches) {
			k = decideWait
		}
	case !d.weighted:
		for i := range d.branches {
			if br := &d.branches[i]; br.rule < 0 || q.ex.book.preds[br.rule].Test(q) {
				k = i
				break
			}
		}
	default:
		var held uint32
		total := 0.0
		for i := range d.branches {
			if br := &d.branches[i]; br.rule < 0 || q.ex.book.preds[br.rule].Test(q) {
				held |= 1 << uint(i)
				total += br.weight
			}
		}
		if total > 0 {
			u := float64(Unit(q.Rand(uint64(d.key))) * total)
			for i := range d.branches {
				if held&(1<<uint(i)) == 0 {
					continue
				}
				k = i
				if u < d.branches[i].weight {
					break
				}
				u -= d.branches[i].weight
			}
		}
	}
	if k >= 0 {
		q.ex.emit(q.self, EventBranch, k, q.target)
		return k
	}
	if elapsed(waited-reaction, q.ex.dt) >= d.window {
		return decideExpired
	}
	return decideWait
}

// marked records a mark. It is the pathEnv method too.
func (q *Query) marked(m int16) {
	q.st.marks |= 1 << uint(m)
	q.ex.emit(q.self, EventMark, int(m), Ref{})
}
