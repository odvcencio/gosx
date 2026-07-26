package vecdb

import (
	"fmt"
	"math/rand"
	"testing"
)

// randomVectors builds n deterministic vectors of the given dimension.
func randomVectors(n, dim int, seed int64) ([]string, [][]float32) {
	rng := rand.New(rand.NewSource(seed))
	ids := make([]string, n)
	vecs := make([][]float32, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("v%d", i)
		vecs[i] = make([]float32, dim)
		for j := range vecs[i] {
			vecs[i][j] = rng.Float32()*2 - 1
		}
	}
	return ids, vecs
}

// BenchmarkPrepareQueryOnly measures the fixed cost every Search pays before it
// looks at a single entry. It answers whether a smaller candidate set could help.
func BenchmarkPrepareQueryOnly(b *testing.B) {
	for _, cfg := range []struct {
		dim  int
		bits int
	}{
		{128, 3},
		{384, 2},
	} {
		b.Run(fmt.Sprintf("dim%d/bits%d", cfg.dim, cfg.bits), func(b *testing.B) {
			idx := NewWithSeed(cfg.dim, cfg.bits, benchSeed)
			_, vecs := randomVectors(1, cfg.dim, benchSeed+3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = idx.quantizer.PrepareQuery(vecs[0])
			}
		})
	}
}

// BenchmarkScanOnly measures the per-entry scan with the query already prepared.
// The gap against BenchmarkSearch is the fixed preparation cost.
func BenchmarkScanOnly(b *testing.B) {
	for _, n := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			idx := NewWithSeed(benchDim, benchBits, benchSeed)
			ids, vecs := randomVectors(n, benchDim, benchSeed)
			idx.AddBatch(ids, vecs)
			_, queries := randomVectors(1, benchDim, benchSeed+1)
			pq := idx.quantizer.PrepareQuery(queries[0])
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var best float32
				for j := range idx.entries {
					score := idx.quantizer.InnerProductPrepared(idx.entries[j].qv, pq)
					if score > best {
						best = score
					}
				}
				_ = best
			}
		})
	}
}

// BenchmarkSearchSmallCorpus measures Search at the corpus sizes GoSX targets:
// route tables and per-site content indexes, which hold thousands of entries.
func BenchmarkSearchSmallCorpus(b *testing.B) {
	for _, n := range []int{100, 500, 1_000, 5_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			idx := NewWithSeed(128, 3, benchSeed)
			ids, vecs := randomVectors(n, 128, benchSeed)
			idx.AddBatch(ids, vecs)
			_, queries := randomVectors(1, 128, benchSeed+1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx.Search(queries[0], 10)
			}
		})
	}
}
