package hub

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/telemetryauthority"
)

func TestEncodeMessageEmbedsRawJSONAsObject(t *testing.T) {
	state := []byte(`{"coreInner":"#ff8800","activity":0.42}`)
	asBytes, err := encodeMessage("tick", map[string]any{"frame": 7, "state": state})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asBytes), `"state":"`) {
		t.Fatalf("[]byte must encode as a base64 string (documented pitfall): %s", asBytes)
	}
	asRaw, err := encodeMessage("tick", map[string]any{"frame": 7, "state": json.RawMessage(state)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asRaw), `"state":{"coreInner":"#ff8800","activity":0.42}`) {
		t.Fatalf("json.RawMessage must embed the object: %s", asRaw)
	}
}

func TestReplayLatchedSendsEveryLatchedTopicToOneClient(t *testing.T) {
	h := New("replay")
	h.Latch("a")
	h.Latch("b")
	h.Latch("never")
	h.Broadcast("a", map[string]any{"v": 1})
	h.Broadcast("b", map[string]any{"v": 2})
	client := queueFixture(h)
	client.ID = "c1"
	h.mu.Lock()
	h.clients[client.ID] = client
	h.mu.Unlock()
	if got := h.ReplayLatched("c1"); got != 2 {
		t.Fatalf("replayed %d payloads, want 2", got)
	}
	if got := h.ReplayLatched("c1"); got != 0 {
		t.Fatalf("a second replay inside ReplayLatchedMinInterval sent %d, want 0", got)
	}
	if got := h.ReplayLatched("missing"); got != 0 {
		t.Fatalf("unknown client replayed %d, want 0", got)
	}
	if len(client.send) != 2 {
		t.Fatalf("client queue holds %d frames, want 2", len(client.send))
	}
}

func TestReplayLatchedAllowsAnotherReplayAfterTheInterval(t *testing.T) {
	h := New("replay-interval")
	h.Latch("a")
	h.Broadcast("a", map[string]any{"v": 1})
	client := queueFixture(h)
	client.ID = "c1"
	h.mu.Lock()
	h.clients[client.ID] = client
	h.mu.Unlock()
	if h.ReplayLatched("c1") != 1 {
		t.Fatal("first replay must send")
	}
	client.lastReplayNanos.Store(time.Now().Add(-2 * ReplayLatchedMinInterval).UnixNano())
	if h.ReplayLatched("c1") != 1 {
		t.Fatal("a replay after the interval must send")
	}
}

// A replay must never put an older latched payload behind a newer broadcast.
func TestReplayLatchedNeverDeliversStaleStateAfterABroadcast(t *testing.T) {
	for round := 0; round < 200; round++ {
		h := New("replay-order")
		h.Latch("a")
		h.Broadcast("a", map[string]any{"v": 0})
		client := queueFixture(h)
		client.ID = "c1"
		client.send = make(chan []byte, 512)
		h.mu.Lock()
		h.clients[client.ID] = client
		h.mu.Unlock()

		done := make(chan struct{})
		go func() {
			defer close(done)
			for v := 1; v <= 20; v++ {
				h.Broadcast("a", map[string]any{"v": v})
			}
		}()
		h.ReplayLatched("c1")
		<-done

		last := -1
		for len(client.send) > 0 {
			var msg struct {
				Data struct {
					V int `json:"v"`
				} `json:"data"`
			}
			if err := json.Unmarshal(<-client.send, &msg); err != nil {
				t.Fatal(err)
			}
			if msg.Data.V < last {
				t.Fatalf("round %d: v=%d arrived after v=%d", round, msg.Data.V, last)
			}
			last = msg.Data.V
		}
	}
}

type broadcastingObserver struct {
	NoopObserver
	hub  *Hub
	once atomic.Bool
}

func (o *broadcastingObserver) Message(_ *Hub, _ *Client, _ TrafficEvent) {
	if o.once.CompareAndSwap(false, true) {
		o.hub.Broadcast("a", map[string]any{"v": 99})
	}
}

// An observer that broadcasts must not deadlock against the latch lock that
// ReplayLatched holds while it queues frames.
func TestReplayLatchedRunsObserversAfterReleasingTheLatchLock(t *testing.T) {
	h := New("replay-observer")
	h.Latch("a")
	h.Broadcast("a", map[string]any{"v": 1})
	obs := &broadcastingObserver{hub: h}
	if _, err := h.UseTelemetryObserver(obs, 1, telemetryauthority.New()); err != nil {
		t.Fatal(err)
	}
	client := queueFixture(h)
	client.ID = "c1"
	h.mu.Lock()
	h.clients[client.ID] = client
	h.mu.Unlock()
	done := make(chan int, 1)
	go func() { done <- h.ReplayLatched("c1") }()
	select {
	case sent := <-done:
		if sent != 1 {
			t.Fatalf("replayed %d, want 1", sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReplayLatched deadlocked with an observer that calls Broadcast")
	}
	if !obs.once.Load() {
		t.Fatal("the observer never ran, so the test proves nothing")
	}
}
