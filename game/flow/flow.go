// Package flow runs a game's top-level states (boot, title, lobby, play,
// pause, results) as a stack, and ties the stack to the simulation pause
// flag and to the input context stack.
//
// A state is pushed over another one (play, then pause over it), replaced
// (title to lobby), or popped. The state below a pushed state is suspended
// and gets resumed when the one above pops. Flow calls each hook in a fixed
// order and never re-enters: a transition requested from inside a hook runs
// after the current one finishes.
//
// Flow has no browser dependency. Call Blur and Hidden from window events,
// or use the js/wasm AttachBrowser helper.
package flow

import (
	"errors"
	"fmt"
	"time"
)

// Conventional state names. Any non-empty string works.
const (
	Boot    = "boot"
	Title   = "title"
	Lobby   = "lobby"
	Play    = "play"
	Pause   = "pause"
	Results = "results"
)

// Pauser is the simulation pause switch. *game.Runtime satisfies it.
type Pauser interface{ SetPaused(bool) }

// InputStack is the input context stack. *actions.Map satisfies it.
type InputStack interface{ SetStack(names ...string) }

// State describes one state. All hooks are optional.
type State struct {
	Name string
	// Input names the input context active while this state is on the stack.
	// Empty means the state contributes no context.
	Input string
	// Pause pauses the simulation while this state is anywhere on the stack.
	Pause bool
	// PauseOnBlur makes Blur and Hidden push Config.PauseState while this
	// state is the top of the stack (set it on play).
	PauseOnBlur bool
	// Enter runs when the state becomes part of the stack. from is the name of
	// the state that was on top before, or "".
	Enter func(f *Flow, from string)
	// Exit runs when the state leaves the stack. to is the name that is on top
	// after the transition, or "".
	Exit func(f *Flow, to string)
	// Suspend runs when another state is pushed over this one.
	Suspend func(f *Flow)
	// Resume runs when the state above this one is popped.
	Resume func(f *Flow)
	// Update runs once per Flow.Update while the state is on top.
	Update func(f *Flow, dt time.Duration)
}

// Config wires a Flow to the rest of the game. Every field is optional.
type Config struct {
	Pauser Pauser
	Input  InputStack
	// PauseState is the state Blur and Hidden push. Default Pause.
	PauseState string
	// OnChange is called after each completed transition with the stack, bottom first.
	OnChange func(stack []string)
}

// Errors returned by Flow methods.
var (
	ErrUnknownState = errors.New("flow: unknown state")
	ErrEmptyStack   = errors.New("flow: nothing to pop")
)

type opKind uint8

const (
	opPush opKind = iota
	opPop
	opReplace
	opReset
)

type op struct {
	kind opKind
	name string
}

// Flow is the state stack. It is not safe for concurrent use.
type Flow struct {
	cfg     Config
	states  map[string]*State
	stack   []*State
	queue   []op
	running bool
	hidden  bool
	err     error
}

// New creates a Flow with states registered. Later states with the same name
// replace earlier ones.
func New(cfg Config, states ...State) *Flow {
	f := &Flow{cfg: cfg, states: map[string]*State{}}
	if f.cfg.PauseState == "" {
		f.cfg.PauseState = Pause
	}
	for i := range states {
		f.Register(states[i])
	}
	return f
}

// Register adds or replaces a state definition. It does not change the stack.
func (f *Flow) Register(s State) {
	cp := s
	f.states[s.Name] = &cp
}

// Push puts the named state on top of the stack; the previous top is suspended.
func (f *Flow) Push(name string) error { return f.do(op{opPush, name}) }

// Pop removes the top state; the one below resumes.
func (f *Flow) Pop() error { return f.do(op{kind: opPop}) }

// Replace swaps the top state for the named one. The old state exits; the
// states below stay as they are.
func (f *Flow) Replace(name string) error { return f.do(op{opReplace, name}) }

// Reset exits every state, top first, then enters the named one on an empty stack.
func (f *Flow) Reset(name string) error { return f.do(op{opReset, name}) }

func (f *Flow) do(o op) error {
	if (o.kind == opPush || o.kind == opReplace || o.kind == opReset) && f.states[o.name] == nil {
		return fmt.Errorf("%w: %q", ErrUnknownState, o.name)
	}
	if o.kind == opPop && len(f.stack) == 0 && len(f.queue) == 0 {
		return ErrEmptyStack
	}
	f.queue = append(f.queue, o)
	if f.running {
		return nil
	}
	f.running = true
	defer func() { f.running = false }()
	for len(f.queue) > 0 {
		next := f.queue[0]
		f.queue = f.queue[1:]
		f.apply(next)
	}
	err := f.err
	f.err = nil
	return err
}

func (f *Flow) top() *State {
	if len(f.stack) == 0 {
		return nil
	}
	return f.stack[len(f.stack)-1]
}

func nameOf(s *State) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func (f *Flow) apply(o op) {
	prev := nameOf(f.top())
	switch o.kind {
	case opPush:
		if t := f.top(); t != nil && t.Suspend != nil {
			t.Suspend(f)
		}
		s := f.states[o.name]
		f.stack = append(f.stack, s)
		f.sync()
		if s.Enter != nil {
			s.Enter(f, prev)
		}
	case opPop:
		if len(f.stack) == 0 {
			f.err = ErrEmptyStack
			return
		}
		s := f.stack[len(f.stack)-1]
		f.stack = f.stack[:len(f.stack)-1]
		f.sync()
		if s.Exit != nil {
			s.Exit(f, nameOf(f.top()))
		}
		if t := f.top(); t != nil && t.Resume != nil {
			t.Resume(f)
		}
	case opReplace:
		var old *State
		if len(f.stack) > 0 {
			old = f.stack[len(f.stack)-1]
			f.stack = f.stack[:len(f.stack)-1]
		}
		s := f.states[o.name]
		if old != nil && old.Exit != nil {
			old.Exit(f, o.name)
		}
		f.stack = append(f.stack, s)
		f.sync()
		if s.Enter != nil {
			s.Enter(f, prev)
		}
	case opReset:
		for len(f.stack) > 0 {
			old := f.stack[len(f.stack)-1]
			f.stack = f.stack[:len(f.stack)-1]
			next := o.name
			if len(f.stack) > 0 {
				next = f.stack[len(f.stack)-1].Name
			}
			if old.Exit != nil {
				old.Exit(f, next)
			}
		}
		s := f.states[o.name]
		f.stack = append(f.stack, s)
		f.sync()
		if s.Enter != nil {
			s.Enter(f, prev)
		}
	}
	f.sync()
	if f.cfg.OnChange != nil {
		f.cfg.OnChange(f.Stack())
	}
}

// sync pushes the pause flag and the input contexts to their targets.
func (f *Flow) sync() {
	if f.cfg.Pauser != nil {
		f.cfg.Pauser.SetPaused(f.Paused())
	}
	if f.cfg.Input != nil {
		var names []string
		for _, s := range f.stack {
			if s.Input != "" {
				names = append(names, s.Input)
			}
		}
		f.cfg.Input.SetStack(names...)
	}
}

// Paused reports whether the simulation should be paused: some state on the
// stack asks for it, or the page is hidden.
func (f *Flow) Paused() bool {
	if f.hidden {
		return true
	}
	for _, s := range f.stack {
		if s.Pause {
			return true
		}
	}
	return false
}

// Current returns the top state's name, or "".
func (f *Flow) Current() string { return nameOf(f.top()) }

// Is reports whether name is the top state.
func (f *Flow) Is(name string) bool { return f.Current() == name }

// Has reports whether name is anywhere on the stack.
func (f *Flow) Has(name string) bool {
	for _, s := range f.stack {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Stack returns the state names, bottom first.
func (f *Flow) Stack() []string {
	out := make([]string, len(f.stack))
	for i, s := range f.stack {
		out[i] = s.Name
	}
	return out
}

// Update calls the top state's Update hook.
func (f *Flow) Update(dt time.Duration) {
	if t := f.top(); t != nil && t.Update != nil {
		t.Update(f, dt)
	}
}

// Blur reports that the window lost focus. If the top state has PauseOnBlur
// the pause state is pushed, and the player must resume it.
func (f *Flow) Blur() {
	if t := f.top(); t != nil && t.PauseOnBlur && f.states[f.cfg.PauseState] != nil && !f.Has(f.cfg.PauseState) {
		_ = f.Push(f.cfg.PauseState)
	}
}

// Hidden reports that the tab was hidden or shown. While hidden the
// simulation is paused whatever the stack says, and a hide also acts like
// Blur. A tab that becomes visible again leaves the pause state in place.
func (f *Flow) Hidden(hidden bool) {
	if hidden == f.hidden {
		return
	}
	f.hidden = hidden
	if hidden {
		f.Blur()
	}
	f.sync()
}
