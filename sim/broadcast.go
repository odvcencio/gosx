//go:build !js || !wasm

package sim

import (
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
