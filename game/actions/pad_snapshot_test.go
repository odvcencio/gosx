package actions

import (
	"testing"

	"m31labs.dev/gosx/game/host"
)

type events []Event

func (e *events) Feed(ev Event) { *e = append(*e, ev) }
func (e *events) ReleaseAll()   {}

func standardPad(index int, id string) host.GamepadState {
	return host.NeutralStandardPad(index, id, "standard")
}

func TestPadPollerReleasesAGonePadAsCanceled(t *testing.T) {
	m := newMap()
	p := NewPadPoller()
	held := standardPad(0, "Xbox")
	held.Buttons[0] = 1
	p.PollStates([]host.GamepadState{held}, m)
	m.Update(frame)
	p.PollStates(nil, m) // the pad left with A down
	m.Update(frame)
	if st := m.State("jump"); !st.Released || !st.Canceled || st.Tapped {
		t.Fatalf("a pad that left: %+v, want a canceled release", st)
	}
}

func TestPadPollerNewIDInTheSlotIsCanceled(t *testing.T) {
	var got events
	p := NewPadPoller()
	a := standardPad(0, "Xbox")
	a.Buttons[7] = .8
	p.PollStates([]host.GamepadState{a}, &got)
	got = got[:0]
	b := standardPad(0, "DualSense")
	b.Buttons[7] = .8
	p.PollStates([]host.GamepadState{b}, &got)
	if len(got) != 2 || !got[0].Canceled || got[0].Value != 0 || got[1].Canceled || got[1].Value != .8 {
		t.Fatalf("events %+v, want the old pad's canceled release then the new pad's press", got)
	}
}

func TestPadPollerShorterListReleasesTheTail(t *testing.T) {
	var got events
	p := NewPadPoller()
	a := standardPad(0, "Odd")
	a.Buttons[16] = 1
	p.PollStates([]host.GamepadState{a}, &got)
	got = got[:0]
	a.Buttons = a.Buttons[:16]
	p.PollStates([]host.GamepadState{a}, &got)
	if len(got) != 1 || got[0].Source != PadButton(16) || got[0].Value != 0 || !got[0].Canceled {
		t.Fatalf("events %+v, want button16 released as canceled", got)
	}
}

func TestPadPollerSlowRampArrives(t *testing.T) {
	var got events
	p := NewPadPoller()
	a := standardPad(0, "Xbox")
	p.PollStates([]host.GamepadState{a}, &got)
	// A trigger creeping up by less than the epsilon each poll must still
	// cross the press line, and settle exactly at rest.
	for v := 0.0; v <= .52; v += .0004 {
		a.Buttons[7] = v
		p.PollStates([]host.GamepadState{a}, &got)
	}
	last := got[len(got)-1]
	if last.Value < .5 {
		t.Fatalf("the ramp's last event is %v: it never reached the press line", last.Value)
	}
	a.Buttons[7] = 0
	p.PollStates([]host.GamepadState{a}, &got)
	if got[len(got)-1].Value != 0 {
		t.Fatal("rest is always sent")
	}
}

func TestTee(t *testing.T) {
	var a, b events
	m := newMap()
	s := Tee(&a, m, &b)
	s.Feed(Event{Source: Key("Space"), Value: 1})
	s.ReleaseAll()
	m.Update(frame) // the press before the blur still reads, as a tap
	m.Update(frame)
	if len(a) != 1 || len(b) != 1 || m.Down("jump") {
		t.Fatalf("tee: a %v, b %v, jump down %v", a, b, m.Down("jump"))
	}
}
