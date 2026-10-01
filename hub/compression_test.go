package hub

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// countingConn wraps a net.Conn and totals bytes read from the peer, so a
// test can measure the wire size of a broadcast frame without decoding it.
type countingConn struct {
	net.Conn
	read atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

// repetitiveSnapshot mimics a broadcast entity snapshot: many small objects
// that share the same keys and similar values. This is the shape
// permessage-deflate compresses well even without context takeover, because
// the redundancy is inside a single message.
func repetitiveSnapshot(n int) map[string]any {
	entities := make([]map[string]any, n)
	for i := range entities {
		entities[i] = map[string]any{
			"id":    i,
			"kind":  "enemy",
			"x":     1000 + i,
			"y":     2000,
			"hp":    100,
			"state": "walking",
		}
	}
	return map[string]any{"tick": 12345, "entities": entities}
}

func TestHubCompressionOffByDefault(t *testing.T) {
	h := New("compression-default")
	server := httptest.NewServer(h)
	defer server.Close()

	dialer := websocket.Dialer{EnableCompression: true}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if got := resp.Header.Get("Sec-WebSocket-Extensions"); got != "" {
		t.Fatalf("hub with EnableCompression unset negotiated an extension: %q", got)
	}
}

func TestHubCompressionNegotiatesWhenEnabled(t *testing.T) {
	h := New("compression-on")
	h.EnableCompression = true
	server := httptest.NewServer(h)
	defer server.Close()

	dialer := websocket.Dialer{EnableCompression: true}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ext := resp.Header.Get("Sec-WebSocket-Extensions")
	if !strings.Contains(ext, "permessage-deflate") {
		t.Fatalf("hub with EnableCompression=true did not negotiate permessage-deflate, got extensions %q", ext)
	}
}

func TestHubCompressionNoOfferStaysUncompressed(t *testing.T) {
	// A hub that enables compression must not force it on a client that
	// never offered the extension.
	h := New("compression-on-no-offer")
	h.EnableCompression = true
	server := httptest.NewServer(h)
	defer server.Close()

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if got := resp.Header.Get("Sec-WebSocket-Extensions"); got != "" {
		t.Fatalf("server negotiated an extension the client never offered: %q", got)
	}
}

func TestHubCompressionRoundTripsCorrectly(t *testing.T) {
	h := New("compression-roundtrip")
	h.EnableCompression = true
	server := httptest.NewServer(h)
	defer server.Close()

	dialer := websocket.Dialer{EnableCompression: true}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	readUntilEvent(t, conn, "__welcome")

	want := repetitiveSnapshot(200)
	h.Broadcast("snapshot", want)

	msg := readUntilEvent(t, conn, "snapshot")
	var got map[string]any
	if err := json.Unmarshal(msg.Data, &got); err != nil {
		t.Fatalf("decode compressed broadcast: %v", err)
	}
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("compressed round trip changed payload:\nwant %s\ngot  %s", wantJSON, gotJSON)
	}
}

func TestHubCompressionReducesWireBytes(t *testing.T) {
	payload := repetitiveSnapshot(400)

	measure := func(t *testing.T, enable bool) int64 {
		t.Helper()
		h := New("compression-bytes")
		h.EnableCompression = enable
		server := httptest.NewServer(h)
		defer server.Close()

		var cc *countingConn
		dialer := websocket.Dialer{
			EnableCompression: true, // client always offers; hub decides
			NetDial: func(network, addr string) (net.Conn, error) {
				raw, err := net.Dial(network, addr)
				if err != nil {
					return nil, err
				}
				cc = &countingConn{Conn: raw}
				return cc, nil
			},
		}
		conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()
		readUntilEvent(t, conn, "__welcome")

		before := cc.read.Load()
		h.Broadcast("snapshot", payload)
		readUntilEvent(t, conn, "snapshot")
		return cc.read.Load() - before
	}

	uncompressed := measure(t, false)
	compressed := measure(t, true)
	t.Logf("wire bytes for one 400-entity snapshot: uncompressed=%d compressed=%d ratio=%.3f", uncompressed, compressed, float64(compressed)/float64(uncompressed))

	if compressed >= uncompressed {
		t.Fatalf("compression did not reduce wire bytes: compressed=%d uncompressed=%d", compressed, uncompressed)
	}
	// Repetitive JSON with a shared key/value shape should compress well
	// within a single message, even without context takeover. Guard against
	// a regression that negotiates the extension but never actually shrinks
	// the frame (for example, a level that only kicks in above some size).
	if ratio := float64(compressed) / float64(uncompressed); ratio > 0.5 {
		t.Fatalf("compression ratio too weak for repetitive JSON: compressed=%d uncompressed=%d ratio=%.2f", compressed, uncompressed, ratio)
	}
}

func TestHubCompressionInvalidLevelFallsBackAndStillWorks(t *testing.T) {
	h := New("compression-bad-level")
	h.EnableCompression = true
	h.CompressionLevel = 999 // out of flate's [-2, 9] range
	server := httptest.NewServer(h)
	defer server.Close()

	dialer := websocket.Dialer{EnableCompression: true}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	readUntilEvent(t, conn, "__welcome")

	h.Broadcast("snapshot", map[string]any{"ok": true})
	msg := readUntilEvent(t, conn, "snapshot")
	var got map[string]any
	if err := json.Unmarshal(msg.Data, &got); err != nil {
		t.Fatalf("decode broadcast after invalid compression level: %v", err)
	}
	if got["ok"] != true {
		t.Fatalf("unexpected payload after invalid compression level: %v", got)
	}
}

func TestResolvedCompressionLevel(t *testing.T) {
	cases := []struct {
		name  string
		level int
		want  int
	}{
		{"zero selects default", 0, defaultCompressionLevel},
		{"in-range value passes through", 3, 3},
		{"huffman-only boundary passes through", -2, -2},
		{"best-compression boundary passes through", 9, 9},
		{"below range falls back", -3, defaultCompressionLevel},
		{"above range falls back", 10, defaultCompressionLevel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New("resolve-level")
			h.CompressionLevel = tc.level
			if got := h.resolvedCompressionLevel(); got != tc.want {
				t.Fatalf("resolvedCompressionLevel() = %d, want %d", got, tc.want)
			}
		})
	}
}
