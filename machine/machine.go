// Package machine is a small, typed finite-state machine for GoSX islands.
//
// Hyper-interactive UI — a media player, a multi-step form, a drag/scrub
// surface, the galaxy scene — accumulates boolean flags and effects until the
// real control flow is invisible and "useEffect hell" sets in. A state machine
// makes the control flow the source of truth: states are explicit, transitions
// are a declared table, and impossible states are unrepresentable.
//
// A Machine's current state is backed by a signal.Signal, so a Computed or an
// island re-renders automatically when the machine transitions — no manual
// subscription wiring, no effect-as-state-sync.
//
//	const (Idle, Playing, Paused = "idle", "playing", "paused")
//	const (Play, Pause, Stop     = "play", "pause", "stop")
//
//	player := machine.New[string, string](Idle)
//	player.On(Idle, Play, Playing).
//	    On(Playing, Pause, Paused).
//	    On(Paused, Play, Playing).
//	    On(Playing, Stop, Idle, machine.Do[string](audio.Stop))
//
//	player.Send(Play)            // -> Playing, subscribers notified
//	player.Is(Playing)           // true
//	view := player.Signal()      // bind into a Computed / island
package machine

import "m31labs.dev/gosx/signal"

// Machine is a typed finite-state machine. S is the state type and E the event
// type; both must be comparable (string or int enums are the common choice).
type Machine[S comparable, E comparable] struct {
	state       *signal.Signal[S]
	transitions map[S]map[E]transition[S]
	onEnter     map[S]func(S)
	onExit      map[S]func(S)
}

type transition[S comparable] struct {
	to     S
	guard  func() bool
	action func()
}

// Option customizes a single transition declared with On.
type Option[S comparable] func(*transition[S])

// Guard attaches a predicate: the transition fires only when fn returns true.
func Guard[S comparable](fn func() bool) Option[S] {
	return func(t *transition[S]) { t.guard = fn }
}

// Do attaches a side effect that runs as part of the transition, after the exit
// hook and before the state changes.
func Do[S comparable](fn func()) Option[S] {
	return func(t *transition[S]) { t.action = fn }
}

// New creates a machine in the given initial state.
func New[S comparable, E comparable](initial S) *Machine[S, E] {
	return &Machine[S, E]{
		state:       signal.New[S](initial),
		transitions: make(map[S]map[E]transition[S]),
		onEnter:     make(map[S]func(S)),
		onExit:      make(map[S]func(S)),
	}
}

// On declares that event e in state from transitions to state to. It returns the
// machine for fluent chaining. A later On for the same (from, e) replaces the
// earlier one.
func (m *Machine[S, E]) On(from S, e E, to S, opts ...Option[S]) *Machine[S, E] {
	tr := transition[S]{to: to}
	for _, opt := range opts {
		opt(&tr)
	}
	byEvent, ok := m.transitions[from]
	if !ok {
		byEvent = make(map[E]transition[S])
		m.transitions[from] = byEvent
	}
	byEvent[e] = tr
	return m
}

// OnEnter registers a hook run whenever the machine enters state s.
func (m *Machine[S, E]) OnEnter(s S, fn func(S)) *Machine[S, E] {
	m.onEnter[s] = fn
	return m
}

// OnExit registers a hook run whenever the machine leaves state s.
func (m *Machine[S, E]) OnExit(s S, fn func(S)) *Machine[S, E] {
	m.onExit[s] = fn
	return m
}

// Send attempts event e from the current state. If a matching transition exists
// and its guard (if any) passes, Send runs the exit hook, the transition action,
// commits the new state (notifying signal subscribers), runs the entry hook, and
// returns true. Otherwise it is a no-op and returns false.
func (m *Machine[S, E]) Send(e E) bool {
	from := m.state.Get()
	tr, ok := m.transitions[from][e]
	if !ok {
		return false
	}
	if tr.guard != nil && !tr.guard() {
		return false
	}
	if exit := m.onExit[from]; exit != nil {
		exit(from)
	}
	if tr.action != nil {
		tr.action()
	}
	m.state.Set(tr.to)
	if enter := m.onEnter[tr.to]; enter != nil {
		enter(tr.to)
	}
	return true
}

// Can reports whether event e would transition from the current state right now
// (the transition exists and its guard, if any, passes).
func (m *Machine[S, E]) Can(e E) bool {
	tr, ok := m.transitions[m.state.Get()][e]
	if !ok {
		return false
	}
	return tr.guard == nil || tr.guard()
}

// State returns the current state.
func (m *Machine[S, E]) State() S { return m.state.Get() }

// Is reports whether the machine is currently in state s.
func (m *Machine[S, E]) Is(s S) bool { return m.state.Get() == s }

// Signal exposes the underlying reactive state for Computed values and island
// rendering. Treat it as read-only; drive changes through Send.
func (m *Machine[S, E]) Signal() *signal.Signal[S] { return m.state }

// Subscribe invokes fn with the new state on every transition and returns an
// unsubscribe function.
func (m *Machine[S, E]) Subscribe(fn func(S)) func() {
	return m.state.Subscribe(func() { fn(m.state.Get()) })
}
