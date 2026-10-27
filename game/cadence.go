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

// Kinds of CadenceState.
const (
	cadenceKindEvery    = "every"
	cadenceKindInterval = "interval"
)

// CadenceState is the game time one Every or EveryInterval wrapper has gathered
// since it last ran. Runtime.Snapshot writes one per wrapper, outer wrapper
// first, in system order.
//
// Restore gives a wrapper the entry that agrees with it on System (the name of
// the system), Kind ("every" or "interval"), Param (n, or the interval in
// nanoseconds) and Depth (0 for a wrapper the runtime holds directly, 1 for the
// wrapper inside it, and so on). A wrapper with no such entry, such as a system
// added or changed since the snapshot, starts from zero, and an entry with no
// wrapper is dropped. Wrappers that agree on all four take their entries in
// order, so give them distinct names if their order can change. A wrapper hidden
// inside another System type is not found, so its time is not saved.
type CadenceState struct {
	System        string `json:"system"`
	Kind          string `json:"kind"`
	Param         int64  `json:"param"`
	Depth         int    `json:"depth"`
	GatheredNanos int64  `json:"gatheredNanos"`
}

// key is s without its gathered time: what Restore matches on.
func (s CadenceState) key() CadenceState {
	s.GatheredNanos = 0
	return s
}

// state describes c at nesting depth depth.
func (c *cadence) state(depth int) CadenceState {
	s := CadenceState{System: c.Name(), Depth: depth, GatheredNanos: int64(c.accum)}
	if c.every > 0 {
		s.Kind, s.Param = cadenceKindEvery, int64(c.every)
	} else {
		s.Kind, s.Param = cadenceKindInterval, int64(c.interval)
	}
	return s
}

// cadences lists the Every and EveryInterval wrappers of r in system order,
// each before the wrapper it wraps, with the state of each.
func (r *Runtime) cadences() ([]*cadence, []CadenceState) {
	var wrappers []*cadence
	var states []CadenceState
	for _, s := range r.systems {
		depth := 0
		for c, ok := s.(*cadence); ok; c, ok = c.inner.(*cadence) {
			wrappers = append(wrappers, c)
			states = append(states, c.state(depth))
			depth++
		}
	}
	return wrappers, states
}

func (r *Runtime) cadenceStates() []CadenceState {
	_, states := r.cadences()
	return states
}

func (r *Runtime) restoreCadences(saved []CadenceState) {
	wrappers, states := r.cadences()
	if len(wrappers) == 0 {
		return
	}
	byKey := make(map[CadenceState][]time.Duration, len(saved))
	for _, s := range saved {
		key := s.key()
		byKey[key] = append(byKey[key], time.Duration(s.GatheredNanos))
	}
	for i, c := range wrappers {
		key := states[i].key()
		c.accum = 0
		if queue := byKey[key]; len(queue) > 0 {
			c.accum, byKey[key] = queue[0], queue[1:]
		}
	}
}
