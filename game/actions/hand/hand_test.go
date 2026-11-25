package hand

import (
	"testing"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

const (
	xboxID = "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)"
	psID   = "DualSense Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 0ce6)"
)

func key(code string, down bool) actions.Event {
	v := 0.0
	if down {
		v = 1
	}
	return actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key(code), Value: v}
}

func padButton(slot, n int, v float64) actions.Event {
	return actions.Event{Device: actions.PadDevice(slot), Source: actions.PadButton(n), Value: v}
}

func padAxis(slot, n int, v float64) actions.Event {
	return actions.Event{Device: actions.PadDevice(slot), Source: actions.PadAxis(n), Value: v}
}

func pads(ids ...string) []host.GamepadState {
	var out []host.GamepadState
	for i, id := range ids {
		out = append(out, host.NeutralStandardPad(i, id, "standard"))
	}
	return out
}

func TestLastDeviceUsedHoldsTheHand(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(xboxID))
	if h, changed := tr.Frame(); h.Device != Keys || changed || tr.Used() {
		t.Fatalf("new tracker: %+v %v", h, changed)
	}
	tr.Feed(padButton(0, 0, 1))
	h, changed := tr.Frame()
	if h.Device != Pad || h.Name() != "xbox" || !changed || h.ID.Maker != actions.MakerMicrosoft {
		t.Fatalf("after A: %+v %v", h, changed)
	}
	tr.Feed(padButton(0, 0, 0))
	if _, changed := tr.Frame(); changed {
		t.Fatal("a release changes nothing")
	}
	tr.Feed(key("Space", true))
	if h, changed := tr.Frame(); h.Name() != "keys" || !changed {
		t.Fatalf("after Space: %+v", h)
	}
	// Two uses in one frame: the last wins.
	tr.Feed(key("Space", false))
	tr.Feed(padButton(0, 1, 1))
	tr.Feed(key("KeyW", true))
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("last use in the frame was a key: %+v", h)
	}
}

func TestStickTakesTheHandOnlyByCrossing(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(psID))
	// A stick nudged short of the line does not take the hand.
	tr.Feed(padAxis(0, 0, .3))
	tr.Feed(padAxis(0, 1, .3)) // magnitude 0.42
	if h, _ := tr.Frame(); h.Device != Keys || tr.Used() {
		t.Fatalf("short push took the hand: %+v", h)
	}
	// Past the line on the diagonal: the two axes together count.
	tr.Feed(padAxis(0, 1, .4)) // magnitude 0.5
	if h, _ := tr.Frame(); h.Name() != "playstation" {
		t.Fatalf("crossing push: %+v", h)
	}
	// The player leaves the stick held and types: the keyboard takes the
	// hand, and the held stick never takes it back.
	tr.Feed(key("KeyA", true))
	tr.Frame()
	for i := 0; i < 10; i++ {
		tr.Feed(padAxis(0, 0, .3+float64(i)*.05)) // wobbling while held past the line
		if h, _ := tr.Frame(); h.Device != Keys {
			t.Fatalf("frame %d: a held stick took the hand back: %+v", i, h)
		}
	}
	// Back to just inside the line is not enough to re-arm.
	tr.Feed(padAxis(0, 0, .35))
	tr.Feed(padAxis(0, 1, .25)) // 0.43
	tr.Frame()
	tr.Feed(padAxis(0, 0, .5)) // 0.56
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("a stick that never came back inside half the line re-took the hand: %+v", h)
	}
	// Back to rest, then pushed again: it takes the hand.
	tr.Feed(padAxis(0, 0, 0))
	tr.Feed(padAxis(0, 1, 0))
	tr.Feed(padAxis(0, 2, .9)) // the other stick counts too
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("right stick push: %+v", h)
	}
}

func TestDriftingStickNeverTakesTheHand(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Feed(key("Enter", true))
	tr.Frame()
	// A worn stick rests at 0.6 when the pad is first seen.
	worn := host.NeutralStandardPad(0, xboxID, "standard")
	worn.Axes[1] = .6
	tr.Pads([]host.GamepadState{worn})
	tr.Feed(padAxis(0, 1, .52)) // the poller's first report, after its deadzone
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("a stick resting past the line took the hand: %+v", h)
	}
	for i := 0; i < 20; i++ {
		tr.Pads([]host.GamepadState{worn})
		tr.Feed(padAxis(0, 1, .50+float64(i%3)*.01))
		if h, _ := tr.Frame(); h.Device != Keys {
			t.Fatalf("drift frame %d took the hand: %+v", i, h)
		}
	}
	// Once it comes back inside, a push counts.
	tr.Feed(padAxis(0, 1, .1))
	tr.Frame()
	tr.Feed(padAxis(0, 1, .8))
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("a push after the stick came back: %+v", h)
	}
}

func TestAnalogTriggerFlutterAtTheThreshold(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(xboxID))
	tr.Feed(padButton(0, 7, .55)) // RT past the press line
	tr.Frame()
	tr.Feed(key("KeyD", true))
	tr.Frame()
	// The trigger flutters around the line while the player types.
	for i, v := range []float64{.49, .5002, .4998, .51, .42, .6} {
		tr.Feed(padButton(0, 7, v))
		if h, _ := tr.Frame(); h.Device != Keys {
			t.Fatalf("flutter %d (%v) took the hand: %+v", i, v, h)
		}
	}
	tr.Feed(padButton(0, 7, .39)) // inside the rearm line: re-armed
	tr.Feed(padButton(0, 7, .7))
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("a real second pull: %+v", h)
	}
}

func TestMouseRules(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(xboxID))
	tr.Feed(padButton(0, 0, 1))
	tr.Frame()
	tr.MouseMoved(500)
	tr.Pointer(PointerMouse)
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("with the zero Rules the mouse never takes the hand: %+v", h)
	}

	tr = NewTracker(Rules{MouseMotion: 24, MouseButtons: true})
	tr.Pads(pads(xboxID))
	tr.Feed(padButton(0, 0, 1))
	tr.Frame()
	// A hand resting on the mouse: a few pixels a frame never add up across frames.
	for i := 0; i < 30; i++ {
		tr.MouseMoved(3)
		tr.MouseMoved(-3)
		if h, _ := tr.Frame(); h.Device != Pad {
			t.Fatalf("jitter frame %d took the hand: %+v", i, h)
		}
	}
	tr.MouseMoved(20)
	tr.MouseMoved(-10)
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("30 px in one frame: %+v", h)
	}
	// In one frame: the mouse moves, then a pad button goes down. The pad was used last.
	tr.Feed(padButton(0, 0, 0))
	tr.Frame()
	tr.MouseMoved(40)
	tr.Feed(padButton(0, 0, 1))
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("pad after mouse in one frame: %+v", h)
	}
	tr.Pointer(PointerMouse)
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("a click: %+v", h)
	}
}

func TestTouch(t *testing.T) {
	tr := NewTracker(Rules{MouseButtons: true})
	tr.Pointer(PointerTouch)
	if h, _ := tr.Frame(); h.Name() != "touch" {
		t.Fatalf("finger: %+v", h)
	}
	tr.Feed(key("Space", true))
	tr.Frame()
	tr.Feed(actions.Event{Device: actions.DeviceTouch, Source: actions.TouchAxis("steer", false), Value: .4})
	if h, _ := tr.Frame(); h.Device != Touch {
		t.Fatalf("on-screen stick: %+v", h)
	}
	tr.Feed(key("Space", false))
	tr.Feed(key("Space", true))
	tr.Frame()
	tr.Feed(actions.Event{Device: actions.DeviceTouch, Source: actions.TouchButton("sprint"), Value: 1})
	if h, _ := tr.Frame(); h.Device != Touch {
		t.Fatalf("on-screen button: %+v", h)
	}
	tr.Pointer(PointerPen)
	if h, _ := tr.Frame(); h.Device != Touch {
		t.Fatalf("pen: %+v", h)
	}
}

func TestIgnoreLeavesTheHand(t *testing.T) {
	tr := NewTracker(Rules{Ignore: func(ev actions.Event) bool {
		return ev.Source == actions.Key("ControlLeft") || ev.Source == actions.PadButton(16)
	}})
	tr.Pads(pads(xboxID))
	tr.Feed(padButton(0, 0, 1))
	tr.Frame()
	tr.Feed(key("ControlLeft", true))
	if h, _ := tr.Frame(); h.Device != Pad {
		t.Fatalf("an ignored key took the hand: %+v", h)
	}
	tr.Feed(key("Space", true))
	tr.Frame()
	tr.Feed(padButton(0, 16, 1))
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("an ignored button took the hand: %+v", h)
	}
}

func TestBlurReArmsKeys(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(xboxID))
	tr.Feed(key("KeyW", true))
	tr.Frame()
	tr.Feed(padButton(0, 0, 1))
	tr.Frame()
	// The window loses the focus with W down; no keyup ever comes. After
	// the focus returns, W going down again is a use.
	tr.ReleaseAll()
	tr.Feed(key("KeyW", true))
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("W after a blur: %+v", h)
	}
}

func TestStartAndSet(t *testing.T) {
	for _, c := range []struct {
		pads       []host.GamepadState
		remembered string
		touchOnly  bool
		want       string
	}{
		{pads(xboxID), "touch", true, "xbox"},
		{nil, "playstation", true, "playstation"},
		{nil, "nonsense", true, "touch"},
		{nil, "", false, "keys"},
		{pads("Stub Standard Pad (STANDARD GAMEPAD)"), "", false, "pad"},
	} {
		if got := Start(c.pads, c.remembered, c.touchOnly).Name(); got != c.want {
			t.Errorf("Start(%d pads, %q, %v) = %q, want %q", len(c.pads), c.remembered, c.touchOnly, got, c.want)
		}
	}
	for _, name := range []string{"keys", "touch", "pad", "xbox", "playstation", "nintendo"} {
		h, ok := Named(name)
		if !ok || h.Name() != name {
			t.Errorf("Named(%q) = %+v, %v", name, h, ok)
		}
	}
	tr := NewTracker(Rules{})
	tr.Set(Hand{Device: Touch})
	if h, changed := tr.Frame(); h.Device != Touch || !changed || tr.Used() {
		t.Fatalf("Set: %+v %v used=%v", h, changed, tr.Used())
	}
	tr.Feed(key("Space", true))
	tr.Frame()
	tr.Set(Hand{Device: Touch})
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatal("Set after a use must not override the player")
	}
}

func TestPadsFillsTheIdentityOfAHandNamedByStyle(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Set(Hand{Device: Pad, Slot: 0, ID: actions.PadIdentity{Style: actions.PadXbox}})
	tr.Frame()
	tr.Pads(pads(xboxID))
	h, changed := tr.Frame()
	if !changed || h.ID.Vendor != 0x045e || h.Name() != "xbox" {
		t.Fatalf("after the first poll: %+v %v", h, changed)
	}
	if !h.Same(Hand{Device: Pad, ID: actions.PadIdentity{Style: actions.PadXbox}}) {
		t.Fatal("Same compares device and style only")
	}
}

// TestPadAdapterTakesTheHand drives the tracker the way a page does, with
// the frame-latched pad test source behind the pad poller.
func TestPadAdapterTakesTheHand(t *testing.T) {
	src := host.NewPadTestSource()
	poller := actions.NewPadPoller()
	tr := NewTracker(Rules{})
	frame := func() Hand {
		src.BeginFrame()
		list := src.Gamepads()
		tr.Pads(list)
		poller.Poll(src, tr)
		src.EndFrame()
		h, _ := tr.Frame()
		return h
	}
	seq := uint64(0)
	set := func(ps ...host.GamepadState) {
		seq++
		if err := src.Enqueue(host.PadSet{Seq: seq, Next: true, Pads: ps}); err != nil {
			t.Fatal(err)
		}
	}
	tr.Feed(key("Space", true))
	frame()
	ps := host.NeutralStandardPad(0, psID, "standard")
	set(ps)
	if h := frame(); h.Device != Keys {
		t.Fatalf("a pad plugged in at rest does not take the hand: %+v", h)
	}
	down := host.NeutralStandardPad(0, psID, "standard")
	down.Buttons[0] = 1
	// A press and a release queued together land on two frames.
	set(down)
	set(ps)
	if h := frame(); h.Name() != "playstation" {
		t.Fatalf("press frame: %+v", h)
	}
	if h := frame(); h.Name() != "playstation" {
		t.Fatalf("release frame: %+v", h)
	}
	// The pad leaves with a stick pushed; the poller sends zeros; the hand stays.
	pushed := host.NeutralStandardPad(0, psID, "standard")
	pushed.Axes[0] = .9
	set(pushed)
	frame()
	set()
	if h := frame(); h.Name() != "playstation" {
		t.Fatalf("after the pad left: %+v", h)
	}
	// Another maker's pad at the same slot takes the hand with its own names.
	xb := host.NeutralStandardPad(0, xboxID, "standard")
	xb.Buttons[0] = 1
	set(xb)
	if h := frame(); h.Name() != "xbox" {
		t.Fatalf("an Xbox pad in the same slot: %+v", h)
	}
}

func TestStickJudgedWholeNotAxisByAxis(t *testing.T) {
	tr := NewTracker(Rules{})
	tr.Pads(pads(xboxID))
	tr.Feed(padAxis(0, 0, .9))
	tr.Frame()
	tr.Feed(key("KeyA", true))
	tr.Frame()
	// The stick swings from right to down in one poll. Read axis by axis it
	// would pass through (0, 0), rearm and fire; it never came back inside.
	tr.Feed(padAxis(0, 0, 0))
	tr.Feed(padAxis(0, 1, .9))
	if h, _ := tr.Frame(); h.Device != Keys {
		t.Fatalf("a swing with the stick held took the hand: %+v", h)
	}
}
