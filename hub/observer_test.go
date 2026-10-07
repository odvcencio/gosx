package hub

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type observedHub struct {
	NoopObserver
	connected, disconnected, closed atomic.Int64
	messages                        chan TrafficEvent
	rejected                        chan RejectionReason
	messageRejected                 chan MessageRejectionReason
	observerPanics                  atomic.Int64
	handlers                        chan HandlerEvent
	disconnects                     chan DisconnectEvent
	broadcasts                      chan [2]int
}

func newObservedHub() *observedHub {
	return &observedHub{messages: make(chan TrafficEvent, 32), rejected: make(chan RejectionReason, 16), messageRejected: make(chan MessageRejectionReason, 16), handlers: make(chan HandlerEvent, 16), disconnects: make(chan DisconnectEvent, 16), broadcasts: make(chan [2]int, 16)}
}
func (o *observedHub) ClientConnected(h *Hub, c *Client, _ *http.Request) {
	// Reenter APIs that acquire h.mu and c.mu: dispatch must hold neither.
	h.On("observed", func(*Context) {})
	c.mu.Lock()
	c.mu.Unlock()
	if h.ClientCount() == 0 || h.Presence().Count() == 0 {
		panic("connection published too early")
	}
	o.connected.Add(1)
}
func (o *observedHub) ClientDisconnected(h *Hub, c *Client, e DisconnectEvent) {
	if h.Presence().Count() != h.ClientCount() {
		panic("presence cleanup incomplete")
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		panic("channels still open")
	}
	o.disconnected.Add(1)
	o.disconnects <- e
}
func (o *observedHub) Closed(h *Hub)                             { h.ClientCount(); h.On("closed", nil); o.closed.Add(1) }
func (o *observedHub) Message(_ *Hub, _ *Client, e TrafficEvent) { o.messages <- e }
func (o *observedHub) Rejected(h *Hub, r RejectionReason)        { h.ClientCount(); o.rejected <- r }
func (o *observedHub) MessageRejected(_ *Hub, _ *Client, r MessageRejectionReason) {
	o.messageRejected <- r
}
func (o *observedHub) ObserverPanicked(*Hub)          { o.observerPanics.Add(1) }
func (o *observedHub) Handler(h *Hub, e HandlerEvent) { h.ClientCount(); o.handlers <- e }
func (o *observedHub) Broadcast(h *Hub, accepted, dropped int) {
	h.ClientCount()
	o.broadcasts <- [2]int{accepted, dropped}
}

func observedConnection(t *testing.T, h *Hub) *websocket.Conn {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	dialer := *websocket.DefaultDialer
	dialer.EnableCompression = h.EnableCompression
	c, _, err := dialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	return c
}

func receiveHubEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("observer event timeout")
		var zero T
		return zero
	}
}

func TestHubObserversLifecycleAndTraffic(t *testing.T) {
	h := New("synthetic-room")
	a, b := newObservedHub(), newObservedHub()
	detach, err := h.UseObserver(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.UseObserver(b); err != nil {
		t.Fatal(err)
	}
	c := observedConnection(t, h)
	_, welcome, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []*observedHub{a, b} {
		e := receiveHubEvent(t, o.messages)
		if e.Direction != Outbound || !e.Written || e.Bytes != len(welcome) || e.QueueDepth != -1 {
			t.Fatalf("welcome traffic: %+v", e)
		}
		if o.connected.Load() != 1 {
			t.Fatal("missing connected event")
		}
	}
	if _, err := h.UseObserver(NoopObserver{}); !errors.Is(err, ErrAfterServe) {
		t.Fatalf("late install: %v", err)
	}
	detach()
	detach()
	payload := []byte(`{"event":"observed"}`)
	if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}
	e := receiveHubEvent(t, b.messages)
	if e.Direction != Inbound || e.Bytes != len(payload) || e.Written {
		t.Fatalf("inbound traffic: %+v", e)
	}
	if got := receiveHubEvent(t, b.handlers); got.Event != "observed" || got.Panicked || got.Duration < 0 {
		t.Fatalf("handler: %+v", got)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := receiveHubEvent(t, b.disconnects); got.Reason != "hub_closed" {
		t.Fatalf("disconnect: %+v", got)
	}
	if b.disconnected.Load() != 1 || b.closed.Load() != 1 || a.disconnected.Load() != 0 || a.closed.Load() != 0 {
		t.Fatal("lifecycle or detach was not once-only")
	}
	if h.observers.Load() != nil {
		t.Fatal("closed hub retains subscribers")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.closed.Load() != 1 {
		t.Fatal("duplicate closed event")
	}
}

func TestHubBroadcastReportsDepartedVersusFull(t *testing.T) {
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"accept", "full", "departed"} {
		c := &Client{ID: name, send: make(chan []byte, 1), binarySend: make(chan []byte, 1)}
		if name == "full" {
			c.send <- []byte("full")
			c.binarySend <- []byte("full")
		}
		if name == "departed" {
			c.closed = true
		}
		h.clients[name] = c
	}
	h.Broadcast("state", 1)
	if got := receiveHubEvent(t, o.broadcasts); got != [2]int{1, 1} {
		t.Fatalf("broadcast: %v", got)
	}
	if sent := h.BroadcastBinary([]byte{0, 1}); sent != 1 {
		t.Fatalf("binary sent=%d", sent)
	}
	if got := receiveHubEvent(t, o.broadcasts); got != [2]int{1, 1} {
		t.Fatalf("binary broadcast: %v", got)
	}
	h.BroadcastWhere("state", 1, func(c *Client) bool { return c.ID == "departed" })
	if got := receiveHubEvent(t, o.broadcasts); got != [2]int{} {
		t.Fatalf("departed recipient counted as drop: %v", got)
	}
}

type panickingHubObserver struct {
	NoopObserver
	calls atomic.Int64
}

func (o *panickingHubObserver) Message(*Hub, *Client, TrafficEvent) {
	o.calls.Add(1)
	panic("observer-panic-secret-canary")
}

func TestHubObserverPanicIsolated(t *testing.T) {
	h := New("synthetic-room")
	bad, good := &panickingHubObserver{}, newObservedHub()
	if _, err := h.UseObserver(bad); err != nil {
		t.Fatal(err)
	}
	if _, err := h.UseObserver(good); err != nil {
		t.Fatal(err)
	}
	buf := &syncLogBuffer{}
	previous := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(previous)
	for i := 0; i < 2; i++ {
		h.observeMessage(nil, TrafficEvent{QueueDepth: -1})
	}
	if bad.calls.Load() != 1 {
		t.Fatal("panicking subscriber was not disabled")
	}
	if good.observerPanics.Load() != 1 || len(good.rejected) != 0 {
		t.Fatal("observer panic must not count as a connection rejection")
	}
	if strings.Contains(buf.String(), "observer-panic-secret-canary") {
		t.Fatal("observer panic text leaked")
	}
	if len(good.messages) != 2 {
		t.Fatal("healthy subscriber lost events")
	}
	h.invokeHandler(func(*Context) { panic("handler-panic-secret-canary") }, &Context{Event: "turn"})
	if e := receiveHubEvent(t, good.handlers); !e.Panicked || e.Event != "turn" {
		t.Fatalf("panic event: %+v", e)
	}
	for _, detail := range []string{"handler-panic-secret-canary", "synthetic-room", "turn", "goroutine"} {
		if !strings.Contains(buf.String(), detail) {
			t.Fatalf("hub diagnostic lost %q", detail)
		}
	}
}

func TestHubObserverOriginClassifiedOnce(t *testing.T) {
	previous := upgrader.CheckOrigin
	defer SetCheckOrigin(previous)
	var checks atomic.Int64
	SetCheckOrigin(func(*http.Request) bool { checks.Add(1); return false })
	h := New("synthetic-room")
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
	if err == nil {
		t.Fatal("origin accepted")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatal("missing origin rejection")
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	if got := receiveHubEvent(t, o.rejected); got != "rejected_origin" || checks.Load() != 1 {
		t.Fatalf("reason=%q checks=%d", got, checks.Load())
	}
}

func TestHubObserverConcurrentDetach(t *testing.T) {
	h := New("synthetic-room")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				detach, err := h.UseObserver(NoopObserver{})
				if err != nil {
					t.Error(err)
					return
				}
				h.observeMessage(nil, TrafficEvent{QueueDepth: -1})
				detach()
				detach()
			}
		}()
	}
	wg.Wait()
	if h.observers.Load() != nil {
		t.Fatal("detached observers retained")
	}
}

func TestHubObserverDispatchAllocations(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h := New("synthetic-room")
		if enabled {
			if _, err := h.UseObserver(NoopObserver{}); err != nil {
				t.Fatal(err)
			}
		}
		if n := testing.AllocsPerRun(1000, func() { h.observeMessage(nil, TrafficEvent{Direction: Inbound, Bytes: 1024, QueueDepth: -1}) }); n != 0 {
			t.Fatalf("enabled=%v allocs=%v", enabled, n)
		}
	}
}

func TestHubObserverLogicalPayloadKinds(t *testing.T) {
	h := New("synthetic-room")
	h.EnableCompression = true
	h.SetBinaryMessageHandler(func(*Client, []byte) bool { return true })
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	h.On("echo", func(ctx *Context) { h.Send(ctx.Client.ID, "reply", strings.Repeat("synthetic", 1000)) })
	c := observedConnection(t, h)
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	receiveHubEvent(t, o.messages)
	payload := []byte(`{"event":"echo"}`)
	if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}
	if e := receiveHubEvent(t, o.messages); e.Direction != Inbound || e.Bytes != len(payload) || e.Binary {
		t.Fatalf("text inbound: %+v", e)
	}
	_, reply, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if e := receiveHubEvent(t, o.messages); e.Direction != Outbound || !e.Written || e.Bytes != len(reply) || e.Binary {
		t.Fatalf("compressed logical outbound: %+v", e)
	}
	binary := []byte{0, 1, 2}
	if err := c.WriteMessage(websocket.BinaryMessage, binary); err != nil {
		t.Fatal(err)
	}
	if e := receiveHubEvent(t, o.messages); e.Direction != Inbound || !e.Binary || e.Bytes != len(binary) {
		t.Fatalf("binary inbound: %+v", e)
	}
	if sent := h.BroadcastBinary(binary); sent != 1 {
		t.Fatalf("sent=%d", sent)
	}
	if _, _, err := c.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if e := receiveHubEvent(t, o.messages); e.Direction != Outbound || !e.Binary || !e.Written || e.Bytes != len(binary) {
		t.Fatalf("binary outbound: %+v", e)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte("{")); err != nil {
		t.Fatal(err)
	}
	if e := receiveHubEvent(t, o.messages); e.Direction != Inbound || e.Bytes != 1 {
		t.Fatalf("malformed payload count: %+v", e)
	}
	if reason := receiveHubEvent(t, o.messageRejected); reason != MessageMalformed {
		t.Fatalf("reason=%q", reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHubRateLimitIsNotConnectionRejection(t *testing.T) {
	h := New("synthetic-room")
	h.MaxMessagesPerSecond, h.MaxMessageBurst = 1, 1
	o := newObservedHub()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	c := observedConnection(t, h)
	if _, _, err := c.ReadMessage(); err != nil { // welcome
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"event":"turn"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if reason := receiveHubEvent(t, o.messageRejected); reason != MessageRateLimited {
		t.Fatalf("message rejection: %q", reason)
	}
	if e := receiveHubEvent(t, o.disconnects); e.Reason != "rate_limited" {
		t.Fatalf("disconnect: %+v", e)
	}
	if o.connected.Load() != 1 || len(o.rejected) != 0 {
		t.Fatal("an accepted connection was also reported rejected")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type queueObserver struct {
	NoopObserver
	text, binary, drops atomic.Uint64
	depth               atomic.Int64
}

func (o *queueObserver) Message(h *Hub, c *Client, e TrafficEvent) {
	// Both transport critical sections must be released before dispatch.
	if c != nil {
		c.mu.Lock()
		c.mu.Unlock()
	}
	h.mu.Lock()
	h.mu.Unlock()
	count := e.Count
	if count == 0 {
		count = 1
	}
	if e.Dropped {
		o.drops.Add(count)
	}
	if e.QueueDepth >= 0 {
		if e.Binary {
			o.binary.Add(count)
		} else {
			o.text.Add(count)
		}
		o.depth.Store(int64(e.QueueDepth))
	}
}

func queueFixture(h *Hub) *Client {
	return &Client{ID: "fixture", Hub: h, send: make(chan []byte, 256), binarySend: make(chan []byte, 256), transport: &transportState{}}
}

func TestQueueSamplesAreIndependentAndDropsAlwaysFire(t *testing.T) {
	h := New("queue-fixture")
	o := &queueObserver{}
	if _, err := h.UseTelemetryObserver(o, 0); err != nil {
		t.Fatal(err)
	}
	c := queueFixture(h)
	for range 63 {
		c.trySend(nil)
		c.tryBinarySend(nil)
	}
	if o.text.Load() != 0 || o.binary.Load() != 0 {
		t.Fatal("default queue sample fired before its 64th attempt")
	}
	c.trySend(nil)
	if o.text.Load() != 1 || o.binary.Load() != 0 || o.depth.Load() != 64 {
		t.Fatal("text sampling changed the binary cadence")
	}
	c.tryBinarySend(nil)
	if o.binary.Load() != 1 || o.depth.Load() != 64 {
		t.Fatal("binary queue was not sampled independently")
	}
	for range 192 {
		c.trySend(nil)
	}
	if c.trySend(nil) || c.trySend(nil) || o.drops.Load() != 2 || c.DropStats().Text != 2 {
		t.Fatal("full queues did not report each loss once")
	}
	h.removeClient(c)
	if c.trySend(nil) || c.tryBinarySend(nil) || o.drops.Load() != 2 {
		t.Fatal("departed client was counted as a drop")
	}
}

func TestTelemetryObserverSlotReservationAndRemoval(t *testing.T) {
	h := New("queue-fixture")
	app := &queueObserver{}
	appDetach, err := h.UseObserver(app)
	if err != nil {
		t.Fatal(err)
	}
	o := &queueObserver{}
	detach, err := h.UseTelemetryObserver(o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.UseTelemetryObserver(NoopObserver{}, 1); !errors.Is(err, ErrObserverConflict) {
		t.Fatal("duplicate telemetry slot was accepted", err)
	}
	c := queueFixture(h)
	c.trySend(nil)
	c.tryBinarySend(nil)
	if o.text.Load() != 1 || o.binary.Load() != 1 || app.text.Load() != 1 {
		t.Fatal("diagnostic queue override or additive observer failed")
	}
	detach()
	detach()
	if h.queueSampleEvery.Load() != 0 || h.telemetryObserver != nil {
		t.Fatal("detaching retained the reservation or override")
	}
	if _, err := h.UseTelemetryObserver(o, 1); err != nil {
		t.Fatal("pre-serve slot could not be reused", err)
	}
	if !h.reserveClientSlot() {
		t.Fatal("fixture admission failed")
	}
	h.releaseClientSlot()
	if _, err := h.UseTelemetryObserver(o, 1); !errors.Is(err, ErrObserverConflict) {
		t.Fatal("duplicate slot lost its error after serving", err)
	}
	appDetach()
}

func TestQueueObservationWarmAllocations(t *testing.T) {
	for _, every := range []uint32{0, 1} {
		h := New("queue-fixture")
		if _, err := h.UseTelemetryObserver(&queueObserver{}, every); err != nil {
			t.Fatal(err)
		}
		c := queueFixture(h)
		if got := testing.AllocsPerRun(1000, func() {
			c.trySend(nil)
			<-c.send
			c.tryBinarySend(nil)
			<-c.binarySend
		}); got != 0 {
			t.Fatal("queue observer allocated", every, got)
		}
	}
}

func TestBroadcastQueueSamplesReleaseLocksAndPreserveMultiplicity(t *testing.T) {
	h := New("queue-fixture")
	o := &queueObserver{}
	if _, err := h.UseTelemetryObserver(o, 1); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		c := queueFixture(h)
		c.ID = id
		h.clients[id] = c
	}
	if h.BroadcastBinary([]byte{0}) != 2 || o.binary.Load() != 2 {
		t.Fatal("coalesced samples lost multiplicity")
	}
	for _, c := range h.clients {
		for len(c.binarySend) < cap(c.binarySend) {
			c.binarySend <- nil
		}
	}
	if h.BroadcastBinary([]byte{0}) != 0 || o.binary.Load() != 4 || o.drops.Load() != 2 {
		t.Fatal("full queues did not report samples and drops independently")
	}
	for _, c := range h.clients {
		for len(c.binarySend) != 0 {
			<-c.binarySend
		}
	}
	payload := []byte{0}
	if got := testing.AllocsPerRun(1000, func() {
		h.BroadcastBinary(payload)
		for _, c := range h.clients {
			<-c.binarySend
		}
	}); got != 0 {
		t.Fatal("observed broadcast allocated", got)
	}
}
