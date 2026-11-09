package stress

import (
	"time"

	"m31labs.dev/gosx/det"
	"m31labs.dev/gosx/game"
)

// GameConfig wraps a Sim in a game.Runtime the way a networked game client
// does: fixed ticks with lag catch-up, a snapshot and input history, and
// prediction-style replays (restore the snapshot from ReplayTicks ago and run
// those ticks again) every ReplayEvery frames.
type GameConfig struct {
	Sim Config
	// ReplayEvery is the number of frames between replays. Zero disables them.
	ReplayEvery int
	// ReplayTicks is how many ticks each replay runs again.
	ReplayTicks int
	// Input returns the input byte of seat at tick. Nil gives every seat no input.
	Input func(tick uint64, seat int) byte
	// Clock is a monotonic millisecond clock for FrameStats. Nil disables timing.
	Clock func() float64
	// Checkpoints lists ticks whose state hash the game records (see
	// Checkpoint), to compare two runs.
	Checkpoints []uint64
}

// DefaultGameConfig replays 6 ticks (200 ms at 30 Hz) every 3 frames, which at
// 60 fps is 20 reconciliations per second: a client 200 ms behind a server
// that sends 20 snapshots per second.
func DefaultGameConfig() GameConfig {
	return GameConfig{Sim: DefaultConfig(), ReplayEvery: 3, ReplayTicks: 6}
}

// historyLen bounds the replay window and the input history.
const historyLen = 64

// FrameStats is the work one Step did, in milliseconds of wall time.
type FrameStats struct {
	// Ticks is the number of new ticks; Replayed the number of ticks run again.
	Ticks, Replayed int
	// SimMS is simulation time without the AI: new ticks, replays, restores.
	SimMS float64
	// AIMS is the AI and rules time inside every tick, new and replayed.
	AIMS float64
	// ReplayMS is the wall time of the replays (part of SimMS plus AIMS).
	ReplayMS float64
	// Mismatches counts replays whose result differed from the state they
	// replaced. With unchanged inputs it must stay zero: it is a running
	// determinism check.
	Mismatches int
}

// Game is a Sim driven by a game.Runtime.
type Game struct {
	cfg     GameConfig
	rt      *game.Runtime
	sim     *Sim
	frames  uint64
	stats   FrameStats
	total   FrameStats
	inputs  [historyLen][]byte
	snaps   [historyLen][]byte
	scratch []byte
	hashes  []uint64
	hashSet []bool
}

func (g *Game) checkpoint() {
	t := g.sim.tick
	for i, c := range g.cfg.Checkpoints {
		if c == t {
			g.hashes[i], g.hashSet[i] = g.sim.Hash(), true
		}
	}
}

// NewGame builds the runtime and the simulation.
func NewGame(cfg GameConfig) *Game {
	g := &Game{cfg: cfg, sim: New(cfg.Sim)}
	if cfg.ReplayTicks >= historyLen {
		g.cfg.ReplayTicks = historyLen - 1
	}
	g.sim.Timing.Clock = cfg.Clock
	size := g.sim.SnapshotSize()
	for i := range g.snaps {
		g.snaps[i] = make([]byte, 0, size)
		g.inputs[i] = make([]byte, g.sim.cfg.Seats)
	}
	g.scratch = make([]byte, 0, size)
	g.hashes = make([]uint64, len(cfg.Checkpoints))
	g.hashSet = make([]bool, len(cfg.Checkpoints))
	tick := time.Second / time.Duration(g.sim.cfg.TickHz)
	g.rt = game.New(game.Config{
		Name:      "GameStress",
		FixedStep: tick,
		MaxDelta:  250 * time.Millisecond,
		// A networked client never drops a due tick.
		CatchUp: game.CatchUpLag,
		Snapshot: game.SnapshotCodecFuncs{
			SnapshotFunc:        func(*game.Runtime) []byte { return g.sim.Snapshot(nil) },
			RestoreSnapshotFunc: func(_ *game.Runtime, b []byte) error { return g.sim.Restore(b) },
		},
		Systems: []game.System{
			game.Func("reconcile", game.PhaseUpdate, g.reconcile),
			game.Func("simulate", game.PhaseFixedUpdate, g.simulate),
		},
	})
	return g
}

// Runtime returns the game.Runtime that drives the simulation.
func (g *Game) Runtime() *game.Runtime { return g.rt }

// Sim returns the simulation.
func (g *Game) Sim() *Sim { return g.sim }

// Stats returns what the last Runtime.Step did.
func (g *Game) Stats() FrameStats {
	st := g.stats
	// stats.SimMS holds the snapshot and restore overhead; add the steps.
	st.SimMS += g.sim.Timing.StepMS - g.sim.Timing.AIMS
	st.AIMS = g.sim.Timing.AIMS
	return st
}

// Checkpoint returns the state hash recorded right after tick t, for a tick
// listed in GameConfig.Checkpoints.
func (g *Game) Checkpoint(t uint64) (uint64, bool) {
	for i, c := range g.cfg.Checkpoints {
		if c == t && g.hashSet[i] {
			return g.hashes[i], true
		}
	}
	return 0, false
}

// Totals returns the sums over every Step.
func (g *Game) Totals() FrameStats { return g.total }

// Step advances the runtime by delta and adds the frame to Totals. A
// loop.Driver calls Runtime().Step itself; read Stats in its render callback.
func (g *Game) Step(delta time.Duration) (game.Frame, error) {
	frame, err := g.rt.Step(delta)
	st := g.Stats()
	g.total.Ticks += st.Ticks
	g.total.Replayed += st.Replayed
	g.total.SimMS += st.SimMS
	g.total.AIMS += st.AIMS
	g.total.ReplayMS += st.ReplayMS
	g.total.Mismatches += st.Mismatches
	return frame, err
}

func (g *Game) now() float64 {
	if g.cfg.Clock == nil {
		return 0
	}
	return g.cfg.Clock()
}

// simulate runs one new tick: record the input and the snapshot before it,
// then step.
func (g *Game) simulate(*game.Context) error {
	t := g.sim.tick
	slot := t % historyLen
	in := g.inputs[slot][:g.sim.cfg.Seats]
	for seat := range in {
		in[seat] = 0
		if g.cfg.Input != nil {
			in[seat] = g.cfg.Input(t, seat)
		}
	}
	t0 := g.now()
	g.snaps[slot] = g.sim.Snapshot(g.snaps[slot][:0])
	g.stats.SimMS += g.now() - t0
	g.sim.Step(in)
	g.stats.Ticks++
	g.checkpoint()
	return nil
}

// reconcile is the prediction-style replay: restore the state from
// ReplayTicks ago through the runtime's snapshot codec and run the same ticks
// again with the recorded inputs, as a client does when an authoritative
// snapshot arrives.
func (g *Game) reconcile(*game.Context) error {
	// The first system of every Step: start this frame's accounting.
	g.stats = FrameStats{}
	g.sim.Timing.Reset()
	g.frames++
	n := uint64(g.cfg.ReplayTicks)
	if g.cfg.ReplayEvery <= 0 || n == 0 || g.frames%uint64(g.cfg.ReplayEvery) != 0 || g.sim.tick < n {
		return nil
	}
	start := g.now()
	stepBefore := g.sim.Timing.StepMS
	before := g.sim.Hash()
	head := g.sim.tick
	from := head - n
	if err := g.rt.RestoreSnapshot(g.snaps[from%historyLen]); err != nil {
		return err
	}
	for t := from; t < head; t++ {
		slot := t % historyLen
		g.snaps[slot] = g.sim.Snapshot(g.snaps[slot][:0])
		g.sim.Step(g.inputs[slot])
		g.stats.Replayed++
		g.checkpoint()
	}
	if g.sim.Hash() != before {
		g.stats.Mismatches++
	}
	elapsed := g.now() - start
	g.stats.ReplayMS += elapsed
	// Restore, snapshots and hashing are simulation time too. The steps' own
	// time is already in sim.Timing, so only the rest is added here.
	g.stats.SimMS += elapsed - (g.sim.Timing.StepMS - stepBefore)
	return nil
}

// Pose is one agent's display pose, interpolated between the last two ticks.
type Pose struct {
	X, Y, Z, Heading, Speed float64
}

// Pose returns agent i's pose at interpolation alpha (0 is the previous tick,
// 1 the current one).
func (g *Game) Pose(i int, alpha float64) Pose {
	a := &g.sim.agents[i]
	inv := 1 - alpha
	heading := a.PHeading + float64(angleDelta(a.PHeading, a.Heading)*alpha)
	return Pose{
		X:       float64(a.PX*inv) + float64(a.X*alpha),
		Y:       float64(a.PLift*inv) + float64(a.Lift*alpha),
		Z:       float64(a.PZ*inv) + float64(a.Z*alpha),
		Heading: heading,
		Speed:   det.Sqrt(float64(a.VX*a.VX) + float64(a.VZ*a.VZ)),
	}
}

func angleDelta(from, to float64) float64 {
	const pi = 3.141592653589793
	d := to - from
	for d > pi {
		d -= 2 * pi
	}
	for d < -pi {
		d += 2 * pi
	}
	return d
}

// ScriptedInput returns a deterministic input source: each seat holds a
// direction (sometimes with sprint) for 20 ticks, then picks another. Runs
// that share the seed get the same inputs, on any thread.
func ScriptedInput(seed uint64) func(tick uint64, seat int) byte {
	return func(tick uint64, seat int) byte {
		x := (tick/20)*0x9E3779B97F4A7C15 ^ (uint64(seat)+1)*0xBF58476D1CE4E5B9 ^ seed
		x ^= x >> 31
		x *= 0x94D049BB133111EB
		x ^= x >> 29
		dirs := [8]byte{InputUp, InputUp | InputRight, InputRight, InputDown | InputRight, InputDown, InputDown | InputLeft, InputLeft, InputUp | InputLeft}
		in := dirs[x%8]
		if x&0x100 != 0 {
			in |= InputSprint
		}
		return in
	}
}
