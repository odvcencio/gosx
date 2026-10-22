package hub

import (
	"encoding/json"
	"strings"
	"testing"
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
	if got := h.ReplayLatched("missing"); got != 0 {
		t.Fatalf("unknown client replayed %d, want 0", got)
	}
	if len(client.send) != 2 {
		t.Fatalf("client queue holds %d frames, want 2", len(client.send))
	}
}
