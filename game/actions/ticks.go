package actions

import (
	"math"
	"time"
)

// EdgeKind is what happened to an action at an Edge.
type EdgeKind uint8

const (
	// PressEdge is an action going down.
	PressEdge EdgeKind = iota + 1
	// ReleaseEdge is an action going up. Edge.Tapped, Canceled and HeldFor
	// describe the press it ends.
	ReleaseEdge
	// HoldEdge is a press reaching its action's Hold time.
	HoldEdge
	// RepeatEdge is a held press repeating (ActionDef.Repeat). A Ticks hears
	// it only for the actions TickOptions.Repeats names.
	RepeatEdge
	// PulseEdge is a press that came from the page, not from a device:
	// Ticks.Pulse.
	PulseEdge
)

// Edge is one change of one action, as the Map resolved it in an Update.
type Edge struct {
	// Context and Name are the action's context and name.
	Context, Name string
	Kind          EdgeKind
	// At is the Map clock of the Update that resolved the edge.
	At time.Duration
	// HeldFor is how long the press had lasted, at a release, a hold or a
	// repeat. It is measured on the Map clock, so it is sampled at the
	// frames: a press reads the same length at 30, 60 or 144 frames a
	// second only to within one frame.
	HeldFor time.Duration
	// Tapped, DoubleTapped and Canceled are as in State.
	Tapped, DoubleTapped, Canceled bool
}

// TickOptions choose what a Ticks hears.
type TickOptions struct {
	// Names are the actions to hear; nil hears every Button action. Axis
	// actions are levels, read with Value.
	Names []string
	// Repeats are the actions whose repeats arrive as RepeatEdge. Others
	// arrive as one press, however long they are held.
	Repeats []string
	// Limit bounds the edges kept between two ticks. Zero selects 256. Past
	// it the oldest edges are dropped and counted in Dropped: a simulation
	// that stopped ticking (a menu) must not grow the buffer without end.
	Limit int
}

// Ticks hands a Map's action edges to a fixed-step simulation. The Map
// resolves input once per drawn frame; a simulation step may run on none,
// one or several of those frames. Ticks keeps every edge the Map resolves
// until the next Tick, so the step that runs next sees each press and
// release exactly once, in order, and the steps after it in the same frame
// see none.
//
// Use it like this, once per drawn frame:
//
//	m.Update(dt)          // the Map tells its Ticks its edges
//	for each fixed step {
//		t.Tick()          // this step's edges
//		in := read(t)     // Pressed, Released, Edges, Down, Value
//	}
//
// A tick sees the interval as it happened: a tap that began and ended
// between two steps reads Pressed and Released together, with the release's
// Tapped and HeldFor, and the final level (Down) up. A game that wants one
// command per step keeps its own rule for which edge wins.
//
// Edges come in the Map's resolution order: by context in the order they
// were defined, then by action in definition order. Two actions that change
// in the same Update are in that order, not in the order the keys moved. It
// is not safe for concurrent use.
type Ticks struct {
	m       *Map
	names   map[string]bool
	repeats map[string]bool
	limit   int
	pending []Edge
	current []Edge
	dropped uint64
	ticked  bool
}

// NewTicks attaches a tick buffer to m. It hears every Update from now on.
func NewTicks(m *Map, o TickOptions) *Ticks {
	t := &Ticks{m: m, limit: o.Limit}
	if t.limit <= 0 {
		t.limit = 256
	}
	if o.Names != nil {
		t.names = map[string]bool{}
		for _, n := range o.Names {
			t.names[normalize(n)] = true
		}
	}
	t.repeats = map[string]bool{}
	for _, n := range o.Repeats {
		t.repeats[normalize(n)] = true
	}
	m.ticks = append(m.ticks, t)
	return t
}

// Detach stops the buffer hearing its Map.
func (t *Ticks) Detach() {
	for i, o := range t.m.ticks {
		if o == t {
			t.m.ticks = append(t.m.ticks[:i], t.m.ticks[i+1:]...)
			break
		}
	}
}

func (m *Map) emit(e Edge) {
	for _, t := range m.ticks {
		t.record(e)
	}
}

func (t *Ticks) record(e Edge) {
	if t.names != nil && !t.names[e.Name] {
		return
	}
	if t.names == nil && e.Kind != PulseEdge {
		if d := t.m.lookupIn(e.Context, e.Name); d != nil && d.Kind != Button {
			return
		}
	}
	if e.Kind == RepeatEdge && !t.repeats[e.Name] {
		return
	}
	if len(t.pending) >= t.limit {
		n := copy(t.pending, t.pending[1:])
		t.pending = t.pending[:n]
		t.dropped++
	}
	t.pending = append(t.pending, e)
}

// lookupIn finds an action definition by context and name.
func (m *Map) lookupIn(ctx, name string) *actionDef {
	if c := m.contexts[ctx]; c != nil {
		return c.index[name]
	}
	return nil
}

// Pulse queues a press of name for the next Tick: a command from the page,
// such as a panel button, that the simulation reads with the presses of the
// devices. It needs no binding, and a pulse never releases an action that a
// device holds. Pressed does not report it: read it with Pulsed or Edges.
func (t *Ticks) Pulse(name string) {
	t.record(Edge{Name: normalize(name), Kind: PulseEdge, At: t.m.now})
}

// Tick starts one fixed step: the edges kept since the last Tick become
// this step's, and the buffer starts empty again.
func (t *Ticks) Tick() {
	t.current, t.pending = t.pending, t.current[:0]
	t.ticked = true
}

// Clear drops the edges kept and this step's: at a snap, a whistle, a
// dialog or a lost device, so a press from before does not act after.
// Held actions stay held (Down reads the Map); a game that acts on a
// release keeps its own record of which presses it saw, and forgets it here.
func (t *Ticks) Clear() {
	t.pending = t.pending[:0]
	t.current = t.current[:0]
}

// Edges are this step's edges, in order. The slice is valid until the next
// Tick or Clear.
func (t *Ticks) Edges() []Edge { return t.current }

// Dropped counts the edges lost to TickOptions.Limit.
func (t *Ticks) Dropped() uint64 { return t.dropped }

// Pending reports whether edges wait for the next Tick.
func (t *Ticks) Pending() bool { return len(t.pending) > 0 }

// State sums up this step for one action. Pressed, Released, HoldStarted,
// Repeated, DoubleTapped mean "at least once in the step"; Tapped, Canceled
// and a released HeldFor are the last release's. Down, Value, Holding and a
// held HeldFor are the Map's levels now. A pulse is not in it: see Pulsed.
func (t *Ticks) State(name string) State {
	name = normalize(name)
	live := t.m.State(name)
	st := State{Down: live.Down, Value: live.Value, Holding: live.Holding, HeldFor: live.HeldFor}
	released := false
	for i := range t.current {
		e := &t.current[i]
		if e.Name != name {
			continue
		}
		switch e.Kind {
		case PressEdge:
			st.Pressed, st.Repeated = true, true
			st.DoubleTapped = st.DoubleTapped || e.DoubleTapped
		case ReleaseEdge:
			st.Released, released = true, true
			st.Tapped, st.Canceled, st.HeldFor = e.Tapped, e.Canceled, e.HeldFor
		case HoldEdge:
			st.HoldStarted = true
		case RepeatEdge:
			st.Repeated = true
		}
	}
	if released && st.Down {
		// Released and pressed again within the step: the level is the new press's.
		st.HeldFor = live.HeldFor
	}
	return st
}

// Pressed reports a press of the action in this step.
func (t *Ticks) Pressed(name string) bool { return t.State(name).Pressed }

// Released reports a release of the action in this step.
func (t *Ticks) Released(name string) bool { return t.State(name).Released }

// Down reports whether the action is held now.
func (t *Ticks) Down(name string) bool { return t.m.Down(name) }

// Value is the action's level now.
func (t *Ticks) Value(name string) float64 { return t.m.Value(name) }

// Pulsed reports a Pulse of name in this step.
func (t *Ticks) Pulsed(name string) bool {
	name = normalize(name)
	for i := range t.current {
		if t.current[i].Kind == PulseEdge && t.current[i].Name == name {
			return true
		}
	}
	return false
}

// Direction is where a flick points.
type Direction uint8

const (
	DirNone Direction = iota
	DirLeft
	DirRight
	DirUp
	DirDown
)

// String names the direction: "left", "right", "up", "down" or "".
func (d Direction) String() string {
	return [...]string{"", "left", "right", "up", "down"}[d]
}

// Flick turns a stick into one-shot directional presses: pushed past Fire it
// fires once in the direction it points most, and it fires again only after
// it has come back inside Rearm. Y grows downward, as on a gamepad. Feed it
// once per frame; send what it fires to a Ticks with Pulse when a
// simulation step reads it.
type Flick struct {
	// Fire and Rearm are the push, from 0 to 1, that fires and that rearms.
	// Zero selects 0.72 and 0.38.
	Fire, Rearm float64
	fired       bool
}

// Update reads the stick for one frame.
func (f *Flick) Update(x, y float64) (Direction, bool) {
	fire, rearm := f.Fire, f.Rearm
	if fire <= 0 {
		fire = .72
	}
	if rearm <= 0 {
		rearm = .38
	}
	strength := math.Max(math.Abs(x), math.Abs(y))
	if strength < rearm {
		f.fired = false
	}
	if strength <= fire || f.fired {
		return DirNone, false
	}
	f.fired = true
	switch {
	case math.Abs(x) > math.Abs(y) && x < 0:
		return DirLeft, true
	case math.Abs(x) > math.Abs(y):
		return DirRight, true
	case y < 0:
		return DirUp, true
	}
	return DirDown, true
}

// Suppress treats the stick as fired: a stick held past Fire at a screen
// change fires again only after it comes back inside Rearm.
func (f *Flick) Suppress() { f.fired = true }
