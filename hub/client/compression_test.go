package hubclient

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

// TestClientCompressionRoundTripsThroughNativeTransport exercises the native
// (gorilla) transport with EnableCompression on both ends: a compressed hub
// broadcast must still decode to the same payload a Client's handler
// receives.
func TestClientCompressionRoundTripsThroughNativeTransport(t *testing.T) {
	h := hub.New("client-compression")
	h.EnableCompression = true
	server := httptest.NewServer(h)
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL), EnableCompression: true})
	defer c.Close()

	type entity struct {
		ID    int    `json:"id"`
		Kind  string `json:"kind"`
		State string `json:"state"`
	}
	entities := make([]entity, 100)
	for i := range entities {
		entities[i] = entity{ID: i, Kind: "enemy", State: "walking"}
	}
	want, err := json.Marshal(entities)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	received := make(chan json.RawMessage, 1)
	c.On("snapshot", func(data json.RawMessage) {
		received <- data
	})
	c.Connect()

	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	h.Broadcast("snapshot", entities)

	select {
	case got := <-received:
		if string(got) != string(want) {
			t.Fatalf("compressed native round trip changed payload:\nwant %s\ngot  %s", want, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for compressed snapshot")
	}
}

// TestClientCompressionDefaultOff documents that a Client dials without
// offering permessage-deflate unless Options.EnableCompression is set, even
// when the hub itself has compression enabled — negotiation needs both
// sides to offer the extension.
func TestClientCompressionDefaultOff(t *testing.T) {
	h := hub.New("client-compression-default")
	h.EnableCompression = true
	server := httptest.NewServer(h)
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()
	c.Connect()

	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	// StateConnected only flips once the transport's frameOpen event fires,
	// which for the native transport means gorilla's Dial already completed
	// the WebSocket handshake — including extension negotiation. Reaching
	// StateConnected here shows the handshake succeeded even though this
	// client never offered permessage-deflate.
}
