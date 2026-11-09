package host

import (
	"encoding/binary"
	"math"
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
	// Connected false when it went away. In an event that ForwardedInput
	// decodes from the binary bridge, Buttons and Axes are reused buffers,
	// valid until the handler returns: copy them to keep them.
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
	subs    []subscription // copy-on-write, in Subscribe order
	nextSub int
	close   func() // set by the browser binding
}

type subscription struct {
	id int
	fn func(InputEvent)
}

func newForwardedInput() *ForwardedInput {
	return &ForwardedInput{keys: map[string]bool{}, pads: map[int]GamepadState{}}
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
			// Reuse the slot's buffers: Gamepads hands out copies.
			pad := f.pads[e.PadIndex]
			pad.Index, pad.ID, pad.Mapping = e.PadIndex, e.PadID, e.Mapping
			pad.Buttons = append(pad.Buttons[:0], e.Buttons...)
			pad.Axes = append(pad.Axes[:0], e.Axes...)
			f.pads[e.PadIndex] = pad
		} else {
			delete(f.pads, e.PadIndex)
		}
	case "blur":
		clear(f.keys)
		clear(f.pads)
		f.pointer.Mask = 0
	}
	subs := f.subs
	f.mu.Unlock()
	for _, sub := range subs {
		sub.fn(e)
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
	// Copy on write: Apply iterates the slice it read without the lock.
	f.subs = append(f.subs[:len(f.subs):len(f.subs)], subscription{id: id, fn: handler})
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for i, sub := range f.subs {
			if sub.id == id {
				next := make([]subscription, 0, len(f.subs)-1)
				f.subs = append(append(next, f.subs[:i]...), f.subs[i+1:]...)
				return
			}
		}
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

// Binary input records. The engine worker bridge (__gosx.worker.onInputBytes)
// sends one record per forwarded event, so the worker does not stringify and
// Go does not parse JSON for every key press, pointer move and pad change.
// All numbers are little endian; the layout is shared with
// client/runtime/host/engine-worker.ts (encodeInputRecord):
//
//	0   u8   version (1)
//	1   u8   kind: 1 key, 2 pointer, 3 pad, 4 blur
//	2   u8   flags: bit 0 down, bit 1 connected
//	3   u8   reserved
//	4   i32  pointer button mask
//	8   i32  pointer id
//	12  i32  pad index
//	16  f64  x
//	24  f64  y
//	32  f64  ts   (page epoch ms)
//	40  f64  at   (worker performance.now ms)
//	48  f64  recv (worker performance.now ms)
//	56  u16  button count, u16 axis count
//	60  u16  byte lengths of code, key, pad id, mapping, pointer type
//	70  u16  reserved
//	72  f64  buttons, then axes
//	    then the UTF-8 bytes of code, key, pad id, mapping and pointer type
const (
	inputRecordVersion = 1
	inputRecordHeader  = 72
)

var inputKinds = [...]string{1: "key", 2: "pointer", 3: "pad", 4: "blur"}

// maxInternedInput bounds inputDecoder.strings, so distinct strings cannot
// grow it without limit.
const maxInternedInput = 256

// inputDecoder decodes binary input records into one reused InputEvent. Key
// codes, pad ids and the like repeat, so it interns them: a known string costs
// a map lookup, not an allocation.
type inputDecoder struct {
	event   InputEvent
	buttons []float64
	axes    []float64
	strings map[string]string
}

func (d *inputDecoder) intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if s, ok := d.strings[string(b)]; ok {
		return s
	}
	s := string(b)
	if d.strings == nil {
		d.strings = make(map[string]string)
	}
	if len(d.strings) < maxInternedInput {
		d.strings[s] = s
	}
	return s
}

// decode parses one record. It returns false for a record it cannot read; the
// event is then unchanged.
func (d *inputDecoder) decode(b []byte) (*InputEvent, bool) {
	if len(b) < inputRecordHeader || b[0] != inputRecordVersion {
		return nil, false
	}
	kind := int(b[1])
	if kind <= 0 || kind >= len(inputKinds) {
		return nil, false
	}
	le := binary.LittleEndian
	nb, na := int(le.Uint16(b[56:])), int(le.Uint16(b[58:]))
	var lens [5]int
	size := inputRecordHeader + 8*(nb+na)
	for i := range lens {
		lens[i] = int(le.Uint16(b[60+2*i:]))
		size += lens[i]
	}
	if len(b) < size {
		return nil, false
	}
	f64 := func(at int) float64 { return math.Float64frombits(le.Uint64(b[at:])) }
	d.buttons = d.buttons[:0]
	d.axes = d.axes[:0]
	at := inputRecordHeader
	for i := 0; i < nb; i++ {
		d.buttons = append(d.buttons, f64(at))
		at += 8
	}
	for i := 0; i < na; i++ {
		d.axes = append(d.axes, f64(at))
		at += 8
	}
	var text [5]string
	for i, n := range lens {
		text[i] = d.intern(b[at : at+n])
		at += n
	}
	e := &d.event
	*e = InputEvent{
		Kind:        inputKinds[kind],
		Code:        text[0],
		Key:         text[1],
		Down:        b[2]&1 != 0,
		PointerType: text[4],
		X:           f64(16),
		Y:           f64(24),
		Mask:        int(int32(le.Uint32(b[4:]))),
		PointerID:   int(int32(le.Uint32(b[8:]))),
		PadIndex:    int(int32(le.Uint32(b[12:]))),
		PadID:       text[2],
		Mapping:     text[3],
		Connected:   b[2]&2 != 0,
		PageEpochMS: f64(32),
		AtMS:        f64(40),
		ReceivedMS:  f64(48),
	}
	if kind == 3 {
		e.Buttons, e.Axes = d.buttons, d.axes
	}
	return e, true
}
