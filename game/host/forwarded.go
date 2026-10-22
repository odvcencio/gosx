package host

import (
	"sort"
	"sync"
)

// InputEvent is one page input event forwarded to a game running in an engine
// worker (engine.Config Thread: worker). The page samples input because events
// and navigator.getGamepads exist only there; the worker receives each event
// with timestamps so a rollback game can place it on its own clock.
type InputEvent struct {
	// Kind is "key", "pointer", "pad" or "blur". A "blur" event means the page
	// lost focus or was hidden: every held key, button and pad is released.
	Kind string `json:"k"`

	// Key events.
	Code string `json:"code,omitempty"` // KeyboardEvent.code, for example "KeyA"
	Key  string `json:"key,omitempty"`  // KeyboardEvent.key, for example "a"
	Down bool   `json:"down,omitempty"`

	// Pointer events: PointerType is "down", "move" or "up". Mask is the
	// PointerEvent.buttons bit mask.
	PointerType string  `json:"type,omitempty"`
	X           float64 `json:"x,omitempty"`
	Y           float64 `json:"y,omitempty"`
	Mask        int     `json:"mask,omitempty"`
	PointerID   int     `json:"pid,omitempty"`

	// Pad events carry the pad's complete state when it changed, or
	// Connected false when it went away.
	PadIndex  int       `json:"index,omitempty"`
	PadID     string    `json:"id,omitempty"`
	Mapping   string    `json:"mapping,omitempty"`
	Connected bool      `json:"connected,omitempty"`
	Buttons   []float64 `json:"buttons,omitempty"`
	Axes      []float64 `json:"axes,omitempty"`

	// PageEpochMS is the page's timestamp for the event in epoch milliseconds.
	PageEpochMS float64 `json:"ts"`
	// AtMS is when the event happened and ReceivedMS is when the worker got
	// it, both in the worker's performance.now() milliseconds.
	AtMS       float64 `json:"at"`
	ReceivedMS float64 `json:"recv"`
}

// Delay is how long the event took to reach the worker, in milliseconds.
func (e InputEvent) Delay() float64 { return e.ReceivedMS - e.AtMS }

// PointerState is the last forwarded pointer position and held buttons.
type PointerState struct {
	X, Y float64
	Mask int
}

// ForwardedInput tracks the input state a worker has been sent and lets the
// game react to each timestamped event. It implements GamepadSource, so
// game/gamepad.Poller works in a worker as it does on the main thread. The zero
// value is not usable; construct one with NewForwardedInput (js/wasm).
type ForwardedInput struct {
	mu      sync.Mutex
	keys    map[string]bool
	pads    map[int]GamepadState
	pointer PointerState
	subs    map[int]func(InputEvent)
	nextSub int
	close   func() // set by the browser binding
}

func newForwardedInput() *ForwardedInput {
	return &ForwardedInput{keys: map[string]bool{}, pads: map[int]GamepadState{}, subs: map[int]func(InputEvent){}}
}

// Apply updates the tracked state with e, then calls every subscriber. The
// browser binding calls it for each forwarded event; tests call it directly.
func (f *ForwardedInput) Apply(e InputEvent) {
	f.mu.Lock()
	switch e.Kind {
	case "key":
		if e.Down {
			f.keys[e.Code] = true
		} else {
			delete(f.keys, e.Code)
		}
	case "pointer":
		f.pointer = PointerState{X: e.X, Y: e.Y, Mask: e.Mask}
	case "pad":
		if e.Connected {
			f.pads[e.PadIndex] = GamepadState{
				Index: e.PadIndex, ID: e.PadID, Mapping: e.Mapping,
				Buttons: append([]float64(nil), e.Buttons...), Axes: append([]float64(nil), e.Axes...),
			}
		} else {
			delete(f.pads, e.PadIndex)
		}
	case "blur":
		clear(f.keys)
		clear(f.pads)
		f.pointer.Mask = 0
	}
	subs := make([]func(InputEvent), 0, len(f.subs))
	ids := make([]int, 0, len(f.subs))
	for id := range f.subs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		subs = append(subs, f.subs[id])
	}
	f.mu.Unlock()
	for _, fn := range subs {
		fn(e)
	}
}

// Subscribe calls handler for every forwarded event, in arrival order, and
// returns a function that stops it.
func (f *ForwardedInput) Subscribe(handler func(InputEvent)) (cancel func()) {
	if handler == nil {
		return func() {}
	}
	f.mu.Lock()
	id := f.nextSub
	f.nextSub++
	f.subs[id] = handler
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
	}
}

// KeyDown reports whether the key with this KeyboardEvent.code is held.
func (f *ForwardedInput) KeyDown(code string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[code]
}

// Pointer returns the last forwarded pointer state.
func (f *ForwardedInput) Pointer() PointerState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pointer
}

// Gamepads implements GamepadSource with the pads the page last reported,
// ordered by slot. The returned snapshots do not alias internal state.
func (f *ForwardedInput) Gamepads() []GamepadState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]GamepadState, 0, len(f.pads))
	for _, pad := range f.pads {
		pad.Buttons = append([]float64(nil), pad.Buttons...)
		pad.Axes = append([]float64(nil), pad.Axes...)
		out = append(out, pad)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}
