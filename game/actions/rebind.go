package actions

import (
	"errors"
	"fmt"
)

// ConflictPolicy says what Rebind does when the new source is already bound.
type ConflictPolicy uint8

const (
	// ConflictReject changes nothing and returns ErrConflict.
	ConflictReject ConflictPolicy = iota
	// ConflictSwap gives the other binding the source this slot had before.
	// If this slot was unbound, the other binding becomes unbound.
	ConflictSwap
	// ConflictUnbind removes the source from the other binding.
	ConflictUnbind
)

// Conflict names a binding that already uses a source.
type Conflict struct {
	Context string
	Action  string
	Slot    int
}

var (
	// ErrConflict is returned by Rebind under ConflictReject.
	ErrConflict = errors.New("actions: binding conflict")
	// ErrUnknownAction means the context or action is not defined.
	ErrUnknownAction = errors.New("actions: unknown action")
	// ErrBadSlot means the slot is outside the action's binding list.
	ErrBadSlot = errors.New("actions: binding slot out of range")
	// ErrWrongClass means the source is on a different device family than
	// the slot it would replace (a key cannot replace a gamepad button).
	ErrWrongClass = errors.New("actions: source device does not match slot")
)

func (m *Map) def(ctx, action string) *actionDef {
	c := m.contexts[ctx]
	if c == nil {
		return nil
	}
	return c.index[action]
}

// Bindings returns a copy of the action's bindings.
func (m *Map) Bindings(ctx, action string) []Binding {
	d := m.def(ctx, action)
	if d == nil {
		return nil
	}
	return append([]Binding(nil), d.Bindings...)
}

// Contexts returns the defined context names in definition order.
func (m *Map) Contexts() []string { return append([]string(nil), m.order...) }

// Actions returns the action names of a context in definition order.
func (m *Map) Actions(ctx string) []string {
	c := m.contexts[ctx]
	if c == nil {
		return nil
	}
	out := make([]string, len(c.defs))
	for i, d := range c.defs {
		out[i] = d.Name
	}
	return out
}

func sign(f float64) int {
	if f < 0 {
		return -1
	}
	return 1
}

func sameBinding(a, b Binding) bool {
	if a.Source != b.Source {
		return false
	}
	if a.Source.Kind == KindPadAxis {
		return sign(a.scale()) == sign(b.scale())
	}
	return true
}

func (m *Map) inScope(ctx *context, other *context) bool {
	if ctx == other {
		return true
	}
	return ctx.Group != "" && ctx.Group == other.Group
}

// Conflicts lists the other bindings, in the same context or group, that use
// the source a rebind of (ctx, action, slot) would use. The binding being
// replaced is not a conflict.
func (m *Map) Conflicts(ctx, action string, slot int, src Source) []Conflict {
	d := m.def(ctx, action)
	if d == nil || slot < 0 || slot >= len(d.Bindings) {
		return nil
	}
	probe := Binding{Source: src, Scale: d.Bindings[slot].Scale}
	self := m.contexts[ctx]
	var out []Conflict
	for _, name := range m.order {
		c := m.contexts[name]
		if !m.inScope(self, c) {
			continue
		}
		for _, o := range c.defs {
			for i, b := range o.Bindings {
				if o == d && i == slot {
					continue
				}
				if sameBinding(b, probe) {
					out = append(out, Conflict{Context: name, Action: o.Name, Slot: i})
				}
			}
		}
	}
	return out
}

// Rebind replaces binding slot of an action with src, keeping the slot's
// Scale. The slot must exist; define spare slots with a zero Source (an
// unbound slot) when a player should be able to add a second key. It returns the conflicts found; with ConflictReject it changes nothing
// and returns ErrConflict when there are any.
func (m *Map) Rebind(ctx, action string, slot int, src Source, policy ConflictPolicy) ([]Conflict, error) {
	d := m.def(ctx, action)
	if d == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnknownAction, ctx, action)
	}
	if slot < 0 || slot >= len(d.Bindings) {
		return nil, ErrBadSlot
	}
	if src.Zero() {
		return nil, ErrBadSource
	}
	old := d.Bindings[slot]
	if !old.Source.Zero() && old.Source.Class() != src.Class() {
		return nil, ErrWrongClass
	}
	conflicts := m.Conflicts(ctx, action, slot, src)
	if len(conflicts) > 0 && policy == ConflictReject {
		return conflicts, ErrConflict
	}
	for _, c := range conflicts {
		o := m.def(c.Context, c.Action)
		switch policy {
		case ConflictSwap:
			o.Bindings[c.Slot].Source = old.Source
		case ConflictUnbind:
			o.Bindings[c.Slot].Source = Source{}
		}
	}
	d.Bindings[slot].Source = src
	return conflicts, nil
}

// Unbind clears a slot. The slot stays in the list so a rebinding screen can
// still show it.
func (m *Map) Unbind(ctx, action string, slot int) error {
	d := m.def(ctx, action)
	if d == nil {
		return ErrUnknownAction
	}
	if slot < 0 || slot >= len(d.Bindings) {
		return ErrBadSlot
	}
	d.Bindings[slot].Source = Source{}
	return nil
}

// Reset restores one action's bindings to those given to Define.
func (m *Map) Reset(ctx, action string) {
	if d := m.def(ctx, action); d != nil {
		d.Bindings = append(d.Bindings[:0], d.defaults...)
	}
}

// ResetAll restores every action's bindings to the defaults.
func (m *Map) ResetAll() {
	for _, c := range m.contexts {
		for _, d := range c.defs {
			d.Bindings = append(d.Bindings[:0], d.defaults...)
		}
	}
}
