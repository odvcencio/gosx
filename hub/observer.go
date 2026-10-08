package hub

import (
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAfterServe means an upgrade reservation has closed observer configuration.
var ErrAfterServe = errors.New("hub: observer configuration closed")

// ErrClosed means the hub has stopped accepting connections.
var ErrClosed = errors.New("hub: closed")

// ErrObserverConflict means a telemetry subscriber already owns this hub slot.
var ErrObserverConflict = errors.New("hub: telemetry observer already registered")

// Direction identifies logical WebSocket application payload traffic.
type Direction uint8

const (
	Inbound Direction = iota
	Outbound
)

// TrafficEvent excludes WebSocket framing, compression and control messages.
// Written is true only after a successful outbound write. QueueDepth is -1
// unless a queue sample accompanies an enqueue. Count zero means one event.
// Broadcast queue/drop callbacks coalesce identical samples with Count and a
// nil Client; this preserves their multiplicity outside the fanout lock.
type TrafficEvent struct {
	Direction  Direction
	Binary     bool
	Bytes      int
	QueueDepth int
	Written    bool
	Dropped    bool // an outbound enqueue failed because its queue was full
	Count      uint64
}

// HandlerEvent describes one complete application handler invocation.
type HandlerEvent struct {
	Event    string
	Duration time.Duration
	Panicked bool
}

// DisconnectEvent keeps the framework reason separate from application text.
// Consumers must classify AppReason against their declared values before export.
type DisconnectEvent struct{ Reason, AppReason string }

// RejectionReason classifies an unsuccessful connection admission. These
// values never describe traffic on an already accepted connection.
type RejectionReason string

const (
	RejectedCapacity RejectionReason = "rejected_capacity" // No connection slot is available.
	RejectedOrigin   RejectionReason = "rejected_origin"   // The origin check refused the upgrade.
	RejectedUpgrade  RejectionReason = "rejected_upgrade"  // The WebSocket upgrade failed.
	RejectedClosed   RejectionReason = "hub_closed"        // Shutdown has stopped admission.
	RejectedOther    RejectionReason = "other"             // A configured policy refused admission.
)

// MessageRejectionReason classifies a payload on an accepted connection.
type MessageRejectionReason string

const (
	MessageRateLimited MessageRejectionReason = "rate_limited" // Inbound message allowance was exhausted.
	MessageMalformed   MessageRejectionReason = "malformed"    // A text payload was not a valid Message.
)

// Observer receives synchronous, concurrent callbacks outside hub/client locks.
// Callbacks must be bounded and must not perform I/O or wait for pump shutdown.
// A panicking observer is detached; its panic text is never logged. Embed
// NoopObserver so future callbacks can be added without changing your type.
// Connection, rejection, message, broadcast, handler, observer-panic and Closed
// callbacks, control RoundTrip and RoundTripTimeout fire now. ClientAssociated
// remains an extension point for the permitted browser association integration.
// Closed is last: closing stops admission for every subscriber and drains all
// admitted callbacks before invoking it. Callbacks must not wait for Hub.Close.
type Observer interface {
	ClientConnected(*Hub, *Client, *http.Request)
	ClientDisconnected(*Hub, *Client, DisconnectEvent)
	Rejected(*Hub, RejectionReason)
	MessageRejected(*Hub, *Client, MessageRejectionReason)
	ObserverPanicked(*Hub)
	Message(*Hub, *Client, TrafficEvent)
	Broadcast(*Hub, int, int)
	Handler(*Hub, HandlerEvent)
	RoundTrip(*Hub, *Client, time.Duration)
	RoundTripTimeout(*Hub, *Client)
	ClientAssociated(*Hub, *Client, string)
	Closed(*Hub)
}

// NoopObserver lets subscribers implement only the callbacks they need.
type NoopObserver struct{}

func (NoopObserver) ClientConnected(*Hub, *Client, *http.Request)          {}
func (NoopObserver) ClientDisconnected(*Hub, *Client, DisconnectEvent)     {}
func (NoopObserver) Rejected(*Hub, RejectionReason)                        {}
func (NoopObserver) MessageRejected(*Hub, *Client, MessageRejectionReason) {}
func (NoopObserver) ObserverPanicked(*Hub)                                 {}
func (NoopObserver) Message(*Hub, *Client, TrafficEvent)                   {}
func (NoopObserver) Broadcast(*Hub, int, int)                              {}
func (NoopObserver) Handler(*Hub, HandlerEvent)                            {}
func (NoopObserver) RoundTrip(*Hub, *Client, time.Duration)                {}
func (NoopObserver) RoundTripTimeout(*Hub, *Client)                        {}
func (NoopObserver) ClientAssociated(*Hub, *Client, string)                {}
func (NoopObserver) Closed(*Hub)                                           {}

type observerSlot struct {
	observer  Observer
	state     atomic.Uint64
	drained   chan struct{}
	drainOnce sync.Once
}

const observerRetired = uint64(1) << 63

func (slot *observerSlot) retire() bool {
	for {
		state := slot.state.Load()
		if state&observerRetired != 0 {
			return false
		}
		if slot.state.CompareAndSwap(state, state|observerRetired) {
			if state == 0 {
				slot.drainOnce.Do(func() { close(slot.drained) })
			}
			return true
		}
	}
}

type observerList struct{ slots []*observerSlot }

// UseObserver adds a subscriber before the first upgrade reservation. Nil is
// ignored. Detach is idempotent and never closes the hub. A callback already in
// progress may finish after detach; no later dispatch admits that subscriber.
func (h *Hub) UseObserver(o Observer) (detach func(), err error) {
	return h.useObserver(o, false, 0)
}

// UseTelemetryObserver reserves one telemetry subscriber independently of
// application observers. It supports the same startup/removal boundary as
// UseObserver. Queue sampling defaults to every 64 attempts per text/binary
// queue; 1 explicitly samples every attempt. Drops are always reported.
func (h *Hub) UseTelemetryObserver(o Observer, queueSampleEvery uint32) (detach func(), err error) {
	if queueSampleEvery == 0 {
		queueSampleEvery = 64
	}
	return h.useObserver(o, true, queueSampleEvery)
}

func (h *Hub) useObserver(o Observer, telemetry bool, queueSampleEvery uint32) (detach func(), err error) {
	if o == nil {
		return func() {}, nil
	}
	if h == nil {
		return nil, ErrClosed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return nil, ErrClosed
	}
	if telemetry && h.telemetryObserver != nil {
		return nil, ErrObserverConflict
	}
	if h.served {
		return nil, ErrAfterServe
	}
	slot := &observerSlot{observer: o, drained: make(chan struct{})}
	list := &observerList{}
	if old := h.observers.Load(); old != nil {
		list.slots = append(list.slots, old.slots...)
	}
	list.slots = append(list.slots, slot)
	if telemetry {
		h.telemetryObserver = slot
		h.queueSampleEvery.Store(queueSampleEvery)
	}
	h.observers.Store(list)
	return func() { h.detachObserver(slot) }, nil
}

func (h *Hub) detachObserver(slot *observerSlot) {
	if !slot.retire() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.telemetryObserver == slot {
		h.telemetryObserver = nil
		h.queueSampleEvery.Store(0)
	}
	old := h.observers.Load()
	if old == nil {
		return
	}
	list := &observerList{slots: make([]*observerSlot, 0, len(old.slots)-1)}
	for _, candidate := range old.slots {
		if candidate != slot {
			list.slots = append(list.slots, candidate)
		}
	}
	if len(list.slots) == 0 {
		h.observers.Store(nil)
	} else {
		h.observers.Store(list)
	}
}

func (h *Hub) observe(visit func(Observer)) {
	// Pumps and reserved registrations keep finishClose from starting until
	// their callbacks return. These dispatches only need the detach check;
	// writing a shared admission counter would contend on every message.
	list := h.observers.Load()
	if list == nil {
		return
	}
	for _, slot := range list.slots {
		if slot.state.Load()&observerRetired == 0 {
			h.invokeObserver(slot, visit)
		}
	}
}

// observeConcurrent protects dispatches whose owner can race finishClose:
// broadcasts, unreserved/released rejections, and nested panic notifications.
func (h *Hub) observeConcurrent(visit func(Observer)) {
	list := h.observers.Load()
	if list == nil {
		return
	}
	for _, slot := range list.slots {
		h.visitObserver(slot, visit)
	}
}

func (h *Hub) visitObserver(slot *observerSlot, visit func(Observer)) {
	for {
		state := slot.state.Load()
		if state&observerRetired != 0 {
			return
		}
		// Admission and retirement share one atomic state. Retirement cannot
		// observe a drained subscriber while a new callback is admitted.
		if slot.state.CompareAndSwap(state, state+1) {
			break
		}
	}
	defer slot.leave()
	h.invokeObserver(slot, visit)
}

func (slot *observerSlot) leave() {
	if slot.state.Add(^uint64(0)) == observerRetired {
		slot.drainOnce.Do(func() { close(slot.drained) })
	}
}

func (h *Hub) invokeObserver(slot *observerSlot, visit func(Observer)) {
	defer func() {
		if recover() != nil {
			h.detachObserver(slot)
			h.observeConcurrent(func(o Observer) { o.ObserverPanicked(h) })
			h.observerWarningMu.Lock()
			warn := h.observerWarningAt.IsZero() || time.Since(h.observerWarningAt) >= time.Minute
			if warn {
				h.observerWarningAt = time.Now()
			}
			h.observerWarningMu.Unlock()
			if warn {
				log.Print("[gosx hub] observer panic; subscriber detached")
			}
		}
	}()
	visit(slot.observer)
}

func (h *Hub) observeMessage(c *Client, event TrafficEvent) {
	h.observe(func(o Observer) { o.Message(h, c, event) })
}

// queueEventLocked is part of the existing enqueue/channel-close critical
// section. Dispatch occurs after that lock is released. Sampling each queue
// has no observer work on the nil path and never rescans broadcast recipients.
func (c *Client) queueEventLocked(binary bool, bytes int, dropped bool) (TrafficEvent, bool) {
	if c.Hub == nil || c.Hub.observers.Load() == nil {
		return TrafficEvent{}, false
	}
	event := TrafficEvent{Direction: Outbound, Binary: binary, Bytes: bytes, QueueDepth: -1, Dropped: dropped}
	if c.transport != nil {
		every := c.Hub.queueSampleEvery.Load()
		if every == 0 {
			every = 64
		}
		index := 0
		queue := c.send
		if binary {
			index, queue = 1, c.binarySend
		}
		c.transport.enqueues[index]++
		if c.transport.enqueues[index] >= every {
			c.transport.enqueues[index] = 0
			event.QueueDepth = len(queue)
		}
	}
	return event, dropped || event.QueueDepth >= 0
}

// The queues have 256 slots. This fixed stack scratch coalesces samples by
// depth, rather than retaining recipients or scanning them again after fanout.
type enqueueBatch struct {
	depths  [257]uint64
	drops   uint64
	sampled bool
}

func (h *Hub) fanout(payload []byte, binary bool, predicate func(*Client) bool) (sent, dropped int) {
	h.mu.RLock()
	if h.observers.Load() != nil {
		return h.observedFanoutLocked(payload, binary, predicate)
	}
	// Keep the ordinary path out of the sampled fanout's scratch frame.
	for _, c := range h.clients {
		if predicate != nil && !predicate(c) {
			continue
		}
		accepted, full := c.enqueue(payload, binary, nil)
		if accepted {
			sent++
		}
		if full {
			dropped++
		}
	}
	h.mu.RUnlock()
	return
}

func (h *Hub) observedFanoutLocked(payload []byte, binary bool, predicate func(*Client) bool) (sent, dropped int) {
	var batch enqueueBatch
	for _, c := range h.clients {
		if predicate != nil && !predicate(c) {
			continue
		}
		accepted, full := c.enqueue(payload, binary, &batch)
		if accepted {
			sent++
		}
		if full {
			dropped++
		}
	}
	h.mu.RUnlock()
	h.publishBatch(&batch, binary, len(payload))
	return
}

func (c *Client) publishEnqueue(event TrafficEvent, batch *enqueueBatch) {
	if batch == nil {
		c.Hub.observeMessage(c, event)
		return
	}
	if event.Dropped {
		batch.drops++
	}
	if event.QueueDepth >= 0 {
		batch.depths[event.QueueDepth]++
		batch.sampled = true
	}
}

func (h *Hub) publishBatch(batch *enqueueBatch, binary bool, bytes int) {
	if batch.drops != 0 {
		h.observeMessage(nil, TrafficEvent{Direction: Outbound, Binary: binary, Bytes: bytes, QueueDepth: -1, Dropped: true, Count: batch.drops})
	}
	if batch.sampled {
		for depth, count := range batch.depths {
			if count != 0 {
				h.observeMessage(nil, TrafficEvent{Direction: Outbound, Binary: binary, Bytes: bytes, QueueDepth: depth, Count: count})
			}
		}
	}
}
