package vecdb

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"testing"
)

const (
	testDim  = 32
	testBits = 2
	testSeed = 42
)

func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	return v
}

func TestAddAndSearch(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))

	// Insert 10 vectors
	vecs := make([][]float32, 10)
	for i := range vecs {
		vecs[i] = randVec(rng, testDim)
		idx.Add(fmt.Sprintf("v%d", i), vecs[i])
	}

	if idx.Len() != 10 {
		t.Fatalf("Len = %d, want 10", idx.Len())
	}

	// Self-lookup: searching with the same vector should return that ID as top-1
	results := idx.Search(vecs[0], 1)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ID != "v0" {
		t.Errorf("top result = %q, want v0", results[0].ID)
	}
}

func TestAddReplace(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))

	original := randVec(rng, testDim)
	replacement := randVec(rng, testDim)
	idx.Add("a", original)
	idx.Add("a", replacement)

	if idx.Len() != 1 {
		t.Fatalf("Len = %d, want 1 after replace", idx.Len())
	}

	results := idx.Search(replacement, 1)
	if len(results) != 1 || results[0].ID != "a" {
		t.Fatalf("search after replace: got %v", results)
	}
}

func TestRemove(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))

	va := randVec(rng, testDim)
	vb := randVec(rng, testDim)
	vc := randVec(rng, testDim)
	idx.Add("a", va)
	idx.Add("b", vb)
	idx.Add("c", vc)

	if !idx.Remove("b") {
		t.Fatal("Remove(b) returned false")
	}
	if idx.Len() != 2 {
		t.Fatalf("Len = %d, want 2", idx.Len())
	}

	// b should not appear in results
	results := idx.Search(vb, 3)
	for _, r := range results {
		if r.ID == "b" {
			t.Error("removed ID 'b' still in search results")
		}
	}

	// a and c should still be findable
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if !ids["a"] || !ids["c"] {
		t.Errorf("expected a and c in results, got %v", results)
	}

	// Remove nonexistent
	if idx.Remove("zzz") {
		t.Error("Remove(zzz) returned true for nonexistent ID")
	}
	if idx.Len() != 2 {
		t.Fatalf("Len = %d after noop remove, want 2", idx.Len())
	}
}

func TestSearchEmpty(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	results := idx.Search(make([]float32, testDim), 10)
	if len(results) != 0 {
		t.Fatalf("search on empty index returned %d results", len(results))
	}
}

func TestSearchKZero(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	idx.Add("a", make([]float32, testDim))
	results := idx.Search(make([]float32, testDim), 0)
	if len(results) != 0 {
		t.Fatalf("search with k=0 returned %d results", len(results))
	}
}

func TestSearchKGreaterThanLen(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))
	for i := 0; i < 3; i++ {
		idx.Add(fmt.Sprintf("v%d", i), randVec(rng, testDim))
	}
	results := idx.Search(randVec(rng, testDim), 100)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3 (all vectors)", len(results))
	}
	// Should be sorted descending by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Error("results not sorted by descending score")
		}
	}
}

func TestDeterminism(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	vecs := make([][]float32, 20)
	for i := range vecs {
		vecs[i] = randVec(rng, testDim)
	}
	query := randVec(rng, testDim)

	run := func() []SearchResult {
		idx := NewWithSeed(testDim, testBits, testSeed)
		for i, v := range vecs {
			idx.Add(fmt.Sprintf("v%d", i), v)
		}
		return idx.Search(query, 5)
	}

	r1 := run()
	r2 := run()
	if len(r1) != len(r2) {
		t.Fatalf("different result counts: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID || r1[i].Score != r2[i].Score {
			t.Fatalf("result %d differs: %v vs %v", i, r1[i], r2[i])
		}
	}
}

func TestAddBatch(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))

	ids := make([]string, 50)
	vecs := make([][]float32, 50)
	for i := range ids {
		ids[i] = fmt.Sprintf("v%d", i)
		vecs[i] = randVec(rng, testDim)
	}
	idx.AddBatch(ids, vecs)

	if idx.Len() != 50 {
		t.Fatalf("Len = %d, want 50", idx.Len())
	}
}

// TestRecall verifies quantized search quality against exact brute-force.
func TestRecall(t *testing.T) {
	dim := 64
	n := 200
	k := 10
	bitWidth := 4
	rng := rand.New(rand.NewSource(123))

	idx := NewWithSeed(dim, bitWidth, 77)
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = randVec(rng, dim)
		idx.Add(fmt.Sprintf("v%d", i), vecs[i])
	}

	// Run 100 queries, measure recall@k
	nQueries := 100
	totalRecall := 0.0
	for q := 0; q < nQueries; q++ {
		query := randVec(rng, dim)

		// Exact brute-force top-k
		type scored struct {
			id    string
			score float64
		}
		exact := make([]scored, n)
		for i, v := range vecs {
			var dot float64
			for j := range v {
				dot += float64(v[j]) * float64(query[j])
			}
			exact[i] = scored{fmt.Sprintf("v%d", i), dot}
		}
		sort.Slice(exact, func(a, b int) bool { return exact[a].score > exact[b].score })

		trueTopK := map[string]bool{}
		for i := 0; i < k; i++ {
			trueTopK[exact[i].id] = true
		}

		results := idx.Search(query, k)
		hits := 0
		for _, r := range results {
			if trueTopK[r.ID] {
				hits++
			}
		}
		totalRecall += float64(hits) / float64(k)
	}
	avgRecall := totalRecall / float64(nQueries)
	if avgRecall < 0.70 {
		t.Errorf("Recall@%d = %.2f, want >= 0.70", k, avgRecall)
	}
	t.Logf("Recall@%d = %.3f (n=%d, dim=%d, %d-bit)", k, avgRecall, n, dim, bitWidth)
}

func TestConcurrentAddSearch(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(99))

	// Pre-generate vectors to avoid rng contention
	const nWorkers = 8
	const opsPerWorker = 200
	workerVecs := make([][][]float32, nWorkers)
	for w := 0; w < nWorkers; w++ {
		workerVecs[w] = make([][]float32, opsPerWorker)
		for i := range workerVecs[w] {
			workerVecs[w][i] = randVec(rng, testDim)
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < nWorkers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				id := fmt.Sprintf("w%d-v%d", worker, i)
				idx.Add(id, workerVecs[worker][i])
				// Interleave searches
				if i%5 == 0 {
					idx.Search(workerVecs[worker][i], 3)
				}
			}
		}(w)
	}
	wg.Wait()

	if idx.Len() != nWorkers*opsPerWorker {
		t.Errorf("Len = %d, want %d", idx.Len(), nWorkers*opsPerWorker)
	}
}

func TestConcurrentRemove(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	rng := rand.New(rand.NewSource(42))

	// Add 500 vectors
	for i := 0; i < 500; i++ {
		idx.Add(fmt.Sprintf("v%d", i), randVec(rng, testDim))
	}

	// Pre-generate query vectors for concurrent searches
	queryVecs := make([][]float32, 50)
	for i := range queryVecs {
		queryVecs[i] = randVec(rand.New(rand.NewSource(int64(i))), testDim)
	}

	// Remove half concurrently while searching
	var wg sync.WaitGroup
	for i := 0; i < 250; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			idx.Remove(fmt.Sprintf("v%d", n*2))
		}(i)
	}
	// Concurrent searches
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			idx.Search(queryVecs[n], 5)
		}(i)
	}
	wg.Wait()

	if idx.Len() != 250 {
		t.Errorf("Len = %d, want 250", idx.Len())
	}
}

func TestZeroVector(t *testing.T) {
	idx := NewWithSeed(testDim, testBits, testSeed)
	zero := make([]float32, testDim)
	idx.Add("zero", zero)
	results := idx.Search(zero, 1)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ID != "zero" {
		t.Errorf("ID = %q, want zero", results[0].ID)
	}
	// Score should be approximately zero
	if math.Abs(float64(results[0].Score)) > 1.0 {
		t.Errorf("zero-vector score = %f, expected near 0", results[0].Score)
	}
}
