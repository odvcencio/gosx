package hub

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/internal/telemetryauthority"
)

func snapshotFixture(h *Hub, id string) *Client {
	c := queueFixture(h)
	c.ID = id
	c.snapshotWake = make(chan struct{}, 1)
	h.clients[id] = c
	return c
}

func takeSnapshot(t *testing.T, c *Client, want []byte) {
	t.Helper()
	select {
	case <-c.snapshotWake:
	default:
		t.Fatal("snapshot not ready")
	}
	got, ok := c.takeSnapshot()
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("snapshot = %v, %v; want %v", got, ok, want)
	}
}

func TestSnapshotSlowReaderRetainsBaselineAndLatest(t *testing.T) {
	h := New("snapshots")
	c := snapshotFixture(h, "slow")
	baseline := []byte{0, 1}
	for i := uint32(0); i < 1024; i++ {
		delta := make([]byte, 5)
		binary.BigEndian.PutUint32(delta[1:], i)
		if h.BroadcastBinarySnapshot(1, baseline, delta) != 1 {
			t.Fatal("slow client rejected an offer")
		}
	}
	if len(c.snapshotWake) != 1 || len(c.binarySend) != 0 || len(c.send) != 0 {
		t.Fatal("snapshot backlog leaked into reliable queues")
	}
	if got := c.SnapshotStats().Replaced; got != 1023 || h.SnapshotStats().Replaced != got {
		t.Fatalf("replacements = %d, want 1023", got)
	}
	if c.DropStats().Total() != 0 || h.DropStats().Total() != 0 {
		t.Fatal("replacements counted as slow-client drops")
	}
	takeSnapshot(t, c, baseline)
	latest := make([]byte, 5)
	binary.BigEndian.PutUint32(latest[1:], 1023)
	takeSnapshot(t, c, latest)
	if len(c.snapshotWake) != 0 {
		t.Fatal("drained snapshot remained ready")
	}
}

func TestSnapshotEpochOrderingAndNewClient(t *testing.T) {
	h := New("epochs")
	c := snapshotFixture(h, "original")
	baseline1, delta1 := []byte{0, 1}, []byte{0, 11}
	baseline2, delta2 := []byte{0, 2}, []byte{0, 22}
	h.BroadcastBinarySnapshot(1, baseline1, delta1)
	// The first baseline may already be in a socket write. The sole writer
	// completes that frame before taking any later delta or epoch baseline.
	takeSnapshot(t, c, baseline1)
	h.BroadcastBinarySnapshot(1, baseline1, []byte{0, 12})
	h.BroadcastBinarySnapshot(2, baseline2, delta2)
	if h.BroadcastBinarySnapshot(1, baseline1, delta1) != 0 {
		t.Fatal("stale epoch was accepted")
	}
	// A duplicate keyframe does not replace the epoch's first baseline or
	// discard a delta already pending against it.
	h.BroadcastBinarySnapshot(2, []byte{0, 99}, nil)
	takeSnapshot(t, c, baseline2)
	takeSnapshot(t, c, delta2)
	if c.SnapshotStats().Replaced != 2 {
		t.Fatal("replaced in-flight baseline or missed a replaced delta")
	}
	joined := snapshotFixture(h, "joined")
	h.BroadcastBinarySnapshot(2, baseline2, []byte{0, 23})
	takeSnapshot(t, c, []byte{0, 23})
	takeSnapshot(t, joined, baseline2)
	takeSnapshot(t, joined, []byte{0, 23})
	if h.BroadcastBinarySnapshot(3, []byte{0, 3}, nil) != 2 {
		t.Fatal("keyframe-only offer rejected")
	}
	takeSnapshot(t, c, []byte{0, 3})
	if len(c.snapshotWake) != 0 {
		t.Fatal("keyframe-only offer created a delta")
	}
}

func TestSnapshotNewEpochReplacesBothPendingFrames(t *testing.T) {
	h := New("epochs")
	c := snapshotFixture(h, "slow")
	h.BroadcastBinarySnapshot(1, []byte{0, 1}, []byte{0, 11})
	h.BroadcastBinarySnapshot(2, []byte{0, 2}, []byte{0, 22})
	if c.SnapshotStats().Replaced != 2 {
		t.Fatal("pending baseline and delta not counted")
	}
	takeSnapshot(t, c, []byte{0, 2})
	takeSnapshot(t, c, []byte{0, 22})
}

func TestSnapshotRejectsInvalidOffersWithoutDisconnect(t *testing.T) {
	h := New("invalid")
	c := snapshotFixture(h, "client")
	for _, offer := range []struct {
		epoch       uint64
		base, delta []byte
	}{
		{0, []byte{0}, nil}, {1, nil, nil}, {1, []byte{1}, nil},
		{1, []byte{0}, []byte{1}},
	} {
		if h.BroadcastBinarySnapshot(offer.epoch, offer.base, offer.delta) != 0 {
			t.Fatal("invalid offer accepted")
		}
	}
	if c.closed || c.snapshot != nil || c.DropStats().Total() != 0 || c.SnapshotStats().Replaced != 0 {
		t.Fatal("invalid offer changed client")
	}
	if h.BroadcastBinarySnapshot(1, []byte{0}, nil) != 1 {
		t.Fatal("valid offer after invalid offers failed")
	}
}

func TestSnapshotAlternatesWithReliableQueuesAndTimer(t *testing.T) {
	h := New("fair")
	c := snapshotFixture(h, "client")
	for _, binary := range []bool{false, true} {
		c = snapshotFixture(h, "client")
		queue := c.send
		if binary {
			queue = c.binarySend
		}
		for i := uint64(1); i <= 10; i++ {
			queue <- []byte{1, byte(i)}
			h.BroadcastBinarySnapshot(i, []byte{0, byte(i)}, []byte{0, byte(i + 10)})
			reliable := c.nextOutbound(nil, false)
			if reliable.snapshot || reliable.closed || reliable.payload[1] != byte(i) {
				t.Fatal("snapshot starved or changed reliable FIFO")
			}
			snapshot := c.nextOutbound(nil, true)
			if !snapshot.snapshot || snapshot.payload[1] != byte(i) {
				t.Fatal("reliable work starved the snapshot lane")
			}
		}
	}
	// Reset to a fresh client so epochs from the two FIFO checks do not
	// affect this timer scheduling check.
	c = snapshotFixture(h, "timer")
	h.BroadcastBinarySnapshot(11, []byte{0, 11}, nil)
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	if frame := c.nextOutbound(ticks, false); frame.kind != 0 {
		t.Fatal("snapshot starved a ready timer")
	}
}

type snapshotObserver struct {
	NoopObserver
	mu     sync.Mutex
	events []TrafficEvent
}

func (o *snapshotObserver) Message(h *Hub, c *Client, event TrafficEvent) {
	h.mu.Lock()
	h.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		c.mu.Unlock()
	}
	o.mu.Lock()
	o.events = append(o.events, event)
	o.mu.Unlock()
}

func TestSnapshotObserverReplacementsAndIndependentSampling(t *testing.T) {
	h := New("observer")
	o := &snapshotObserver{}
	_, err := h.UseTelemetryObserver(o, 2, telemetryauthority.New())
	if err != nil {
		t.Fatal(err)
	}
	c := snapshotFixture(h, "client")
	h.BroadcastBinarySnapshot(1, []byte{0, 1}, []byte{0, 2})
	if len(o.events) != 0 {
		t.Fatal("snapshot sampled too early")
	}
	c.trySend([]byte("text"))
	c.tryBinarySend([]byte{1})
	h.BroadcastBinarySnapshot(1, []byte{0, 1}, []byte{0, 3})
	var replaced, sampled uint64
	for _, e := range o.events {
		if !e.Snapshot || !e.Binary || e.Dropped || e.Written {
			t.Fatalf("unexpected snapshot event: %+v", e)
		}
		if e.Replaced {
			replaced += e.Count
			if e.Bytes != 2 || e.QueueDepth != -1 {
				t.Fatal(e)
			}
		}
		if e.QueueDepth >= 0 {
			sampled += e.Count
			if e.QueueDepth != 2 {
				t.Fatal(e)
			}
		}
	}
	if replaced != 1 || sampled != 1 {
		t.Fatalf("replacement/sample counts = %d/%d", replaced, sampled)
	}
	c.trySend([]byte("text"))
	c.tryBinarySend([]byte{1})
	if len(o.events) != 4 {
		t.Fatal("snapshot changed reliable queue sample cadence")
	}
}

func TestSnapshotConcurrentCloseAndWrittenObservation(t *testing.T) {
	h := New("live")
	o := &snapshotObserver{}
	_, err := h.UseObserver(o)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	conn := dialHub(t, server.URL)
	defer conn.Close()
	readUntilEvent(t, conn, "__welcome")
	baseline, delta := []byte{0, 1}, []byte{0, 2}
	h.BroadcastBinarySnapshot(1, baseline, delta)
	for _, want := range [][]byte{baseline, delta} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		kind, got, err := conn.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(got, want) {
			t.Fatalf("live frame = %d, %v, %v; want %v", kind, got, err, want)
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for epoch := uint64(2); epoch <= 300; epoch++ {
				h.BroadcastBinarySnapshot(epoch, baseline, delta)
				h.Broadcast("reliable", epoch)
				h.BroadcastBinary([]byte{0, 4})
			}
		}()
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if h.BroadcastBinarySnapshot(1000, baseline, delta) != 0 {
		t.Fatal("closed hub accepted a snapshot")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	var written int
	for _, e := range o.events {
		if e.Snapshot && e.Written {
			if !e.Binary || e.Replaced || e.Dropped || e.QueueDepth != -1 {
				t.Fatal(e)
			}
			written++
		}
	}
	if written < 2 {
		t.Fatal("successful snapshot writes were not observed")
	}
}

func TestSnapshotCloseWaitsForEnqueueObservation(t *testing.T) {
	h := New("closing")
	snapshotFixture(h, "client")
	o := &blockingEnqueueObserver{closingObserver: closingObserver{entered: make(chan struct{}), release: make(chan struct{})}}
	_, err := h.UseTelemetryObserver(o, 1, telemetryauthority.New())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { h.BroadcastBinarySnapshot(1, []byte{0}, nil); close(done) }()
	receiveHubEvent(t, o.entered)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); err != context.DeadlineExceeded || o.closed.Load() {
		t.Fatal("premature observer closure", err)
	}
	close(o.release)
	receiveHubEvent(t, done)
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !o.closed.Load() {
		t.Fatal("observer closure missing")
	}
}

func TestSnapshotBlockedSocketKeepsLatestEpochDecodable(t *testing.T) {
	h := New("gated-snapshot")
	listener := &writeGateListener{accepted: make(chan *writeGateConn, 1)}
	server := httptest.NewUnstartedServer(h)
	listener.Listener = server.Listener
	server.Listener = listener
	server.Start()
	defer server.Close()
	conn := dialHub(t, server.URL)
	defer conn.Close()
	serverConn := <-listener.accepted
	defer serverConn.releaseWrites()
	readUntilEvent(t, conn, "__welcome")
	serverConn.blockWrites()
	baseline1 := []byte{0, 1}
	h.BroadcastBinarySnapshot(1, baseline1, []byte{0, 11})
	select {
	case <-serverConn.entered:
	case <-time.After(time.Second):
		t.Fatal("baseline write did not enter transport gate")
	}
	for range 1024 {
		h.BroadcastBinarySnapshot(1, baseline1, []byte{0, 12})
	}
	h.BroadcastBinarySnapshot(2, []byte{0, 2}, []byte{0, 22})
	h.BroadcastBinarySnapshot(2, []byte{0, 2}, []byte{0, 23})
	if h.SnapshotStats().Replaced != 1026 || h.DropStats().Total() != 0 {
		t.Fatal("in-flight baseline counted as replaced, or queued replacements missed")
	}
	h.mu.RLock()
	for _, c := range h.clients {
		c.mu.Lock()
		if len(c.binarySend) != 0 || len(c.send) != 0 || len(c.snapshotWake) != 1 || c.snapshot.epoch != 2 {
			t.Error("blocked writer accumulated stale reliable frames")
		}
		c.mu.Unlock()
	}
	h.mu.RUnlock()
	serverConn.releaseWrites()
	// The one in-flight frame cannot be replaced. Its old pending delta is
	// replaced, then the new baseline must precede the newest dependent delta.
	for _, want := range [][]byte{baseline1, {0, 2}, {0, 23}} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		kind, got, err := conn.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(got, want) {
			t.Fatalf("gated frame = %d, %v, %v; want %v", kind, got, err, want)
		}
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
