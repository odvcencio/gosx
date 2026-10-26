package sim

import (
	"encoding/binary"
	"errors"
	"sync"

	"m31labs.dev/gosx/hub"
)

// Broadcaster delivers one tick's authoritative state to hub clients.
// The Runner calls it from the tick goroutine, once per broadcast, with state
// from Simulation.State. Implementations must not retain or mutate state after
// returning unless they copy it.
type Broadcaster interface {
	Broadcast(h *hub.Hub, frame uint64, state []byte)
}

// BroadcasterFunc adapts a function into a Broadcaster.
type BroadcasterFunc func(h *hub.Hub, frame uint64, state []byte)

// Broadcast calls f.
func (f BroadcasterFunc) Broadcast(h *hub.Hub, frame uint64, state []byte) { f(h, frame, state) }

// Binary snapshot frame layout (all integers big endian). Every frame starts
// with the 0x00 application prefix that hub.BroadcastBinarySnapshot requires.
//
//	keyframe: 00 'K' epoch(8) frame(8) state...
//	delta:    00 'D' epoch(8) frame(8) prefixLen(4) suffixLen(4) middle...
//
// A delta rebuilds the state as keyframeState[:prefixLen] + middle +
// keyframeState[len-suffixLen:]. It depends only on its keyframe, never on an
// earlier delta, as the hub snapshot lane requires.
const (
	binaryKeyframe  = 'K'
	binaryDelta     = 'D'
	binaryHeaderLen = 2 + 8 + 8
)

// BinarySnapshotOptions configures NewBinarySnapshotBroadcaster.
type BinarySnapshotOptions struct {
	// KeyframeEvery sends a full keyframe every N broadcasts and deltas in
	// between. Zero or one sends only keyframes.
	KeyframeEvery int
}

type binarySnapshotBroadcaster struct {
	mu       sync.Mutex
	every    int
	epoch    uint64
	baseline []byte // full keyframe frame
	state    []byte // state inside baseline
	since    int
}

// NewBinarySnapshotBroadcaster returns a Broadcaster that uses
// hub.BroadcastBinarySnapshot with a keyframe plus delta encoding. Clients
// need a custom decoder; DecodeBinarySnapshot is the Go reference. State bytes
// are copied before they are offered.
func NewBinarySnapshotBroadcaster(opts BinarySnapshotOptions) Broadcaster {
	every := opts.KeyframeEvery
	if every < 1 {
		every = 1
	}
	return &binarySnapshotBroadcaster{every: every}
}

func (b *binarySnapshotBroadcaster) Broadcast(h *hub.Hub, frame uint64, state []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var delta []byte
	if b.baseline != nil && b.since < b.every {
		delta = encodeBinaryDelta(b.epoch, frame, b.state, state)
	}
	if delta == nil {
		b.epoch++
		b.state = append([]byte(nil), state...)
		b.baseline = encodeBinaryKeyframe(b.epoch, frame, b.state)
		b.since = 1
	} else {
		b.since++
	}
	h.BroadcastBinarySnapshot(b.epoch, b.baseline, delta)
}

func binaryHeader(kind byte, epoch, frame uint64, extra int) []byte {
	buf := make([]byte, binaryHeaderLen, binaryHeaderLen+extra)
	buf[1] = kind
	binary.BigEndian.PutUint64(buf[2:], epoch)
	binary.BigEndian.PutUint64(buf[10:], frame)
	return buf
}

func encodeBinaryKeyframe(epoch, frame uint64, state []byte) []byte {
	return append(binaryHeader(binaryKeyframe, epoch, frame, len(state)), state...)
}

// encodeBinaryDelta returns nil when a delta would not be smaller than the
// full state, so the caller starts a new keyframe instead.
func encodeBinaryDelta(epoch, frame uint64, base, state []byte) []byte {
	p := 0
	for p < len(base) && p < len(state) && base[p] == state[p] {
		p++
	}
	s := 0
	for s < len(base)-p && s < len(state)-p && base[len(base)-1-s] == state[len(state)-1-s] {
		s++
	}
	middle := state[p : len(state)-s]
	if 8+len(middle) >= len(state) {
		return nil
	}
	buf := binaryHeader(binaryDelta, epoch, frame, 8+len(middle))
	buf = binary.BigEndian.AppendUint32(buf, uint32(p))
	buf = binary.BigEndian.AppendUint32(buf, uint32(s))
	return append(buf, middle...)
}

// ErrBinarySnapshot reports a malformed or mismatched binary snapshot frame.
var ErrBinarySnapshot = errors.New("sim: invalid binary snapshot frame")

// DecodeBinarySnapshot decodes a frame produced by NewBinarySnapshotBroadcaster.
// For a keyframe, pass baseline as nil. For a delta, pass the keyframe frame it
// was encoded against. It returns the epoch, the simulation frame and the state.
func DecodeBinarySnapshot(frame, baseline []byte) (epoch, simFrame uint64, state []byte, err error) {
	if len(frame) < binaryHeaderLen || frame[0] != 0 {
		return 0, 0, nil, ErrBinarySnapshot
	}
	epoch = binary.BigEndian.Uint64(frame[2:])
	simFrame = binary.BigEndian.Uint64(frame[10:])
	switch frame[1] {
	case binaryKeyframe:
		return epoch, simFrame, append([]byte(nil), frame[binaryHeaderLen:]...), nil
	case binaryDelta:
		if len(frame) < binaryHeaderLen+8 || len(baseline) < binaryHeaderLen || baseline[1] != binaryKeyframe ||
			binary.BigEndian.Uint64(baseline[2:]) != epoch {
			return 0, 0, nil, ErrBinarySnapshot
		}
		base := baseline[binaryHeaderLen:]
		p := int(binary.BigEndian.Uint32(frame[binaryHeaderLen:]))
		s := int(binary.BigEndian.Uint32(frame[binaryHeaderLen+4:]))
		if p+s > len(base) {
			return 0, 0, nil, ErrBinarySnapshot
		}
		out := append([]byte(nil), base[:p]...)
		out = append(out, frame[binaryHeaderLen+8:]...)
		out = append(out, base[len(base)-s:]...)
		return epoch, simFrame, out, nil
	}
	return 0, 0, nil, ErrBinarySnapshot
}
