package seats

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

const (
	frame  = 16 * time.Millisecond
	xboxID = "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)"
	psID   = "DualSense Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 0ce6)"
)

func newMap() *actions.Map {
	m := actions.NewMap()
	m.Define(actions.Context{Name: "play", Actions: []actions.ActionDef{
		{Name: "act", Bindings: []actions.Binding{{Source: actions.Key("Space")}, {Source: actions.PadButton(0)}}},
		{Name: "sprint", Bindings: []actions.Binding{{Source: actions.Key("ShiftLeft")}, {Source: actions.PadButton(7)}}},
		{Name: "pause", Bindings: []actions.Binding{{Source: actions.Key("Escape")}, {Source: actions.PadButton(9)}}},
		{Name: "steer-y", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.PadAxis(1)}}},
	}})
	m.Push("play")
	return m
}

// rig drives Seats the way a page does: one poll read once, SyncPads, the
// poller's events through the seats, then Update.
type rig struct {
	t      *testing.T
	s      *Seats
	poller *actions.PadPoller
	pads   []host.GamepadState
}

func newRig(t *testing.T, cfg Config) *rig {
	if cfg.NewMap == nil {
		cfg.NewMap = newMap
	}
	if cfg.Prefer == nil {
		// Keyboard Home (0), pads Visitor (1) first, as the football drill does.
		cfg.Prefer = func(d Device) int {
			if d.Keyboard() {
				return 0
			}
			return 1
		}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, s: s, poller: actions.NewPadPoller()}
}

func (r *rig) frame() {
	r.s.SyncPads(r.pads)
	r.poller.PollStates(r.pads, r.s)
	r.s.Update(frame)
}

func (r *rig) key(code string, down bool) {
	v := 0.0
	if down {
		v = 1
	}
	r.s.Feed(actions.Event{Device: actions.DeviceKeyboard, Source: actions.Key(code), Value: v})
}

func stdPad(slot int, id string) host.GamepadState {
	return host.NeutralStandardPad(slot, id, "standard")
}

func (r *rig) plug(p host.GamepadState) { r.pads = append(r.pads, p) }

func (r *rig) button(slot, n int, v float64) {
	for i := range r.pads {
		if r.pads[i].Index == slot {
			r.pads[i].Buttons[n] = v
		}
	}
}

func (r *rig) axis(slot, n int, v float64) {
	for i := range r.pads {
		if r.pads[i].Index == slot {
			r.pads[i].Axes[n] = v
		}
	}
}

func (r *rig) unplug(slot int) {
	for i := range r.pads {
		if r.pads[i].Index == slot {
			r.pads = append(r.pads[:i], r.pads[i+1:]...)
			return
		}
	}
}

func (r *rig) settle() {
	for i := 0; i < 2; i++ {
		r.frame()
	}
}

func TestClaimByPressAndArmAfterRest(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	// Space on the keyboard claims Home and does not snap.
	r.key("Space", true)
	r.frame()
	if d, ok := r.s.Device(0); !ok || !d.Keyboard() {
		t.Fatalf("Home: %+v %v", d, ok)
	}
	if r.s.Map(0).Pressed("act") || r.s.Armed(0) {
		t.Fatal("the claiming press must not act, and the seat is not armed while it is held")
	}
	// A pad's A claims the Visitor while Space is still down.
	r.button(0, 0, 1)
	r.frame()
	if d, ok := r.s.Device(1); !ok || d.Pad != 0 || d.ID.Style != actions.PadXbox {
		t.Fatalf("Visitor: %+v %v", d, ok)
	}
	if !r.s.Ready() || r.s.Armed(0) || r.s.Armed(1) {
		t.Fatal("ready, both disarmed")
	}
	// Let go of both; one frame at rest arms both.
	r.key("Space", false)
	r.button(0, 0, 0)
	r.frame()
	if !r.s.Armed(0) || !r.s.Armed(1) {
		t.Fatal("a frame that ends at rest, with only releases in it, arms both")
	}
	r.key("Space", true)
	r.frame()
	if !r.s.Map(0).Pressed("act") || r.s.Map(1).Pressed("act") {
		t.Fatal("a fresh Space presses Home's act only")
	}
}

func TestTwoSeatsHoldAtOnce(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.key("Enter", true)
	r.key("Enter", false)
	r.button(0, 0, 1)
	r.frame()
	r.button(0, 0, 0)
	r.settle()
	r.key("ShiftLeft", true)
	r.button(0, 7, 1)
	r.axis(0, 1, 1)
	r.frame()
	if !r.s.Map(0).Down("sprint") || !r.s.Map(1).Down("sprint") || r.s.Map(0).Value("steer-y") != 0 || r.s.Map(1).Value("steer-y") != 1 {
		t.Fatal("each seat reads its own device only")
	}
}

func TestNoClaimsFromHeldRepeatsSticksTriggersOrTheMouse(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.button(0, 7, 1) // RT
	r.axis(0, 1, 1)   // stick
	r.frame()
	r.s.Feed(actions.Event{Device: actions.DeviceMouse, Source: actions.MouseButton(0), Value: 1})
	r.key("KeyW", true) // not a claim key
	r.frame()
	if _, ok := r.s.Device(0); ok {
		t.Fatal("no seat")
	}
	if _, ok := r.s.Device(1); ok {
		t.Fatal("no seat")
	}
	// A second analog report above the line is not a new press.
	r.button(0, 0, .6)
	r.frame()
	r.button(0, 0, .9)
	r.frame()
	if r.s.Generation() != 1 {
		t.Fatalf("generation %d: one claim only", r.s.Generation())
	}
}

func TestModalBlocksClaimsAndDisarms(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.key("Space", true)
	r.key("Space", false)
	r.settle()
	if !r.s.Armed(0) {
		t.Fatal("Home armed")
	}
	r.s.SetModal(true)
	if r.s.Armed(0) {
		t.Fatal("a modal disarms")
	}
	r.button(0, 0, 1)
	r.frame()
	if _, ok := r.s.Device(1); ok {
		t.Fatal("no claim under a modal")
	}
	r.s.SetModal(false)
	r.frame()
	if _, ok := r.s.Device(1); ok {
		t.Fatal("A held through the close claims nothing")
	}
	// A claimed Start still reaches the seat's Map while disarmed.
	r.s.SetModal(true)
	r.key("Escape", true)
	r.frame()
	if !r.s.Pressed("pause") || r.s.Armed(0) {
		t.Fatal("menus read a disarmed seat's presses")
	}
	r.button(0, 0, 0)
	r.frame()
	r.s.SetModal(false)
	r.button(0, 0, 1)
	r.frame()
	if _, ok := r.s.Device(1); !ok {
		t.Fatal("a fresh A after the modal claims")
	}
}

func TestLossAndReclaim(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.key("Space", true)
	r.key("Space", false)
	r.button(0, 0, 1)
	r.frame()
	r.button(0, 0, 0)
	r.settle()
	// The pad leaves with RT held.
	r.button(0, 7, 1)
	r.frame()
	if !r.s.Map(1).Down("sprint") {
		t.Fatal("sprint held")
	}
	r.unplug(0)
	r.frame()
	if _, ok := r.s.Device(1); ok {
		t.Fatal("the seat lost its pad")
	}
	if l, ok := r.s.Lost(1); !ok || l.Pad != 0 || !r.s.AnyLost() || r.s.Ready() {
		t.Fatalf("waiting: %+v %v", l, ok)
	}
	if st := r.s.Map(1).State("sprint"); st.Down {
		t.Fatalf("the lost pad's sprint is released: %+v", st)
	}
	// The keyboard cannot take the waiting seat: it holds Home.
	r.key("Enter", true)
	r.frame()
	r.key("Enter", false)
	if _, ok := r.s.Device(1); ok {
		t.Fatal("a held device claims nothing")
	}
	// Back without A: still waiting.
	back := stdPad(0, xboxID)
	back.Buttons[7] = 1
	r.plug(back)
	r.frame()
	if _, ok := r.s.Device(1); ok {
		t.Fatal("a returning pad needs a press")
	}
	// A with RT still held: claims, but is not armed until all is let go.
	r.button(0, 0, 1)
	r.frame()
	if _, ok := r.s.Device(1); !ok || r.s.Armed(1) {
		t.Fatal("reclaimed, disarmed")
	}
	r.button(0, 0, 0)
	r.frame()
	r.frame()
	if r.s.Armed(1) {
		t.Fatal("RT held: still disarmed")
	}
	r.button(0, 7, 0)
	r.settle()
	if !r.s.Armed(1) {
		t.Fatal("armed at rest")
	}
	r.button(0, 7, 1)
	r.frame()
	if !r.s.Map(1).Pressed("sprint") {
		t.Fatal("a fresh RT sprints")
	}
}

func TestSameSlotOtherIDIsALoss(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.button(0, 0, 1)
	r.frame()
	r.button(0, 0, 0)
	r.settle()
	// Another pad appears in slot 0 with A down and no frame without a pad.
	other := stdPad(0, psID)
	other.Buttons[0] = 1
	r.pads = []host.GamepadState{other}
	r.frame()
	d, ok := r.s.Device(1)
	if !ok || d.ID.Style != actions.PadPlayStation || r.s.Armed(1) {
		t.Fatalf("the replacement claims the waiting seat, disarmed: %+v %v", d, ok)
	}
}

func TestReclaimPrefersTheMatchingID(t *testing.T) {
	r := newRig(t, Config{Prefer: func(Device) int { return 0 }})
	r.plug(stdPad(0, xboxID))
	r.plug(stdPad(1, psID))
	r.frame()
	r.button(0, 0, 1) // Xbox -> seat 0
	r.frame()
	r.button(1, 0, 1) // PS -> seat 1
	r.frame()
	r.pads = nil
	r.frame()
	if !r.s.AnyLost() {
		t.Fatal("both lost")
	}
	// The PlayStation pad comes back first, in the Xbox's old slot.
	ps := stdPad(0, psID)
	ps.Buttons[0] = 1
	r.pads = []host.GamepadState{ps}
	r.frame()
	if d, ok := r.s.Device(1); !ok || d.ID.Style != actions.PadPlayStation || d.Pad != 0 {
		t.Fatalf("the PS pad goes to its own seat: %+v %v", d, ok)
	}
	// A pad matching neither goes to the first waiting seat.
	odd := stdPad(2, "Stub Standard Pad (STANDARD GAMEPAD)")
	odd.Buttons[0] = 1
	r.pads = append(r.pads, odd)
	r.frame()
	if d, ok := r.s.Device(0); !ok || d.Pad != 2 {
		t.Fatalf("the first waiting seat: %+v %v", d, ok)
	}
}

func TestIdenticalPadsTakeWaitingSeatsInOrder(t *testing.T) {
	r := newRig(t, Config{Prefer: func(Device) int { return 0 }})
	r.plug(stdPad(0, xboxID))
	r.plug(stdPad(1, xboxID))
	r.frame()
	r.button(0, 0, 1)
	r.frame()
	r.button(1, 0, 1)
	r.frame()
	// Only slot 1 drops; slot 0 keeps its seat.
	r.unplug(1)
	r.frame()
	if _, ok := r.s.Device(0); !ok {
		t.Fatal("the twin still connected keeps its seat")
	}
	twin := stdPad(3, xboxID)
	twin.Buttons[0] = 1
	r.plug(twin)
	r.frame()
	if d, ok := r.s.Device(1); !ok || d.Pad != 3 {
		t.Fatalf("the twin at a new slot takes the waiting seat: %+v %v", d, ok)
	}
	if s, ok := r.s.SeatFor(actions.PadDevice(1)); ok {
		t.Fatalf("no route left on the old slot: seat %d", s)
	}
}

func TestBlurDisarmsAndAHeldTriggerKeepsTheSeatDisarmed(t *testing.T) {
	r := newRig(t, Config{})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.button(0, 0, 1)
	r.frame()
	r.button(0, 0, 0)
	r.settle()
	r.button(0, 7, .8)
	r.frame()
	r.s.ReleaseAll() // blur
	// The poller sends no new event for a trigger that did not move, but the
	// poll still shows it held.
	r.settle()
	if r.s.Armed(1) {
		t.Fatal("a trigger held through a blur keeps the seat disarmed")
	}
	r.button(0, 7, .01)
	r.settle()
	if r.s.Armed(1) {
		t.Fatal("with the zero Rest, 0.01 is held")
	}
	r2 := newRig(t, Config{Rest: actions.Rest{Button: .05, Axis: .1}})
	r2.plug(stdPad(0, xboxID))
	r2.frame()
	r2.button(0, 0, 1)
	r2.frame()
	r2.button(0, 0, 0)
	r2.button(0, 7, .01)
	r2.settle()
	if !r2.s.Armed(1) {
		t.Fatal("under the Rest line a trigger is at rest")
	}
}

func TestATapWhileDisarmedDoesNotActOnTheArmingFrame(t *testing.T) {
	r := newRig(t, Config{})
	r.key("Space", true)
	r.key("Space", false)
	r.settle()
	r.s.Disarm()
	r.key("Space", true)
	r.key("Space", false)
	r.frame()
	if r.s.Armed(0) {
		t.Fatal("a frame with input does not arm")
	}
	r.frame()
	if !r.s.Armed(0) || r.s.Map(0).Pressed("act") {
		t.Fatal("armed on a quiet frame with nothing pressed")
	}
}

func TestSwapReleaseLeave(t *testing.T) {
	r := newRig(t, Config{Ticks: &actions.TickOptions{}})
	r.plug(stdPad(0, xboxID))
	r.frame()
	r.key("Space", true)
	r.key("Space", false)
	r.button(0, 0, 1)
	r.frame()
	r.button(0, 0, 0)
	r.settle()
	r.key("ShiftLeft", true)
	r.frame()
	gen := r.s.Generation()
	if !r.s.Swap(0, 1) || r.s.Generation() == gen || !r.s.TakeChanged() {
		t.Fatal("swap is a change")
	}
	if d, _ := r.s.Device(0); d.Keyboard() {
		t.Fatal("the pad now holds Home")
	}
	if r.s.Map(1).Down("sprint") {
		t.Fatal("a swap releases held input")
	}
	r.s.Ticks(1).Tick()
	if r.s.Ticks(1).Pressed("sprint") {
		t.Fatal("a swap clears the tick buffers")
	}
	if r.s.Swap(0, 5) {
		t.Fatal("out of range")
	}
	if h, ok := r.s.Hand(0); !ok || h.Name() != "xbox" {
		t.Fatalf("Home's hand: %+v", h)
	}
	r.s.Release(1)
	if _, ok := r.s.Device(1); ok {
		t.Fatal("released")
	}
	r.s.Leave()
	if _, ok := r.s.Device(0); ok || r.s.Ready() {
		t.Fatal("left")
	}
	r.key("ShiftLeft", false)
	r.button(0, 7, 1)
	r.frame()
	if r.s.Map(0).Down("sprint") {
		t.Fatal("no route after leaving")
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("NewMap is required")
	}
	shared := actions.NewMap()
	if _, err := New(Config{NewMap: func() *actions.Map { return shared }}); err == nil {
		t.Fatal("seats need distinct Maps")
	}
	if _, err := New(Config{Seats: -1, NewMap: newMap}); err == nil {
		t.Fatal("negative seats")
	}
	s, err := New(Config{NewMap: newMap})
	if err != nil || s.Len() != 2 || s.Map(2) != nil || s.Armed(-1) {
		t.Fatal("defaults and range checks")
	}
}

// TestPadAdapter drives the seats through the frame-latched pad test source,
// as the page checks do.
func TestPadAdapter(t *testing.T) {
	src := host.NewPadTestSource()
	s, _ := New(Config{NewMap: newMap})
	poller := actions.NewPadPoller()
	seq := uint64(0)
	set := func(ps ...host.GamepadState) {
		seq++
		if err := src.Enqueue(host.PadSet{Seq: seq, Next: true, Pads: ps}); err != nil {
			t.Fatal(err)
		}
	}
	step := func() {
		src.BeginFrame()
		list := src.Gamepads()
		s.SyncPads(list)
		poller.PollStates(list, s)
		s.Update(frame)
		src.EndFrame()
	}
	a := stdPad(0, xboxID)
	down := stdPad(0, xboxID)
	down.Buttons[0] = 1
	set(a)
	step()
	// Press and release queued together land on two frames: one claim.
	set(down)
	set(a)
	step()
	if _, ok := s.Device(0); !ok {
		t.Fatal("A claimed seat 0")
	}
	step()
	step()
	if !s.Armed(0) {
		t.Fatal("armed after the release frame and a quiet one")
	}
	set()
	step()
	if _, ok := s.Lost(0); !ok {
		t.Fatal("lost when the adapter drops the pad")
	}
}
