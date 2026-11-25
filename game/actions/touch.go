package actions

import "math"

// DefaultStickRadius is the travel of an on-screen stick, from its centre to
// full deflection, when a Stick does not give one. It is in the units of the
// positions given to Touches: CSS pixels in a browser.
const DefaultStickRadius = 56

// Stick is the shape of an on-screen stick: how far a finger travels for
// full deflection, and where the stick's centre is.
type Stick struct {
	// Radius is the travel from the centre to full deflection. Zero means
	// DefaultStickRadius.
	Radius float64
	// Deadzone is the share of Radius, from 0 up to 1, inside which the
	// stick reads zero. Past it the value rises from zero, so nothing jumps.
	Deadzone float64
	// Floating puts the centre where the finger lands, so the player need
	// not look for the stick. Without it the centre is the one given to
	// StickDown, and a finger that lands off the centre deflects the stick
	// at once.
	Floating bool
	// Follow lets the centre trail a finger that goes past Radius, so that
	// pulling back the other way answers at once.
	Follow bool
}

func (s Stick) radius() float64 {
	if s.Radius > 0 {
		return s.Radius
	}
	return DefaultStickRadius
}

// StickState is where a touched stick stands.
type StickState struct {
	// Name is the stick's name.
	Name string
	// CenterX and CenterY are the stick's centre, in the units of the
	// positions given to Touches.
	CenterX, CenterY float64
	// X and Y are the deflection, each in [-1,1] and never longer than 1
	// together. Y grows downward.
	X, Y float64
}

type finger struct {
	name   string
	stick  bool
	shape  Stick
	cx, cy float64
	x, y   float64
	sx, sy Source
}

// Touches turns fingers on on-screen sticks and buttons into events for a
// Sink. A finger is a pointer id; a pen and the mouse count as fingers. Each
// finger holds one stick or one button, and several fingers can hold several
// controls at once: one thumb on a stick and the other on a button. A stick
// belongs to the first finger that takes it. A button stays down until every
// finger on it has gone.
//
// A touch ends in one of two ways. Up is the player lifting the finger.
// Cancel is the touch being taken away (the browser's pointercancel, a lost
// pointer capture, the window losing the focus): the button's release is fed
// with Event.Canceled set, so the action reads State.Canceled.
//
// Touches has no browser code. The js/wasm AttachTouch function feeds it from
// pointer events; a test or another host can call it directly. It is not safe
// for concurrent use.
type Touches struct {
	// OnEnd, if set, is called after a finger's touch has ended, however it
	// ended: with the finger, the name of the control it held, whether that
	// was a stick, and whether the touch was taken away, not lifted. A host
	// uses it to undo what it drew for the touch.
	OnEnd func(pointer int, name string, stick, canceled bool)

	sink    Sink
	fingers map[int]*finger
	sticks  map[string]int
	holds   map[string]int
	free    []*finger
	drop    []int
}

// NewTouches creates a tracker that feeds sink. Its events carry DeviceTouch.
func NewTouches(sink Sink) *Touches {
	return &Touches{sink: sink, fingers: map[int]*finger{}, sticks: map[string]int{}, holds: map[string]int{}}
}

func (t *Touches) take() *finger {
	if n := len(t.free); n > 0 {
		f := t.free[n-1]
		t.free = t.free[:n-1]
		*f = finger{}
		return f
	}
	return &finger{}
}

func (t *Touches) feed(src Source, v float64, canceled bool) {
	t.sink.Feed(Event{Device: DeviceTouch, Source: src, Value: v, Canceled: canceled})
}

// StickDown starts a touch on the stick called name: finger pointer landed at
// (x, y), and (cx, cy) is the centre the stick rests at. It reports false,
// and changes nothing, when the finger already holds a control or another
// finger holds the stick.
func (t *Touches) StickDown(pointer int, name string, x, y, cx, cy float64, shape Stick) (StickState, bool) {
	if name == "" || t.fingers[pointer] != nil {
		return StickState{}, false
	}
	if _, taken := t.sticks[name]; taken {
		return StickState{}, false
	}
	f := t.take()
	f.name, f.stick, f.shape = name, true, shape
	f.sx, f.sy = TouchAxis(name, false), TouchAxis(name, true)
	f.cx, f.cy = cx, cy
	if shape.Floating {
		f.cx, f.cy = x, y
	}
	t.fingers[pointer] = f
	t.sticks[name] = pointer
	t.deflect(f, x, y)
	return f.state(), true
}

// Move moves a finger to (x, y). When the finger holds a stick it returns
// the stick's new state and true.
func (t *Touches) Move(pointer int, x, y float64) (StickState, bool) {
	f := t.fingers[pointer]
	if f == nil || !f.stick {
		return StickState{}, false
	}
	t.deflect(f, x, y)
	return f.state(), true
}

func (f *finger) state() StickState {
	return StickState{Name: f.name, CenterX: f.cx, CenterY: f.cy, X: f.x, Y: f.y}
}

// deflect works out the stick's two axes from the finger's position and
// feeds the ones that changed.
func (t *Touches) deflect(f *finger, px, py float64) {
	r := f.shape.radius()
	dx, dy := px-f.cx, py-f.cy
	d := math.Hypot(dx, dy)
	if f.shape.Follow && d > r {
		// Bring the centre along so the finger sits on the rim.
		f.cx += dx * (d - r) / d
		f.cy += dy * (d - r) / d
		dx, dy = px-f.cx, py-f.cy
		d = r
	}
	var x, y float64
	if d > 0 {
		m := math.Min(d, r) / r
		if dz := f.shape.Deadzone; dz > 0 {
			if dz >= 1 {
				dz = 0.99
			}
			if m <= dz {
				m = 0
			} else {
				m = (m - dz) / (1 - dz)
			}
		}
		x, y = dx/d*m, dy/d*m
	}
	if x != f.x {
		f.x = x
		t.feed(f.sx, x, false)
	}
	if y != f.y {
		f.y = y
		t.feed(f.sy, y, false)
	}
}

// ButtonDown puts finger pointer on the button called name. It reports
// false, and changes nothing, when the finger already holds a control.
func (t *Touches) ButtonDown(pointer int, name string) bool {
	if name == "" || t.fingers[pointer] != nil {
		return false
	}
	f := t.take()
	f.name = name
	t.fingers[pointer] = f
	t.holds[name]++
	if t.holds[name] == 1 {
		t.feed(TouchButton(name), 1, false)
	}
	return true
}

// Up ends the touch of a finger the player lifted. It returns the name of
// the control the finger held, whether that was a stick, and false when the
// finger held nothing.
func (t *Touches) Up(pointer int) (name string, stick, ok bool) {
	return t.end(pointer, false)
}

// Cancel ends the touch of a finger that was taken away. A button it was
// the last finger on is released as canceled.
func (t *Touches) Cancel(pointer int) (name string, stick, ok bool) {
	return t.end(pointer, true)
}

func (t *Touches) end(pointer int, canceled bool) (string, bool, bool) {
	f := t.fingers[pointer]
	if f == nil {
		return "", false, false
	}
	delete(t.fingers, pointer)
	name, stick := f.name, f.stick
	if stick {
		delete(t.sticks, name)
		if f.x != 0 {
			t.feed(f.sx, 0, canceled)
		}
		if f.y != 0 {
			t.feed(f.sy, 0, canceled)
		}
	} else {
		t.holds[name]--
		if t.holds[name] <= 0 {
			delete(t.holds, name)
			t.feed(TouchButton(name), 0, canceled)
		}
	}
	t.free = append(t.free, f)
	if t.OnEnd != nil {
		t.OnEnd(pointer, name, stick, canceled)
	}
	return name, stick, true
}

// Drop cancels every touch on the control called name: the stick's finger,
// or all the fingers on the button. Call it when the control goes away, or
// stops being offered, with a finger still on it; the finger must touch
// again to act. It reports whether a finger was on it.
func (t *Touches) Drop(name string) bool {
	t.drop = t.drop[:0]
	for pointer, f := range t.fingers {
		if f.name == name {
			t.drop = append(t.drop, pointer)
		}
	}
	for _, pointer := range t.drop {
		t.end(pointer, true)
	}
	return len(t.drop) > 0
}

// Held reports whether a finger is on the button called name.
func (t *Touches) Held(name string) bool { return t.holds[name] > 0 }

// Stick returns the state of the stick called name, and false when no
// finger holds it.
func (t *Touches) Stick(name string) (StickState, bool) {
	pointer, ok := t.sticks[name]
	if !ok {
		return StickState{}, false
	}
	return t.fingers[pointer].state(), true
}

// Active returns the number of fingers that hold a control.
func (t *Touches) Active() int { return len(t.fingers) }

// ReleaseAll cancels every touch: call it when the window loses the focus or
// the page is hidden. A finger still on the glass must touch again to act.
func (t *Touches) ReleaseAll() {
	t.drop = t.drop[:0]
	for pointer := range t.fingers {
		t.drop = append(t.drop, pointer)
	}
	for _, pointer := range t.drop {
		t.end(pointer, true)
	}
}
