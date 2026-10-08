package hub

import "testing"

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
