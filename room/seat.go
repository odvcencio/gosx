package room

import (
	"time"

	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
)

// inputRingSize is how many ticks of one seat's input the server holds. A
// client may run at most this many ticks ahead of the server, minus one.
const inputRingSize = 128

// inputRing buffers one seat's inputs by tick in fixed storage, so receiving
// and consuming input never allocates.
type inputRing struct {
	ticks [inputRingSize]uint64
	lens  [inputRingSize]uint8
	data  [inputRingSize][wire.MaxInputSize]byte
}

func (q *inputRing) put(tick uint64, in []byte) {
	i := tick % inputRingSize
	if q.ticks[i] == tick {
		return // redundant copy of an input already held
	}
	q.ticks[i] = tick
	q.lens[i] = uint8(copy(q.data[i][:], in))
}

func (q *inputRing) get(tick uint64) ([]byte, bool) {
	i := tick % inputRingSize
	if tick == 0 || q.ticks[i] != tick {
		return nil, false
	}
	return q.data[i][:q.lens[i]], true
}

func (q *inputRing) reset() { clear(q.ticks[:]) }

// seat is the server's state for one seat. Guarded by Room.mu.
type seat struct {
	index int
	role  string
	kind  wire.SeatKind
	name  string
	// token resumes a human seat; empty for bots and empty seats.
	token string
	// client is the attached link's ID; empty while the person is away.
	client string
	// expires ends a reserved human seat whose person is not connected.
	// Zero while connected.
	expires time.Duration
	// fill marks a bot that took an empty seat, which a person may take over.
	// A bot covering an absent person's seat is not a fill.
	fill bool
	bot  mode.Bot

	ring  inputRing
	acked uint64 // every input through this tick was received or passed
	last  [wire.MaxInputSize]byte
	lastN int
	// usedBot reports that a bot produced this tick's input.
	usedBot bool

	late, missed uint64
}

func (s *seat) human() bool     { return s.kind == wire.SeatHuman }
func (s *seat) connected() bool { return s.kind == wire.SeatHuman && s.client != "" }

func (s *seat) info() wire.SeatInfo {
	return wire.SeatInfo{
		Kind:      s.kind,
		Connected: s.connected(),
		Covered:   s.kind == wire.SeatHuman && s.client == "",
		Role:      s.role,
		Name:      s.name,
	}
}

func (s *seat) flags() uint8 {
	var f uint8
	if s.kind == wire.SeatHuman {
		f |= wire.FlagHuman
		if s.client != "" {
			f |= wire.FlagConnected
		}
	}
	if s.usedBot {
		f |= wire.FlagBot
	}
	return f
}

// resetMatch clears per-match input state.
func (s *seat) resetMatch() {
	s.ring.reset()
	s.acked, s.lastN, s.usedBot = 0, 0, false
	s.late, s.missed = 0, 0
}
