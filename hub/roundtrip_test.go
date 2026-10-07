package hub

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

type transportObserver struct {
	NoopObserver
	rtt          chan time.Duration
	clients      chan *Client
	disconnects  chan DisconnectEvent
	timeouts     atomic.Int64
	disconnected atomic.Int64
}

func newTransportObserver() *transportObserver {
	return &transportObserver{rtt: make(chan time.Duration, 16), clients: make(chan *Client, 4), disconnects: make(chan DisconnectEvent, 4)}
}
func (o *transportObserver) ClientConnected(_ *Hub, c *Client, _ *http.Request) {
	o.clients <- c
}
func (o *transportObserver) ClientDisconnected(_ *Hub, _ *Client, e DisconnectEvent) {
	o.disconnected.Add(1)
	o.disconnects <- e
}
func (o *transportObserver) RoundTrip(h *Hub, c *Client, d time.Duration) {
	h.ClientCount()
	c.mu.Lock()
	c.mu.Unlock()
	o.rtt <- d
}
func (o *transportObserver) RoundTripTimeout(h *Hub, _ *Client) {
	h.ClientCount()
	o.timeouts.Add(1)
}

func syntheticTransportClient(t *testing.T) (*Client, *telemetrytest.FakeClock, *transportObserver) {
	t.Helper()
	h := New("transport-fixture")
	f := telemetrytest.NewClock(time.Unix(100, 0))
	o := newTransportObserver()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	c := &Client{ID: "fixture", Hub: h, send: make(chan []byte, 256), binarySend: make(chan []byte, 256), transport: &transportState{clock: f}}
	h.clients[c.ID] = c
	h.presence.add(c.ID)
	return c, f, o
}

func TestControlRoundTripMatchingAndMonotonicTime(t *testing.T) {
	c, f, o := syntheticTransportClient(t)
	c.observePong("unsolicited")
	first := c.startPing(f.Now().Monotonic)
	if err := f.Advance(25 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	f.JumpWall(-24 * time.Hour)
	c.observePong("wrong")
	wrong := first
	wrong[7]++
	c.observePong(string(wrong[:]))
	if len(o.rtt) != 0 || o.timeouts.Load() != 0 {
		t.Fatal("unmatched pong changed measurement")
	}
	c.observePong(string(first[:]))
	if got := receiveHubEvent(t, o.rtt); got != 25*time.Millisecond {
		t.Fatal("wall jump affected elapsed RTT", got)
	}
	c.observePong(string(first[:]))
	if len(o.rtt) != 0 {
		t.Fatal("duplicate pong was counted")
	}
	second := c.startPing(f.Now().Monotonic)
	if second == first {
		t.Fatal("sequence was reused")
	}
	c.observePong(string(first[:]))
	if err := f.Advance(readWait); err != nil {
		t.Fatal(err)
	}
	c.observePong(string(second[:]))
	if got := receiveHubEvent(t, o.rtt); got != readWait {
		t.Fatal("60-second boundary was rejected", got)
	}
}

func TestControlRoundTripTimeoutOnce(t *testing.T) {
	c, f, o := syntheticTransportClient(t)
	first := c.startPing(0)
	if err := f.Advance(pingPeriod); err != nil {
		t.Fatal(err)
	}
	second := c.startPing(f.Now().Monotonic)
	if o.timeouts.Load() != 1 {
		t.Fatal("unanswered ping not counted once")
	}
	c.observePong(string(first[:]))
	if err := f.Advance(readWait + time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	c.observePong(string(second[:]))
	c.observePong(string(second[:]))
	c.abandonPing(true)
	if len(o.rtt) != 0 || o.timeouts.Load() != 2 {
		t.Fatal("expired/duplicate measurement changed counters")
	}
	c.startPing(f.Now().Monotonic)
	c.setDisconnectReason("read_timeout", "")
	c.Hub.removeClient(c)
	c.Hub.removeClient(c)
	if o.timeouts.Load() != 3 || o.disconnected.Load() != 1 {
		t.Fatal("read timeout cleanup was not once-only")
	}
}

func waitTransport(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !condition() {
		t.Fatal("transport owner did not reach expected state")
	}
}

func TestControlPingUsesOneExistingTicker(t *testing.T) {
	h := New("transport-fixture")
	h.SlowClient = SlowClientPolicy{DropThreshold: 20, CheckInterval: time.Second}
	f := telemetrytest.NewClock(time.Unix(100, 0))
	h.transportClock = f
	o := newTransportObserver()
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	conn := observedConnection(t, h)
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	client := receiveHubEvent(t, o.clients)
	waitTransport(t, func() bool { return f.PendingTimers() == 1 })
	ping := make(chan string, 2)
	conn.SetPingHandler(func(payload string) error { ping <- payload; return nil })
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	if err := f.Advance(53 * time.Second); err != nil {
		t.Fatal(err)
	}
	// The faster policy timer must not turn into a faster heartbeat.
	time.Sleep(10 * time.Millisecond)
	if len(ping) != 0 {
		t.Fatal("policy check sent an early control ping")
	}
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	payload := receiveHubEvent(t, ping)
	if len(payload) != 8 || f.PendingTimers() != 1 {
		t.Fatal("measured ping or timer ownership changed")
	}
	if err := f.Advance(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteControl(websocket.PongMessage, []byte(payload), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if got := receiveHubEvent(t, o.rtt); got != 20*time.Millisecond {
		t.Fatal(got)
	}
	client.textDropped.Add(20)
	if err := f.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if got := receiveHubEvent(t, o.disconnects); got.Reason != "slow_client" {
		t.Fatal(got)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-readDone
	if f.PendingTimers() != 0 || o.disconnected.Load() != 1 {
		t.Fatal("timer or connection survived pump cleanup")
	}
}
