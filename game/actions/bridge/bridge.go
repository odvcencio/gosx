// Package bridge publishes action state from package actions to a game.Input,
// so systems that read game.Input see the same actions.
package bridge

import (
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/actions"
)

// Publisher sends changes in a Map's action states to a game.Input.
type Publisher struct {
	names []string
	last  []actions.State
}

// New creates a Publisher for the named actions. Only these actions are
// published.
func New(names ...string) *Publisher {
	return &Publisher{names: append([]string(nil), names...), last: make([]actions.State, len(names))}
}

// Publish compares each action with the previous call and applies the change
// to in: EventActionDown on press, EventActionUp on release, and
// EventActionValue when an axis value changes while held. Call it after
// Map.Update and before game.Input is read. playerID is stamped on each event.
func (p *Publisher) Publish(m *actions.Map, in *game.Input, playerID string) {
	if p == nil || m == nil || in == nil {
		return
	}
	for i, name := range p.names {
		st := m.State(name)
		prev := p.last[i]
		switch {
		case st.Down && !prev.Down && st.Value < 0:
			// A negative axis keeps its sign: EventActionDown would store 1.
			in.Apply(game.InputEvent{Kind: game.EventActionValue, Action: name, Value: st.Value, PlayerID: playerID})
		case st.Pressed || (st.Down && !prev.Down):
			in.Apply(game.InputEvent{Kind: game.EventActionDown, Action: name, Value: nonZero(st.Value), PlayerID: playerID})
		case st.Released || (!st.Down && prev.Down):
			in.Apply(game.InputEvent{Kind: game.EventActionUp, Action: name, PlayerID: playerID})
		case st.Down && st.Value != prev.Value:
			in.Apply(game.InputEvent{Kind: game.EventActionValue, Action: name, Value: st.Value, PlayerID: playerID})
		}
		p.last[i] = st
	}
}

func nonZero(v float64) float64 {
	if v == 0 {
		return 1
	}
	if v < 0 {
		return -v
	}
	return v
}
