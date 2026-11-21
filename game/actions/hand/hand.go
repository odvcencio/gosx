// Package hand tracks which device the player has in hand: the keyboard, a
// gamepad or the touch screen. Screens name controls for that device ("Press
// A", "Press Space", nothing on a touch screen), so they change as soon as
// the player picks up another one.
//
// The last device used holds the hand. A Tracker is an actions.Sink: tee
// every input event into it, call Pads with each gamepad poll, and call Frame
// once per drawn frame. It reads no clock and keeps no browser state, so a
// native test drives it event by event. The js/wasm AttachPointers function
// reports fingers, pens and the mouse from the page's pointer events.
//
// A device takes the hand only on a deliberate use: a key going down, a pad
// button going down, a stick pushed across the takeover line from inside it,
// a finger on the screen, and (when the Rules allow) a mouse click or a
// mouse moved far enough in one frame. A stick that rests past the line, by
// drift or because the player left it held while reaching for the mouse,
// never takes the hand back: only the crossing counts, and the stick must
// come back well inside the line before it can cross again.
package hand

import (
	"math"
	"strconv"
	"strings"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

// Device is the kind of device in hand.
type Device uint8

const (
	// Keys is the keyboard, with or without a mouse.
	Keys Device = iota
	// Pad is a gamepad.
	Pad
	// Touch is the touch screen.
	Touch
)

// Hand is the device in the player's hand. The zero Hand is the keyboard.
type Hand struct {
	Device Device
	// Slot is the gamepad's slot in the browser's list, or -1 for a gamepad
	// named by its style only (Named, a remembered hand). Meaningful only
	// for a Pad.
	Slot int
	// ID is what the browser's id says about the gamepad: its maker and
	// whose button names it wears. A Pad with no known id wears the
	// standard names (A, B, X, Y).
	ID actions.PadIdentity
}

// Style is whose button names the hand's gamepad wears.
func (h Hand) Style() actions.PadStyle {
	if h.Device != Pad {
		return actions.PadGeneric
	}
	return h.ID.Style
}

// Name is the hand's name as a page carries it, for a stylesheet and for
// browser checks: "keys", "touch", or for a gamepad its style's name, "pad",
// "xbox", "playstation" or "nintendo".
func (h Hand) Name() string {
	switch h.Device {
	case Touch:
		return "touch"
	case Pad:
		return actions.StyleName(h.ID.Style)
	}
	return "keys"
}

// Same reports whether two hands name controls the same way: the same
// device and, for a gamepad, the same style. A screen redraws its prompts
// only when the hand it names changes in this sense.
func (h Hand) Same(o Hand) bool {
	return h.Device == o.Device && h.Style() == o.Style()
}

// Named is the hand a page name stands for, the reverse of Name. A gamepad
// named by its style has no slot and no maker.
func Named(name string) (Hand, bool) {
	switch name {
	case "keys":
		return Hand{}, true
	case "touch":
		return Hand{Device: Touch}, true
	case "pad":
		return Hand{Device: Pad, Slot: -1}, true
	case "xbox":
		return Hand{Device: Pad, Slot: -1, ID: actions.PadIdentity{Style: actions.PadXbox}}, true
	case "playstation":
		return Hand{Device: Pad, Slot: -1, ID: actions.PadIdentity{Style: actions.PadPlayStation}}, true
	case "nintendo":
		return Hand{Device: Pad, Slot: -1, ID: actions.PadIdentity{Style: actions.PadNintendo}}, true
	}
	return Hand{}, false
}

// Start is the hand before the player has used anything: the first gamepad
// the browser lists, else the hand used last time (its Name, remembered by
// the game), else the touch screen when it is the only pointer (a phone or a
// tablet), else the keyboard. A browser lists a gamepad only after one of
// its buttons was pressed while the page was open, so until then the
// remembered hand stands in.
func Start(pads []host.GamepadState, remembered string, touchOnly bool) Hand {
	if len(pads) > 0 {
		return Hand{Device: Pad, Slot: pads[0].Index, ID: actions.ParsePadID(pads[0].ID)}
	}
	if h, ok := Named(remembered); ok {
		return h
	}
	if touchOnly {
		return Hand{Device: Touch}
	}
	return Hand{}
}

// Rules say what takes the hand.
type Rules struct {
	// StickTakeover is how far, from 0 to 1, a stick must be pushed to put
	// its gamepad in hand, measured on the two axes together after the pad
	// poller's deadzone. Zero selects 0.5. The stick must then come back
	// inside 80 percent of it before it can take the hand again.
	StickTakeover float64
	// MouseMotion is how far, in CSS pixels, the mouse must move within one
	// frame to put the keyboard in hand. Zero means mouse motion never takes
	// the hand.
	MouseMotion float64
	// MouseButtons makes a mouse click put the keyboard in hand.
	MouseButtons bool
	// Ignore, if set, is asked about each key, gamepad button and stick
	// event; returning true leaves the hand alone. A game passes the sources
	// it binds nothing to, so a browser shortcut or a modifier key does not
	// change the screens' prompts.
	Ignore func(ev actions.Event) bool
}

// PointerKind is the kind of pointer that went down on the page.
type PointerKind uint8

const (
	PointerMouse PointerKind = iota + 1
	PointerTouch
	PointerPen
)

type srcKey struct {
	device string
	src    actions.Source
}

// stick is one gamepad stick's two axes, and whether it may take the hand.
// dirty is set when an axis changed since the stick was last judged.
type stick struct {
	x, y         float64
	armed, dirty bool
	device       string
}

// Tracker follows the device in hand. It is not safe for concurrent use.
type Tracker struct {
	rules   Rules
	hand    Hand
	used    bool
	changed bool
	// armed is true for a button that may take the hand: it is up, or has
	// come back below half the press threshold since it last went down.
	armed  map[srcKey]bool
	sticks map[stickKey]*stick
	pads   map[int]actions.PadIdentity
	spare  map[int]actions.PadIdentity // the map Pads swaps in, so a poll allocates nothing
	moved  float64
}

type stickKey struct {
	device string
	pair   int
}

// NewTracker creates a tracker whose hand is the keyboard until Set or a use.
func NewTracker(r Rules) *Tracker {
	if r.StickTakeover <= 0 || r.StickTakeover > 1 {
		r.StickTakeover = .5
	}
	return &Tracker{rules: r, armed: map[srcKey]bool{}, sticks: map[stickKey]*stick{}, pads: map[int]actions.PadIdentity{}, spare: map[int]actions.PadIdentity{}}
}

// Hand is the device in hand now.
func (t *Tracker) Hand() Hand { return t.hand }

// Used reports whether the player has used any device: until then the hand
// is the one given to Set (Start's guess).
func (t *Tracker) Used() bool { return t.used }

// Set puts h in hand without a use, as the guess before the first one. It
// does nothing once the player has used a device.
func (t *Tracker) Set(h Hand) {
	if t.used || h == t.hand {
		return
	}
	t.hand, t.changed = h, true
}

func (t *Tracker) take(h Hand) {
	t.used = true
	if h != t.hand {
		t.hand, t.changed = h, true
	}
}

// Pads records the gamepads in one poll, so a gamepad that takes the hand
// carries its identity. Call it with the same list the pad poller reads,
// before the poller feeds this poll's events. A gamepad seen for the first
// time (or a new one in a slot) starts with its sticks where they rest: a
// stick that already rests past the takeover line, a worn one that drifts,
// is not a push, and takes the hand only after it has come back inside.
func (t *Tracker) Pads(pads []host.GamepadState) {
	prev := t.pads
	t.pads, t.spare = t.spare, prev
	clear(t.pads)
	for _, p := range pads {
		old, ok := prev[p.Index]
		if ok && old.Raw == p.ID {
			// The same pad as the last poll: no new parse, no new baseline.
			t.pads[p.Index] = old
			continue
		}
		id := actions.ParsePadID(p.ID)
		t.pads[p.Index] = id
		t.judgeSticks() // the stick moves of the last poll, before the new pad's baseline
		device := actions.PadDevice(p.Index)
		for pair := 0; pair < 2; pair++ {
			st := &stick{armed: true}
			if len(p.Axes) > 2*pair+1 {
				st.armed = math.Hypot(p.Axes[2*pair], p.Axes[2*pair+1]) < t.rules.StickTakeover*rearm
			}
			st.device = device
			t.sticks[stickKey{device, pair}] = st
		}
	}
	for slot := range prev {
		if _, ok := t.pads[slot]; !ok {
			device := actions.PadDevice(slot)
			delete(t.sticks, stickKey{device, 0})
			delete(t.sticks, stickKey{device, 1})
		}
	}
	if t.hand.Device == Pad {
		if id, ok := t.pads[t.hand.Slot]; ok && id != t.hand.ID {
			// The gamepad in hand was named by its style only (Set, Named),
			// or another gamepad took its slot.
			t.hand.ID, t.changed = id, true
		}
	}
}

func (t *Tracker) pad(device string) Hand {
	slot, _ := strconv.Atoi(strings.TrimPrefix(device, "pad:"))
	return Hand{Device: Pad, Slot: slot, ID: t.pads[slot]}
}

// Feed implements actions.Sink. Keys, gamepad buttons and sticks, and
// on-screen controls take the hand here. Mouse events are left to Pointer
// and MouseMoved, because a browser follows a tap with mouse events of its
// own that are not a mouse.
func (t *Tracker) Feed(ev actions.Event) {
	s := ev.Source
	if t.rules.Ignore != nil && s.Kind != actions.KindMouseMotion && s.Kind != actions.KindMouseButton && t.rules.Ignore(ev) {
		return
	}
	switch s.Kind {
	case actions.KindKey, actions.KindPadButton, actions.KindTouchButton:
		k := srcKey{ev.Device, s}
		armed, seen := t.armed[k]
		if !seen {
			armed = true
		}
		v := math.Abs(ev.Value)
		switch {
		case v >= actions.DefaultThreshold && armed:
			t.armed[k] = false
			switch s.Kind {
			case actions.KindKey:
				t.take(Hand{})
			case actions.KindPadButton:
				t.take(t.pad(ev.Device))
			default:
				t.take(Hand{Device: Touch})
			}
		case v < actions.DefaultThreshold*rearm:
			delete(t.armed, k)
		}
	case actions.KindPadAxis:
		n, err := strconv.Atoi(strings.TrimPrefix(s.Code, "axis"))
		if err != nil || n < 0 || n > 3 {
			return // a third stick or a hat: not a stick the player pushes
		}
		k := stickKey{ev.Device, n / 2}
		st := t.sticks[k]
		if st == nil {
			st = &stick{armed: true, device: ev.Device}
			t.sticks[k] = st
		}
		if n%2 == 0 {
			st.x = ev.Value
		} else {
			st.y = ev.Value
		}
		// A poll reports a stick's two axes one at a time; it is judged
		// whole, at the end of the frame.
		st.dirty = true
	case actions.KindTouchAxis:
		if ev.Value != 0 {
			t.take(Hand{Device: Touch})
		}
	}
}

// judgeSticks checks each stick that moved: pushed across the takeover line
// from inside the rearm line, it takes the hand.
func (t *Tracker) judgeSticks() {
	for k, st := range t.sticks {
		if !st.dirty {
			continue
		}
		st.dirty = false
		m := math.Hypot(st.x, st.y)
		switch {
		case m >= t.rules.StickTakeover && st.armed:
			st.armed = false
			t.take(t.pad(k.device))
		case m < t.rules.StickTakeover*rearm:
			st.armed = true
		}
	}
}

// rearm is the share of a takeover line a stick must come back inside, and
// of the press threshold a button must come back below, before either can
// take the hand again: 0.4 for a stick line of 0.5.
const rearm = .8

// ReleaseAll implements actions.Sink: the window lost the focus, and every
// key and button counts as up. The hand stays where it is.
// A gamepad keeps reporting while the window is away, so its sticks stay as
// they are.
func (t *Tracker) ReleaseAll() { clear(t.armed) }

// Pointer reports a pointer going down anywhere on the page: a finger or a
// pen puts the touch screen in hand, and a mouse click the keyboard when
// Rules.MouseButtons is set.
func (t *Tracker) Pointer(kind PointerKind) {
	switch kind {
	case PointerTouch, PointerPen:
		t.take(Hand{Device: Touch})
	case PointerMouse:
		if t.rules.MouseButtons {
			t.take(Hand{})
		}
	}
}

// MouseMoved reports mouse motion, in CSS pixels, from the page's pointer
// moves of the mouse only. When this frame's motion reaches
// Rules.MouseMotion the keyboard takes the hand, at that moment: a gamepad
// button pressed later in the same frame takes it back.
func (t *Tracker) MouseMoved(px float64) {
	if t.rules.MouseMotion <= 0 {
		return
	}
	before := t.moved
	t.moved += math.Abs(px)
	if before < t.rules.MouseMotion && t.moved >= t.rules.MouseMotion {
		t.take(Hand{})
	}
}

// Frame ends one drawn frame. It returns the hand and whether it changed
// since the last Frame. Within a frame the last use wins.
func (t *Tracker) Frame() (Hand, bool) {
	t.judgeSticks()
	t.moved = 0
	changed := t.changed
	t.changed = false
	return t.hand, changed
}
