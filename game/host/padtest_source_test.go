package host_test

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

type edge struct {
	Frame  uint64
	Device string
	Code   string
	Value  float64
}

type edgeSink struct {
	frame  uint64
	events []edge
}

func (s *edgeSink) Feed(e actions.Event) {
	s.events = append(s.events, edge{s.frame, e.Device, e.Source.Code, e.Value})
}
func (s *edgeSink) ReleaseAll() {}

func (s *edgeSink) take() []edge {
	out := s.events
	s.events = nil
	return out
}

// step runs one frame the way a game would: begin, poll, end.
func step(src *host.PadTestSource, poller *actions.PadPoller, sink *edgeSink, read bool) host.PadFrameReceipt {
	sink.frame = src.BeginFrame()
	if read {
		poller.Poll(src, sink)
	}
	return src.EndFrame()
}

func pad(index int, id string) host.GamepadState {
	return host.NeutralStandardPad(index, id, "standard")
}

func held(p host.GamepadState, button int) host.GamepadState {
	p.Buttons[button] = 1
	return p
}

func mustEnqueue(t *testing.T, s *host.PadTestSource, req host.PadSet) {
	t.Helper()
	if err := s.Enqueue(req); err != nil {
		t.Fatalf("enqueue seq %d: %v", req.Seq, err)
	}
}

func TestPadTestSourceStableSlots(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(1, "B"), held(pad(0, "A"), 0)}})
	step(src, poller, sink, true)
	got := src.Gamepads()
	if len(got) != 2 || got[0].Index != 0 || got[1].Index != 1 {
		t.Fatalf("pads not in ascending index order: %+v", got)
	}
	sink.take()
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true, Pads: []host.GamepadState{pad(1, "B")}})
	step(src, poller, sink, true)
	got = src.Gamepads()
	if len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("unplugging slot 0 must leave slot 1 at index 1, got %+v", got)
	}
	// The poller releases pad:0's held button and never renames pad:1.
	want := []edge{{2, "pad:0", "button0", 0}}
	if ev := sink.take(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("events %+v, want %+v", ev, want)
	}
}

func TestPadTestSourceAtomicTwoPadChange(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A"), pad(1, "B")}})
	step(src, poller, sink, true)
	sink.take()
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true, Pads: []host.GamepadState{held(pad(0, "A"), 3), held(pad(1, "B"), 4)}})
	// Reads before the frame begins see neither change.
	if got := src.Gamepads(); got[0].Buttons[3] != 0 || got[1].Buttons[4] != 0 {
		t.Fatal("a queued change leaked before BeginFrame")
	}
	step(src, poller, sink, true)
	want := []edge{{2, "pad:0", "button3", 1}, {2, "pad:1", "button4", 1}}
	if ev := sink.take(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("both pads must change in one frame: %+v, want %+v", ev, want)
	}
}

func TestPadTestSourceSameIDReplugNeedsAbsentPoll(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{held(pad(0, "SAME"), 0)}})
	step(src, poller, sink, true)
	sink.take()

	// Unplug and replug queued together still land on two frames, so the
	// poller sees the slot empty and releases the held button.
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true})
	mustEnqueue(t, src, host.PadSet{Seq: 3, Next: true, Pads: []host.GamepadState{held(pad(0, "SAME"), 0)}})
	step(src, poller, sink, true)
	step(src, poller, sink, true)
	want := []edge{{2, "pad:0", "button0", 0}, {3, "pad:0", "button0", 1}}
	if ev := sink.take(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("events %+v, want %+v", ev, want)
	}

	// Without an absent poll, a same-ID replacement shows no edge at all:
	// the poller cannot tell it from the pad it already had.
	mustEnqueue(t, src, host.PadSet{Seq: 4, Next: true, Pads: []host.GamepadState{held(pad(0, "SAME"), 0)}})
	step(src, poller, sink, true)
	if ev := sink.take(); len(ev) != 0 {
		t.Fatalf("replacement with the same ID and values made events: %+v", ev)
	}
}

func TestPadTestSourceLateExactFrameRejected(t *testing.T) {
	src := host.NewPadTestSource()
	for i := 0; i < 3; i++ {
		src.BeginFrame()
		src.EndFrame()
	}
	src.BeginFrame() // frame 4 is open
	for _, frame := range []uint64{0, 3, 4} {
		err := src.Enqueue(host.PadSet{Seq: frame + 1, Frame: frame, Pads: []host.GamepadState{pad(0, "A")}})
		var late *host.PadLateError
		if !errors.As(err, &late) || late.Current != 4 || late.Requested != frame {
			t.Fatalf("frame %d: err = %v, want PadLateError at current frame 4", frame, err)
		}
	}
	src.EndFrame()
	mustEnqueue(t, src, host.PadSet{Seq: 10, Frame: 6, Pads: []host.GamepadState{pad(0, "A")}})
	src.BeginFrame() // 5: not due
	if len(src.Gamepads()) != 0 {
		t.Fatal("exact-frame change applied early")
	}
	src.EndFrame()
	src.BeginFrame() // 6: due
	if len(src.Gamepads()) != 1 {
		t.Fatal("exact-frame change not applied at its frame")
	}
	if r := src.EndFrame(); r.Seq != 10 || r.LatchedFrame != 6 || r.Frame != 6 {
		t.Fatalf("receipt %+v", r)
	}
}

func TestPadTestSourceDuplicateReadsDoNotAdvance(t *testing.T) {
	src := host.NewPadTestSource()
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true, Pads: []host.GamepadState{held(pad(0, "A"), 0)}})
	src.BeginFrame()
	a, b, c := src.Gamepads(), src.Gamepads(), src.Gamepads()
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(b, c) || a[0].Buttons[0] != 0 {
		t.Fatalf("reads in one frame differ or consumed the second change: %+v %+v %+v", a, b, c)
	}
	r := src.EndFrame()
	if r.Seq != 1 || r.SampledFrames != 1 || r.Drawn != 1 {
		t.Fatalf("receipt %+v: three reads must count once and acknowledge only seq 1", r)
	}
	src.BeginFrame()
	if src.Gamepads()[0].Buttons[0] != 1 {
		t.Fatal("second change did not land on the next frame")
	}
	if r := src.EndFrame(); r.Seq != 2 || r.SampledFrames != 2 {
		t.Fatalf("receipt %+v", r)
	}
	// A read outside a frame returns the state but marks nothing.
	src.Gamepads()
	if d, s := src.Counts(); d != 2 || s != 2 {
		t.Fatalf("counts drawn=%d sampled=%d", d, s)
	}
}

func TestPadTestSourcePressThenReleaseOrdered(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	step(src, poller, sink, true)
	sink.take()
	// Queued together, as a fast driver would.
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true, Pads: []host.GamepadState{held(pad(0, "A"), 0)}})
	mustEnqueue(t, src, host.PadSet{Seq: 3, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	r2 := step(src, poller, sink, true)
	r3 := step(src, poller, sink, true)
	if r2.Seq != 2 || r3.Seq != 3 || r2.Frame >= r3.Frame {
		t.Fatalf("receipts out of order: %+v then %+v", r2, r3)
	}
	want := []edge{{2, "pad:0", "button0", 1}, {3, "pad:0", "button0", 0}}
	if ev := sink.take(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("events %+v, want %+v", ev, want)
	}
}

func TestPadTestSourceUnreadChangeAckedByFirstReadingFrame(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	if r := step(src, poller, sink, false); r.Seq != 0 || r.Sampled {
		t.Fatalf("a frame with no read acknowledged: %+v", r)
	}
	r := step(src, poller, sink, true)
	if r.Seq != 1 || r.LatchedFrame != 1 || r.Frame != 2 || r.SampledFrames != 1 || r.Drawn != 2 {
		t.Fatalf("receipt %+v", r)
	}
	if r := step(src, poller, sink, true); r.Seq != 0 {
		t.Fatalf("acknowledged twice: %+v", r)
	}
}

func TestPadTestSourceMissedExactFrame(t *testing.T) {
	src := host.NewPadTestSource()
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	mustEnqueue(t, src, host.PadSet{Seq: 2, Frame: 1, Pads: []host.GamepadState{pad(0, "B")}})
	src.BeginFrame()
	src.Gamepads()
	src.EndFrame()
	src.BeginFrame()
	src.Gamepads()
	r := src.EndFrame()
	if !reflect.DeepEqual(r.Missed, []uint64{2}) {
		t.Fatalf("missed = %v, want [2]", r.Missed)
	}
	if got := src.Gamepads(); got[0].ID != "A" {
		t.Fatal("a missed change must not be applied late")
	}
}

func TestPadTestSourceSequenceAndLose(t *testing.T) {
	src := host.NewPadTestSource()
	mustEnqueue(t, src, host.PadSet{Seq: 5, Next: true, Pads: []host.GamepadState{pad(0, "A")}})
	for _, seq := range []uint64{0, 4, 5} {
		if err := src.Enqueue(host.PadSet{Seq: seq, Next: true}); !errors.Is(err, host.ErrPadSequence) {
			t.Fatalf("seq %d: %v", seq, err)
		}
	}
	src.BeginFrame()
	if len(src.Gamepads()) != 1 {
		t.Fatal("pad missing")
	}
	src.EndFrame()
	src.Lose()
	src.BeginFrame()
	if len(src.Gamepads()) != 0 {
		t.Fatal("a lost source must expose no pads")
	}
	if err := src.Enqueue(host.PadSet{Seq: 6, Next: true}); !errors.Is(err, host.ErrPadSourceLost) {
		t.Fatalf("enqueue after Lose: %v", err)
	}
}

func TestPadTestSourceCopiesCallerSlices(t *testing.T) {
	src := host.NewPadTestSource()
	p := pad(0, "A")
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{p}})
	p.Buttons[0], p.Axes[0] = 1, 1
	src.BeginFrame()
	got := src.Gamepads()
	if got[0].Buttons[0] != 0 || got[0].Axes[0] != 0 {
		t.Fatal("source kept the caller's slices")
	}
}

func TestPadTestSourceValidation(t *testing.T) {
	long := make([]byte, host.PadTestMaxID+1)
	for i := range long {
		long[i] = 'x'
	}
	bad := map[string]host.GamepadState{
		"negative index":   pad(-1, "A"),
		"index too high":   pad(host.PadTestMaxSlots, "A"),
		"empty id":         pad(0, ""),
		"long id":          pad(0, string(long)),
		"mapping":          {Index: 0, ID: "A", Mapping: "weird"},
		"too many buttons": {Index: 0, ID: "A", Buttons: make([]float64, host.PadTestMaxButtons+1)},
		"too many axes":    {Index: 0, ID: "A", Axes: make([]float64, host.PadTestMaxAxes+1)},
		"button above 1":   {Index: 0, ID: "A", Buttons: []float64{1.5}},
		"button below 0":   {Index: 0, ID: "A", Buttons: []float64{-0.1}},
		"button NaN":       {Index: 0, ID: "A", Buttons: []float64{math.NaN()}},
		"axis above 1":     {Index: 0, ID: "A", Axes: []float64{1.01}},
		"axis below -1":    {Index: 0, ID: "A", Axes: []float64{-1.01}},
		"axis Inf":         {Index: 0, ID: "A", Axes: []float64{math.Inf(1)}},
	}
	for name, st := range bad {
		src := host.NewPadTestSource()
		if err := src.Enqueue(host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{st}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
		// A refused change must not use up its sequence number.
		if err := src.Enqueue(host.PadSet{Seq: 1, Next: true}); err != nil {
			t.Errorf("%s: refused change consumed seq 1: %v", name, err)
		}
	}
	src := host.NewPadTestSource()
	if err := src.Enqueue(host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{pad(0, "A"), pad(0, "B")}}); err == nil {
		t.Error("duplicate index accepted")
	}
	edge := host.GamepadState{Index: 3, ID: "A", Mapping: "xr-standard", Buttons: []float64{0, 1}, Axes: []float64{-1, 1}}
	if err := src.Enqueue(host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{edge}}); err != nil {
		t.Errorf("edge values refused: %v", err)
	}
}

func TestPadPollerIgnoresNonStandardMapping(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	odd := host.NeutralStandardPad(0, "Odd Pad", "")
	odd.Buttons[0] = 1
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{odd}})
	step(src, poller, sink, true)
	if ev := sink.take(); len(ev) != 0 {
		t.Fatalf("non-standard pad produced events: %+v", ev)
	}
	poller.AllowNonStandard = true
	step(src, poller, sink, true)
	if ev := sink.take(); len(ev) != 1 || ev[0].Code != "button0" {
		t.Fatalf("with AllowNonStandard: %+v", ev)
	}
}

func TestPadPollerReleasesOnIDChange(t *testing.T) {
	src := host.NewPadTestSource()
	poller, sink := actions.NewPadPoller(), &edgeSink{}
	mustEnqueue(t, src, host.PadSet{Seq: 1, Next: true, Pads: []host.GamepadState{held(pad(0, "ONE"), 2)}})
	step(src, poller, sink, true)
	sink.take()
	mustEnqueue(t, src, host.PadSet{Seq: 2, Next: true, Pads: []host.GamepadState{held(pad(0, "TWO"), 2)}})
	step(src, poller, sink, true)
	want := []edge{{2, "pad:0", "button2", 0}, {2, "pad:0", "button2", 1}}
	if ev := sink.take(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("events %+v, want %+v", ev, want)
	}
}

func TestPadTestSessionProtocol(t *testing.T) {
	src := host.NewPadTestSource()
	sess := host.NewPadTestSession(src, "tok")
	if h := sess.Hello(); h.Type != host.PadMsgHello || h.Token != "tok" || h.Version != host.PadTestProtocolVersion {
		t.Fatalf("hello %+v", h)
	}
	set := host.PadMessage{Type: host.PadMsgSet, Seq: 1, At: host.PadAtNext, Pads: host.PadWireFrom([]host.GamepadState{pad(0, "A")})}
	if out := sess.Handle(set); len(out) != 0 {
		t.Fatalf("set replied %+v", out)
	}
	// A watch that is already satisfied answers at once.
	out := sess.Handle(host.PadMessage{Type: host.PadMsgWatch, Watch: 9, Relative: true})
	if len(out) != 1 || out[0].Type != host.PadMsgProgress || out[0].Watch != 9 {
		t.Fatalf("immediate watch: %+v", out)
	}
	sess.Handle(host.PadMessage{Type: host.PadMsgWatch, Watch: 10, Drawn: 2, Relative: true})
	src.BeginFrame()
	src.Gamepads()
	msgs := sess.EndFrame()
	types := []string{}
	for _, m := range msgs {
		types = append(types, m.Type)
	}
	if !reflect.DeepEqual(types, []string{host.PadMsgApplied, host.PadMsgProgress}) {
		t.Fatalf("first frame sent %v (applied, then the ready progress)", types)
	}
	src.BeginFrame()
	msgs = sess.EndFrame()
	if len(msgs) != 1 || msgs[0].Watch != 10 || msgs[0].Drawn != 2 {
		t.Fatalf("watch for 2 more frames: %+v", msgs)
	}
	// Late exact frame, bad sequence and bad value come back as rejected.
	for name, tc := range map[string]struct {
		m      host.PadMessage
		reason string
	}{
		"late":     {host.PadMessage{Type: host.PadMsgSet, Seq: 2, At: host.PadAtFrame, Frame: 1}, host.PadRejectLate},
		"sequence": {host.PadMessage{Type: host.PadMsgSet, Seq: 1, At: host.PadAtNext}, host.PadRejectSequence},
		"invalid":  {host.PadMessage{Type: host.PadMsgSet, Seq: 3, At: host.PadAtNext, Pads: []host.PadWire{{Index: 9, ID: "A"}}}, host.PadRejectInvalid},
		"bad at":   {host.PadMessage{Type: host.PadMsgSet, Seq: 4, At: "later"}, host.PadRejectInvalid},
	} {
		out := sess.Handle(tc.m)
		if len(out) != 1 || out[0].Type != host.PadMsgRejected || out[0].Reason != tc.reason || out[0].Frame != 2 {
			t.Errorf("%s: %+v", name, out)
		}
	}
}

func TestPadMessageCodec(t *testing.T) {
	data, err := host.EncodePadMessage(host.PadMessage{Type: host.PadMsgSet, Seq: 3, At: host.PadAtNext,
		Pads: host.PadWireFrom([]host.GamepadState{pad(0, "A")})})
	if err != nil {
		t.Fatal(err)
	}
	m, err := host.DecodePadMessage(data)
	if err != nil || m.Seq != 3 || len(m.Pads) != 1 || len(m.Pads[0].Buttons) != 17 {
		t.Fatalf("round trip: %+v %v", m, err)
	}
	for name, raw := range map[string]string{
		"unknown field": `{"type":"hello","extra":1}`,
		"unknown type":  `{"type":"bogus"}`,
		"trailing":      `{"type":"hello"} {"type":"hello"}`,
		"not json":      `hello`,
	} {
		if _, err := host.DecodePadMessage([]byte(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := host.DecodePadMessage(make([]byte, host.PadTestMaxMessage+1)); err == nil {
		t.Error("oversize accepted")
	}
}

func TestPadTestEndpoint(t *testing.T) {
	v := host.FormatPadTestEndpoint(8123, "0123456789abcdef0123456789abcdef")
	port, token, err := host.ParsePadTestEndpoint(v)
	if err != nil || port != 8123 || token != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("%q -> %d %q %v", v, port, token, err)
	}
	for _, bad := range []string{"", "8123", "0.0123456789abcdef", "70000.0123456789abcdef", "08123.0123456789abcdef",
		"8123.short", "8123.0123456789abcdef!", "x.0123456789abcdef", "8123.0123456789abcdef.more"} {
		if _, _, err := host.ParsePadTestEndpoint(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got := host.PadTestWebSocketURL(9); got != "ws://127.0.0.1:9/gosx-padtest" {
		t.Fatal(got)
	}
}
