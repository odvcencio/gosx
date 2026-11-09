package teamai

import (
	"fmt"
	"strconv"
	"strings"

	"m31labs.dev/gosx/det"
)

// MemorySlots is the number of per-agent memory values (see
// Executor.SetMemory). They are part of the executor's snapshot.
const MemorySlots = 4

// Fact is a compiled, named number a rule can read from a Query, such as
// "nearest_opponent.dist" or "signal.clock". Get one from [Book.ParseFact].
// A fact whose subject is missing (no target, no opponent, an inactive object)
// reads as NaN, and every comparison with NaN is false.
type Fact struct {
	subj  uint8
	prop  uint8
	index int16
}

// Fact subjects.
const (
	subjNone uint8 = iota
	subjSelf
	subjTarget
	subjNearestOpp
	subjNearestMate
	subjObject
	subjLandmark
	subjSignal
	subjTeam
	subjMark
	subjMem
	subjTick
)

// Fact properties of a body subject.
const (
	propX uint8 = iota + 1
	propY
	propDX
	propDY
	propVX
	propVY
	propDist
	propSpeed
	propClosing
	propExists
	propStepTime
	propAssignTime
	propPathTime
	propContact
	propAgentSignal // index is the agent signal
)

var bodyProps = []struct {
	name string
	prop uint8
}{
	{"x", propX}, {"y", propY}, {"dx", propDX}, {"dy", propDY},
	{"vx", propVX}, {"vy", propVY}, {"dist", propDist}, {"speed", propSpeed},
	{"closing", propClosing}, {"exists", propExists},
}

var selfProps = []struct {
	name string
	prop uint8
}{
	{"step_time", propStepTime}, {"assign_time", propAssignTime},
	{"path_time", propPathTime}, {"contact", propContact},
}

// ParseFact compiles a fact name. The names are:
//
//	tick                         the world tick
//	signal.<name>                a host signal
//	team.<name>                  a team signal of the agent's team
//	mark.<name>                  1 once the agent has reached the mark in its
//	                             current assignment, else 0
//	mem.<0-3>                    agent memory
//	<subject>.<property>         a body fact
//
// Subjects are self, target, nearest_opponent, nearest_teammate,
// object.<name> and landmark.<name>. Properties are x and y (the subject's
// position in the agent's team frame, lateral signed by the agent's side),
// dx and dy (the same, relative to the agent), vx and vy, dist, speed, closing
// (the speed at which the subject and the agent approach each other), exists
// (1 or 0), and the agent signals for agent subjects. self also has
// step_time, assign_time and path_time in seconds, and contact (1 after an
// engage step made contact).
func (b *Book) ParseFact(name string) (Fact, error) {
	head, rest, _ := strings.Cut(name, ".")
	switch head {
	case "tick":
		if rest != "" {
			break
		}
		return Fact{subj: subjTick}, nil
	case "signal", "team", "mark":
		table, subj := b.signals, subjSignal
		switch head {
		case "team":
			table, subj = b.teamSignals, subjTeam
		case "mark":
			table, subj = b.marks, subjMark
		}
		i := indexOf(table, rest)
		if i < 0 {
			return Fact{}, fmt.Errorf("fact %q: unknown %s %q", name, head, rest)
		}
		return Fact{subj: subj, index: int16(i)}, nil
	case "mem":
		i, err := strconv.Atoi(rest)
		if err != nil || i < 0 || i >= MemorySlots {
			return Fact{}, fmt.Errorf("fact %q: memory slots are mem.0 to mem.%d", name, MemorySlots-1)
		}
		return Fact{subj: subjMem, index: int16(i)}, nil
	case "self":
		return b.bodyFact(name, subjSelf, 0, rest, true)
	case "target":
		return b.bodyFact(name, subjTarget, 0, rest, true)
	case "nearest_opponent":
		return b.bodyFact(name, subjNearestOpp, 0, rest, true)
	case "nearest_teammate":
		return b.bodyFact(name, subjNearestMate, 0, rest, true)
	case "object", "landmark":
		which, prop, ok := strings.Cut(rest, ".")
		if !ok {
			break
		}
		if head == "object" {
			i := indexOf(b.objects, which)
			if i < 0 {
				return Fact{}, fmt.Errorf("fact %q: unknown object %q", name, which)
			}
			return b.bodyFact(name, subjObject, int16(i), prop, false)
		}
		id, found := b.Landmark(which)
		if !found {
			return Fact{}, fmt.Errorf("fact %q: unknown landmark %q", name, which)
		}
		return b.bodyFact(name, subjLandmark, int16(id), prop, false)
	}
	return Fact{}, fmt.Errorf("fact %q: want tick, signal.<name>, team.<name>, mark.<name>, mem.<n>, or a subject (self, target, nearest_opponent, nearest_teammate, object.<name>, landmark.<name>) and a property", name)
}

func (b *Book) bodyFact(name string, subj uint8, index int16, prop string, agent bool) (Fact, error) {
	for _, p := range bodyProps {
		if p.name == prop {
			return Fact{subj: subj, prop: p.prop, index: index}, nil
		}
	}
	if subj == subjSelf {
		for _, p := range selfProps {
			if p.name == prop {
				return Fact{subj: subj, prop: p.prop}, nil
			}
		}
	}
	if agent {
		if i := indexOf(b.agentSignals, prop); i >= 0 {
			return Fact{subj: subj, prop: propAgentSignal, index: int16(i)}, nil
		}
	}
	return Fact{}, fmt.Errorf("fact %q: unknown property %q", name, prop)
}

// Query is the read-only view of the world from one agent during one tick.
// Predicates and Choosers receive it. It is valid only during the call that
// receives it; do not keep it.
type Query struct {
	ex     *Executor
	w      *World
	self   int
	st     *agentState
	body   *Body
	frame  *Frame
	target Ref
	// nearest caches, -2 until computed
	nearOpp, nearMate int
}

func (q *Query) reset(ex *Executor, w *World, self int) {
	q.ex = ex
	q.w = w
	q.self = self
	q.st = &ex.agents[self]
	q.body = &w.Agents[self]
	q.frame = ex.frameOf(w, q.body.Team)
	q.target = Ref{}
	q.nearOpp, q.nearMate = -2, -2
}

// World returns the world view.
func (q *Query) World() *World { return q.w }

// Book returns the executor's Book.
func (q *Query) Book() *Book { return q.ex.book }

// Self returns the agent's index.
func (q *Query) Self() int { return q.self }

// Body returns the agent's body.
func (q *Query) Body() *Body { return q.body }

// Frame returns the agent's team frame.
func (q *Query) Frame() *Frame { return q.frame }

// Side returns the agent's lateral sign, +1 or -1, from its slot.
func (q *Query) Side() float64 { return q.st.sign }

// Target returns the current step's target.
func (q *Query) Target() Ref { return q.target }

// Assignment returns the agent's assignment and step index.
func (q *Query) Assignment() (AssignmentID, int) {
	return AssignmentID(q.st.assignment), int(q.st.step)
}

// StepTime returns the seconds spent in the current step.
func (q *Query) StepTime() float64 { return elapsed(q.st.stepTicks, q.ex.dt) }

// Marked reports whether the agent reached mark i in its assignment.
func (q *Query) Marked(i int) bool { return i >= 0 && i < 32 && q.st.marks&(1<<uint(i)) != 0 }

// Memory returns agent memory slot i.
func (q *Query) Memory(i int) float64 { return q.st.mem[i] }

// Rand returns 64 random bits keyed by the world seed and tick, the agent,
// and key. The same arguments give the same bits everywhere.
func (q *Query) Rand(key uint64) uint64 {
	return Mix(q.w.Seed, q.w.Tick, uint64(q.self), key)
}

// NearestOpponent returns the index of the nearest active agent of another
// team, or -1. Ties go to the lower index.
func (q *Query) NearestOpponent() int {
	if q.nearOpp == -2 {
		q.nearOpp = q.nearest(-1, false)
	}
	return q.nearOpp
}

// NearestTeammate returns the index of the nearest other active agent of the
// agent's team, or -1.
func (q *Query) NearestTeammate() int {
	if q.nearMate == -2 {
		q.nearMate = q.nearest(-1, true)
	}
	return q.nearMate
}

// nearest finds the nearest active agent of the agent's team (mates) or of
// another team, with role (or any role when role < 0).
func (q *Query) nearest(role RoleID, mates bool) int {
	best, bestD := -1, 0.0
	agents := q.w.Agents
	for j := range agents {
		o := &agents[j]
		if j == q.self || !o.Active() || (o.Team == q.body.Team) != mates || (role >= 0 && o.Role != role) {
			continue
		}
		d := o.Pos.Sub(q.body.Pos).Len2()
		if best < 0 || d < bestD {
			best, bestD = j, d
		}
	}
	return best
}

// RefBody returns the position and velocity r points at, and false when r
// points at nothing, at an inactive body, or out of range.
func (q *Query) RefBody(r Ref) (pos, vel Vec2, ok bool) {
	switch r.Kind {
	case RefAgent:
		if int(r.Index) < len(q.w.Agents) && r.Index >= 0 {
			b := &q.w.Agents[r.Index]
			return b.Pos, b.Vel, b.Active()
		}
	case RefObject:
		if int(r.Index) < len(q.w.Objects) && r.Index >= 0 {
			b := &q.w.Objects[r.Index]
			return b.Pos, b.Vel, b.Active()
		}
	case RefLandmark:
		if int(r.Index) < len(q.ex.book.landmarks) && r.Index >= 0 {
			return q.landmarkPos(LandmarkID(r.Index)), Vec2{}, true
		}
	}
	return Vec2{}, Vec2{}, false
}

func (q *Query) landmarkPos(id LandmarkID) Vec2 {
	return q.ex.book.LandmarkPos(id, q.frame, q.w.Landmarks)
}

// localSigned converts a world offset to the agent's (lateral, depth) axes,
// lateral signed by its side.
func (q *Query) localSigned(d Vec2) Vec2 {
	l := Vec2{X: d.Dot(q.frame.Right()), Y: d.Dot(q.frame.Fwd())}
	l.X = float64(l.X * q.st.sign)
	return l
}

// Fact returns the value of f for this agent this tick.
func (q *Query) Fact(f Fact) float64 {
	nan := det.NaN()
	switch f.subj {
	case subjTick:
		return float64(q.w.Tick)
	case subjSignal:
		if int(f.index) < len(q.w.Signals) {
			return q.w.Signals[f.index]
		}
		return nan
	case subjTeam:
		n := len(q.ex.book.teamSignals)
		if i := q.body.Team*n + int(f.index); q.body.Team >= 0 && i < len(q.w.TeamSignals) {
			return q.w.TeamSignals[i]
		}
		return nan
	case subjMark:
		if q.Marked(int(f.index)) {
			return 1
		}
		return 0
	case subjMem:
		return q.st.mem[f.index]
	}
	if f.subj == subjSelf {
		switch f.prop {
		case propStepTime:
			return elapsed(q.st.stepTicks, q.ex.dt)
		case propAssignTime:
			return elapsed(q.st.assignTicks, q.ex.dt)
		case propPathTime:
			return elapsed(q.st.path.ticks, q.ex.dt)
		case propContact:
			if q.st.contact {
				return 1
			}
			return 0
		}
	}
	var ref Ref
	switch f.subj {
	case subjSelf:
		ref = Agent(q.self)
	case subjTarget:
		ref = q.target
	case subjNearestOpp:
		if i := q.NearestOpponent(); i >= 0 {
			ref = Agent(i)
		}
	case subjNearestMate:
		if i := q.NearestTeammate(); i >= 0 {
			ref = Agent(i)
		}
	case subjObject:
		ref = Object(int(f.index))
	case subjLandmark:
		ref = Ref{Kind: RefLandmark, Index: f.index}
	}
	pos, vel, ok := q.RefBody(ref)
	if f.prop == propExists {
		if ok {
			return 1
		}
		return 0
	}
	if !ok {
		return nan
	}
	switch f.prop {
	case propX:
		return q.localSigned(pos.Sub(q.frame.Origin)).X
	case propY:
		return q.localSigned(pos.Sub(q.frame.Origin)).Y
	case propDX:
		return q.localSigned(pos.Sub(q.body.Pos)).X
	case propDY:
		return q.localSigned(pos.Sub(q.body.Pos)).Y
	case propVX:
		return q.localSigned(vel).X
	case propVY:
		return q.localSigned(vel).Y
	case propDist:
		return pos.Dist(q.body.Pos)
	case propSpeed:
		return vel.Len()
	case propClosing:
		rel := pos.Sub(q.body.Pos)
		d := rel.Len()
		if d <= epsilon {
			return 0
		}
		return -rel.Dot(vel.Sub(q.body.Vel)) / d
	case propAgentSignal:
		if ref.Kind != RefAgent {
			return nan
		}
		n := len(q.ex.book.agentSignals)
		if i := int(ref.Index)*n + int(f.index); i < len(q.w.AgentSignals) {
			return q.w.AgentSignals[i]
		}
	}
	return nan
}
