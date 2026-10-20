package telemetry

import (
	"testing"
	"time"

	"m31labs.dev/gosx/internal/clock"
)

func BenchmarkLoopObserve(b *testing.B) {
	l := portableLoop(b)
	info := TickInfo{StateBytes: 4096, Inputs: 4, Lag: time.Millisecond}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.Observe(time.Millisecond, info)
	}
}

func BenchmarkLoopBeginEnd(b *testing.B) {
	l := portableLoop(b)
	l.kind.owner.opts.Clock = clock.New()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.End(l.Begin(), TickInfo{})
	}
}

func BenchmarkLoopHealth(b *testing.B) {
	l := portableLoop(b)
	for i := range 1000 {
		l.Observe(time.Duration(i)*time.Microsecond, TickInfo{})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.Health()
	}
}

func BenchmarkLoopHealthOverflow(b *testing.B) {
	l := portableLoop(b)
	for i := range 1000 {
		l.Observe(time.Duration(i)*100*time.Microsecond, TickInfo{})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.Health()
	}
}

func BenchmarkLoopNil(b *testing.B) {
	var l *Loop
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		l.End(l.Begin(), TickInfo{})
	}
}
