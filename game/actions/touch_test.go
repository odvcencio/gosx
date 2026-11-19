package actions

import (
	"math"
	"testing"
)

func touchMap() *Map {
	m := NewMap()
	m.Define(Context{Name: "play", Actions: []ActionDef{
		{Name: "steerX", Kind: Axis, Bindings: []Binding{{Source: TouchAxis("steer", false)}}},
		{Name: "steerY", Kind: Axis, Bindings: []Binding{{Source: TouchAxis("steer", true)}}},
		{Name: "sprint", Bindings: []Binding{{Source: Key("ShiftLeft")}, {Source: TouchButton("sprint")}}},
		{Name: "throw", Bindings: []Binding{{Source: TouchButton("target1")}}},
		{Name: "pause", Bindings: []Binding{{Source: Key("Escape")}, {Source: Key("KeyP")}}},
	}})
	m.Push("play")
	return m
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// One thumb on the stick and the other on a button: both read, and each
// ends on its own.
func TestTouchStickAndButtonTogether(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	shape := Stick{Radius: 50}
	if _, ok := tt.StickDown(1, "steer", 100, 300, 100, 300, shape); !ok {
		t.Fatal("the stick refused its first finger")
	}
	tt.Move(1, 125, 300)
	if !tt.ButtonDown(2, "sprint") {
		t.Fatal("the button refused the second finger")
	}
	m.Update(frame)
	if x, y := m.Vec2("steerX", "steerY"); !near(x, .5) || y != 0 {
		t.Fatalf("stick reads (%v, %v), want (0.5, 0)", x, y)
	}
	if st := m.State("sprint"); !st.Down || !st.Pressed {
		t.Fatalf("sprint with a finger on it: %+v", st)
	}
	if tt.Active() != 2 || !tt.Held("sprint") {
		t.Fatalf("fingers %d, sprint held %v", tt.Active(), tt.Held("sprint"))
	}

	// The stick moves under the held button; the button is not disturbed.
	tt.Move(1, 100, 350)
	m.Update(frame)
	if x, y := m.Vec2("steerX", "steerY"); x != 0 || !near(y, 1) {
		t.Fatalf("stick reads (%v, %v), want (0, 1): y grows downward", x, y)
	}
	if st := m.State("sprint"); !st.Down || st.Pressed || st.Released {
		t.Fatalf("sprint while the stick moved: %+v", st)
	}

	// The button's finger lifts; the stick stays.
	if name, stick, ok := tt.Up(2); !ok || stick || name != "sprint" {
		t.Fatalf("Up(2) = %q, %v, %v", name, stick, ok)
	}
	m.Update(frame)
	if st := m.State("sprint"); st.Down || !st.Released || st.Canceled {
		t.Fatalf("sprint after the finger lifted: %+v", st)
	}
	if _, y := m.Vec2("steerX", "steerY"); !near(y, 1) {
		t.Fatalf("the stick let go with the button: y %v", y)
	}
	if name, stick, ok := tt.Up(1); !ok || !stick || name != "steer" {
		t.Fatalf("Up(1) = %q, %v, %v", name, stick, ok)
	}
	m.Update(frame)
	if x, y := m.Vec2("steerX", "steerY"); x != 0 || y != 0 {
		t.Fatalf("stick after its finger lifted: (%v, %v)", x, y)
	}
	if tt.Active() != 0 {
		t.Fatalf("%d fingers left", tt.Active())
	}
}

// A touch the browser takes away is not the player letting go.
func TestTouchCancelIsNotARelease(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	tt.ButtonDown(1, "target1")
	m.Update(frame)
	tt.Cancel(1)
	m.Update(frame)
	if st := m.State("throw"); !st.Released || !st.Canceled {
		t.Fatalf("a canceled touch: %+v, want Released and Canceled", st)
	}
	if st := m.State("throw"); st.Tapped {
		t.Fatalf("a press that was taken away counted as a tap: %+v", st)
	}
	m.Update(frame)
	if st := m.State("throw"); st.Released || st.Canceled {
		t.Fatalf("the cancel lasted more than one frame: %+v", st)
	}

	tt.ButtonDown(1, "target1")
	m.Update(frame)
	tt.Up(1)
	m.Update(frame)
	if st := m.State("throw"); !st.Released || st.Canceled {
		t.Fatalf("a lifted finger: %+v, want Released and not Canceled", st)
	}

	// The window loses the focus with a finger down.
	tt.ButtonDown(3, "target1")
	tt.StickDown(4, "steer", 10, 10, 0, 0, Stick{Radius: 10})
	m.Update(frame)
	tt.ReleaseAll()
	m.Update(frame)
	if st := m.State("throw"); !st.Released || !st.Canceled {
		t.Fatalf("ReleaseAll on the touches: %+v", st)
	}
	if x, y := m.Vec2("steerX", "steerY"); x != 0 || y != 0 || tt.Active() != 0 {
		t.Fatalf("after ReleaseAll: stick (%v, %v), %d fingers", x, y, tt.Active())
	}
	// A move from the finger that is still on the glass does nothing.
	if _, ok := tt.Move(4, 50, 50); ok {
		t.Fatal("a released finger still moved the stick")
	}
}

// Two fingers on one button: it stays down until both have gone.
func TestTouchButtonHeldByTwoFingers(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	tt.ButtonDown(1, "sprint")
	tt.ButtonDown(2, "sprint")
	m.Update(frame)
	tt.Up(1)
	m.Update(frame)
	if st := m.State("sprint"); !st.Down || st.Released {
		t.Fatalf("one finger of two lifted: %+v", st)
	}
	tt.Up(2)
	m.Update(frame)
	if st := m.State("sprint"); st.Down || !st.Released {
		t.Fatalf("both fingers lifted: %+v", st)
	}
}

// A finger holds one control, and a stick has one finger.
func TestTouchOneControlPerFinger(t *testing.T) {
	tt := NewTouches(NewMap())
	if _, ok := tt.StickDown(1, "steer", 0, 0, 0, 0, Stick{}); !ok {
		t.Fatal("first finger refused")
	}
	if _, ok := tt.StickDown(2, "steer", 0, 0, 0, 0, Stick{}); ok {
		t.Fatal("a second finger took a stick that is held")
	}
	if tt.ButtonDown(1, "sprint") {
		t.Fatal("the stick's finger took a button as well")
	}
	if _, ok := tt.StickDown(3, "", 0, 0, 0, 0, Stick{}); ok || tt.ButtonDown(3, "") {
		t.Fatal("a control with no name was taken")
	}
	if _, _, ok := tt.Up(9); ok {
		t.Fatal("a finger that held nothing ended a touch")
	}
	tt.Up(1)
	if _, ok := tt.StickDown(2, "steer", 0, 0, 0, 0, Stick{}); !ok {
		t.Fatal("the stick stayed taken after its finger lifted")
	}
}

func TestTouchStickShapes(t *testing.T) {
	read := func(shape Stick, cx, cy float64, path ...[2]float64) StickState {
		tt := NewTouches(NewMap())
		st, ok := tt.StickDown(1, "steer", path[0][0], path[0][1], cx, cy, shape)
		if !ok {
			t.Fatal("stick refused")
		}
		for _, p := range path[1:] {
			st, _ = tt.Move(1, p[0], p[1])
		}
		return st
	}
	// Fixed centre: a finger that lands off the centre deflects at once.
	if st := read(Stick{Radius: 40}, 100, 100, [2]float64{120, 100}); !near(st.X, .5) || st.Y != 0 || st.CenterX != 100 {
		t.Fatalf("fixed: %+v", st)
	}
	// Floating centre: the stick starts at rest where the finger lands.
	if st := read(Stick{Radius: 40, Floating: true}, 100, 100, [2]float64{300, 220}); st.X != 0 || st.Y != 0 || st.CenterX != 300 || st.CenterY != 220 {
		t.Fatalf("floating at rest: %+v", st)
	}
	// Past the rim the deflection stays on the unit circle.
	st := read(Stick{Radius: 40, Floating: true}, 0, 0, [2]float64{0, 0}, [2]float64{300, 400})
	if !near(st.X, .6) || !near(st.Y, .8) || st.CenterX != 0 {
		t.Fatalf("past the rim: %+v", st)
	}
	// Follow: the centre trails the finger, so pulling back answers at once.
	st = read(Stick{Radius: 40, Floating: true, Follow: true}, 0, 0, [2]float64{0, 0}, [2]float64{100, 0})
	if !near(st.CenterX, 60) || !near(st.X, 1) {
		t.Fatalf("follow, pushed out: %+v", st)
	}
	st = read(Stick{Radius: 40, Floating: true, Follow: true}, 0, 0, [2]float64{0, 0}, [2]float64{100, 0}, [2]float64{60, 0})
	if !near(st.X, 0) {
		t.Fatalf("follow, pulled back 40: X %v, want 0", st.X)
	}
	// The dead zone reads zero, then rises from zero.
	if st := read(Stick{Radius: 100, Deadzone: .2}, 0, 0, [2]float64{15, 0}); st.X != 0 {
		t.Fatalf("inside the dead zone: %+v", st)
	}
	if st := read(Stick{Radius: 100, Deadzone: .2}, 0, 0, [2]float64{60, 0}); !near(st.X, .5) {
		t.Fatalf("past the dead zone: X %v, want 0.5", st.X)
	}
	// No radius given: the default.
	if st := read(Stick{}, 0, 0, [2]float64{DefaultStickRadius / 2, 0}); !near(st.X, .5) {
		t.Fatalf("default radius: X %v", st.X)
	}
}

// A tap of an on-screen button between two frames is still a press.
func TestTouchTapBetweenFrames(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	m.Update(frame)
	tt.ButtonDown(1, "target1")
	tt.Up(1)
	m.Update(frame)
	if st := m.State("throw"); !st.Pressed {
		t.Fatalf("a tap between frames: %+v", st)
	}
	m.Update(frame)
	if st := m.State("throw"); !st.Released || st.Canceled {
		t.Fatalf("the frame after the tap: %+v", st)
	}
}

// Feeding and moving a held stick does not allocate.
func TestTouchMoveDoesNotAllocate(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	tt.StickDown(1, "steer", 0, 0, 0, 0, Stick{Radius: 50})
	m.Update(frame)
	x := 0.0
	if n := testing.AllocsPerRun(200, func() {
		x += .25
		tt.Move(1, math.Mod(x, 50), 3)
		m.Update(frame)
		_, _ = m.Vec2("steerX", "steerY")
	}); n != 0 {
		t.Fatalf("Move, Update and Vec2 allocate %v times a frame", n)
	}
}

func TestTouchSourcesRoundTrip(t *testing.T) {
	for _, s := range []Source{TouchButton("sprint"), TouchAxis("steer", false), TouchAxis("steer", true)} {
		got, err := ParseSource(s.String())
		if err != nil || got != s {
			t.Fatalf("%q parsed to %+v, %v", s.String(), got, err)
		}
		if s.Class() != ClassTouch {
			t.Fatalf("%+v is of class %d", s, s.Class())
		}
		if Label(s, 0) != "" {
			t.Fatalf("an on-screen control has the label %q", Label(s, 0))
		}
	}
	if TouchAxis("steer", false) == TouchAxis("steer", true) {
		t.Fatal("the two axes of a stick are one source")
	}
}

// The browser used the key: the game does not see that press, and sees the
// next one, however the two fall between frames.
func TestSuppressDropsThePressTheBrowserUsed(t *testing.T) {
	esc := Key("Escape")
	down := func(m *Map) { m.Feed(Event{Device: DeviceKeyboard, Source: esc, Value: 1}) }
	up := func(m *Map) { m.Feed(Event{Device: DeviceKeyboard, Source: esc, Value: 0}) }

	// Pressed, used by the browser, let go, pressed again: all in one frame.
	m := touchMap()
	m.Update(frame)
	down(m)
	if !m.Suppress(esc) {
		t.Fatal("Suppress found nothing to withdraw")
	}
	up(m)
	down(m)
	up(m)
	m.Update(frame)
	if !m.Pressed("pause") {
		t.Fatal("the second press was lost with the first")
	}

	// Only the press the browser used: nothing reaches the game.
	m = touchMap()
	m.Update(frame)
	down(m)
	m.Suppress(esc)
	m.Update(frame)
	if st := m.State("pause"); st.Pressed || st.Down {
		t.Fatalf("the withdrawn press reached the game: %+v", st)
	}
	up(m)
	m.Update(frame)
	if st := m.State("pause"); st.Pressed || st.Released {
		t.Fatalf("letting go of a withdrawn key: %+v", st)
	}
	down(m)
	m.Update(frame)
	if !m.Pressed("pause") {
		t.Fatal("the next press did not count")
	}

	// The tap came and went before the browser used it.
	m = touchMap()
	m.Update(frame)
	down(m)
	up(m)
	m.Suppress(esc)
	m.Update(frame)
	if m.Pressed("pause") {
		t.Fatal("a withdrawn tap reached the game")
	}

	// A key that is up and was not pressed: nothing to withdraw, and the
	// next press counts.
	m = touchMap()
	m.Update(frame)
	if m.Suppress(esc) {
		t.Fatal("Suppress withdrew a key that was not pressed")
	}
	down(m)
	m.Update(frame)
	if !m.Pressed("pause") {
		t.Fatal("a press after an empty Suppress was lost")
	}

	// A key the game already holds down: the action is released, as canceled.
	m = touchMap()
	down(m)
	m.Update(frame)
	m.Suppress(esc)
	m.Update(frame)
	if st := m.State("pause"); !st.Released || !st.Canceled {
		t.Fatalf("withdrawing a held key: %+v", st)
	}
}

// The window loses the focus, or the action's context leaves the stack: the
// release is not the player's.
func TestCanceledReleases(t *testing.T) {
	m := touchMap()
	m.Feed(Event{Device: DeviceKeyboard, Source: Key("ShiftLeft"), Value: 1})
	m.Update(frame)
	m.ReleaseAll()
	m.Update(frame)
	if st := m.State("sprint"); !st.Released || !st.Canceled {
		t.Fatalf("ReleaseAll: %+v", st)
	}

	m.Feed(Event{Device: DeviceKeyboard, Source: Key("ShiftLeft"), Value: 1})
	m.Update(frame)
	m.Feed(Event{Device: DeviceKeyboard, Source: Key("ShiftLeft"), Value: 0})
	m.Update(frame)
	if st := m.State("sprint"); !st.Released || st.Canceled {
		t.Fatalf("a key let go: %+v", st)
	}

	m.Define(Context{Name: "menu", Opaque: true})
	m.Feed(Event{Device: DeviceKeyboard, Source: Key("ShiftLeft"), Value: 1})
	m.Update(frame)
	m.Push("menu")
	m.Update(frame)
	m.Pop()
	// The state is read through the active contexts, so look after the pop.
	if st := m.State("sprint"); !st.Released || !st.Canceled {
		t.Fatalf("the context left the stack with the key held: %+v", st)
	}
}

func TestRepeatClicks(t *testing.T) {
	for _, c := range []struct {
		code, tag, kind, role string
		want                  bool
	}{
		{"Enter", "BUTTON", "", "", true},
		{"NumpadEnter", "button", "submit", "", true},
		{"Enter", "A", "", "", true},
		{"Enter", "SUMMARY", "", "", true},
		{"Enter", "INPUT", "submit", "", true},
		{"Enter", "INPUT", "text", "", false},
		{"Enter", "DIV", "", "button", true},
		{"Enter", "DIV", "", "", false},
		{"Enter", "SELECT", "", "", false},
		{"Space", "BUTTON", "", "", true},
		{"Space", "INPUT", "text", "", false},
		{"KeyA", "BUTTON", "", "", false},
	} {
		if got := RepeatClicks(c.code, c.tag, c.kind, c.role); got != c.want {
			t.Errorf("RepeatClicks(%q, %q, %q, %q) = %v, want %v", c.code, c.tag, c.kind, c.role, got, c.want)
		}
	}
}

// A control that goes away with fingers on it: Drop ends their touches, as
// canceled, and tells the host each one.
func TestTouchDrop(t *testing.T) {
	m := touchMap()
	tt := NewTouches(m)
	var ended []string
	tt.OnEnd = func(pointer int, name string, stick, canceled bool) {
		kind := "button"
		if stick {
			kind = "stick"
		}
		if !canceled {
			kind = "lifted " + kind
		}
		ended = append(ended, kind+" "+name)
	}
	tt.StickDown(1, "steer", 30, 0, 0, 0, Stick{Radius: 60})
	tt.ButtonDown(2, "sprint")
	tt.ButtonDown(3, "sprint")
	m.Update(frame)
	if !tt.Drop("steer") {
		t.Fatal("Drop found no finger on the stick")
	}
	m.Update(frame)
	if x, _ := m.Vec2("steerX", "steerY"); x != 0 || tt.Active() != 2 {
		t.Fatalf("after the stick was dropped: x %v, %d fingers", x, tt.Active())
	}
	if _, ok := tt.Move(1, 60, 0); ok {
		t.Fatal("the dropped finger still moves the stick")
	}
	if !tt.Drop("sprint") || tt.Drop("sprint") || tt.Drop("nothing") {
		t.Fatal("Drop on the button: want true once, then false")
	}
	m.Update(frame)
	if st := m.State("sprint"); !st.Released || !st.Canceled {
		t.Fatalf("a dropped button: %+v", st)
	}
	if len(ended) != 3 || ended[0] != "stick steer" || ended[1] != "button sprint" || ended[2] != "button sprint" {
		t.Fatalf("the host heard %v", ended)
	}
	// The same finger may take the stick again with a new touch, and a lift
	// is told apart from a touch that was taken away.
	if _, ok := tt.StickDown(1, "steer", 0, 0, 0, 0, Stick{}); !ok {
		t.Fatal("the stick stayed taken after Drop")
	}
	tt.Up(1)
	if len(ended) != 4 || ended[3] != "lifted stick steer" {
		t.Fatalf("after a lift the host heard %v", ended)
	}
}
