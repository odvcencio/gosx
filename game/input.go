package game

import (
	"encoding/json"
	"strings"
)

// EventKind identifies one input event shape.
type EventKind string

const (
	EventActionDown  EventKind = "action:down"
	EventActionUp    EventKind = "action:up"
	EventActionValue EventKind = "action:value"
	EventKeyDown     EventKind = "key:down"
	EventKeyUp       EventKind = "key:up"
	EventPointerMove EventKind = "pointer:move"
	EventPointerDown EventKind = "pointer:down"
	EventPointerUp   EventKind = "pointer:up"
	EventGamepad     EventKind = "gamepad"
	EventTouch       EventKind = "touch"
)

// InputEvent is the transport-neutral input record consumed by Input.
type InputEvent struct {
	Kind     EventKind       `json:"kind"`
	Action   string          `json:"action,omitempty"`
	Code     string          `json:"code,omitempty"`
	PlayerID string          `json:"playerID,omitempty"`
	DeviceID string          `json:"deviceID,omitempty"`
	Value    float64         `json:"value,omitempty"`
	X        float64         `json:"x,omitempty"`
	Y        float64         `json:"y,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// Binding maps a low-level input event onto an action name.
type Binding struct {
	Action string    `json:"action"`
	Kind   EventKind `json:"kind"`
	Code   string    `json:"code,omitempty"`
	Scale  float64   `json:"scale,omitempty"`
}

// Key maps a keyboard code/key onto an action.
func Key(action, code string) Binding {
	return Binding{Action: action, Kind: EventKeyDown, Code: code, Scale: 1}
}

// Button maps a gamepad button code onto an action.
func Button(action, code string) Binding {
	return Binding{Action: action, Kind: EventGamepad, Code: code, Scale: 1}
}

// ActionState is the per-frame state for one logical action.
type ActionState struct {
	Down     bool    `json:"down,omitempty"`
	Pressed  bool    `json:"pressed,omitempty"`
	Released bool    `json:"released,omitempty"`
	Value    float64 `json:"value,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Source   string  `json:"source,omitempty"`
	Sequence uint64  `json:"sequence,omitempty"`
}

// Input stores current action state plus the raw events delivered this frame.
type Input struct {
	bindings []Binding
	actions  map[string]ActionState
	events   []InputEvent
	pointerX float64
	pointerY float64
	sequence uint64
}

// NewInput creates an input mapper with optional bindings.
func NewInput(bindings ...Binding) *Input {
	in := &Input{
		actions: make(map[string]ActionState),
	}
	in.Bind(bindings...)
	return in
}

// Bind appends bindings to the input map.
func (in *Input) Bind(bindings ...Binding) {
	if in == nil {
		return
	}
	for _, binding := range bindings {
		binding.Action = normalizeAction(binding.Action)
		binding.Code = normalizeCode(binding.Code)
		if binding.Action == "" {
			continue
		}
		if binding.Scale == 0 {
			binding.Scale = 1
		}
		in.bindings = append(in.bindings, binding)
	}
}

// Apply records an input event and updates mapped action state.
func (in *Input) Apply(event InputEvent) {
	if in == nil {
		return
	}
	event.Action = normalizeAction(event.Action)
	event.Code = normalizeCode(event.Code)
	in.sequence++
	in.events = append(in.events, event)
	switch event.Kind {
	case EventPointerMove, EventPointerDown, EventPointerUp, EventTouch:
		in.pointerX = event.X
		in.pointerY = event.Y
	}
	if event.Action != "" {
		switch event.Kind {
		case EventActionUp:
			in.trigger(event.Action, false, 0, event)
		case EventActionValue:
			in.value(event.Action, event.Value, event)
		default:
			value := event.Value
			if value == 0 {
				value = 1
			}
			in.trigger(event.Action, true, value, event)
		}
	}
	for _, binding := range in.bindings {
		if binding.Code != "" && binding.Code != event.Code {
			continue
		}
		switch event.Kind {
		case EventKeyDown:
			if binding.Kind == EventKeyDown {
				in.trigger(binding.Action, true, binding.Scale, event)
			}
		case EventKeyUp:
			if binding.Kind == EventKeyDown || binding.Kind == EventKeyUp {
				in.trigger(binding.Action, false, 0, event)
			}
		case EventGamepad:
			if binding.Kind == EventGamepad {
				in.value(binding.Action, event.Value*binding.Scale, event)
			}
		}
	}
}

func (in *Input) trigger(action string, down bool, value float64, event InputEvent) {
	action = normalizeAction(action)
	if action == "" {
		return
	}
	state := in.actions[action]
	if down && !state.Down {
		state.Pressed = true
	}
	if !down && state.Down {
		state.Released = true
	}
	state.Down = down
	state.Value = value
	state.X = event.X
	state.Y = event.Y
	state.Source = event.Code
	state.Sequence = in.sequence
	in.actions[action] = state
}

func (in *Input) value(action string, value float64, event InputEvent) {
	action = normalizeAction(action)
	if action == "" {
		return
	}
	state := in.actions[action]
	if value != 0 && !state.Down {
		state.Pressed = true
	}
	if value == 0 && state.Down {
		state.Released = true
	}
	state.Down = value != 0
	state.Value = value
	state.X = event.X
	state.Y = event.Y
	state.Source = event.Code
	state.Sequence = in.sequence
	in.actions[action] = state
}

// EndFrame clears edge-triggered flags and raw events while preserving held
// action state.
func (in *Input) EndFrame() {
	if in == nil {
		return
	}
	for action, state := range in.actions {
		state.Pressed = false
		state.Released = false
		if !state.Down {
			state.Value = 0
		}
		in.actions[action] = state
	}
	in.events = nil
}

// Action returns the current state for action.
func (in *Input) Action(action string) ActionState {
	if in == nil {
		return ActionState{}
	}
	return in.actions[normalizeAction(action)]
}

// Down reports whether action is currently held.
func (in *Input) Down(action string) bool {
	return in.Action(action).Down
}

// Pressed reports whether action transitioned down this frame.
func (in *Input) Pressed(action string) bool {
	return in.Action(action).Pressed
}

// Released reports whether action transitioned up this frame.
func (in *Input) Released(action string) bool {
	return in.Action(action).Released
}

// Pointer returns the latest pointer coordinates in surface-local pixels.
func (in *Input) Pointer() (x, y float64) {
	if in == nil {
		return 0, 0
	}
	return in.pointerX, in.pointerY
}

// Events returns the raw events delivered in the current frame.
func (in *Input) Events() []InputEvent {
	if in == nil || len(in.events) == 0 {
		return nil
	}
	out := make([]InputEvent, len(in.events))
	copy(out, in.events)
	return out
}

func normalizeAction(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}

func normalizeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}
