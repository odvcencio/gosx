package signal

import "testing"

func BenchmarkDeriveRecompute(b *testing.B) {
	base := New(0)
	c := Derive(func() int { return base.Get() * 2 })
	defer c.Stop()
	c.Subscribe(func() {})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		base.Set(i)
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
		deps[0].Set(i)
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
