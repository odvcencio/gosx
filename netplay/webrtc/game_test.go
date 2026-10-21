package webrtc

import (
	"encoding/binary"
	"errors"

	"m31labs.dev/gosx/netplay"
)

// duelPad is one player's held buttons for one frame.
type duelPad uint8

type duelCodec struct{}

func (duelCodec) Size() int                  { return 1 }
func (duelCodec) Put(dst []byte, in duelPad) { dst[0] = byte(in) }
func (duelCodec) Get(src []byte) duelPad     { return duelPad(src[0]) }

// duelGame is a tiny deterministic game. chaos folds every input into the
// state, so one wrong input anywhere in history changes the checksum.
type duelGame struct {
	frame int32
	x     [2]int32
	prev  [2]duelPad
	score [2]int32
	chaos uint64
}

func newDuelGame() *duelGame { return &duelGame{x: [2]int32{80, 120}} }

func (g *duelGame) Step(in []duelPad) {
	for p := 0; p < 2; p++ {
		if in[p]&1 != 0 {
			g.x[p] -= 2
		}
		if in[p]&2 != 0 {
			g.x[p] += 2
		}
		if pressed := in[p] &^ g.prev[p]; pressed&4 != 0 {
			d := g.x[0] - g.x[1]
			if d < 0 {
				d = -d
			}
			if d < 40 {
				g.score[p]++
			}
		}
		g.prev[p] = in[p]
	}
	g.chaos = g.chaos*6364136223846793005 + 1442695040888963407 + uint64(in[0])<<8 | uint64(in[1]) + uint64(uint32(g.x[0]+g.x[1]))<<16
	g.frame++
}

const duelStateSize = 4 + 2*(4+1+4) + 8

func (g *duelGame) Save(dst []byte) []byte {
	le := binary.LittleEndian
	dst = le.AppendUint32(dst, uint32(g.frame))
	for p := 0; p < 2; p++ {
		dst = le.AppendUint32(dst, uint32(g.x[p]))
		dst = append(dst, byte(g.prev[p]))
		dst = le.AppendUint32(dst, uint32(g.score[p]))
	}
	return le.AppendUint64(dst, g.chaos)
}

func (g *duelGame) Load(src []byte) error {
	if len(src) != duelStateSize {
		return errors.New("bad state")
	}
	le := binary.LittleEndian
	g.frame = int32(le.Uint32(src))
	o := 4
	for p := 0; p < 2; p++ {
		g.x[p] = int32(le.Uint32(src[o:]))
		g.prev[p] = duelPad(src[o+4])
		g.score[p] = int32(le.Uint32(src[o+5:]))
		o += 9
	}
	g.chaos = le.Uint64(src[o:])
	return nil
}

func (g *duelGame) Events(dst []netplay.Event) []netplay.Event { return dst }

// duelInput is a deterministic pseudo-random input for player p at frame f.
func duelInput(seed uint64, p int, f int64) duelPad {
	h := (uint64(f/6)+seed)*0x9E3779B97F4A7C15 ^ uint64(p+1)*0xC2B2AE3D27D4EB4F
	h ^= h >> 29
	return duelPad(h & 7)
}
