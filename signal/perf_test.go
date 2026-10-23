package signal

import (
	"fmt"
	"testing"
)

func BenchmarkDeriveRecompute(b *testing.B) {
	base := New(0)
	c := Derive(func() int { return base.Get() * 2 })
	defer c.Stop()
	c.Subscribe(func() {})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		base.Set(i + 1)
		_ = c.Get()
	}
}

func BenchmarkDeriveFiveDeps(b *testing.B) {
	deps := make([]*Signal[int], 5)
	for i := range deps {
		deps[i] = New(i)
	}
	c := Derive(func() int {
		sum := 0
		for _, d := range deps {
			sum += d.Get()
		}
		return sum
	})
	defer c.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deps[0].Set(i + 1)
		_ = c.Get()
	}
}

func BenchmarkBatchTen(b *testing.B) {
	s := New(0)
	s.Subscribe(func() {})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Batch(func() {
			for j := 0; j < 10; j++ {
				s.Set(i*10 + j)
			}
		})
	}
}

func BenchmarkDeriveDependencyChanges(b *testing.B) {
	for _, kind := range []string{"switch-source", "reorder", "grow-shrink"} {
		b.Run(kind, func(b *testing.B) {
			route := New(0)
			sources := []*Signal[int]{New(1), New(2), New(3), New(4), New(5)}
			computed := Derive(func() int {
				odd := route.Get()%2 != 0
				switch kind {
				case "switch-source":
					if odd {
						return sources[0].Get()
					}
					return sources[1].Get()
				case "reorder":
					if odd {
						return sources[0].Get() + sources[1].Get()
					}
					return sources[1].Get() + sources[0].Get()
				default:
					sum := sources[0].Get()
					if odd {
						for _, source := range sources[1:] {
							sum += source.Get()
						}
					}
					return sum
				}
			})
			defer computed.Stop()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				route.Set(i + 1)
				_ = computed.Get()
			}
		})
	}
}

func BenchmarkDeriveLifecycle(b *testing.B) {
	for _, count := range []int{1, 5, 16} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			sources := make([]*Signal[int], count)
			for i := range sources {
				sources[i] = New(i)
			}
			compute := func() int {
				sum := 0
				for _, source := range sources {
					sum += source.Get()
				}
				return sum
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				computed := Derive(compute)
				if got := computed.Get(); got != count*(count-1)/2 {
					b.Fatalf("computed = %d", got)
				}
				computed.Stop()
			}
		})
	}
}
