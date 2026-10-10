//go:build !js || !wasm

package telemetry

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hub"
)

func hubWireFixture(tb testing.TB, observed bool, recipients int) (*hub.Hub, []*websocket.Conn) {
	tb.Helper()
	h := hub.New("pair-fixture")
	if observed {
		tel, _ := hubTelemetry(tb)
		g, err := tel.NewHubGroup("room", HubOptions{})
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := g.Attach(h); err != nil {
			tb.Fatal(err)
		}
	}
	srv := httptest.NewServer(h)
	connections := make([]*websocket.Conn, 0, recipients)
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.Close(ctx)
		for _, c := range connections {
			c.Close()
		}
		srv.Close()
	})
	for range recipients {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			tb.Fatal(err)
		}
		connections = append(connections, c)
		c.SetReadDeadline(time.Now().Add(5 * time.Minute))
		if _, _, err := c.ReadMessage(); err != nil {
			tb.Fatal(err)
		}
	}
	return h, connections
}

func hubReadBatch(tb testing.TB, connections []*websocket.Conn) {
	tb.Helper()
	for _, c := range connections {
		if _, _, err := c.ReadMessage(); err != nil {
			tb.Fatal(err)
		}
	}
}

func BenchmarkHubAccountingMessage(b *testing.B) {
	tel, _ := hubTelemetry(b)
	g, _ := tel.NewHubGroup("room", HubOptions{})
	h := hub.New("fixture")
	g.Attach(h)
	a := tel.hubs.attached[h]
	e := hub.TrafficEvent{Direction: hub.Outbound, Bytes: 128, Written: true, QueueDepth: -1}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		a.Message(h, nil, e)
	}
}

// Real fanout and the real group subscriber are measured. Socket draining
// stays outside the timed enqueue phase; successful-write accounting runs at
// its separate pump owner and is also measured by BenchmarkHubAccountingMessage.
func BenchmarkHubGroupBroadcast256(b *testing.B) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	for _, observed := range []bool{false, true} {
		name := "baseline"
		if observed {
			name = "telemetry"
		}
		b.Run(name, func(b *testing.B) {
			h, connections := hubWireFixture(b, observed, 256)
			payload := []byte{1, 2, 3, 4}
			b.ReportAllocs()
			b.ResetTimer()
			var elapsed time.Duration
			for range b.N {
				start := time.Now()
				if h.BroadcastBinary(payload) != 256 {
					b.Fatal("fanout dropped a recipient")
				}
				elapsed += time.Since(start)
				hubReadBatch(b, connections)
			}
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "fanout-ns/op")
		})
	}
}

func TestHubGroupTimingPairs(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	for _, recipients := range []int{1, 256} {
		base, baseConnections := hubWireFixture(t, false, recipients)
		head, headConnections := hubWireFixture(t, true, recipients)
		payload := []byte{1, 2, 3, 4}
		measure := func(h *hub.Hub, connections []*websocket.Conn) float64 {
			var elapsed time.Duration
			const iterations = 100
			for range iterations {
				start := time.Now()
				n := h.BroadcastBinary(payload)
				elapsed += time.Since(start)
				if n != recipients {
					t.Fatal("paired fanout dropped a recipient")
				}
				hubReadBatch(t, connections)
			}
			return float64(elapsed.Nanoseconds()) / iterations
		}
		for pair := range 10 {
			runtime.GC()
			var a, b float64
			if pair%2 == 0 {
				a, b = measure(base, baseConnections), measure(head, headConnections)
			} else {
				b, a = measure(head, headConnections), measure(base, baseConnections)
			}
			t.Logf("recipients=%d pair=%d baseline_ns=%.3f telemetry_ns=%.3f delta_ns=%.3f", recipients, pair, a, b, b-a)
		}
	}
}

func TestHubGroupActualFanoutAllocationDifference(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	for _, recipients := range []int{1, 256} {
		var allocations [2]float64
		for index, observed := range []bool{false, true} {
			h, connections := hubWireFixture(t, observed, recipients)
			payload := []byte{1, 2, 3, 4}
			runtime.Gosched()
			allocations[index] = testing.AllocsPerRun(100, func() {
				if h.BroadcastBinary(payload) != recipients {
					t.Fatal("allocation fixture lost a recipient")
				}
			})
			for range 101 {
				hubReadBatch(t, connections)
			} // warm call plus measured calls
		}
		if allocations[1] > allocations[0] {
			t.Fatalf("recipients=%d added allocations: baseline=%g telemetry=%g", recipients, allocations[0], allocations[1])
		}
		t.Logf("recipients=%d baseline=%g telemetry=%g allocations", recipients, allocations[0], allocations[1])
	}
}
