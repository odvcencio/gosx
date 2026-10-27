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
// The wrapper keeps the system's Name and Phase. Runtime.Snapshot saves the
// game time it has gathered and Restore puts it back, so a restored run sees the
// same Delta as a run that never stopped. See CadenceState.
func Every(n int, s System) System {
	if n < 1 {
		n = 1
	}
	return &cadence{inner: s, every: uint64(n)}
}

// EveryInterval runs s whenever d of game time has gathered, regardless of the
// step size. ctx.Delta holds the gathered time (d, or more when a coarse step
// overshoots; the overshoot is passed on, not dropped). Snapshots save the
// gathered time, as for Every. d <= 0 runs every tick.
func EveryInterval(d time.Duration, s System) System {
	return &cadence{inner: s, interval: d}
}

// CadenceState is the game time one Every or EveryInterval wrapper has gathered
// since it last ran. Runtime.Snapshot writes one per wrapper, outer wrapper
// first, in system order. Restore gives each wrapper the entry with its
// system's name, taking entries of one name in order. A wrapper with no entry
// (a system added since the snapshot) starts from zero, and an entry with no
// wrapper is dropped. A wrapper hidden inside another System type is not found,
// so its time is not saved.
type CadenceState struct {
	System        string `json:"system"`
	GatheredNanos int64  `json:"gatheredNanos"`
}

// cadences lists the Every and EveryInterval wrappers of r in system order,
// each before the wrapper it wraps.
func (r *Runtime) cadences() []*cadence {
	var out []*cadence
	for _, s := range r.systems {
		for c, ok := s.(*cadence); ok; c, ok = c.inner.(*cadence) {
			out = append(out, c)
		}
	}
	return out
}

func (r *Runtime) cadenceStates() []CadenceState {
	var out []CadenceState
	for _, c := range r.cadences() {
		out = append(out, CadenceState{System: c.Name(), GatheredNanos: int64(c.accum)})
	}
	return out
}

func (r *Runtime) restoreCadences(saved []CadenceState) {
	wrappers := r.cadences()
	if len(wrappers) == 0 {
		return
	}
	byName := make(map[string][]time.Duration, len(saved))
	for _, s := range saved {
		byName[s.System] = append(byName[s.System], time.Duration(s.GatheredNanos))
	}
	for _, c := range wrappers {
		name := c.Name()
		c.accum = 0
		if queue := byName[name]; len(queue) > 0 {
			c.accum, byName[name] = queue[0], queue[1:]
		}
	}
}
