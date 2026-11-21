package actions

import (
	"testing"
	"time"
)

func repeatMap() *Map {
	m := NewMap()
	m.Define(Context{Name: "menu", Actions: []ActionDef{
		{Name: "next", Repeat: Repeat{Delay: 400 * time.Millisecond, Interval: 50 * time.Millisecond}, Bindings: []Binding{{Source: Key("ArrowRight")}, {Source: PadButton(15)}}},
		{Name: "plain", Bindings: []Binding{{Source: Key("Enter")}}},
	}})
	m.Push("menu")
	return m
}

// repeats returns the Map clock of every frame on which name read Repeated,
// over n frames of dt.
func repeats(m *Map, name string, dt time.Duration, n int) []time.Duration {
	var at []time.Duration
	for i := 0; i < n; i++ {
		m.Update(dt)
		if m.Repeated(name) {
			at = append(at, m.Now())
		}
	}
	return at
}

func TestRepeatFiresOnPressThenAfterDelayThenEveryInterval(t *testing.T) {
	m := repeatMap()
	press(m, Key("ArrowRight"))
	at := repeats(m, "next", 10*time.Millisecond, 60) // 600 ms
	want := []time.Duration{10, 410, 460, 510, 560}
	if len(at) != len(want) {
		t.Fatalf("repeats at %v, want %v ms", at, want)
	}
	for i := range want {
		if at[i] != want[i]*time.Millisecond {
			t.Fatalf("repeats at %v, want %v ms", at, want)
		}
	}
}

func TestRepeatSameTimesAtAnyFrameRate(t *testing.T) {
	// The same hold at 100 Hz and at 50 Hz repeats at the same count, give or
	// take the frame the schedule falls in.
	for _, dt := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 7 * time.Millisecond} {
		m := repeatMap()
		press(m, Key("ArrowRight"))
		n := int(1000 * time.Millisecond / dt)
		if got := len(repeats(m, "next", dt, n)); got < 12 || got > 14 {
			t.Errorf("dt %v: %d repeats in 1 s, want the press and about 12", dt, got)
		}
	}
}

func TestRepeatLongFrameFiresOnce(t *testing.T) {
	m := repeatMap()
	press(m, Key("ArrowRight"))
	m.Update(frame)
	m.Update(2 * time.Second) // a stall
	if !m.Repeated("next") {
		t.Fatal("the frame after a stall should repeat")
	}
	m.Update(10 * time.Millisecond)
	if m.Repeated("next") {
		t.Fatal("a stall must not leave a burst of repeats behind it")
	}
}

func TestRepeatStopsOnReleaseAndRestartsOnPress(t *testing.T) {
	m := repeatMap()
	press(m, Key("ArrowRight"))
	repeats(m, "next", 10*time.Millisecond, 45)
	release(m, Key("ArrowRight"))
	m.Update(frame)
	if m.Repeated("next") {
		t.Fatal("a release frame is not a repeat")
	}
	press(m, Key("ArrowRight"))
	if at := repeats(m, "next", 10*time.Millisecond, 30); len(at) != 1 {
		t.Fatalf("a new press waits the whole delay again; repeats at %v", at)
	}
}

func TestRepeatWithoutConfigEqualsPressed(t *testing.T) {
	m := repeatMap()
	press(m, Key("Enter"))
	m.Update(frame)
	if !m.Repeated("plain") {
		t.Fatal("the press frame reads Repeated")
	}
	for i := 0; i < 100; i++ {
		m.Update(frame)
		if m.Repeated("plain") {
			t.Fatal("an action with no Repeat never repeats")
		}
	}
}

func TestSuppressHeldStopsRepeat(t *testing.T) {
	m := repeatMap()
	press(m, PadButton(15))
	repeats(m, "next", 10*time.Millisecond, 45)
	m.SuppressHeld()
	if at := repeats(m, "next", 10*time.Millisecond, 50); len(at) != 0 {
		t.Fatalf("a withdrawn press repeats at %v", at)
	}
}

func TestSuppressHeldCarryOverLatch(t *testing.T) {
	m := newMap()
	press(m, PadButton(0))
	m.Update(frame)
	if !m.Pressed("jump") {
		t.Fatal("press")
	}
	// The press ended the last screen; the next screen opens with it held.
	if !m.SuppressHeld() {
		t.Fatal("SuppressHeld should report the held button")
	}
	m.Update(frame)
	st := m.State("jump")
	if st.Down || !st.Released || !st.Canceled || st.Tapped {
		t.Fatalf("after SuppressHeld: %+v, want a canceled release", st)
	}
	for i := 0; i < 5; i++ {
		m.Update(frame)
		if m.Down("jump") || m.Pressed("jump") {
			t.Fatal("the held button stays out")
		}
	}
	// Another source of the same action still acts at once.
	press(m, Key("Space"))
	m.Update(frame)
	if !m.Pressed("jump") {
		t.Fatal("a fresh press on another source acts")
	}
	release(m, Key("Space"))
	m.Update(frame)
	// Let go and press again: it counts.
	release(m, PadButton(0))
	m.Update(frame)
	press(m, PadButton(0))
	m.Update(frame)
	if !m.Pressed("jump") {
		t.Fatal("the withdrawn button counts again once let go and pressed")
	}
}

func TestSuppressHeldDropsATapSinceTheLastUpdate(t *testing.T) {
	m := newMap()
	press(m, Key("Space"))
	release(m, Key("Space"))
	if !m.SuppressHeld() {
		t.Fatal("a tap not yet reported is something to withdraw")
	}
	m.Update(frame)
	if m.Pressed("jump") {
		t.Fatal("the tap is dropped")
	}
	press(m, Key("Space"))
	m.Update(frame)
	if !m.Pressed("jump") {
		t.Fatal("the next press counts: a dropped tap leaves nothing held back")
	}
}

func TestPushAfterATapDoesNotSwallowTheNextPress(t *testing.T) {
	// Regression: a context change used to leave a key that came and went
	// since the last Update suppressed with nothing held, so its next press
	// was ignored.
	m := newMap()
	m.Push("gameplay")
	press(m, Key("Escape"))
	release(m, Key("Escape"))
	m.Push("menu")
	m.Update(frame)
	if m.Pressed("back") {
		t.Fatal("the tap before the push does not press the menu")
	}
	press(m, Key("Escape"))
	m.Update(frame)
	if !m.Pressed("back") {
		t.Fatal("the next press after the push counts")
	}
}

func TestSuppressHeldNothingHeld(t *testing.T) {
	m := newMap()
	m.Feed(Event{Source: Motion(CodeMouseDX), Value: 5})
	if m.SuppressHeld() {
		t.Fatal("motion is never held")
	}
}

func TestNeutral(t *testing.T) {
	m := newMap()
	if !m.Neutral(Rest{}) {
		t.Fatal("a new Map is at rest")
	}
	m.Feed(Event{Source: PadAxis(1), Value: .3})
	if m.Neutral(Rest{}) {
		t.Fatal("a pushed stick is not at rest, though no action is down")
	}
	m.SuppressHeld()
	if m.Neutral(Rest{}) {
		t.Fatal("a suppressed stick still is not at rest")
	}
	m.Feed(Event{Source: PadAxis(1)})
	m.Feed(Event{Source: Key("KeyQ"), Value: 1}) // bound to nothing
	if m.Neutral(Rest{}) {
		t.Fatal("an unbound key held is not at rest")
	}
	m.Feed(Event{Source: Key("KeyQ")})
	m.Feed(Event{Source: Motion(CodeMouseDY), Value: 9})
	if !m.Neutral(Rest{}) {
		t.Fatal("everything let go is at rest; motion does not count")
	}
	m.Feed(Event{Source: TouchButton("sprint"), Value: 1})
	m.ReleaseAll()
	if !m.Neutral(Rest{}) {
		t.Fatal("ReleaseAll leaves the Map at rest")
	}
}

func TestNeutralRest(t *testing.T) {
	m := newMap()
	m.Feed(Event{Source: PadButton(7), Value: .02}) // a trigger resting a little above 0
	m.Feed(Event{Source: PadAxis(0), Value: .05})
	if m.Neutral(Rest{}) {
		t.Fatal("with the zero Rest any level is held")
	}
	if !m.Neutral(Rest{Button: .05, Axis: .1}) {
		t.Fatal("levels under the rest lines are at rest")
	}
	m.Feed(Event{Source: PadButton(7), Value: .05})
	if m.Neutral(Rest{Button: .05, Axis: .1}) {
		t.Fatal("a level at the line is held")
	}
}
