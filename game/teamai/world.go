package teamai

// RoleID indexes Spec.Roles. NoRole is -1.
type RoleID int16

// NoRole marks an agent or slot without a role.
const NoRole RoleID = -1

// Body flags.
const (
	// FlagInactive takes an agent out of play: the executor gives it a zero
	// command, nearest-agent searches skip it, and slot assignment ignores it.
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
	// Reaction is the number of ticks a decision branch or interrupt must
	// hold before the agent acts on it. Use it to scale difficulty.
	Reaction uint16
	// Flags holds FlagInactive and host bits from 1<<8 up.
	Flags uint32
}

// Active reports whether the body is in play.
func (b *Body) Active() bool { return b.Flags&FlagInactive == 0 }
