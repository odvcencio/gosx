package game

import "time"

// cadence wraps a System so it runs on a slower schedule than its phase.
type cadence struct {
	inner    System
	every    uint64        // run when Tick%every == 0; 0 when interval based
	interval time.Duration // run when accumulated game time >= interval
	accum    time.Duration
}

func (c *cadence) Name() string { return c.inner.Name() }
func (c *cadence) Phase() Phase { return c.inner.Phase() }

func (c *cadence) Update(ctx *Context) error {
	c.accum += ctx.Delta
	if c.every > 0 {
		if ctx.Tick%c.every != 0 {
			return nil
		}
	} else if c.accum < c.interval {
		return nil
	}
	saved := ctx.Delta
	ctx.Delta, c.accum = c.accum, 0
	defer func() { ctx.Delta = saved }()
	return c.inner.Update(ctx)
}

// Every runs s once every n ticks of its phase and skips the rest. For a
// PhaseFixedUpdate system a tick is a fixed step, counted from the start of the
// clock, so the schedule is the same on every run and after Restore. For other
// phases a tick is a frame. On a run, ctx.Delta holds the game time gathered
// since the last run (n fixed steps for a fixed-update system), so the system
// can integrate the whole span. n below 2 runs every tick.
//
// This is the multi-rate tool for coarse-step simulations: set FixedStep to the
// finest interval you need (one game hour, say), run fast systems every step,
// and slower ones with Every(24, daily) or Every(24*30, monthly).
//
// The wrapper keeps the system's Name and Phase. Its delta accumulator is not
// saved, so after Restore the first run after a skipped stretch sees a shorter
// Delta than a run that never restored.
func Every(n int, s System) System {
	if n < 1 {
		n = 1
	}
	return &cadence{inner: s, every: uint64(n)}
}

// EveryInterval runs s whenever d of game time has gathered, regardless of the
// step size. ctx.Delta holds the gathered time (d, or more when a coarse step
// overshoots; the overshoot is passed on, not dropped). The accumulator is not
// saved by snapshots. d <= 0 runs every tick.
func EveryInterval(d time.Duration, s System) System {
	return &cadence{inner: s, interval: d}
}
