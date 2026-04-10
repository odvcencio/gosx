package field

import (
	"encoding/json"
	"sync"

	"github.com/odvcencio/gosx/hub"
)

const fieldEventPrefix = "field:"

// streamState holds, for each (hub, topic) pair, the most recent published
// field (used as the delta base for the next publish) and the set of local
// subscribers. The hub-keyed map ensures two hubs with the same topic name
// don't share state.
type streamState struct {
	mu     sync.Mutex
	topics map[*hub.Hub]map[string]*topicState
}

type topicState struct {
	last        *Field          // most recent published field (delta base)
	subscribers []chan<- *Field // local in-process subscribers
}

var streams = &streamState{topics: make(map[*hub.Hub]map[string]*topicState)}

func (s *streamState) get(h *hub.Hub, topic string) *topicState {
	if s.topics[h] == nil {
		s.topics[h] = make(map[string]*topicState)
	}
	if s.topics[h][topic] == nil {
		s.topics[h][topic] = &topicState{}
	}
	return s.topics[h][topic]
}

// PublishField broadcasts a quantized field to all subscribers of the topic.
//
// Two delivery paths run in parallel:
//  1. Local in-process subscribers (registered via SubscribeField) receive
//     the decoded *Field directly through their channels.
//  2. Connected WebSocket clients receive the JSON-encoded Quantized payload
//     via hub.Broadcast.
//
// If opts.DeltaAgainst is nil and a previous field exists for this topic,
// PublishField automatically uses the previous field as the delta base.
func PublishField(h *hub.Hub, topic string, f *Field, opts QuantizeOptions) {
	streams.mu.Lock()
	state := streams.get(h, topic)
	if opts.DeltaAgainst == nil {
		opts.DeltaAgainst = state.last
	}
	state.last = f
	subs := make([]chan<- *Field, len(state.subscribers))
	copy(subs, state.subscribers)
	streams.mu.Unlock()

	q := f.Quantize(opts)

	// 1. Local dispatch — send the decoded field directly. Each subscriber
	// receives a fresh copy that has the delta already applied (via Quantize's
	// reverse path: re-decompress + apply).
	for _, ch := range subs {
		decoded := decodeForSubscriber(q, opts.DeltaAgainst)
		select {
		case ch <- decoded:
		default:
			// Drop if subscriber is slow; never block PublishField.
		}
	}

	// 2. WebSocket broadcast — JSON-encode and ship to connected clients.
	payload, err := json.Marshal(q)
	if err != nil {
		return
	}
	h.Broadcast(fieldEventPrefix+topic, json.RawMessage(payload))
}

// decodeForSubscriber reconstructs the *Field that PublishField just emitted,
// applying delta against the previous base if necessary. We deliberately
// round-trip through the codec rather than handing out the original *Field,
// because the codec is lossy and subscribers should observe the same data
// the wire format will produce. This guarantees server and client agreement.
func decodeForSubscriber(q *Quantized, base *Field) *Field {
	if q.IsDelta && base != nil {
		return ApplyDelta(base, q)
	}
	return q.Decompress()
}

// SubscribeField returns a channel that receives every field published to
// the topic on the given hub. The channel is buffered (size 4); if a
// subscriber is slow, updates are dropped to avoid blocking publishers.
//
// Subscriptions are scoped to the (hub, topic) pair. Calling SubscribeField
// twice on the same hub/topic returns two independent channels.
func SubscribeField(h *hub.Hub, topic string) <-chan *Field {
	ch := make(chan *Field, 4)
	streams.mu.Lock()
	state := streams.get(h, topic)
	state.subscribers = append(state.subscribers, ch)
	streams.mu.Unlock()
	return ch
}
