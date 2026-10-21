package netplay

import "encoding/binary"

// pad is the held-button state of one player for one frame.
type pad uint8

const (
	padLeft pad = 1 << iota
	padRight
	padAttack
	padBlock
)

// Event kinds emitted by fightGame.
const (
	evHit   uint16 = 1
	evBlock uint16 = 2
	evKO    uint16 = 3
)

type padCodec struct{}

func (padCodec) Size() int              { return 1 }
func (padCodec) Put(dst []byte, in pad) { dst[0] = byte(in) }
func (padCodec) Get(src []byte) pad     { return pad(src[0]) }

// fightGame is a tiny deterministic integer fighting game. Every field
// that influences the future is part of Save, so a wrong input anywhere in
// history shows up in the state (chaos folds all inputs into one word).
type fightGame struct {
	frame   int32
	x       [2]int32
	hp      [2]int32
	prev    [2]pad
	atk     [2]int32 // 0 idle, 1.. frames into an attack
	stun    [2]int32
	chaos   uint64
	resetIn int32

	ev  [8]Event
	nev int

	// faultFrames lists game frames at which this instance misbehaves, to
	// model a platform-specific desync. Not part of Save.
	faultFrames []int32
}

func newFightGame() *fightGame {
	g := &fightGame{}
	g.reset()
	return g
}

func (g *fightGame) reset() {
	g.x = [2]int32{80, 120}
	g.hp = [2]int32{100, 100}
	g.atk = [2]int32{}
	g.stun = [2]int32{}
	g.resetIn = 0
}

func absI(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (g *fightGame) emit(kind uint16, source uint32, a, b int32) {
	if g.nev == len(g.ev) {
		return
	}
	e := Event{Frame: int64(g.frame), Kind: kind, Source: source}
	binary.LittleEndian.PutUint32(e.Data[0:], uint32(a))
	binary.LittleEndian.PutUint32(e.Data[4:], uint32(b))
	g.ev[g.nev] = e
	g.nev++
}

func (g *fightGame) Step(inputs []pad) {
	g.nev = 0
	if g.resetIn > 0 {
		g.resetIn--
		if g.resetIn == 0 {
			g.reset()
			g.hp = [2]int32{100, 100}
		}
	}
	for p := 0; p < 2; p++ {
		in := inputs[p]
		pressed := in &^ g.prev[p] // edge derived from held state
		switch {
		case g.stun[p] > 0:
			g.stun[p]--
		case g.atk[p] > 0:
			g.atk[p]++
			if g.atk[p] > 9 {
				g.atk[p] = 0
			}
		case pressed&padAttack != 0:
			g.atk[p] = 1
		default:
			if in&padLeft != 0 {
				g.x[p] -= 2
			}
			if in&padRight != 0 {
				g.x[p] += 2
			}
		}
		if g.x[p] < 0 {
			g.x[p] = 0
		}
		if g.x[p] > 200 {
			g.x[p] = 200
		}
		g.prev[p] = in
	}
	// Hits are decided against the pre-hit state so that same-frame trades land.
	var hit [2]bool
	var blocked [2]bool
	dist := absI(g.x[0] - g.x[1])
	for p := 0; p < 2; p++ {
		q := 1 - p
		if (g.atk[p] == 4 || g.atk[p] == 6) && g.resetIn == 0 && dist <= 24 {
			if g.prev[q]&padBlock != 0 && g.atk[q] == 0 && g.stun[q] == 0 {
				blocked[p] = true
			} else {
				hit[p] = true
			}
		}
	}
	for p := 0; p < 2; p++ {
		q := 1 - p
		move := uint32(1)
		if g.atk[p] == 6 {
			move = 2
		}
		src := uint32(p)<<16 | move
		switch {
		case hit[p]:
			g.hp[q] -= 10
			g.stun[q] = 6
			g.atk[q] = 0
			g.emit(evHit, src, 10, int32(q))
		case blocked[p]:
			g.hp[q]--
			g.emit(evBlock, src, 1, int32(q))
		}
	}
	if g.resetIn == 0 && (g.hp[0] <= 0 || g.hp[1] <= 0) {
		g.resetIn = 20
		g.emit(evKO, 0, g.hp[0], g.hp[1])
	}
	g.chaos = g.chaos*6364136223846793005 + 1442695040888963407 +
		uint64(inputs[0])<<8 | uint64(inputs[1]) + uint64(uint32(g.x[0]+g.x[1]))<<16
	for _, ff := range g.faultFrames {
		if ff == g.frame {
			g.chaos ^= 0xdeadbeef
			g.hp[0]++
		}
	}
	g.frame++
}

func (g *fightGame) Save(dst []byte) []byte {
	le := binary.LittleEndian
	dst = le.AppendUint32(dst, uint32(g.frame))
	for p := 0; p < 2; p++ {
		dst = le.AppendUint32(dst, uint32(g.x[p]))
		dst = le.AppendUint32(dst, uint32(g.hp[p]))
		dst = append(dst, byte(g.prev[p]))
		dst = le.AppendUint32(dst, uint32(g.atk[p]))
		dst = le.AppendUint32(dst, uint32(g.stun[p]))
	}
	dst = le.AppendUint64(dst, g.chaos)
	dst = le.AppendUint32(dst, uint32(g.resetIn))
	return dst
}

const fightStateSize = 4 + 2*(4+4+1+4+4) + 8 + 4

func (g *fightGame) Load(src []byte) error {
	if len(src) != fightStateSize {
		return errBadState
	}
	le := binary.LittleEndian
	g.frame = int32(le.Uint32(src))
	o := 4
	for p := 0; p < 2; p++ {
		g.x[p] = int32(le.Uint32(src[o:]))
		g.hp[p] = int32(le.Uint32(src[o+4:]))
		g.prev[p] = pad(src[o+8])
		g.atk[p] = int32(le.Uint32(src[o+9:]))
		g.stun[p] = int32(le.Uint32(src[o+13:]))
		o += 17
	}
	g.chaos = le.Uint64(src[o:])
	g.resetIn = int32(le.Uint32(src[o+8:]))
	g.nev = 0
	return nil
}

func (g *fightGame) Events(dst []Event) []Event { return append(dst, g.ev[:g.nev]...) }
