package seats

import (
	"testing"

	"m31labs.dev/gosx/game/actions"
)

// record is a sink that keeps what it is fed; ReleaseAll counts and forgets.
type record struct {
	got      []actions.Event
	releases int
}

func (r *record) Feed(ev actions.Event) { r.got = append(r.got, ev) }
func (r *record) ReleaseAll()           { r.got, r.releases = nil, r.releases+1 }

// onlyRig is a rig whose events reach the seats and Only through one tee, the
// seats first, as a page wires them.
func onlyRig(t *testing.T, owner *int) (*rig, *record, actions.Sink) {
	r := newRig(t, Config{})
	rec := &record{}
	tee := actions.Tee(r.s, Only(r.s, func() int { return *owner }, rec))
	return r, rec, tee
}

func TestOnlyFeedsTheOwnersDeviceOnly(t *testing.T) {
	owner := 0
	r, rec, tee := onlyRig(t, &owner)
	r.plug(stdPad(0, xboxID))
	r.frame()
	// The keyboard's Enter claims seat 0 and, the seats being fed first, it
	// already reaches the owner's sink.
	enter := actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("Enter"), Value: 1}
	tee.Feed(enter)
	if len(rec.got) != 1 || rec.got[0] != enter {
		t.Fatalf("the claiming press: sink got %+v", rec.got)
	}
	tee.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("Enter")})
	// The pad's A claims seat 1 and does not reach seat 0's sink.
	r.button(0, 0, 1)
	r.s.SyncPads(r.pads)
	r.poller.PollStates(r.pads, tee)
	r.button(0, 0, 0)
	r.s.SyncPads(r.pads)
	r.poller.PollStates(r.pads, tee)
	if d, ok := r.s.Device(1); !ok || d.Pad != 0 {
		t.Fatalf("seat 1: %+v %v", d, ok)
	}
	rec.got = nil
	key := actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("KeyS"), Value: 1}
	mouse := actions.Event{Device: actions.DeviceMouse, Source: actions.MouseButton(0), Value: 1}
	pad := actions.Event{Device: actions.PadDevice(0), Source: actions.PadButton(13), Value: 1}
	tee.Feed(key)
	tee.Feed(mouse)
	tee.Feed(pad)
	if len(rec.got) != 2 || rec.got[0] != key || rec.got[1] != mouse {
		t.Fatalf("the keyboard owner's sink got %+v", rec.got)
	}
	// A device no seat holds reaches nothing.
	rec.got = nil
	tee.Feed(actions.Event{Device: actions.PadDevice(3), Source: actions.PadButton(0), Value: 1})
	if len(rec.got) != 0 {
		t.Fatalf("an unclaimed pad reached the sink: %+v", rec.got)
	}
}

func TestOnlyReleasesWhenTheDeviceBehindTheSeatChanges(t *testing.T) {
	owner := 0
	r, rec, tee := onlyRig(t, &owner)
	r.plug(stdPad(0, xboxID))
	r.frame()
	tee.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("Enter"), Value: 1})
	r.button(0, 0, 1)
	r.s.SyncPads(r.pads)
	r.poller.PollStates(r.pads, tee)
	released := rec.releases
	// The owner changes to the pad's seat: the keyboard's held Enter must not
	// stay held on the sink, and the pad now reaches it.
	owner = 1
	pad := actions.Event{Device: actions.PadDevice(0), Source: actions.PadButton(13), Value: 1}
	tee.Feed(pad)
	if rec.releases != released+1 || len(rec.got) != 1 || rec.got[0] != pad {
		t.Fatalf("after the owner changed: %d releases (was %d), got %+v", rec.releases, released, rec.got)
	}
	// The same device again releases nothing.
	tee.Feed(pad)
	if rec.releases != released+1 {
		t.Fatal("a release without a change of device")
	}
	// The pad is lost: its seat holds nothing, so nothing is fed and the
	// pad's held buttons are let go.
	r.unplug(0)
	r.s.SyncPads(r.pads)
	if _, ok := r.s.Device(1); ok {
		t.Fatal("setup: the seat still holds the lost pad")
	}
	tee.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key("KeyS"), Value: 1})
	if rec.releases != released+2 || len(rec.got) != 0 {
		t.Fatalf("after the loss: %d releases, got %+v", rec.releases-released, rec.got)
	}
	// ReleaseAll passes through.
	tee.ReleaseAll()
	if rec.releases != released+3 {
		t.Fatal("ReleaseAll did not reach the sink")
	}
}
