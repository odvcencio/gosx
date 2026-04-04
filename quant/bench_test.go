package quant

import (
	"math"
	"math/rand"
	"sync"
	"testing"
)

func BenchmarkQuantize384_2bit(b *testing.B) {
	q := NewWithSeed(384, 2, 42)
	x := randomUnitVector(384, rand.New(rand.NewSource(99)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Quantize(x)
	}
}

func BenchmarkDequantize384_2bit(b *testing.B) {
	q := NewWithSeed(384, 2, 42)
	x := randomUnitVector(384, rand.New(rand.NewSource(99)))
	packed, _ := q.Quantize(x)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Dequantize(packed)
	}
}

func BenchmarkMSEInnerProduct384_2bit(b *testing.B) {
	q := NewWithSeed(384, 2, 42)
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(384, rng)
	y := randomUnitVector(384, rng)
	packed, norm := q.Quantize(x)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.InnerProduct(packed, norm, y)
	}
}

func BenchmarkIPQuantize384_3bit(b *testing.B) {
	q := NewIPWithSeed(384, 3, 42)
	x := randomUnitVector(384, rand.New(rand.NewSource(99)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Quantize(x)
	}
}

func BenchmarkIPInnerProduct384_3bit(b *testing.B) {
	q := NewIPWithSeed(384, 3, 42)
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(384, rng)
	y := randomUnitVector(384, rng)
	qx := q.Quantize(x)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.InnerProduct(qx, y)
	}
}

func BenchmarkIPPreparedQuery384_3bit(b *testing.B) {
	q := NewIPWithSeed(384, 3, 42)
	rng := rand.New(rand.NewSource(99))
	x := randomUnitVector(384, rng)
	y := randomUnitVector(384, rng)
	qx := q.Quantize(x)
	pq := q.PrepareQuery(y)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.InnerProductPrepared(qx, pq)
	}
}

func TestQuantizerConcurrency(t *testing.T) {
	q := NewWithSeed(128, 2, 42)
	rng := rand.New(rand.NewSource(99))
	vecs := make([][]float32, 100)
	for i := range vecs {
		vecs[i] = randomUnitVector(128, rng)
	}
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				packed, norm := q.Quantize(vecs[offset+i])
				recon := q.Dequantize(packed)
				if len(recon) != 128 {
					t.Errorf("bad recon len %d", len(recon))
				}
				_ = norm
			}
		}(g * 10)
	}
	wg.Wait()
}

func TestIPQuantizerConcurrency(t *testing.T) {
	q := NewIPWithSeed(128, 3, 42)
	rng := rand.New(rand.NewSource(99))
	vecs := make([][]float32, 100)
	for i := range vecs {
		vecs[i] = randomUnitVector(128, rng)
	}
	y := randomUnitVector(128, rng)
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				qx := q.Quantize(vecs[offset+i])
				_ = q.InnerProduct(qx, y)
			}
		}(g * 10)
	}
	wg.Wait()
}

func TestDequantizedUnitNorm(t *testing.T) {
	q := NewWithSeed(384, 2, 42)
	rng := rand.New(rand.NewSource(99))
	for trial := 0; trial < 100; trial++ {
		x := randomUnitVector(384, rng)
		packed, _ := q.Quantize(x)
		recon := q.Dequantize(packed)
		var normSq float64
		for _, v := range recon {
			normSq += float64(v) * float64(v)
		}
		norm := math.Sqrt(normSq)
		if math.Abs(norm-1.0) > 0.2 {
			t.Errorf("trial %d: dequantized norm %.4f, want ≈ 1.0", trial, norm)
		}
	}
}
