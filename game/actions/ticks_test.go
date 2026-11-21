package actions

import (
	"testing"
	"time"
)

func tickMap() (*Map, *Ticks) {
	m := newMap()
	return m, NewTicks(m, TickOptions{})
}

func TestTicksPressReachesTheNextTickOnly(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame) // F1, no tick
	m.Update(frame) // F2, no tick
	m.Update(frame) // F3, one tick
	tk.Tick()
	if st := tk.State("jump"); !st.Pressed || !st.Down || st.Released {
		t.Fatalf("first tick after the press: %+v", st)
	}
	m.Update(frame) // F4, three ticks
	for i := 0; i < 3; i++ {
		tk.Tick()
		if st := tk.State("jump"); st.Pressed || !st.Down {
			t.Fatalf("F4 tick %d: %+v, want held with no press", i, st)
		}
	}
}

func TestTicksOneFrameSeveralTicks(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	var got []bool
	for i := 0; i < 3; i++ {
		tk.Tick()
		got = append(got, tk.Pressed("jump"))
	}
	if !got[0] || got[1] || got[2] {
		t.Fatalf("presses by tick: %v, want the first tick only", got)
	}
}

func TestTicksTapBetweenTicks(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	release(m, Key("Space"))
	m.Update(frame) // the latched press
	m.Update(frame) // its release
	tk.Tick()
	st := tk.State("jump")
	if !st.Pressed || !st.Released || st.Down || !st.Tapped || st.Canceled {
		t.Fatalf("a whole tap in one interval: %+v", st)
	}
	es := tk.Edges()
	if len(es) != 2 || es[0].Kind != PressEdge || es[1].Kind != ReleaseEdge {
		t.Fatalf("edges %+v, want press then release", es)
	}
}

func TestTicksTickBetweenPressAndRelease(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	release(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	if st := tk.State("jump"); !st.Pressed || st.Released {
		t.Fatalf("tick on the latched frame: %+v", st)
	}
	m.Update(frame)
	tk.Tick()
	if st := tk.State("jump"); st.Pressed || !st.Released || !st.Tapped {
		t.Fatalf("next tick: %+v", st)
	}
}

func TestTicksReleaseAndPressAgainBetweenFrames(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	release(m, Key("Space"))
	press(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	es := tk.Edges()
	if len(es) != 2 || es[0].Kind != ReleaseEdge || es[1].Kind != PressEdge {
		t.Fatalf("edges %+v, want the old press's release, then the new press", es)
	}
	if st := tk.State("jump"); !st.Down || !st.Pressed || !st.Released {
		t.Fatalf("state %+v", st)
	}
}

func TestTicksTwoBindingsOneHeld(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	// The pad's A taps while Space stays down: the action never went up.
	press(m, PadButton(0))
	m.Update(frame)
	release(m, PadButton(0))
	m.Update(frame)
	tk.Tick()
	if es := tk.Edges(); len(es) != 0 {
		t.Fatalf("edges %+v, want none: the action stayed down", es)
	}
}

func TestTicksContextChangeIsACanceledRelease(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	m.Push("menu") // opaque: gameplay leaves the active list
	m.Update(frame)
	tk.Tick()
	st := tk.State("jump")
	if !st.Released || !st.Canceled || st.Tapped {
		t.Fatalf("after an opaque push: %+v", st)
	}
	m.Update(frame)
	tk.Tick()
	if len(tk.Edges()) != 0 {
		t.Fatalf("the canceled release came twice: %+v", tk.Edges())
	}
}

func TestTicksSuppressHeldIsACanceledRelease(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	tk.Tick()
	m.SuppressHeld()
	m.Update(frame)
	tk.Tick()
	if st := tk.State("jump"); !st.Released || !st.Canceled {
		t.Fatalf("after SuppressHeld: %+v", st)
	}
}

func TestTicksReleaseLengthKeepsItsFrame(t *testing.T) {
	for _, c := range []struct {
		held   time.Duration
		tapped bool
	}{{180 * time.Millisecond, true}, {190 * time.Millisecond, true}, {200 * time.Millisecond, false}} {
		m, tk := tickMap()
		press(m, Key("Space"))
		m.Update(10 * time.Millisecond)
		m.Update(c.held)
		release(m, Key("Space"))
		m.Update(10 * time.Millisecond)
		// The step that reads it comes three frames later.
		for i := 0; i < 3; i++ {
			m.Update(500 * time.Millisecond)
		}
		tk.Tick()
		st := tk.State("jump")
		if st.Tapped != c.tapped || st.HeldFor != c.held+10*time.Millisecond {
			t.Errorf("held %v: %+v, want tapped %v and HeldFor from the release frame", c.held, st, c.tapped)
		}
	}
}

func TestTicksPulse(t *testing.T) {
	m, tk := tickMap()
	tk.Pulse("keep")
	tk.Tick()
	if !tk.Pulsed("keep") || tk.Pressed("keep") {
		t.Fatal("a pulse reads Pulsed, not Pressed")
	}
	tk.Tick()
	if tk.Pulsed("keep") {
		t.Fatal("a pulse lasts one tick")
	}
	// A pulse never releases a device's held press of the same name.
	press(m, Key("Space"))
	m.Update(frame)
	tk.Pulse("jump")
	tk.Tick()
	if !tk.Down("jump") || tk.Released("jump") {
		t.Fatal("the pulse released the held key")
	}
}

func TestTicksClear(t *testing.T) {
	m, tk := tickMap()
	press(m, Key("Space"))
	m.Update(frame)
	tk.Pulse("keep")
	tk.Clear()
	tk.Tick()
	if tk.Pressed("jump") || tk.Pulsed("keep") {
		t.Fatal("Clear drops what waits")
	}
	if !tk.Down("jump") {
		t.Fatal("Clear leaves the level: the key is still held")
	}
	m.Update(frame)
	tk.Tick()
	tk.Clear()
	if len(tk.Edges()) != 0 {
		t.Fatal("Clear drops this step's edges too")
	}
}

func TestTicksRepeatsOnlyWhenAsked(t *testing.T) {
	m := repeatMap()
	plain := NewTicks(m, TickOptions{})
	rep := NewTicks(m, TickOptions{Repeats: []string{"next"}})
	press(m, Key("ArrowRight"))
	for i := 0; i < 60; i++ {
		m.Update(10 * time.Millisecond)
	}
	plain.Tick()
	rep.Tick()
	if n := len(plain.Edges()); n != 1 {
		t.Fatalf("plain: %d edges, want the press only", n)
	}
	if n := len(rep.Edges()); n != 5 {
		t.Fatalf("with repeats: %d edges, want the press and 4 repeats", n)
	}
}

func TestTicksNamesAndAxes(t *testing.T) {
	m := newMap()
	all := NewTicks(m, TickOptions{})
	some := NewTicks(m, TickOptions{Names: []string{"pause"}})
	press(m, Key("KeyD"))
	press(m, Key("Space"))
	press(m, Key("Escape"))
	m.Update(frame)
	all.Tick()
	some.Tick()
	for _, e := range all.Edges() {
		if e.Name == "moveX" {
			t.Fatal("axis actions are levels, not edges")
		}
	}
	if len(all.Edges()) != 2 || len(some.Edges()) != 1 || some.Edges()[0].Name != "pause" {
		t.Fatalf("all %+v, some %+v", all.Edges(), some.Edges())
	}
	if all.Value("moveX") != 1 {
		t.Fatal("Value reads the level")
	}
	some.Detach()
	release(m, Key("Escape"))
	m.Update(frame)
	some.Tick()
	if len(some.Edges()) != 0 {
		t.Fatal("a detached buffer hears nothing")
	}
}

func TestTicksLimit(t *testing.T) {
	m := newMap()
	tk := NewTicks(m, TickOptions{Limit: 4})
	for i := 0; i < 5; i++ {
		press(m, Key("Space"))
		m.Update(frame)
		release(m, Key("Space"))
		m.Update(frame)
	}
	tk.Tick()
	if len(tk.Edges()) != 4 || tk.Dropped() != 6 {
		t.Fatalf("%d edges, %d dropped", len(tk.Edges()), tk.Dropped())
	}
	if e := tk.Edges()[3]; e.Kind != ReleaseEdge {
		t.Fatalf("the newest edges are kept: %+v", tk.Edges())
	}
}

func TestFlick(t *testing.T) {
	var f Flick
	if d, ok := f.Update(-.8, .1); !ok || d != DirLeft {
		t.Fatalf("left: %v %v", d, ok)
	}
	for _, x := range []float64{-.9, -.75, -.5, -.4} {
		if _, ok := f.Update(x, 0); ok {
			t.Fatalf("a held flick fired again at %v", x)
		}
	}
	f.Update(-.2, 0) // back inside: rearmed
	if d, ok := f.Update(0, -.9); !ok || d != DirUp {
		t.Fatalf("up: %v %v", d, ok)
	}
	f.Update(0, 0)
	f.Suppress()
	if _, ok := f.Update(.9, 0); ok {
		t.Fatal("suppressed: a stick must come back before it fires")
	}
	f.Update(0, 0)
	if d, ok := f.Update(0, .8); !ok || d != DirDown || d.String() != "down" {
		t.Fatalf("down: %v %v", d, ok)
	}
}
