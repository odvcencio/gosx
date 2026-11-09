// Package roomtest is a tiny generic game for testing and demonstrating
// package room and package game/netsync: every seat steers one body around a
// bounded arena, and optional props drift and bounce. It has no rules beyond
// movement. It runs on a game.Runtime, uses integer fixed-point math so every
// platform computes the same states, and builds for js/wasm.
package roomtest

import (
	"encoding/binary"
	"errors"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/sim"
)

// Input bits. An input is one byte of held buttons.
const (
	Left  byte = 1 << 0
	Right byte = 1 << 1
	Up    byte = 1 << 2
	Down  byte = 1 << 3
	Boost byte = 1 << 4
)

// World units are fixed-point thousandths.
const (
	Unit   = 1000
	Arena  = 20 * Unit // half extent: positions stay within [-Arena, Arena]
	Radius = Unit / 2
	accel  = 60
	boost  = 2
	maxV   = 600
)

// Body is one moving body. Seat bodies come first, then props.
type Body struct {
	X, Y, VX, VY int32
	Bounces      uint32
}

// World is the game state.
type World struct {
	Tick   uint64
	Seats  int
	Bodies []Body
}

// Game is a game.Runtime that runs a World. It implements sim.Simulation and
// sim.CheckedRestorer through the embedded runtime.
type Game struct {
	*game.Runtime
	World *World
	buf   []byte
}

// NewGame builds a game with seats steered bodies and props drifting ones,
// placed from seed.
func NewGame(seats, props int, seed uint64, tickRate int) *Game {
	w := &World{Seats: seats, Bodies: make([]Body, seats+props)}
	rng := seed | 1
	next := func() int32 { // xorshift64*
		rng ^= rng >> 12
		rng ^= rng << 25
		rng ^= rng >> 27
		return int32((rng * 2685821657736338717) >> 40)
	}
	for i := range w.Bodies {
		b := &w.Bodies[i]
		b.X = next() % (Arena - Radius)
		b.Y = next() % (Arena - Radius)
		if i >= seats {
			b.VX, b.VY = next()%maxV, next()%maxV
		}
	}
	g := &Game{World: w}
	g.Runtime = game.New(game.Config{
		Name:      "roomtest-bodies",
		FixedStep: time.Second / time.Duration(tickRate),
		Systems: []game.System{game.Func("bodies", game.PhaseFixedUpdate, func(ctx *game.Context) error {
			w.Step(ctx.NetworkInputs)
			return nil
		})},
		Snapshot: game.SnapshotCodecFuncs{
			SnapshotFunc:        func(*game.Runtime) []byte { return g.encode() },
			StateFunc:           func(*game.Runtime) []byte { return g.encode() },
			RestoreSnapshotFunc: func(_ *game.Runtime, data []byte) error { return w.Decode(data) },
		},
		ManualPhysics: true,
	})
	return g
}

// Of returns the Game inside a simulation built by this package, or nil.
func Of(s sim.Simulation) *Game {
	g, _ := s.(*Game)
	return g
}

// Step advances the world one tick with the seats' inputs, keyed by
// mode.PlayerID.
func (w *World) Step(inputs map[string]sim.Input) {
	w.Tick++
	for i := range w.Bodies {
		b := &w.Bodies[i]
		if i < w.Seats {
			var held byte
			if in, ok := inputs[mode.PlayerID(i)]; ok && len(in.Data) > 0 {
				held = in.Data[0]
			}
			a := int32(accel)
			if held&Boost != 0 {
				a *= boost
			}
			if held&Left != 0 {
				b.VX -= a
			}
			if held&Right != 0 {
				b.VX += a
			}
			if held&Up != 0 {
				b.VY -= a
			}
			if held&Down != 0 {
				b.VY += a
			}
			b.VX -= b.VX / 8 // drag
			b.VY -= b.VY / 8
		}
		b.VX, b.VY = clamp(b.VX, -maxV, maxV), clamp(b.VY, -maxV, maxV)
		b.X += b.VX
		b.Y += b.VY
		if b.X < -Arena || b.X > Arena {
			b.X = clamp(b.X, -Arena, Arena)
			b.VX = -b.VX
			b.Bounces++
		}
		if b.Y < -Arena || b.Y > Arena {
			b.Y = clamp(b.Y, -Arena, Arena)
			b.VY = -b.VY
			b.Bounces++
		}
	}
	// Push overlapping bodies apart, in index order, so it is deterministic.
	for i := range w.Bodies {
		for j := i + 1; j < len(w.Bodies); j++ {
			a, b := &w.Bodies[i], &w.Bodies[j]
			dx, dy := b.X-a.X, b.Y-a.Y
			if abs(dx) >= 2*Radius || abs(dy) >= 2*Radius {
				continue
			}
			px, py := sign(dx)*(2*Radius-abs(dx))/2, sign(dy)*(2*Radius-abs(dy))/2
			if abs(dx) > abs(dy) {
				a.X, b.X = a.X-px, b.X+px
			} else {
				a.Y, b.Y = a.Y-py, b.Y+py
			}
		}
	}
}

const bodyBytes = 20

// Encode appends the world's binary snapshot to dst: a fixed layout, so
// deltas between ticks are small.
func (w *World) Encode(dst []byte) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, w.Tick)
	dst = binary.LittleEndian.AppendUint16(dst, uint16(w.Seats))
	dst = binary.LittleEndian.AppendUint16(dst, uint16(len(w.Bodies)))
	for _, b := range w.Bodies {
		dst = binary.LittleEndian.AppendUint32(dst, uint32(b.X))
		dst = binary.LittleEndian.AppendUint32(dst, uint32(b.Y))
		dst = binary.LittleEndian.AppendUint32(dst, uint32(b.VX))
		dst = binary.LittleEndian.AppendUint32(dst, uint32(b.VY))
		dst = binary.LittleEndian.AppendUint32(dst, b.Bounces)
	}
	return dst
}

// ErrSnapshot reports a snapshot that does not fit the world.
var ErrSnapshot = errors.New("roomtest: snapshot does not match the world")

// Decode restores a snapshot written by Encode. It leaves w unchanged on error
// and does not allocate when the body count matches.
func (w *World) Decode(data []byte) error {
	if len(data) < 12 {
		return ErrSnapshot
	}
	seats := int(binary.LittleEndian.Uint16(data[8:]))
	n := int(binary.LittleEndian.Uint16(data[10:]))
	if seats != w.Seats || n != len(w.Bodies) || len(data) != 12+n*bodyBytes {
		return ErrSnapshot
	}
	w.Tick = binary.LittleEndian.Uint64(data)
	p := data[12:]
	for i := range w.Bodies {
		b := &w.Bodies[i]
		b.X = int32(binary.LittleEndian.Uint32(p))
		b.Y = int32(binary.LittleEndian.Uint32(p[4:]))
		b.VX = int32(binary.LittleEndian.Uint32(p[8:]))
		b.VY = int32(binary.LittleEndian.Uint32(p[12:]))
		b.Bounces = binary.LittleEndian.Uint32(p[16:])
		p = p[bodyBytes:]
	}
	return nil
}

// encode returns a snapshot that the caller owns. sim.Simulation.Snapshot
// results are retained by callers, so it copies out of a reused buffer.
func (g *Game) encode() []byte {
	g.buf = g.World.Encode(g.buf[:0])
	return append([]byte(nil), g.buf...)
}

// ChaseBot steers its body toward the nearest other body, boosting when far.
// It reads the simulation only.
var ChaseBot mode.Bot = mode.BotFunc(func(ctx mode.BotContext, dst []byte) []byte {
	g := Of(ctx.Sim)
	if g == nil || ctx.Seat >= len(g.World.Bodies) {
		return append(dst, 0)
	}
	me := g.World.Bodies[ctx.Seat]
	best, bestD := -1, int64(1<<62)
	for i, b := range g.World.Bodies {
		if i == ctx.Seat {
			continue
		}
		dx, dy := int64(b.X-me.X), int64(b.Y-me.Y)
		if d := dx*dx + dy*dy; d < bestD {
			best, bestD = i, d
		}
	}
	var held byte
	if best >= 0 {
		t := g.World.Bodies[best]
		if t.X < me.X-Radius {
			held |= Left
		} else if t.X > me.X+Radius {
			held |= Right
		}
		if t.Y < me.Y-Radius {
			held |= Up
		} else if t.Y > me.Y+Radius {
			held |= Down
		}
		if bestD > int64(8*Unit)*int64(8*Unit) {
			held |= Boost
		}
	}
	return append(dst, held)
})

// Options configures Mode.
type Options struct {
	Name      string
	Seats     int
	Props     int
	TickRate  int
	Countdown time.Duration
	Duration  time.Duration
	LobbyWait time.Duration
}

// Mode returns a room mode that plays this game.
func Mode(o Options) mode.Mode {
	if o.Name == "" {
		o.Name = "bodies"
	}
	if o.Seats <= 0 {
		o.Seats = 2
	}
	if o.TickRate <= 0 {
		o.TickRate = 30
	}
	seats, props, rate := o.Seats, o.Props, o.TickRate
	return mode.Mode{
		Name: o.Name, Seats: seats, TickRate: rate, InputSize: 1,
		Countdown: o.Countdown, Duration: o.Duration, LobbyWait: o.LobbyWait,
		NewSim: func(seed uint64) sim.Simulation { return NewGame(seats, props, seed, rate) },
		NewBot: func(int, string) mode.Bot { return ChaseBot },
		Results: func(s sim.Simulation) []byte {
			g := Of(s)
			if g == nil {
				return nil
			}
			out := make([]byte, 0, seats*4)
			for i := 0; i < seats; i++ {
				out = binary.LittleEndian.AppendUint32(out, g.World.Bodies[i].Bounces)
			}
			return out
		},
	}
}

func clamp(v, lo, hi int32) int32 { return max(lo, min(hi, v)) }

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int32) int32 {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 1
}
