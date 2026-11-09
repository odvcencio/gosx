package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

func TestInputRoundTrip(t *testing.T) {
	var in Input
	in.First, in.Count = 41, 3
	in.Inputs[0] = []byte{1}
	in.Inputs[1] = nil
	in.Inputs[2] = []byte{2, 3, 4}
	frame := in.Append(nil)
	if k, ok := KindOf(frame); !ok || k != KindInput {
		t.Fatalf("kind %v %v", k, ok)
	}
	var out Input
	if err := out.Decode(frame); err != nil {
		t.Fatal(err)
	}
	if out.First != 41 || out.Count != 3 || out.Last() != 43 {
		t.Fatalf("decoded %+v", out)
	}
	for i := 0; i < 3; i++ {
		if !bytes.Equal(out.Inputs[i], in.Inputs[i]) {
			t.Fatalf("input %d: %v != %v", i, out.Inputs[i], in.Inputs[i])
		}
	}
	// Tick zero does not exist: ticks count from one.
	in.First = 0
	if err := out.Decode(in.Append(nil)); err != ErrMalformed {
		t.Fatalf("first tick 0 accepted: %v", err)
	}
}

func TestPingPongRoundTrip(t *testing.T) {
	ping := Ping{Seq: 7, Sent: -123456789}
	var gotPing Ping
	if err := gotPing.Decode(ping.Append(nil)); err != nil || gotPing != ping {
		t.Fatalf("ping %+v %v", gotPing, err)
	}
	pong := Pong{Seq: 7, Sent: 99, Tick: 1234, Frac: 32768}
	var gotPong Pong
	if err := gotPong.Decode(pong.Append(nil)); err != nil || gotPong != pong {
		t.Fatalf("pong %+v %v", gotPong, err)
	}
	if got := gotPong.ServerTick(); got != 1234.5 {
		t.Fatalf("server tick %v", got)
	}
}

func TestWelcomeRoundTrip(t *testing.T) {
	w := Welcome{Version: Version, Seat: -1, Seats: 4, TickRate: 30, InputDelay: 2, SnapshotEvery: 1,
		Seed: 0xfeedface, Mode: "duel", Code: "7K3QX", Token: "abcdef"}
	var got Welcome
	if err := got.Decode(w.Append(nil)); err != nil {
		t.Fatal(err)
	}
	if got != w {
		t.Fatalf("got %+v want %+v", got, w)
	}
	bad := w
	bad.Seat = 4
	if err := got.Decode(bad.Append(nil)); err != ErrMalformed {
		t.Fatal("seat outside the room accepted")
	}
}

func TestRoomRoundTrip(t *testing.T) {
	m := Room{Phase: PhaseCountdown, Remaining: 2500 * time.Millisecond, Tick: 0, Seats: []SeatInfo{
		{Kind: SeatHuman, Connected: true, Role: "red", Name: "ada"},
		{Kind: SeatBot, Role: "blue", Name: "bot"},
		{Kind: SeatHuman, Covered: true, Role: "blue"},
		{Kind: SeatEmpty},
	}, Results: []byte(`{"winner":0}`)}
	var got Room
	if err := got.Decode(m.Append(nil)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("got %+v want %+v", got, m)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	seats := []SeatState{{Flags: FlagHuman | FlagConnected, Lead: 3, Input: []byte{9}}, {Flags: FlagBot, Input: nil}}
	frame := AppendSnapshotHeader(nil, true, 5, 99, seats)
	frame = append(frame, "state"...)
	var s Snapshot
	if err := s.Decode(frame); err != nil {
		t.Fatal(err)
	}
	if !s.Keyframe || s.Epoch != 5 || s.Tick != 99 || string(s.Body) != "state" || len(s.Seats) != 2 ||
		s.Seats[0].Lead != 3 || !bytes.Equal(s.Seats[0].Input, []byte{9}) || s.Seats[1].Flags != FlagBot {
		t.Fatalf("decoded %+v", s)
	}
	delta := AppendSnapshotHeader(nil, false, 5, 100, seats)
	if err := s.Decode(delta); err != nil || s.Keyframe || s.Tick != 100 || len(s.Body) != 0 {
		t.Fatalf("delta %+v %v", s, err)
	}
	if err := s.Decode(AppendSnapshotHeader(nil, true, 0, 1, nil)); err != ErrMalformed {
		t.Fatal("epoch 0 accepted")
	}
}

func TestKindOfAndRetryable(t *testing.T) {
	if _, ok := KindOf([]byte{1, 'i'}); ok {
		t.Fatal("CRDT prefix accepted")
	}
	if _, ok := KindOf([]byte{0, 'z'}); ok {
		t.Fatal("unknown kind accepted")
	}
	if !KindWelcome.Reliable() || KindDelta.Reliable() {
		t.Fatal("reliable kinds")
	}
	if Retryable(CloseRoomOver) || Retryable(CloseReplaced) || !Retryable(1006) || !Retryable(CloseShutdown) {
		t.Fatal("retryable codes")
	}
}

func roundTripDelta(t testing.TB, e *DeltaEncoder, base, target []byte) []byte {
	t.Helper()
	e.SetBase(base)
	delta := e.Append(nil, target)
	got, err := ApplyDelta(nil, base, delta, 0)
	if err != nil {
		t.Fatalf("apply: %v (base %d target %d)", err, len(base), len(target))
	}
	if !bytes.Equal(got, target) {
		t.Fatalf("delta round trip differs (base %d target %d)", len(base), len(target))
	}
	return delta
}

func TestDeltaFixedLayoutIsSmall(t *testing.T) {
	// 64 records of 16 bytes; 3 records change one field each.
	base := make([]byte, 64*16)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range base {
		base[i] = byte(rng.IntN(256))
	}
	target := append([]byte(nil), base...)
	for _, rec := range []int{3, 30, 60} {
		binary.LittleEndian.PutUint32(target[rec*16+4:], rng.Uint32())
	}
	var e DeltaEncoder
	delta := roundTripDelta(t, &e, base, target)
	if len(delta) > 40 {
		t.Fatalf("delta of 3 changed fields in 1 KiB is %d bytes; want <= 40", len(delta))
	}
}

func TestDeltaShiftedJSONFindsRuns(t *testing.T) {
	type body struct {
		ID   int     `json:"id"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
		Name string  `json:"name"`
	}
	bodies := make([]body, 40)
	for i := range bodies {
		bodies[i] = body{ID: i, X: float64(i) * 1.5, Y: 2, Name: "body"}
	}
	base, _ := json.Marshal(bodies)
	bodies[2].X = 123.456789 // longer number: shifts everything after it
	bodies[20].Y = -7
	target, _ := json.Marshal(bodies)
	var e DeltaEncoder
	delta := roundTripDelta(t, &e, base, target)
	if len(delta)*8 > len(target) {
		t.Fatalf("delta %d bytes for a %d byte target; want under 1/8", len(delta), len(target))
	}
}

func TestDeltaEdgeCases(t *testing.T) {
	var e DeltaEncoder
	roundTripDelta(t, &e, nil, nil)
	roundTripDelta(t, &e, nil, []byte("fresh"))
	roundTripDelta(t, &e, []byte("old state"), nil)
	roundTripDelta(t, &e, []byte("abc"), []byte("abc"))
	same := bytes.Repeat([]byte("0123456789"), 500)
	if d := roundTripDelta(t, &e, same, same); len(d) > 8 {
		t.Fatalf("identical 5 KB state took %d delta bytes", len(d))
	}
	grown := append(append([]byte(nil), same...), "tail"...)
	roundTripDelta(t, &e, same, grown)
	roundTripDelta(t, &e, grown, same)
}

func TestDeltaRandomMutations(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	var e DeltaEncoder
	for round := 0; round < 300; round++ {
		base := make([]byte, rng.IntN(3000))
		for i := range base {
			base[i] = byte(rng.IntN(8)) // low entropy, like real state
		}
		target := append([]byte(nil), base...)
		for m := rng.IntN(20); m > 0; m-- {
			if len(target) == 0 {
				target = append(target, byte(rng.IntN(256)))
				continue
			}
			p := rng.IntN(len(target))
			switch rng.IntN(3) {
			case 0:
				target[p] = byte(rng.IntN(256))
			case 1:
				target = append(target[:p], append([]byte{byte(rng.IntN(256)), byte(rng.IntN(256))}, target[p:]...)...)
			default:
				end := min(len(target), p+rng.IntN(10)+1)
				target = append(target[:p], target[end:]...)
			}
		}
		roundTripDelta(t, &e, base, target)
	}
}

func TestApplyDeltaRejectsHostileOps(t *testing.T) {
	base := []byte("0123456789")
	for name, delta := range map[string][]byte{
		"empty":           nil,
		"too long":        binary.AppendUvarint(nil, MaxStateSize+1),
		"copy past base":  append(binary.AppendUvarint(nil, 8), 9, 16), // copy 8 from pos 8
		"copy before":     append(binary.AppendUvarint(nil, 4), 1, 1),  // rel -1
		"literal overrun": append(binary.AppendUvarint(nil, 2), 6, 'a', 'b', 'c'),
		"zero literal":    append(binary.AppendUvarint(nil, 1), 0),
		"trailing bytes":  append(binary.AppendUvarint(nil, 1), 2, 'a', 0),
		"truncated":       append(binary.AppendUvarint(nil, 4), 8, 'a'),
	} {
		if _, err := ApplyDelta(nil, base, delta, 0); err != ErrDelta {
			t.Errorf("%s: err %v, want ErrDelta", name, err)
		}
	}
	if _, err := ApplyDelta(nil, base, binary.AppendUvarint(nil, 0), 0); err != nil {
		t.Fatalf("empty target: %v", err)
	}
}

func TestWireAllocations(t *testing.T) {
	var in Input
	in.First, in.Count = 100, 8
	for i := 0; i < 8; i++ {
		in.Inputs[i] = []byte{byte(i), 1, 2, 3}
	}
	buf := make([]byte, 0, 256)
	frame := in.Append(buf[:0])
	var out Input
	seats := make([]SeatState, 4)
	for i := range seats {
		seats[i] = SeatState{Flags: FlagHuman, Lead: 2, Input: []byte{1, 2}}
	}
	snapBuf := make([]byte, 0, 4096)
	var snap Snapshot
	snap.Seats = make([]SeatState, 0, 8)
	base := bytes.Repeat([]byte("state-record-16b"), 64)
	target := append([]byte(nil), base...)
	target[100] ^= 0xff
	var e DeltaEncoder
	e.SetBase(base)
	decoded := make([]byte, 0, len(target))
	// In order: each check reads what the one before it wrote.
	checks := []struct {
		name string
		fn   func()
	}{
		{"Input.Append", func() { frame = in.Append(buf[:0]) }},
		{"Input.Decode", func() { _ = out.Decode(frame) }},
		{"Snapshot encode", func() {
			snapBuf = AppendSnapshotHeader(snapBuf[:0], false, 1, 2, seats)
			snapBuf = e.Append(snapBuf, target)
		}},
		{"Snapshot.Decode", func() { _ = snap.Decode(snapBuf) }},
		{"ApplyDelta", func() {
			_ = snap.Decode(snapBuf)
			decoded, _ = ApplyDelta(decoded, base, snap.Body, 0)
		}},
	}
	for _, c := range checks {
		c.fn()
		if n := testing.AllocsPerRun(100, c.fn); n != 0 {
			t.Errorf("%s: %v allocations per run, want 0", c.name, n)
		}
	}
	if !bytes.Equal(decoded, target) {
		t.Fatal("decoded delta differs")
	}
}

func BenchmarkDeltaEncode4KB(b *testing.B) {
	base := make([]byte, 4096)
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range base {
		base[i] = byte(rng.IntN(256))
	}
	target := append([]byte(nil), base...)
	for i := 0; i < 32; i++ {
		target[rng.IntN(len(target))] ^= 0x55
	}
	var e DeltaEncoder
	e.SetBase(base)
	dst := make([]byte, 0, 8192)
	b.SetBytes(int64(len(target)))
	b.ReportAllocs()
	for b.Loop() {
		dst = e.Append(dst[:0], target)
	}
}
