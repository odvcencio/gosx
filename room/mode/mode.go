// Package mode describes a playable game mode for package room (server) and
// package game/netsync (client): how many seats it has, how to build its
// simulation, and how a bot plays a seat. Put a game's modes in a package that
// both its server and its js/wasm client import, so an offline client builds
// exactly the match the server would.
//
// The package holds no game rules. A Mode is configuration plus two
// constructors the game supplies.
package mode

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	// C1: sim imports hub (and net/http) today. game/netsync reaches sim only
	// through this package and through game. When the C1 import diet moves
	// sim.Simulation and sim.Input to a hub-free package, switch this import.
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// Defaults applied by Normalize.
const (
	DefaultTickRate  = 30
	DefaultCountdown = 3 * time.Second
	DefaultInputSize = 16
)

// Mode is one playable configuration of a game.
type Mode struct {
	// Name identifies the mode in join requests ("duel", "ffa"). Required.
	Name string
	// Seats is the number of players, 1 to wire.MaxSeats. Every seat is
	// always played, by a person or by a bot.
	Seats int
	// Roles names each seat's role ("red", "blue"). Empty, or one entry per
	// seat.
	Roles []string
	// TickRate is the fixed simulation rate in ticks per second. Zero selects
	// DefaultTickRate. The simulation must advance exactly one step per Tick.
	TickRate int
	// NewSim builds a fresh simulation for a match from seed. Required. The
	// server and every client call it with the room's seed, so it must be
	// deterministic: the same seed gives the same simulation.
	NewSim func(seed uint64) sim.Simulation
	// NewBot builds the bot that plays seat when no person does. Nil selects
	// IdleBot, which holds no input.
	NewBot func(seat int, role string) Bot
	// LobbyWait is how long a room waits after its first person sits down
	// before the countdown starts, to let others join. Zero starts the
	// countdown at once, so a person who arrives alone plays at once.
	LobbyWait time.Duration
	// Countdown runs between the lobby and the match. Zero selects
	// DefaultCountdown; a negative value skips it.
	Countdown time.Duration
	// Duration ends a running match after this much play. Zero means the match
	// runs until Over reports true.
	Duration time.Duration
	// Over, when set, ends the match at the first tick it reports true.
	Over func(s sim.Simulation, tick uint64) bool
	// Results, when set, encodes the match result for the results phase.
	Results func(s sim.Simulation) []byte
	// InputSize is the largest input one seat sends per tick, in bytes. Zero
	// selects DefaultInputSize; the most is wire.MaxInputSize.
	InputSize int
	// NoHotJoin keeps people out of a running match. By default a person who
	// joins a running room takes over a seat a bot was filling.
	NoHotJoin bool
}

// Bot produces one seat's input for each tick. A room calls it on the server
// for every seat no person plays; netsync calls it on the client when it plays
// offline. Inputs are held state (which buttons are down this tick), not
// edges, so repeating the last input is always a valid prediction.
type Bot interface {
	// Input appends this tick's input for ctx.Seat to dst and returns it. dst
	// has capacity for the mode's InputSize; a longer input is cut.
	Input(ctx BotContext, dst []byte) []byte
}

// BotContext is what a bot sees. Sim is the simulation after the previous
// tick. A bot must only read it. Seed is fixed for the match.
type BotContext struct {
	Tick uint64
	Seat int
	Role string
	Seed uint64
	Sim  sim.Simulation
}

// BotFunc adapts a function into a Bot.
type BotFunc func(ctx BotContext, dst []byte) []byte

// Input calls f.
func (f BotFunc) Input(ctx BotContext, dst []byte) []byte { return f(ctx, dst) }

// IdleBot holds no input.
var IdleBot Bot = BotFunc(func(_ BotContext, dst []byte) []byte { return dst })

// ErrInvalid reports a Mode that cannot run.
var ErrInvalid = errors.New("mode: invalid mode")

// Normalize returns m with defaults filled in, or an error wrapping ErrInvalid.
func (m Mode) Normalize() (Mode, error) {
	switch {
	case m.Name == "" || len(m.Name) > wire.MaxText:
		return m, fmt.Errorf("%w: name must be 1 to %d bytes", ErrInvalid, wire.MaxText)
	case m.Seats < 1 || m.Seats > wire.MaxSeats:
		return m, fmt.Errorf("%w %q: seats must be 1 to %d", ErrInvalid, m.Name, wire.MaxSeats)
	case len(m.Roles) != 0 && len(m.Roles) != m.Seats:
		return m, fmt.Errorf("%w %q: %d roles for %d seats", ErrInvalid, m.Name, len(m.Roles), m.Seats)
	case m.NewSim == nil:
		return m, fmt.Errorf("%w %q: NewSim is required", ErrInvalid, m.Name)
	case m.TickRate < 0 || m.TickRate > 240:
		return m, fmt.Errorf("%w %q: tick rate must be 1 to 240", ErrInvalid, m.Name)
	case m.InputSize < 0 || m.InputSize > wire.MaxInputSize:
		return m, fmt.Errorf("%w %q: input size must be at most %d", ErrInvalid, m.Name, wire.MaxInputSize)
	}
	if m.TickRate == 0 {
		m.TickRate = DefaultTickRate
	}
	if m.Countdown == 0 {
		m.Countdown = DefaultCountdown
	}
	if m.Countdown < 0 {
		m.Countdown = 0
	}
	if m.InputSize == 0 {
		m.InputSize = DefaultInputSize
	}
	if m.Roles != nil {
		m.Roles = append([]string(nil), m.Roles...)
	}
	return m, nil
}

// Period returns the duration of one tick.
func (m Mode) Period() time.Duration {
	rate := m.TickRate
	if rate <= 0 {
		rate = DefaultTickRate
	}
	return time.Second / time.Duration(rate)
}

// Role returns seat's role, or "" when the mode has none.
func (m Mode) Role(seat int) string {
	if seat < 0 || seat >= len(m.Roles) {
		return ""
	}
	return m.Roles[seat]
}

// Bot returns the bot for seat: NewBot's, or IdleBot.
func (m Mode) Bot(seat int) Bot {
	if m.NewBot != nil {
		if b := m.NewBot(seat, m.Role(seat)); b != nil {
			return b
		}
	}
	return IdleBot
}

var playerIDs = func() [wire.MaxSeats]string {
	var ids [wire.MaxSeats]string
	for i := range ids {
		ids[i] = "p" + strconv.Itoa(i)
	}
	return ids
}()

// PlayerID is the key under which seat's input reaches Simulation.Tick:
// "p0", "p1", and so on. It does not allocate.
func PlayerID(seat int) string {
	if seat >= 0 && seat < len(playerIDs) {
		return playerIDs[seat]
	}
	return "p" + strconv.Itoa(seat)
}

// SeatOf parses a PlayerID back into its seat, or returns -1.
func SeatOf(playerID string) int {
	if len(playerID) < 2 || playerID[0] != 'p' {
		return -1
	}
	n, err := strconv.Atoi(playerID[1:])
	if err != nil || n < 0 || n >= wire.MaxSeats || PlayerID(n) != playerID {
		return -1
	}
	return n
}

// Inputs is a reusable per-tick input table keyed by PlayerID. Set and Map do
// not allocate after construction, so a server or client tick loop can hand
// the same map to Simulation.Tick every tick. Simulation.Tick must not keep the
// map or its byte slices; game.Runtime copies them.
type Inputs struct {
	m    map[string]sim.Input
	bufs [][]byte
}

// NewInputs makes a table for seats seats whose inputs are at most size bytes.
func NewInputs(seats, size int) *Inputs {
	t := &Inputs{m: make(map[string]sim.Input, seats), bufs: make([][]byte, seats)}
	for i := 0; i < seats; i++ {
		t.bufs[i] = make([]byte, 0, size)
		t.m[PlayerID(i)] = sim.Input{}
	}
	return t
}

// Buffer returns seat's empty input buffer with the table's capacity, for a
// Bot or a sampler to append into.
func (t *Inputs) Buffer(seat int) []byte { return t.bufs[seat][:0] }

// Set copies data as seat's input for this tick, cut to the table's size.
func (t *Inputs) Set(seat int, data []byte) {
	buf := t.bufs[seat][:0]
	if len(data) > cap(buf) {
		data = data[:cap(buf)]
	}
	buf = append(buf, data...)
	t.bufs[seat] = buf
	t.m[PlayerID(seat)] = sim.Input{Data: buf}
}

// Get returns seat's current input.
func (t *Inputs) Get(seat int) []byte { return t.bufs[seat] }

// Map returns the table as Simulation.Tick takes it.
func (t *Inputs) Map() map[string]sim.Input { return t.m }

// Seats returns the number of seats.
func (t *Inputs) Seats() int { return len(t.bufs) }
