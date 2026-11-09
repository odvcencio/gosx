package teamai

// RoleID indexes Spec.Roles. NoRole is -1.
type RoleID int16

// NoRole marks an agent or slot without a role.
const NoRole RoleID = -1

// Body flags.
const (
	// FlagInactive takes an agent or object out of play: the executor gives
	// an inactive agent a zero command, target selection skips it, and slot
	// assignment ignores it.
	FlagInactive uint32 = 1 << iota
)

// Body is the host's per-tick kinematic state of one agent or tracked object.
// The host owns it; the toolkit only reads it.
type Body struct {
	Pos Vec2
	Vel Vec2
	// Team is the agent's team index into World.Teams.
	Team int
	// Role indexes Spec.Roles.
	Role RoleID
	// MaxSpeed is the top speed in units per second. Path and step speeds
	// are fractions of it.
	MaxSpeed float64
	// MaxAccel is the acceleration limit in units per second squared, used
	// for arrival and interception. Zero means instant.
	MaxAccel float64
	// Reaction is the number of ticks a decision or interrupt waits before
	// the agent acts on it. Use it to scale difficulty.
	Reaction uint16
	// Flags holds FlagInactive and host bits from 1<<8 up.
	Flags uint32
}

// Active reports whether the body is in play.
func (b *Body) Active() bool { return b.Flags&FlagInactive == 0 }

// Team is the per-tick view of one team.
type Team struct {
	// Frame is the team's reference frame. Formation slots, local landmarks
	// and paths use it.
	Frame Frame
}

// World is the read-only view the executor reads each tick. The host fills it
// from its simulation state before calling Executor.Step and must not change
// it during the call. Every slice is indexed by an ID from the Book.
type World struct {
	// Tick is the simulation tick. Weighted decisions hash it.
	Tick uint64
	// Seed is the match seed. Weighted decisions hash it.
	Seed uint64
	// Field bounds edge slots, clamped slots and cutoffs. A zero Rect means
	// no bounds.
	Field Rect
	// Teams are indexed by Body.Team.
	Teams []Team
	// Agents are the bodies the executor drives, indexed by agent.
	Agents []Body
	// Objects are tracked bodies such as a ball or a flag, indexed by the
	// order of Spec.Objects.
	Objects []Body
	// Landmarks holds the positions of host landmarks, indexed by
	// LandmarkID. Entries for local landmarks are ignored.
	Landmarks []Vec2
	// Signals are host scalars, indexed by the order of Spec.Signals.
	Signals []float64
	// TeamSignals are per-team host scalars: team t's value of team signal
	// i is TeamSignals[t*len(Spec.TeamSignals)+i].
	TeamSignals []float64
	// AgentSignals are per-agent host scalars: agent a's value of agent
	// signal i is AgentSignals[a*len(Spec.AgentSignals)+i].
	AgentSignals []float64
}

// RefKind is the kind of thing a Ref points at.
type RefKind uint8

const (
	// RefNone points at nothing.
	RefNone RefKind = iota
	// RefAgent points at World.Agents[Index].
	RefAgent
	// RefObject points at World.Objects[Index].
	RefObject
	// RefLandmark points at landmark Index.
	RefLandmark
)

// Ref points at an agent, an object or a landmark: a step's target.
type Ref struct {
	Kind  RefKind
	Index int16
}

// Agent returns a Ref to agent i.
func Agent(i int) Ref { return Ref{Kind: RefAgent, Index: int16(i)} }

// Object returns a Ref to object i.
func Object(i int) Ref { return Ref{Kind: RefObject, Index: int16(i)} }

// Valid reports whether r points at something.
func (r Ref) Valid() bool { return r.Kind != RefNone }
