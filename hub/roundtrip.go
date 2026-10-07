package hub

import (
	"encoding/binary"
	"sync"
	"time"

	"m31labs.dev/gosx/internal/clock"
)

// transportState belongs to the existing connection pumps. It is allocated
// only when an observer or slow-client policy needs transport bookkeeping.
type transportState struct {
	clock clock.Clock
	slow  SlowClientPolicy
	// Queue counters share Client.mu with enqueue/channel closure.
	enqueues [2]uint32
	// Pong handling is the reader; measured pings belong to the writer.
	mu       sync.Mutex
	sequence uint64
	started  time.Duration
	pending  bool
	// These fields belong exclusively to writePump.
	nextPing, lastCheck time.Duration
	lastDrops           DropStats
}

func (h *Hub) transport(policy SlowClientPolicy) *transportState {
	if policy.DropThreshold == 0 && h.observers.Load() == nil {
		return nil
	}
	h.mu.Lock()
	if h.transportClock == nil {
		h.transportClock = clock.New()
	}
	c := h.transportClock
	h.mu.Unlock()
	return &transportState{clock: c, slow: policy}
}

// startPing replaces at most one measured ping. Replacement reports the
// unanswered previous ping once; a failed write abandons the new measurement.
func (c *Client) startPing(now time.Duration) [8]byte {
	s := c.transport
	s.mu.Lock()
	missed := s.pending
	s.sequence++
	if s.sequence == 0 {
		s.sequence++
	}
	seq := s.sequence
	s.started, s.pending = now, true
	s.mu.Unlock()
	if missed {
		c.Hub.observe(func(o Observer) { o.RoundTripTimeout(c.Hub, c) })
	}
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], seq)
	return payload
}

func (c *Client) abandonPing(timeout bool) {
	if c.transport == nil {
		return
	}
	s := c.transport
	s.mu.Lock()
	pending := s.pending
	s.pending = false
	s.mu.Unlock()
	if pending && timeout {
		c.Hub.observe(func(o Observer) { o.RoundTripTimeout(c.Hub, c) })
	}
}

func (c *Client) observePong(payload string) {
	if c.transport == nil || len(payload) != 8 {
		return
	}
	seq := uint64(0)
	for i := range 8 {
		seq = seq<<8 | uint64(payload[i])
	}
	s := c.transport
	s.mu.Lock()
	if !s.pending || seq != s.sequence {
		s.mu.Unlock()
		return
	}
	d := s.clock.Now().Monotonic - s.started
	s.pending = false
	s.mu.Unlock()
	if d < 0 || d > readWait {
		c.Hub.observe(func(o Observer) { o.RoundTripTimeout(c.Hub, c) })
		return
	}
	c.Hub.observe(func(o Observer) { o.RoundTrip(c.Hub, c, d) })
}

func after(now, delta time.Duration) time.Duration {
	const max = time.Duration(1<<63 - 1)
	if now > max-delta {
		return max
	}
	return now + delta
}
