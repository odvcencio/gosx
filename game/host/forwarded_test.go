package host

import (
	"encoding/binary"
	"encoding/hex"
	"math"
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

// inputRecordGoldenKey is the record client/runtime/host/engine-worker.ts
// encodes for {k:"key", code:"KeyA", key:"a", down:true, ts:5380} received at
// worker time 400 (engine-worker.test.js asserts the same bytes).
const inputRecordGoldenKey = "0101010000000000000000000000000000000000000000000000000000000000000000000004b5400000000000c077400000000000007940000000000400010000000000000000004b65794161"

func TestInputRecordGolden(t *testing.T) {
	raw, err := hex.DecodeString(inputRecordGoldenKey)
	if err != nil {
		t.Fatal(err)
	}
	var d inputDecoder
	e, ok := d.decode(raw)
	if !ok {
		t.Fatal("the golden record did not decode")
	}
	want := InputEvent{Kind: "key", Code: "KeyA", Key: "a", Down: true, PageEpochMS: 5380, AtMS: 380, ReceivedMS: 400}
	if !reflect.DeepEqual(*e, want) {
		t.Fatalf("decoded %+v, want %+v", *e, want)
	}
}

// encodeInputRecord mirrors encodeInputRecord in engine-worker.ts, for tests.
func encodeInputRecord(e InputEvent) []byte {
	kinds := map[string]byte{"key": 1, "pointer": 2, "pad": 3, "blur": 4}
	texts := []string{e.Code, e.Key, e.PadID, e.Mapping, e.PointerType}
	b := make([]byte, inputRecordHeader, inputRecordHeader+8*(len(e.Buttons)+len(e.Axes))+64)
	le := binary.LittleEndian
	b[0], b[1] = inputRecordVersion, kinds[e.Kind]
	if e.Down {
		b[2] |= 1
	}
	if e.Connected {
		b[2] |= 2
	}
	le.PutUint32(b[4:], uint32(int32(e.Mask)))
	le.PutUint32(b[8:], uint32(int32(e.PointerID)))
	le.PutUint32(b[12:], uint32(int32(e.PadIndex)))
	for i, v := range []float64{e.X, e.Y, e.PageEpochMS, e.AtMS, e.ReceivedMS} {
		le.PutUint64(b[16+8*i:], math.Float64bits(v))
	}
	le.PutUint16(b[56:], uint16(len(e.Buttons)))
	le.PutUint16(b[58:], uint16(len(e.Axes)))
	for _, v := range append(append([]float64(nil), e.Buttons...), e.Axes...) {
		b = le.AppendUint64(b, math.Float64bits(v))
	}
	for i, s := range texts {
		le.PutUint16(b[60+2*i:], uint16(len(s)))
		b = append(b, s...)
	}
	return b
}

func TestInputRecordRoundTripAndRejects(t *testing.T) {
	var d inputDecoder
	events := []InputEvent{
		{Kind: "pointer", PointerType: "move", X: 3.5, Y: -2, Mask: 1, PointerID: 7, PageEpochMS: 1, AtMS: 2, ReceivedMS: 3},
		{Kind: "pad", PadIndex: 2, PadID: "Pad", Mapping: "standard", Connected: true, Buttons: []float64{1, .5}, Axes: []float64{-1}},
		{Kind: "blur", PageEpochMS: 9},
	}
	for _, want := range events {
		got, ok := d.decode(encodeInputRecord(want))
		if !ok || !reflect.DeepEqual(*got, want) {
			t.Fatalf("decoded %+v (ok %v), want %+v", got, ok, want)
		}
	}
	good := encodeInputRecord(events[1])
	for name, bad := range map[string][]byte{
		"short":     good[:10],
		"truncated": good[:len(good)-1],
		"version":   append([]byte{9}, good[1:]...),
		"kind":      append([]byte{1, 0}, good[2:]...),
	} {
		if _, ok := d.decode(bad); ok {
			t.Errorf("%s record decoded", name)
		}
	}
}

// Decoding and applying a repeated event allocates nothing.
func TestInputRecordDecodeAndApplyAllocateNothing(t *testing.T) {
	f := newForwardedInput()
	f.Subscribe(func(InputEvent) {})
	var d inputDecoder
	key := encodeInputRecord(InputEvent{Kind: "key", Code: "KeyA", Key: "a", Down: true})
	pad := encodeInputRecord(InputEvent{Kind: "pad", PadIndex: 0, PadID: "Pad", Mapping: "standard", Connected: true, Buttons: make([]float64, 17), Axes: make([]float64, 4)})
	run := func() {
		for _, record := range [][]byte{key, pad} {
			e, _ := d.decode(record)
			f.Apply(*e)
		}
	}
	run()
	if allocs := testing.AllocsPerRun(500, run); allocs != 0 {
		t.Fatalf("decode and apply allocated %v times per event pair", allocs)
	}
}
