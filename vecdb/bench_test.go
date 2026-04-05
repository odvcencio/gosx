package vecdb

import (
	"fmt"
	"math/rand"
	"testing"
)

const (
	benchDim  = 384
	benchBits = 2
	benchSeed = int64(12345)
)

func benchIndex(b *testing.B, n int) *Index {
	b.Helper()
	idx := NewWithSeed(benchDim, benchBits, benchSeed)
	rng := rand.New(rand.NewSource(benchSeed))
	ids := make([]string, n)
	vecs := make([][]float32, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("v%d", i)
		vecs[i] = make([]float32, benchDim)
		for j := range vecs[i] {
			vecs[i][j] = rng.Float32()*2 - 1
		}
	}
	idx.AddBatch(ids, vecs)
	return idx
}

func BenchmarkAdd(b *testing.B) {
	idx := NewWithSeed(benchDim, benchBits, benchSeed)
	rng := rand.New(rand.NewSource(benchSeed))
	vecs := make([][]float32, b.N)
	for i := range vecs {
		vecs[i] = make([]float32, benchDim)
		for j := range vecs[i] {
			vecs[i][j] = rng.Float32()*2 - 1
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Add(fmt.Sprintf("v%d", i), vecs[i])
	}
}

func BenchmarkAddBatch1K(b *testing.B) {
	rng := rand.New(rand.NewSource(benchSeed))
	ids := make([]string, 1000)
	vecs := make([][]float32, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%d", i)
		vecs[i] = make([]float32, benchDim)
		for j := range vecs[i] {
			vecs[i][j] = rng.Float32()*2 - 1
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := NewWithSeed(benchDim, benchBits, benchSeed)
		idx.AddBatch(ids, vecs)
	}
}

func BenchmarkRemove(b *testing.B) {
	rng := rand.New(rand.NewSource(benchSeed))
	vecs := make([][]float32, b.N)
	for i := range vecs {
		vecs[i] = make([]float32, benchDim)
		for j := range vecs[i] {
			vecs[i][j] = rng.Float32()*2 - 1
		}
	}
	idx := NewWithSeed(benchDim, benchBits, benchSeed)
	for i := 0; i < b.N; i++ {
		idx.Add(fmt.Sprintf("v%d", i), vecs[i])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Remove(fmt.Sprintf("v%d", i))
	}
}

func benchSearch(b *testing.B, n, k int) {
	idx := benchIndex(b, n)
	rng := rand.New(rand.NewSource(benchSeed + 1))
	query := make([]float32, benchDim)
	for i := range query {
		query[i] = rng.Float32()*2 - 1
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(query, k)
	}
}

func BenchmarkSearch1K(b *testing.B)   { benchSearch(b, 1_000, 10) }
func BenchmarkSearch10K(b *testing.B)  { benchSearch(b, 10_000, 10) }
func BenchmarkSearch100K(b *testing.B) { benchSearch(b, 100_000, 10) }

func BenchmarkSearchParallel(b *testing.B) {
	idx := benchIndex(b, 10_000)
	rng := rand.New(rand.NewSource(benchSeed + 1))
	query := make([]float32, benchDim)
	for i := range query {
		query[i] = rng.Float32()*2 - 1
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx.Search(query, 10)
		}
	})
}
