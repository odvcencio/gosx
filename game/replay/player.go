package replay

import (
	"errors"
	"fmt"
	"time"

	"m31labs.dev/gosx/sim/simcore"
)

// Speeds the player accepts. SetSpeed clamps to this range.
const (
	MinSpeed = 0.25
	MaxSpeed = 4.0
)

// Speeds lists the usual playback speeds for a speed picker.
var Speeds = []float64{0.25, 0.5, 1, 2, 4}

// ErrOutOfRange reports a seek to a tick the recording does not hold.
var ErrOutOfRange = errors.New("replay: tick outside the recording")

// Player plays a Recording on a simulation. The simulation must be a fresh
// instance built the same way as the recorded one (same config, same seats);
// the player restores keyframes into it. Do not tick the simulation yourself
// while a Player owns it.
//
// A Player is not safe for concurrent use.
type Player struct {
	sim    simcore.Simulation
	check  simcore.CheckedRestorer
	rec    *Recording
	tick   uint64
	loaded bool
	speed  float64
	paused bool
	loop   bool
	acc    time.Duration // time carried toward the next tick
	step   time.Duration
	inputs map[string]simcore.Input
	ids    idCache
	// Stats of the last Seek.
	lastResim int
	seeks     uint64
	err       error
}

// NewPlayer prepares to play rec on sim and seeks to the first tick. The
// player starts paused at speed 1.
func NewPlayer(sim simcore.Simulation, rec *Recording) (*Player, error) {
	if rec == nil || len(rec.segs) == 0 {
		return nil, errors.New("replay: empty recording")
	}
	p := &Player{sim: sim, rec: rec, speed: 1, paused: true, inputs: map[string]simcore.Input{}}
	p.check, _ = sim.(simcore.CheckedRestorer)
	p.step = time.Second / 60
	if hz := rec.TickRate(); hz > 0 {
		p.step = time.Duration(float64(time.Second) / hz)
	}
	if err := p.Seek(rec.Start()); err != nil {
		return nil, err
	}
	return p, nil
}

// Sim returns the simulation the player drives, for reading state to render.
func (p *Player) Sim() simcore.Simulation { return p.sim }

// Recording returns the recording being played.
func (p *Player) Recording() *Recording { return p.rec }

// Tick is the tick the simulation shows now.
func (p *Player) Tick() uint64 { return p.tick }

// Start and End bound the seekable ticks.
func (p *Player) Start() uint64 { return p.rec.Start() }

// End is the last tick the player can reach.
func (p *Player) End() uint64 { return p.rec.End() }

// AtEnd reports that the player is at the last tick.
func (p *Player) AtEnd() bool { return p.tick >= p.rec.End() }

// Err returns the first error a playback step hit, such as a refused snapshot.
func (p *Player) Err() error { return p.err }

// Seeks counts the Seek calls that moved the player. A renderer that blends
// the previous and current state resets its blend when this changes, because
// the previous tick is no longer the one just before.
func (p *Player) Seeks() uint64 { return p.seeks }

// LastResim is how many ticks the last Seek re-simulated after its keyframe.
func (p *Player) LastResim() int { return p.lastResim }

func (p *Player) restore(snap []byte) error {
	if p.check != nil {
		return p.check.RestoreSnapshot(snap)
	}
	p.sim.Restore(snap)
	return nil
}

// Seek shows the state at tick. Moving forward from the current tick inside the
// same keyframe span re-simulates only the gap. Moving backward, or across a
// keyframe, restores the nearest keyframe at or before tick first, so the cost
// is bounded by the keyframe interval.
func (p *Player) Seek(tick uint64) error {
	if tick < p.rec.Start() || tick > p.rec.End() {
		return fmt.Errorf("%w: %d not in [%d, %d]", ErrOutOfRange, tick, p.rec.Start(), p.rec.End())
	}
	si := p.rec.segmentFor(tick)
	seg := p.rec.segs[si]
	from := p.tick
	if !p.loaded || tick < p.tick || seg.start > p.tick {
		if err := p.restore(seg.snapshot); err != nil {
			p.err = err
			return fmt.Errorf("replay: restore keyframe %d: %w", seg.start, err)
		}
		p.loaded = true
		from = seg.start
	}
	p.lastResim = int(tick - from)
	p.seeks++
	for t := from; t < tick; t++ {
		if err := p.advanceTo(t); err != nil {
			return err
		}
	}
	p.tick = tick
	p.acc = 0
	return nil
}

// advanceTo ticks the simulation from tick t to t+1 using the recorded inputs.
func (p *Player) advanceTo(t uint64) error {
	segs := p.rec.segs
	si := p.rec.segmentFor(t)
	seg := segs[si]
	i := int(t - seg.start)
	if i >= seg.ticks() {
		return ErrOutOfRange
	}
	end := len(seg.inputs)
	if i+1 < seg.ticks() {
		end = int(seg.offsets[i+1])
	}
	if err := decodeInputs(seg.inputs[seg.offsets[i]:end], p.inputs, &p.ids); err != nil {
		p.err = err
		return err
	}
	p.sim.Tick(p.inputs)
	return nil
}

// StepForward advances n ticks, stopping at the end. It returns the ticks run.
func (p *Player) StepForward(n int) int {
	ran := 0
	for ; ran < n && p.tick < p.rec.End(); ran++ {
		if err := p.advanceTo(p.tick); err != nil {
			break
		}
		p.tick++
	}
	p.acc = 0
	return ran
}

// StepBack moves n ticks earlier, stopping at the start, by seeking.
func (p *Player) StepBack(n int) error {
	target := p.rec.Start()
	if uint64(n) < p.tick-target {
		target = p.tick - uint64(n)
	}
	return p.Seek(target)
}

// Pause stops Advance from moving the tick.
func (p *Player) Pause() { p.paused = true }

// Resume lets Advance move the tick. At the end of the recording with Loop off
// it stays put; with Loop on it wraps to the start.
func (p *Player) Resume() { p.paused = false }

// Paused reports whether playback is paused.
func (p *Player) Paused() bool { return p.paused }

// SetLoop makes playback wrap to the start at the end.
func (p *Player) SetLoop(on bool) { p.loop = on }

// SetSpeed sets the playback speed, clamped to [MinSpeed, MaxSpeed].
func (p *Player) SetSpeed(s float64) {
	switch {
	case s != s || s < MinSpeed:
		s = MinSpeed
	case s > MaxSpeed:
		s = MaxSpeed
	}
	p.speed = s
}

// Speed is the playback speed.
func (p *Player) Speed() float64 { return p.speed }

// Advance moves playback forward by dt of wall time at the current speed. It
// runs whole ticks and carries the remainder, and returns the ticks run. A
// paused player does nothing. Read Alpha afterward to interpolate rendering
// between the previous tick and this one.
func (p *Player) Advance(dt time.Duration) int {
	if p.paused || dt <= 0 {
		return 0
	}
	p.acc += time.Duration(float64(dt) * p.speed)
	ran := 0
	for p.acc >= p.step {
		if p.tick >= p.rec.End() {
			if p.loop {
				carry := p.acc
				if err := p.Seek(p.rec.Start()); err != nil {
					p.paused = true
					break
				}
				p.acc = carry
				continue
			}
			p.acc = 0
			p.paused = true
			break
		}
		if err := p.advanceTo(p.tick); err != nil {
			p.paused = true
			break
		}
		p.tick++
		p.acc -= p.step
		ran++
	}
	return ran
}

// Alpha is the fraction, in [0, 1), of the next tick that wall time has
// carried. Render the state at Tick and the one before it, blended by Alpha.
func (p *Player) Alpha() float64 {
	if p.paused || p.step <= 0 {
		return 0
	}
	return float64(p.acc) / float64(p.step)
}

// TickDuration is the wall time of one tick at speed 1.
func (p *Player) TickDuration() time.Duration { return p.step }
