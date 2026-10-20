package hub

import (
	"time"

	"github.com/gorilla/websocket"
)

// SnapshotStats counts pending application snapshot frames superseded by a
// newer frame. Replacements are intentional and never contribute to DropStats
// or SlowClient eviction. Counts include departed clients in the hub total.
type SnapshotStats struct{ Replaced uint64 }

// SnapshotStats returns this hub's lifetime snapshot replacement count.
func (h *Hub) SnapshotStats() SnapshotStats {
	if h == nil {
		return SnapshotStats{}
	}
	return SnapshotStats{Replaced: h.snapshotReplaced.Load()}
}

// SnapshotStats returns this client's snapshot replacement count.
func (c *Client) SnapshotStats() SnapshotStats {
	if c == nil {
		return SnapshotStats{}
	}
	return SnapshotStats{Replaced: c.snapshotReplaced.Load()}
}

// binarySnapshot is protected by Client.mu. Only the writer takes frames;
// taking a baseline commits its ordering before any delta for that epoch.
type binarySnapshot struct {
	epoch    uint64
	baseline []byte
	delta    []byte
}

// BroadcastBinarySnapshot offers the latest state to every connected client.
// Each client retains at most a pending baseline and the newest delta, separate
// from the existing text and binary/CRDT queues. One additional frame may be
// in a socket write; that frame cannot be replaced. The return value counts
// clients accepting the offer, including an unchanged offer for their epoch.
//
// Epoch must be nonzero and increase for each new baseline. Baseline must be
// a complete application frame; delta may be nil, or an independently decodable
// update against that baseline (never against a previous delta). Both frames
// must start with 0x00, the application prefix. Invalid offers are ignored.
// Stale epochs are ignored per client. The first baseline for an epoch is used;
// duplicate offers for that epoch only replace its pending delta. A nil delta
// leaves an existing pending delta intact. Supply the baseline on every offer
// so newly connected clients receive it before their first delta.
//
// Payload ownership matches BroadcastBinary: GoSX retains the provided slices
// without copying. Do not mutate them after offering them. Concurrent offers
// for the same epoch take effect in enqueue order per client; applications
// should serialize producers if a global ordering is required. Binary frames
// are not latched or decoded by the stock GoSX browser runtime.
func (h *Hub) BroadcastBinarySnapshot(epoch uint64, baseline, delta []byte) int {
	if h == nil || epoch == 0 || len(baseline) == 0 || baseline[0] != 0 || (len(delta) != 0 && delta[0] != 0) {
		return 0
	}
	h.mu.RLock()
	if h.closing {
		h.mu.RUnlock()
		return 0
	}
	// Coalesce matching observer samples outside all connection/hub locks.
	var events map[TrafficEvent]uint64
	if h.observers.Load() != nil {
		events = make(map[TrafficEvent]uint64)
	}
	sent := 0
	for _, c := range h.clients {
		accepted, samples := c.offerSnapshot(epoch, baseline, delta, events != nil)
		if accepted {
			sent++
		}
		for _, event := range samples {
			if event.Snapshot {
				events[event]++
			}
		}
	}
	h.mu.RUnlock()
	for event, count := range events {
		event.Count = count
		h.observeEnqueue(nil, event)
	}
	h.observeConcurrent(func(o Observer) { o.Broadcast(h, sent, 0) })
	return sent
}

func (c *Client) offerSnapshot(epoch uint64, baseline, delta []byte, observed bool) (bool, [3]TrafficEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var events [3]TrafficEvent
	if c.closed || (c.snapshot != nil && epoch < c.snapshot.epoch) {
		return false, events
	}
	var replaced [2][]byte
	if c.snapshot == nil || epoch > c.snapshot.epoch {
		if c.snapshot != nil {
			replaced = [2][]byte{c.snapshot.baseline, c.snapshot.delta}
		}
		c.snapshot = &binarySnapshot{epoch: epoch, baseline: baseline, delta: delta}
	} else if len(delta) != 0 {
		replaced[1] = c.snapshot.delta
		c.snapshot.delta = delta
	}
	for i, payload := range replaced {
		if len(payload) != 0 {
			c.snapshotReplaced.Add(1)
			if c.Hub != nil {
				c.Hub.snapshotReplaced.Add(1)
			}
			if observed {
				events[i] = TrafficEvent{Direction: Outbound, Binary: true, Snapshot: true, Replaced: true, Bytes: len(payload), QueueDepth: -1}
			}
		}
	}
	if c.snapshotWake == nil {
		c.snapshotWake = make(chan struct{}, 1)
	}
	c.wakeSnapshotLocked()
	if observed && c.transport != nil {
		every := c.Hub.queueSampleEvery.Load()
		if every == 0 {
			every = 64
		}
		c.transport.enqueues[2]++
		if c.transport.enqueues[2] >= every {
			c.transport.enqueues[2] = 0
			depth, bytes := 0, len(baseline)
			if len(c.snapshot.baseline) != 0 {
				depth++
			}
			if len(c.snapshot.delta) != 0 {
				depth++
			}
			if len(delta) != 0 {
				bytes = len(delta)
			}
			events[2] = TrafficEvent{Direction: Outbound, Binary: true, Snapshot: true, Bytes: bytes, QueueDepth: depth}
		}
	}
	return true, events
}

func (c *Client) wakeSnapshotLocked() {
	if c.snapshot != nil && (len(c.snapshot.baseline) != 0 || len(c.snapshot.delta) != 0) {
		select {
		case c.snapshotWake <- struct{}{}:
		default:
		}
	}
}

func (c *Client) takeSnapshot() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.snapshot == nil {
		return nil, false
	}
	var payload []byte
	if len(c.snapshot.baseline) != 0 {
		payload, c.snapshot.baseline = c.snapshot.baseline, nil
	} else {
		payload, c.snapshot.delta = c.snapshot.delta, nil
	}
	c.wakeSnapshotLocked()
	return payload, len(payload) != 0
}

type outboundFrame struct {
	kind     int // zero is a timer tick
	payload  []byte
	closed   bool
	snapshot bool
}

// Alternate snapshot frames with ready reliable work. Both categories get a
// bounded turn; the existing text/binary selection remains fair within the
// reliable category. Timers participate in each reliable turn as before.
func (c *Client) nextOutbound(ticks <-chan time.Time, preferSnapshot bool) outboundFrame {
	for {
		if preferSnapshot {
			select {
			case <-c.snapshotWake:
				if payload, ok := c.takeSnapshot(); ok {
					return outboundFrame{kind: websocket.BinaryMessage, payload: payload, snapshot: true}
				}
			default:
			}
		} else {
			select {
			case payload, ok := <-c.send:
				return outboundFrame{kind: websocket.TextMessage, payload: payload, closed: !ok}
			case payload, ok := <-c.binarySend:
				return outboundFrame{kind: websocket.BinaryMessage, payload: payload, closed: !ok}
			case <-ticks:
				return outboundFrame{}
			default:
			}
		}
		select {
		case payload, ok := <-c.send:
			return outboundFrame{kind: websocket.TextMessage, payload: payload, closed: !ok}
		case payload, ok := <-c.binarySend:
			return outboundFrame{kind: websocket.BinaryMessage, payload: payload, closed: !ok}
		case <-c.snapshotWake:
			if payload, ok := c.takeSnapshot(); ok {
				return outboundFrame{kind: websocket.BinaryMessage, payload: payload, snapshot: true}
			}
		case <-ticks:
			return outboundFrame{}
		}
	}
}
