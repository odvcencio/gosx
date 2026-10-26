package host

import (
	"reflect"
	"testing"
)

func TestForwardedInputTracksKeysPointerAndPads(t *testing.T) {
	f := newForwardedInput()
	f.Apply(InputEvent{Kind: "key", Code: "KeyA", Down: true})
	f.Apply(InputEvent{Kind: "key", Code: "KeyD", Down: true})
	f.Apply(InputEvent{Kind: "key", Code: "KeyD", Down: false})
	if !f.KeyDown("KeyA") || f.KeyDown("KeyD") || f.KeyDown("KeyZ") {
		t.Fatal("key state wrong")
	}
	f.Apply(InputEvent{Kind: "pointer", PointerType: "move", X: 3, Y: 4, Mask: 1})
	if f.Pointer() != (PointerState{X: 3, Y: 4, Mask: 1}) {
		t.Fatalf("pointer %+v", f.Pointer())
	}
	f.Apply(InputEvent{Kind: "pad", PadIndex: 1, PadID: "B", Mapping: "standard", Connected: true, Buttons: []float64{1}, Axes: []float64{.5}})
	f.Apply(InputEvent{Kind: "pad", PadIndex: 0, PadID: "A", Mapping: "standard", Connected: true, Buttons: []float64{0, 1}})
	pads := f.Gamepads()
	if len(pads) != 2 || pads[0].Index != 0 || pads[1].Index != 1 || pads[1].Axes[0] != .5 {
		t.Fatalf("pads must be ordered by slot: %+v", pads)
	}
	pads[0].Buttons[0] = 99
	if f.Gamepads()[0].Buttons[0] != 0 {
		t.Fatal("a snapshot must not alias the tracker's state")
	}
	f.Apply(InputEvent{Kind: "pad", PadIndex: 0, Connected: false})
	if got := f.Gamepads(); len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("disconnect: %+v", got)
	}
}

func TestForwardedInputBlurReleasesEverything(t *testing.T) {
	f := newForwardedInput()
	f.Apply(InputEvent{Kind: "key", Code: "KeyA", Down: true})
	f.Apply(InputEvent{Kind: "pointer", X: 1, Y: 2, Mask: 3})
	f.Apply(InputEvent{Kind: "pad", PadIndex: 0, Connected: true, Buttons: []float64{1}})
	f.Apply(InputEvent{Kind: "blur"})
	if f.KeyDown("KeyA") || len(f.Gamepads()) != 0 || f.Pointer().Mask != 0 {
		t.Fatal("blur must release keys, pads and pointer buttons")
	}
	if f.Pointer().X != 1 {
		t.Fatal("blur keeps the last pointer position")
	}
}

func TestForwardedInputSubscribersSeeEventsInOrderUntilCancelled(t *testing.T) {
	f := newForwardedInput()
	var got []string
	cancel := f.Subscribe(func(e InputEvent) { got = append(got, "a:"+e.Code) })
	f.Subscribe(func(e InputEvent) { got = append(got, "b:"+e.Code) })
	f.Apply(InputEvent{Kind: "key", Code: "1", Down: true})
	cancel()
	cancel()
	f.Apply(InputEvent{Kind: "key", Code: "2", Down: true})
	if want := []string{"a:1", "b:1", "b:2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if f.Subscribe(nil) == nil {
		t.Fatal("a nil handler still returns a cancel function")
	}
}

func TestForwardedInputEventDelay(t *testing.T) {
	if d := (InputEvent{AtMS: 10.5, ReceivedMS: 12}).Delay(); d != 1.5 {
		t.Fatalf("delay %v", d)
	}
}
