package field

import (
	"encoding/json"
	"sync"

	enc "m31labs.dev/gosx/crdt/encoding"
	"m31labs.dev/gosx/hub"
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

// get returns (and lazily creates) the topicState for the given hub and topic.
//
// IMPORTANT: callers must hold s.mu before calling get. This function reads
// and mutates s.topics directly without acquiring the lock itself, so calling
// it from an unlocked context is a data race.
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
func PublishField(h *hub.Hub, topic string, f *Field, opts QuantizeOptions) error {
	q, err := quantizeAndDispatch(h, topic, f, opts)
	if err != nil {
		return err
	}

	// WebSocket broadcast — JSON-encode and ship to connected clients.
	// encoding/json base64-encodes Packed and Preview, so the frame is about
	// 33% larger than q.WireSize. Use PublishFieldBinary when both ends can
	// speak the compact binary form.
	payload, err := json.Marshal(q)
	if err != nil {
		return err
	}
	if h != nil {
		h.Broadcast(fieldEventPrefix+topic, json.RawMessage(payload))
	}
	return nil
}

// fieldBinaryPrefix marks a binary hub frame as a quantized field payload.
// Hub.SyncDoc allocates prefixes 1 through 255 for CRDT documents, so 0 is free
// for application protocols.
const fieldBinaryPrefix byte = 0x00

// PublishFieldBinary is PublishField over the compact binary wire form. It
// returns the number of bytes in the frame.
//
// The frame is about 25% smaller than the JSON frame that PublishField sends,
// because encoding/json base64-encodes the packed payload. Local subscribers
// and the delta bookkeeping behave exactly as they do for PublishField.
//
// The frame layout is one prefix byte, the topic as a length-prefixed string,
// then the payload of Quantized.MarshalBinary. Decode it with DecodeFieldFrame.
//
// The stock GoSX browser runtime ignores binary hub frames, so a caller must
// supply its own decoder before switching a page to this transport.
func PublishFieldBinary(h *hub.Hub, topic string, f *Field, opts QuantizeOptions) (int, error) {
	q, err := quantizeAndDispatch(h, topic, f, opts)
	if err != nil {
		return 0, err
	}
	body, err := q.MarshalBinary()
	if err != nil {
		return 0, err
	}
	frame := make([]byte, 0, 1+10+len(topic)+len(body))
	frame = append(frame, fieldBinaryPrefix)
	frame = enc.AppendULEB128(frame, uint64(len(topic)))
	frame = append(frame, topic...)
	frame = append(frame, body...)
	if h != nil {
		h.BroadcastBinary(frame)
	}
	return len(frame), nil
}

// DecodeFieldFrame reads a frame produced by PublishFieldBinary.
func DecodeFieldFrame(frame []byte) (string, *Quantized, error) {
	const op = "field.DecodeFieldFrame"
	if len(frame) == 0 {
		return "", nil, fieldError(op, "frame is empty")
	}
	if frame[0] != fieldBinaryPrefix {
		return "", nil, fieldError(op, "frame prefix is %d, want %d", frame[0], fieldBinaryPrefix)
	}
	r := &wireReader{buf: frame, pos: 1}
	name, err := r.blob()
	if err != nil {
		return "", nil, fieldError(op, "%v", err)
	}
	topic := string(name)
	q, err := DecodeQuantized(frame[r.pos:])
	if err != nil {
		return topic, nil, err
	}
	return topic, q, nil
}

// quantizeAndDispatch runs the shared part of both publish paths: it picks the
// delta base, quantizes, records the new base, and hands the decoded field to
// the local subscribers.
func quantizeAndDispatch(h *hub.Hub, topic string, f *Field, opts QuantizeOptions) (*Quantized, error) {
	streams.mu.Lock()
	state := streams.get(h, topic)
	if opts.DeltaAgainst == nil {
		opts.DeltaAgainst = state.last
	}
	subs := make([]chan<- *Field, len(state.subscribers))
	copy(subs, state.subscribers)
	streams.mu.Unlock()

	q, err := f.QuantizeChecked(opts)
	if err != nil {
		return nil, err
	}

	var decoded *Field
	if len(subs) > 0 {
		decoded, err = decodeForSubscriber(q, opts.DeltaAgainst)
		if err != nil {
			return nil, err
		}
	}

	streams.mu.Lock()
	state = streams.get(h, topic)
	state.last = f
	streams.mu.Unlock()

	// Local dispatch — decode once and share the decoded *Field across all
	// local subscribers. Subscribers must treat the received *Field as
	// read-only; this is the documented contract for SubscribeField.
	for _, ch := range subs {
		select {
		case ch <- decoded:
		default:
			// Drop if the subscriber is slow; never block the publisher.
		}
	}
	return q, nil
}

// decodeForSubscriber reconstructs the *Field that PublishField just emitted,
// applying delta against the previous base if necessary. We deliberately
// round-trip through the codec rather than handing out the original *Field,
// because the codec is lossy and subscribers should observe the same data
// the wire format will produce. This guarantees server and client agreement.
func decodeForSubscriber(q *Quantized, base *Field) (*Field, error) {
	if q.IsDelta && base != nil {
		return ApplyDeltaChecked(base, q)
	}
	return q.DecompressChecked()
}

// SubscribeField returns a channel that receives every field published to
// the topic on the given hub. The channel is buffered (size 4); if a
// subscriber is slow, updates are dropped to avoid blocking publishers.
// Subscribers must treat received *Field values as read-only.
//
// Subscriptions are scoped to the (hub, topic) pair. Calling SubscribeField
// twice on the same hub/topic returns two independent channels.
//
// Subscriptions are permanent for the lifetime of the process — there is no
// unsubscribe API yet. Use this for long-lived consumers (rendering loops,
// sim-state mirrors). Short-lived subscribers will leak channels and grow
// the per-topic dispatch list. A cancellation API will be added in a
// follow-up if/when needed.
func SubscribeField(h *hub.Hub, topic string) <-chan *Field {
	ch := make(chan *Field, 4)
	streams.mu.Lock()
	state := streams.get(h, topic)
	state.subscribers = append(state.subscribers, ch)
	streams.mu.Unlock()
	return ch
}
