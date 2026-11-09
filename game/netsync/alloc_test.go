package netsync

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// countSim adds up input bytes per seat. It allocates nothing after warm-up,
// so allocation counts below belong to netsync.
type countSim struct {
	tick uint64
	sums [2]uint64
	buf  []byte
}

func (s *countSim) Tick(in map[string]sim.Input) {
	s.tick++
	for i := range s.sums {
		for _, b := range in[mode.PlayerID(i)].Data {
			s.sums[i] += uint64(b)
		}
	}
}
func (s *countSim) Snapshot() []byte {
	s.buf = binary.LittleEndian.AppendUint64(s.buf[:0], s.tick)
	for _, v := range s.sums {
		s.buf = binary.LittleEndian.AppendUint64(s.buf, v)
	}
	return s.buf
}
func (s *countSim) Restore(b []byte) { _ = s.RestoreSnapshot(b) }
func (s *countSim) RestoreSnapshot(b []byte) error {
	if len(b) != 24 {
		return errors.New("size")
	}
	s.tick = binary.LittleEndian.Uint64(b)
	s.sums[0], s.sums[1] = binary.LittleEndian.Uint64(b[8:]), binary.LittleEndian.Uint64(b[16:])
	return nil
}
func (s *countSim) State() []byte { return s.Snapshot() }

// scriptConn hands out queued frames and swallows sends.
type scriptConn struct {
	queue [][]byte
	sent  int
}

func (c *scriptConn) Send([]byte) error { c.sent++; return nil }
func (c *scriptConn) Poll(fn func([]byte)) wire.ConnState {
	for _, f := range c.queue {
		fn(f)
	}
	c.queue = c.queue[:0]
	return wire.ConnOpen
}
func (c *scriptConn) Close(int, string)          {}
func (c *scriptConn) CloseStatus() (int, string) { return 0, "" }

func snapshotFrame(keyframe bool, epoch, tick uint64, base []byte, sums [2]uint64) []byte {
	state := binary.LittleEndian.AppendUint64(nil, tick)
	state = binary.LittleEndian.AppendUint64(state, sums[0])
	state = binary.LittleEndian.AppendUint64(state, sums[1])
	seats := []wire.SeatState{{Flags: wire.FlagHuman | wire.FlagConnected, Lead: 2, Input: []byte{1}}, {Flags: wire.FlagBot, Input: []byte{2}}}
	frame := wire.AppendSnapshotHeader(nil, keyframe, epoch, tick, seats)
	if keyframe {
		return append(frame, state...)
	}
	var e wire.DeltaEncoder
	e.SetBase(base)
	return e.Append(frame, state)
}

// TestFrameAllocations: once warm, a frame that runs ticks and sends input
// allocates nothing, and neither does a frame that applies a delta snapshot,
// restores it and replays the predicted ticks.
func TestFrameAllocations(t *testing.T) {
	now := time.Duration(0)
	m := mode.Mode{Name: "count", Seats: 2, TickRate: 60, InputSize: 1,
		NewSim: func(uint64) sim.Simulation { return &countSim{} }}
	conn := &scriptConn{}
	c, err := New(Config{URL: "ws://x/ws", Mode: m, Now: func() time.Duration { return now }, CatchUpBudget: -1,
		Dial:  func(string) (Conn, error) { return conn, nil },
		Input: func(_ uint64, dst []byte) []byte { return append(dst, 1) }})
	if err != nil {
		t.Fatal(err)
	}
	w := wire.Welcome{Version: wire.Version, Seat: 0, Seats: 2, TickRate: 60, InputDelay: 2, SnapshotEvery: 1, Mode: "count", Code: "ABCDE", Token: "t"}
	room := wire.Room{Phase: wire.PhaseRunning, Seats: []wire.SeatInfo{{Kind: wire.SeatHuman, Connected: true}, {Kind: wire.SeatBot}}}
	key := snapshotFrame(true, 1, 0, nil, [2]uint64{})
	conn.queue = append(conn.queue, w.Append(nil), room.Append(nil), key)
	frame := 16 * time.Millisecond
	for range 30 {
		now += frame
		c.Frame(frame)
	}
	if c.Status() != StatusOnline || c.Tick() == 0 {
		t.Fatalf("status %v tick %d", c.Status(), c.Tick())
	}
	if n := testing.AllocsPerRun(100, func() { now += frame; c.Frame(frame) }); n != 0 {
		t.Fatalf("steady frame: %v allocations, want 0", n)
	}

	// Deltas against the tick-0 keyframe, one per frame, each one tick newer
	// and a few ticks behind the client, so every one restores and replays.
	var snap wire.Snapshot
	_ = snap.Decode(key)
	base := append([]byte(nil), snap.Body...)
	deltas := make([][]byte, 0, 256)
	start := c.Tick() - 4
	for i := uint64(0); i < 256; i++ {
		deltas = append(deltas, snapshotFrame(false, 1, start+i, base, [2]uint64{i, 2 * i}))
	}
	next := 0
	for range 20 { // warm the delta path
		conn.queue = append(conn.queue, deltas[next])
		next++
		now += frame
		c.Frame(frame)
	}
	before, replayed := c.Stats().Reconciles, c.Stats().Replayed
	n := testing.AllocsPerRun(100, func() {
		conn.queue = append(conn.queue, deltas[next])
		next++
		now += frame
		c.Frame(frame)
	})
	if got := c.Stats().Reconciles - before; got < 100 {
		t.Fatalf("only %d reconciles during the measurement", got)
	}
	if got := c.Stats().Replayed - replayed; got < 100 {
		t.Fatalf("only %d replayed ticks during the measurement; the replay path was not exercised", got)
	}
	if n != 0 {
		t.Fatalf("frame with a delta snapshot: %v allocations, want 0", n)
	}
}

func TestInputFrameRedundancy(t *testing.T) {
	now := time.Duration(0)
	m := mode.Mode{Name: "count", Seats: 2, TickRate: 60, InputSize: 1, NewSim: func(uint64) sim.Simulation { return &countSim{} }}
	var sent [][]byte
	conn := &recordConn{sent: &sent}
	c, _ := New(Config{URL: "ws://x/ws", Mode: m, Now: func() time.Duration { return now }, CatchUpBudget: -1, Redundancy: 4,
		Dial: func(string) (Conn, error) { return conn, nil }, Input: func(t uint64, dst []byte) []byte { return append(dst, byte(t)) }})
	w := wire.Welcome{Version: wire.Version, Seat: 0, Seats: 2, TickRate: 60, InputDelay: 2, SnapshotEvery: 1, Mode: "count", Code: "ABCDE"}
	room := wire.Room{Phase: wire.PhaseRunning, Seats: make([]wire.SeatInfo, 2)}
	conn.queue = [][]byte{w.Append(nil), room.Append(nil), snapshotFrame(true, 1, 0, nil, [2]uint64{})}
	for range 20 {
		now += 16 * time.Millisecond
		c.Frame(16 * time.Millisecond)
	}
	var last wire.Input
	found := false
	for _, f := range sent {
		if k, _ := wire.KindOf(f); k == wire.KindInput {
			if err := last.Decode(f); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no input frame sent")
	}
	// The server acknowledged through tick 0+2 (lead 2 in the keyframe), so
	// the frame carries at most Redundancy ticks and ends at the newest one.
	if last.Count < 1 || last.Count > 4 || last.Last() != c.Tick() {
		t.Fatalf("input frame first %d count %d, client tick %d", last.First, last.Count, c.Tick())
	}
	for i := 0; i < last.Count; i++ {
		if got := last.Inputs[i]; len(got) != 1 || got[0] != byte(last.First+uint64(i)) {
			t.Fatalf("input for tick %d is %v", last.First+uint64(i), got)
		}
	}
}

type recordConn struct {
	queue [][]byte
	sent  *[][]byte
}

func (c *recordConn) Send(f []byte) error {
	*c.sent = append(*c.sent, append([]byte(nil), f...))
	return nil
}
func (c *recordConn) Poll(fn func([]byte)) wire.ConnState {
	for _, f := range c.queue {
		fn(f)
	}
	c.queue = nil
	return wire.ConnOpen
}
func (c *recordConn) Close(int, string)          {}
func (c *recordConn) CloseStatus() (int, string) { return 0, "" }
