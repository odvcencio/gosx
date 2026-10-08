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

// Direction identifies logical WebSocket application payload traffic.
type Direction uint8

const (
	Inbound Direction = iota
	Outbound
)

// TrafficEvent excludes WebSocket framing, compression and control messages.
// Written is true only after a successful outbound write. QueueDepth is -1
// unless a queue sample accompanies an enqueue.
type TrafficEvent struct {
	Direction  Direction
	Binary     bool
	Bytes      int
	QueueDepth int
	Written    bool
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
// callbacks fire now. RoundTrip, RoundTripTimeout and ClientAssociated are
// extension points; their producers are added in later integrations.
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
	if h.served {
		return nil, ErrAfterServe
	}
	slot := &observerSlot{observer: o, drained: make(chan struct{})}
	list := &observerList{}
	if old := h.observers.Load(); old != nil {
		list.slots = append(list.slots, old.slots...)
	}
	list.slots = append(list.slots, slot)
	h.observers.Store(list)
	return func() { h.detachObserver(slot) }, nil
}

func (h *Hub) detachObserver(slot *observerSlot) {
	if !slot.retire() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
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
