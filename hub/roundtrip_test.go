package hub

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

func TestControlPingKeepsPhaseWhenHandlingDelayShrinks(t *testing.T) {
	s := &transportState{nextPing: pingPeriod}
	if !s.pingDue(pingPeriod+200*time.Millisecond) || s.nextPing != 2*pingPeriod {
		t.Fatal("first delayed tick lost its phase", s.nextPing)
	}
	if !s.pingDue(2*pingPeriod+50*time.Millisecond) || s.nextPing != 3*pingPeriod {
		t.Fatal("a less delayed tick skipped the next ping", s.nextPing)
	}
	if s.pingDue(2*pingPeriod + time.Second) {
		t.Fatal("same deadline sent twice")
	}
}

func TestControlPingMaximumScheduledGap(t *testing.T) {
	for _, seconds := range []int{1, 5, 10, 30, 40, 50, 60} {
		t.Run((time.Duration(seconds) * time.Second).String(), func(t *testing.T) {
			s := &transportState{slow: SlowClientPolicy{DropThreshold: 1, CheckInterval: time.Duration(seconds) * time.Second}, nextPing: pingPeriod}
			interval := s.interval()
			if interval > s.slow.CheckInterval || interval > pingPeriod {
				t.Fatal("timer exceeds its check or ping period", interval)
			}
			last, largest := time.Duration(0), time.Duration(0)
			pings := 0
			for tick := 1; tick <= 600; tick++ {
				now := time.Duration(tick) * interval
				if !s.pingDue(now) {
					continue
				}
				gap := now - last
				if gap > largest {
					largest = gap
				}
				if gap > pingPeriod {
					t.Fatalf("ping gap %s exceeds %s at tick %d", gap, pingPeriod, tick)
				}
				if s.nextPing%pingPeriod != 0 {
					t.Fatal("deadline drifted from its original schedule")
				}
				last = now
				pings++
			}
			if pings < 10 {
				t.Fatal("cadence test exercised too few pings", pings)
			}
			t.Logf("check=%ds tick=%s largest_ping_gap=%s", seconds, interval, largest)
		})
	}
}

func TestControlPingCoalescesMissedDeadlines(t *testing.T) {
	s := &transportState{nextPing: pingPeriod}
	if !s.pingDue(3*pingPeriod+time.Second) || s.nextPing != 4*pingPeriod {
		t.Fatal("missed deadlines changed phase", s.nextPing)
	}
	if s.pingDue(3*pingPeriod + time.Second) {
		t.Fatal("coalesced ticks caused a catch-up burst")
	}
}

func TestControlPingSequenceUsesConnectionEntropy(t *testing.T) {
	seed := []byte{0x41, 0x28, 0x96, 0x37, 0x02, 0x55, 0x68, 0x19}
	sequence, err := readPingSequence(bytes.NewReader(seed))
	if err != nil || sequence != binary.BigEndian.Uint64(seed) {
		t.Fatal("entropy was not used", sequence, err)
	}
	if _, err := readPingSequence(bytes.NewReader(seed[:7])); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("short entropy accepted", err)
	}
	h := New("entropy-fixture")
	if _, err := h.UseObserver(NoopObserver{}); err != nil {
		t.Fatal(err)
	}
	first, err := h.transport(SlowClientPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.transport(SlowClientPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if first.sequence == second.sequence {
		t.Fatal("connections reused a ping seed")
	}
	c := &Client{Hub: h, transport: &transportState{sequence: sequence}}
	ping := c.startPing(0)
	if binary.BigEndian.Uint64(ping[:]) != sequence+1 {
		t.Fatal("first ping discarded its randomized seed")
	}
}

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
