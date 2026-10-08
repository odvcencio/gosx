package hub

import (
	"runtime"
	"strconv"
	"testing"
	"time"
)

func BenchmarkHubObserverDispatch(b *testing.B) {
	for _, name := range []string{"disabled", "enabled"} {
		b.Run(name, func(b *testing.B) {
			h := New("synthetic-room")
			if name == "enabled" {
				if _, err := h.UseObserver(NoopObserver{}); err != nil {
					b.Fatal(err)
				}
			}
			e := TrafficEvent{Direction: Inbound, Bytes: 1024, QueueDepth: -1}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.observeMessage(nil, e)
			}
		})
	}
}

func BenchmarkHubObservedEnqueue(b *testing.B) {
	for _, observed := range []bool{false, true} {
		b.Run(strconv.FormatBool(observed), func(b *testing.B) {
			h := New("queue-fixture")
			c := queueFixture(h)
			if observed {
				if _, err := h.UseTelemetryObserver(&queueObserver{}, 0); err != nil {
					b.Fatal(err)
				}
			} else {
				c.transport = nil
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				c.trySend(nil)
				<-c.send
			}
		})
	}
}

func BenchmarkHubObservedBroadcast256(b *testing.B) {
	for _, observed := range []bool{false, true} {
		b.Run(strconv.FormatBool(observed), func(b *testing.B) {
			h := New("queue-fixture")
			clients := make([]*Client, 256)
			for i := range clients {
				c := queueFixture(h)
				c.ID = strconv.Itoa(i)
				if !observed {
					c.transport = nil
				}
				clients[i], h.clients[c.ID] = c, c
			}
			if observed {
				if _, err := h.UseTelemetryObserver(&queueObserver{}, 0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			payload := []byte{0, 1}
			b.ResetTimer()
			for range b.N {
				h.BroadcastBinary(payload)
				for _, c := range clients {
					<-c.binarySend
				}
			}
		})
	}
}

// Timings are reported; zero added allocation remains a separate hard gate.
func TestHubTransportTimingPairs(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, recipients := range []int{1, 256} {
		baseline, head := New("pair-fixture"), New("pair-fixture")
		baseClients, headClients := make([]*Client, recipients), make([]*Client, recipients)
		for i := range recipients {
			baseClients[i], headClients[i] = queueFixture(baseline), queueFixture(head)
			baseClients[i].transport = nil
			id := strconv.Itoa(i)
			baseline.clients[id], head.clients[id] = baseClients[i], headClients[i]
		}
		if _, err := head.UseTelemetryObserver(NoopObserver{}, 0); err != nil {
			t.Fatal(err)
		}
		payload := []byte{0, 1}
		const iterations = 2000
		measure := func(h *Hub, clients []*Client) float64 {
			started := time.Now()
			for range iterations {
				if recipients == 1 {
					clients[0].tryBinarySend(payload)
				} else {
					h.BroadcastBinary(payload)
				}
				for _, c := range clients {
					<-c.binarySend
				}
			}
			return float64(time.Since(started).Nanoseconds()) / iterations
		}
		for pair := range 10 {
			runtime.GC()
			var a, b float64
			if pair%2 == 0 {
				a, b = measure(baseline, baseClients), measure(head, headClients)
			} else {
				b, a = measure(head, headClients), measure(baseline, baseClients)
			}
			t.Logf("recipients=%d pair=%d baseline_ns=%.3f observed_ns=%.3f delta_ns=%.3f", recipients, pair, a, b, b-a)
		}
	}
}

func BenchmarkHubObserverDispatchParallel(b *testing.B) {
	for _, name := range []string{"disabled", "enabled"} {
		b.Run(name, func(b *testing.B) {
			h := New("synthetic-room")
			if name == "enabled" {
				if _, err := h.UseObserver(NoopObserver{}); err != nil {
					b.Fatal(err)
				}
			}
			e := TrafficEvent{Direction: Inbound, Bytes: 1024, QueueDepth: -1}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					h.observeMessage(nil, e)
				}
			})
		})
	}
}
