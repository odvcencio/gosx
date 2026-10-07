package hub

import (
	"errors"
	"log"
	"net/http"
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
// Closed stops new dispatches. A dispatch admitted before closing may overlap
// Closed, so subscribers must synchronize their state and tolerate that overlap.
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
	observer Observer
	active   atomic.Bool
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
	slot := &observerSlot{observer: o}
	slot.active.Store(true)
	list := &observerList{}
	if old := h.observers.Load(); old != nil {
		list.slots = append(list.slots, old.slots...)
	}
	list.slots = append(list.slots, slot)
	h.observers.Store(list)
	return func() { h.detachObserver(slot) }, nil
}

func (h *Hub) detachObserver(slot *observerSlot) {
	if !slot.active.Swap(false) {
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
	list := h.observers.Load()
	if list == nil {
		return
	}
	for _, slot := range list.slots {
		h.visitObserver(slot, visit)
	}
}

func (h *Hub) visitObserver(slot *observerSlot, visit func(Observer)) {
	if !slot.active.Load() {
		return
	}
	defer func() {
		if recover() != nil {
			h.detachObserver(slot)
			h.observe(func(o Observer) { o.ObserverPanicked(h) })
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
