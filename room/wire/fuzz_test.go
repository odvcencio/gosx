package wire

import (
	"bytes"
	"testing"
	"time"
)

// FuzzDecodeFrame feeds arbitrary bytes to every decoder. None may panic, and
// a frame that decodes must re-encode to bytes that decode to the same value.
func FuzzDecodeFrame(f *testing.F) {
	var in Input
	in.First, in.Count = 9, 2
	in.Inputs[0], in.Inputs[1] = []byte{1}, []byte{2, 3}
	f.Add(in.Append(nil))
	f.Add((&Ping{Seq: 1, Sent: 2}).Append(nil))
	f.Add((&Pong{Seq: 1, Sent: 2, Tick: 3, Frac: 4}).Append(nil))
	f.Add((&Welcome{Version: 1, Seat: 0, Seats: 2, TickRate: 30, SnapshotEvery: 1, Mode: "m", Code: "C", Token: "t"}).Append(nil))
	f.Add((&Room{Phase: PhaseRunning, Remaining: time.Second, Seats: []SeatInfo{{Kind: SeatBot, Role: "r"}}}).Append(nil))
	f.Add(append(AppendSnapshotHeader(nil, true, 1, 2, []SeatState{{Flags: 1, Input: []byte{5}}}), "body"...))
	f.Add([]byte{0, 'd', 1, 2, 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, frame []byte) {
		var in Input
		if in.Decode(frame) == nil {
			var again Input
			if err := again.Decode(in.Append(nil)); err != nil || again.First != in.First || again.Count != in.Count {
				t.Fatalf("input re-encode: %v", err)
			}
			for i := 0; i < in.Count; i++ {
				if !bytes.Equal(again.Inputs[i], in.Inputs[i]) {
					t.Fatal("input bytes changed")
				}
			}
		}
		var ping Ping
		if ping.Decode(frame) == nil {
			var again Ping
			if err := again.Decode(ping.Append(nil)); err != nil || again != ping {
				t.Fatal("ping re-encode")
			}
		}
		var pong Pong
		if pong.Decode(frame) == nil {
			var again Pong
			if err := again.Decode(pong.Append(nil)); err != nil || again != pong {
				t.Fatal("pong re-encode")
			}
		}
		var w Welcome
		if w.Decode(frame) == nil {
			var again Welcome
			if err := again.Decode(w.Append(nil)); err != nil || again != w {
				t.Fatal("welcome re-encode")
			}
		}
		var room Room
		if room.Decode(frame) == nil {
			var again Room
			if err := again.Decode(room.Append(nil)); err != nil || again.Phase != room.Phase || len(again.Seats) != len(room.Seats) {
				t.Fatal("room re-encode")
			}
		}
		var snap Snapshot
		if snap.Decode(frame) == nil {
			if snap.Epoch == 0 || len(snap.Seats) > MaxSeats {
				t.Fatal("snapshot invariants")
			}
			// A delta body must never panic against any base.
			_, _ = ApplyDelta(nil, frame, snap.Body, 1<<16)
		}
	})
}

// FuzzDelta checks that ApplyDelta survives arbitrary deltas and that encoding
// any target against any base round-trips exactly.
func FuzzDelta(f *testing.F) {
	f.Add([]byte("hello world state"), []byte("hello brave world state"), []byte{})
	f.Add([]byte{}, []byte("x"), []byte{2, 2, 'a', 'b'})
	f.Add(bytes.Repeat([]byte{1, 2, 3, 4}, 64), bytes.Repeat([]byte{1, 2, 3, 5}, 64), []byte{8, 1, 0})
	f.Fuzz(func(t *testing.T, base, target, hostile []byte) {
		if len(base) > 1<<16 || len(target) > 1<<16 {
			return
		}
		out, err := ApplyDelta(nil, base, hostile, 1<<16)
		if err == nil && len(out) > 1<<16 {
			t.Fatal("ApplyDelta exceeded maxLen")
		}
		var e DeltaEncoder
		e.SetBase(base)
		delta := e.Append(nil, target)
		got, err := ApplyDelta(nil, base, delta, 1<<16)
		if err != nil || !bytes.Equal(got, target) {
			t.Fatalf("round trip: %v", err)
		}
	})
}
