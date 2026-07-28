package hub

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/crdt"
)

// newStalledClient returns a client whose send buffers are already full, so the
// next trySend must drop.
func newStalledClient(h *Hub, id string) *Client {
	client := &Client{
		ID:         id,
		Hub:        h,
		send:       make(chan []byte, 1),
		binarySend: make(chan []byte, 1),
		syncStates: newPeerSyncState(),
	}
	client.send <- []byte("filler")
	client.binarySend <- []byte("filler")
	h.mu.Lock()
	h.clients[id] = client
	h.mu.Unlock()
	return client
}

func TestBroadcastCountsDroppedMessages(t *testing.T) {
	h := New("drops")
	client := newStalledClient(h, "slow")

	for i := 0; i < 3; i++ {
		h.Broadcast("tick", map[string]int{"n": i})
	}

	if got := client.DropStats().Text; got != 3 {
		t.Fatalf("client text drops = %d, want 3", got)
	}
	if got := h.DropStats().Text; got != 3 {
		t.Fatalf("hub text drops = %d, want 3", got)
	}
	if got := h.DropStats().Total(); got != 3 {
		t.Fatalf("hub total drops = %d, want 3", got)
	}
	per := h.ClientDropStats()
	if per["slow"].Text != 3 {
		t.Fatalf("per-client map text drops = %d, want 3", per["slow"].Text)
	}
}

func TestSendAndBroadcastWhereCountDrops(t *testing.T) {
	h := New("drops")
	client := newStalledClient(h, "slow")

	h.Send("slow", "direct", "payload")
	h.BroadcastWhere("scoped", "payload", func(*Client) bool { return true })

	if got := client.DropStats().Text; got != 2 {
		t.Fatalf("client text drops = %d, want 2", got)
	}
	if got := h.DropStats().Text; got != 2 {
		t.Fatalf("hub text drops = %d, want 2", got)
	}
}

func TestBinarySendCountsDropsSeparately(t *testing.T) {
	h := New("drops")
	client := newStalledClient(h, "slow")

	if client.tryBinarySend([]byte{1, 2, 3}) {
		t.Fatal("tryBinarySend reported success on a full buffer")
	}
	stats := client.DropStats()
	if stats.Binary != 1 || stats.Text != 0 {
		t.Fatalf("client drops = %+v, want Binary 1 and Text 0", stats)
	}
	if got := h.DropStats().Binary; got != 1 {
		t.Fatalf("hub binary drops = %d, want 1", got)
	}
}

func TestClosedClientDropsAreNotCounted(t *testing.T) {
	h := New("drops")
	client := newStalledClient(h, "gone")
	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()

	h.Broadcast("tick", 1)

	if got := h.DropStats().Total(); got != 0 {
		t.Fatalf("hub drops = %d, want 0 for a departed client", got)
	}
}

func TestDropStatsSurviveClientRemoval(t *testing.T) {
	h := New("drops")
	client := newStalledClient(h, "slow")
	h.Broadcast("tick", 1)

	h.mu.Lock()
	delete(h.clients, client.ID)
	h.mu.Unlock()

	if got := h.DropStats().Text; got != 1 {
		t.Fatalf("hub text drops after removal = %d, want 1", got)
	}
	if len(h.ClientDropStats()) != 0 {
		t.Fatal("ClientDropStats still lists a departed client")
	}
}

func TestReadLimitIsPerConnection(t *testing.T) {
	h := New("limits")
	writer := &Client{ID: "writer", Hub: h}
	reader := &Client{ID: "reader", Hub: h}

	if got := h.readLimitFor(writer); got != maxMessageSize {
		t.Fatalf("read limit with no sync docs = %d, want %d", got, maxMessageSize)
	}

	h.SyncDoc("notes", crdt.NewDoc())

	if got := h.readLimitFor(writer); got != defaultSyncMessageSize {
		t.Fatalf("allow-all read limit = %d, want %d", got, defaultSyncMessageSize)
	}

	h.SetBinaryAuthorizer(func(c *Client, _ string) bool { return c.ID == "writer" })
	if got := h.readLimitFor(writer); got != defaultSyncMessageSize {
		t.Fatalf("authorized read limit = %d, want %d", got, defaultSyncMessageSize)
	}
	if got := h.readLimitFor(reader); got != maxMessageSize {
		t.Fatalf("unauthorized read limit = %d, want %d", got, maxMessageSize)
	}

	h.MaxSyncMessageSize = 1024
	if got := h.readLimitFor(writer); got != 1024 {
		t.Fatalf("configured read limit = %d, want 1024", got)
	}
}

// TestLiveConnectionKeepsSmallLimitWithoutSyncDocs proves the served path picks
// the per-connection limit and does not raise it for a hub with no documents.
func TestLiveConnectionKeepsSmallLimitWithoutSyncDocs(t *testing.T) {
	h := New("limits-live")
	server := httptest.NewServer(h)
	defer server.Close()

	conn := dialHub(t, server.URL)
	defer conn.Close()
	readUntilEvent(t, conn, "__welcome")

	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.clients) != 1 {
		t.Fatalf("client count = %d, want 1", len(h.clients))
	}
	for _, client := range h.clients {
		if got := h.readLimitFor(client); got != maxMessageSize {
			t.Fatalf("read limit = %d, want %d", got, maxMessageSize)
		}
	}
}

func dialHub(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// BenchmarkBroadcastFanout measures the fan-out path that now counts drops. The
// counters only run on the drop branch, so a healthy client pays nothing.
func BenchmarkBroadcastFanout(b *testing.B) {
	h := New("bench")
	const clients = 64
	for i := 0; i < clients; i++ {
		client := &Client{
			ID:         fmt.Sprintf("c%d", i),
			Hub:        h,
			send:       make(chan []byte, 256),
			binarySend: make(chan []byte, 256),
			syncStates: newPeerSyncState(),
		}
		h.mu.Lock()
		h.clients[client.ID] = client
		h.mu.Unlock()
	}
	drain := func() {
		h.mu.RLock()
		defer h.mu.RUnlock()
		for _, client := range h.clients {
			for len(client.send) > 0 {
				<-client.send
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Broadcast("tick", map[string]int{"n": i})
		if i%200 == 199 {
			b.StopTimer()
			drain()
			b.StartTimer()
		}
	}
}
