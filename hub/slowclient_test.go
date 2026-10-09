package hub

import (
	"bytes"
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/telemetrytest"
)

func TestSlowClientPolicyValidation(t *testing.T) {
	for _, p := range []SlowClientPolicy{{}, {DropThreshold: 20}, {DropThreshold: 1, CheckInterval: 100 * time.Millisecond}, {DropThreshold: 1, CheckInterval: time.Minute}} {
		if err := p.Validate(); err != nil {
			t.Fatal(p, err)
		}
	}
	p, err := (SlowClientPolicy{DropThreshold: 20}).normalized()
	if err != nil || p.CheckInterval != time.Second || (&transportState{slow: p}).interval() != time.Second {
		t.Fatal("default policy interval", p, err)
	}
	p.CheckInterval = time.Minute
	if (&transportState{slow: p}).interval() != pingPeriod {
		t.Fatal("policy replaced the control-ping interval")
	}
	for _, p := range []SlowClientPolicy{{DropThreshold: -1}, {CheckInterval: -1}, {DropThreshold: 1, CheckInterval: 99 * time.Millisecond}, {DropThreshold: 1, CheckInterval: time.Minute + 1}} {
		if !errors.Is(p.Validate(), ErrInvalidSlowClient) {
			t.Fatal("invalid policy accepted", p)
		}
		h := New("transport-fixture")
		h.SlowClient = p
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 503 || h.served || h.pendingClients != 0 {
			t.Fatal("invalid policy admitted an upgrade")
		}
	}
}

func TestInvalidSlowClientWarningIsFixedAndRateLimited(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	h := New("private-policy-canary")
	h.SlowClient = SlowClientPolicy{DropThreshold: -1}
	for range 20 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/private-path-canary", nil))
		if w.Code != 503 {
			t.Fatal("invalid policy admitted an upgrade")
		}
	}
	if strings.Count(logs.String(), "class=invalid_slow_client") != 1 || strings.Contains(logs.String(), "canary") {
		t.Fatal("policy log leaked state or repeated", logs.String())
	}
	logs.Reset()
	start := time.Now()
	h.warnInvalidSlowClient(start.Add(59 * time.Second))
	h.warnInvalidSlowClient(start.Add(time.Minute))
	h.warnInvalidSlowClient(start.Add(time.Minute))
	if strings.Count(logs.String(), "class=invalid_slow_client") != 1 {
		t.Fatal("warning did not honor its one-minute window", logs.String())
	}
}

func TestSlowClientEvictionConcurrentWithBroadcastDisconnectAndClose(t *testing.T) {
	for iteration := range 24 {
		t.Run(string(rune('a'+iteration)), func(t *testing.T) {
			h := New("eviction-fixture")
			h.SlowClient = SlowClientPolicy{DropThreshold: 1, CheckInterval: 100 * time.Millisecond}
			clock := telemetrytest.NewClock(time.Unix(100, 0))
			h.transportClock = clock
			o := newTransportObserver()
			if _, err := h.UseObserver(o); err != nil {
				t.Fatal(err)
			}
			conn := observedConnection(t, h)
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			client := receiveHubEvent(t, o.clients)
			waitTransport(t, func() bool { return clock.PendingTimers() == 1 })
			client.textDropped.Add(1)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Go(func() {
				<-start
				for range 300 {
					h.BroadcastBinary([]byte{0})
					runtime.Gosched()
				}
			})
			wg.Go(func() {
				<-start
				for range 8 {
					h.Disconnect(client.ID, "server")
					runtime.Gosched()
				}
			})
			wg.Go(func() {
				<-start
				if err := clock.Advance(100 * time.Millisecond); err != nil {
					t.Error(err)
				}
			})
			wg.Go(func() {
				<-start
				runtime.Gosched()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := h.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			close(start)
			wg.Wait()
			if err := h.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if o.disconnected.Load() != 1 || h.ClientCount() != 0 || clock.PendingTimers() != 0 {
				t.Fatal("connection cleanup or timer removal was not once-only")
			}
			event := receiveHubEvent(t, o.disconnects)
			if event.Reason == "panic" {
				t.Fatal("concurrent cleanup panicked")
			}
			client.mu.Lock()
			closed := client.closed
			client.mu.Unlock()
			if !closed {
				t.Fatal("client queues remained open")
			}
			for range client.send {
			}
			for range client.binarySend {
			}
		})
	}
}

func TestSlowClientUsesIntervalDropDelta(t *testing.T) {
	c, _, _ := syntheticTransportClient(t)
	c.transport.slow = SlowClientPolicy{DropThreshold: 20, CheckInterval: time.Second}
	c.textDropped.Add(10)
	if c.slowClient(500 * time.Millisecond) {
		t.Fatal("early policy check")
	}
	c.binaryDropped.Add(9)
	if c.slowClient(time.Second) {
		t.Fatal("subthreshold policy evicted")
	}
	if c.slowClient(2 * time.Second) {
		t.Fatal("lifetime total was reused")
	}
	c.binaryDropped.Add(20)
	if !c.slowClient(3 * time.Second) {
		t.Fatal("threshold delta was not evicted")
	}
	if c.slowClient(4 * time.Second) {
		t.Fatal("old drops were counted again")
	}
	c.transport.lastDrops = DropStats{}
	c.textDropped.Store(^uint64(0))
	c.binaryDropped.Store(^uint64(0))
	if !c.slowClient(5 * time.Second) {
		t.Fatal("drop arithmetic overflow bypassed eviction")
	}
}

// simulateSlowClientChecks drives slowClient from shared ticks, each delayed
// by a small deterministic handling time. It returns the check windows, the
// time of the first eviction (or -1), and the number of ticks run.
func simulateSlowClientChecks(t *testing.T, checkInterval time.Duration, dropFactor float64, total time.Duration) (windows []time.Duration, evictedAt time.Duration) {
	t.Helper()
	const threshold = 1000
	c, _, _ := syntheticTransportClient(t)
	c.transport.slow = SlowClientPolicy{DropThreshold: threshold, CheckInterval: checkInterval}
	interval := c.transport.interval()
	rng := rand.New(rand.NewPCG(uint64(checkInterval), 7))
	perSecond := dropFactor * threshold / checkInterval.Seconds()
	var added uint64
	last := time.Duration(0)
	for k := time.Duration(1); k*interval <= total; k++ {
		now := k*interval + 100*time.Microsecond + time.Duration(rng.Int64N(int64(4900*time.Microsecond)))
		want := uint64(perSecond * (k * interval).Seconds())
		c.textDropped.Add(want - added)
		added = want
		before := c.transport.lastCheck
		evict := c.slowClient(now)
		if c.transport.lastCheck != before {
			windows = append(windows, now-last)
			last = now
		}
		if evict {
			return windows, now
		}
	}
	return windows, -1
}

func TestSlowClientChecksFollowConfiguredInterval(t *testing.T) {
	for _, seconds := range []int{1, 5, 7, 10, 20, 30, 40, 50, 60} {
		checkInterval := time.Duration(seconds) * time.Second
		t.Run(checkInterval.String(), func(t *testing.T) {
			interval := (&transportState{slow: SlowClientPolicy{DropThreshold: 1, CheckInterval: checkInterval}}).interval()
			low, high := checkInterval-interval/2, checkInterval+interval/2

			windows, evictedAt := simulateSlowClientChecks(t, checkInterval, 0.6, time.Hour)
			if evictedAt >= 0 {
				t.Fatalf("client at 0.6x the threshold rate evicted at %v", evictedAt)
			}
			if len(windows) == 0 {
				t.Fatal("no checks ran")
			}
			for i, w := range windows {
				if w < low || w > high {
					t.Fatalf("window %d = %v, want within [%v, %v]", i, w, low, high)
				}
			}

			windows, evictedAt = simulateSlowClientChecks(t, checkInterval, 1.5, time.Hour)
			if evictedAt < 0 || evictedAt > 2*high {
				t.Fatalf("client at 1.5x the threshold rate evicted at %v, want within %v", evictedAt, 2*high)
			}
			for i, w := range windows {
				if w < low || w > high {
					t.Fatalf("window %d = %v, want within [%v, %v]", i, w, low, high)
				}
			}
		})
	}
}
