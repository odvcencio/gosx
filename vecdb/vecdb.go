package vecdb

import (
	"container/heap"
	"sort"
	"sync"

	"github.com/odvcencio/turboquant"
)

// SearchResult holds one result from a similarity search.
type SearchResult struct {
	ID    string
	Score float32
}

type entry struct {
	id string
	qv turboquant.IPQuantized
}

// Index is an in-memory vector index backed by TurboQuant IP quantization.
// Safe for concurrent use: multiple goroutines may call Search concurrently,
// and Add/Remove are serialized with respect to each other and to Search.
type Index struct {
	mu        sync.RWMutex
	quantizer *turboquant.IPQuantizer
	entries   []entry
	idIndex   map[string]int
}

// New creates a vector index for the given dimension and bit-width.
// dim is the vector dimension (must be >= 2).
// bitWidth is bits per coordinate for quantization (must be >= 2, typically 2-4).
// Uses a random seed for the underlying quantizer.
func New(dim, bitWidth int) *Index {
	return &Index{
		quantizer: turboquant.NewIP(dim, bitWidth),
		entries:   make([]entry, 0),
		idIndex:   make(map[string]int),
	}
}

// NewWithSeed creates a deterministic vector index.
// Two indices with the same dim, bitWidth, and seed will produce identical
// quantizations and search results for the same inputs.
func NewWithSeed(dim, bitWidth int, seed int64) *Index {
	return &Index{
		quantizer: turboquant.NewIPWithSeed(dim, bitWidth, seed),
		entries:   make([]entry, 0),
		idIndex:   make(map[string]int),
	}
}

// Add inserts a vector with the given string identifier.
// If a vector with the same ID already exists, it is replaced.
// The vector is quantized immediately on insertion.
// vec must have length equal to dim.
func (idx *Index) Add(id string, vec []float32) {
	qv := idx.quantizer.Quantize(vec)
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if pos, ok := idx.idIndex[id]; ok {
		idx.entries[pos].qv = qv
		return
	}
	idx.idIndex[id] = len(idx.entries)
	idx.entries = append(idx.entries, entry{id: id, qv: qv})
}

// AddBatch inserts multiple vectors. Equivalent to calling Add for each
// pair but acquires the write lock only once. Quantization is performed
// outside the lock for better concurrency.
// ids and vecs must have the same length. Each vecs[i] must have length equal to dim.
func (idx *Index) AddBatch(ids []string, vecs [][]float32) {
	qvs := make([]turboquant.IPQuantized, len(ids))
	for i := range ids {
		qvs[i] = idx.quantizer.Quantize(vecs[i])
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for i, id := range ids {
		if pos, ok := idx.idIndex[id]; ok {
			idx.entries[pos].qv = qvs[i]
			continue
		}
		idx.idIndex[id] = len(idx.entries)
		idx.entries = append(idx.entries, entry{id: id, qv: qvs[i]})
	}
}

// Remove deletes the vector with the given ID using swap-and-pop.
// Returns true if the vector existed and was removed, false if the ID was not found.
func (idx *Index) Remove(id string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	pos, ok := idx.idIndex[id]
	if !ok {
		return false
	}
	last := len(idx.entries) - 1
	if pos != last {
		idx.entries[pos] = idx.entries[last]
		idx.idIndex[idx.entries[pos].id] = pos
	}
	idx.entries = idx.entries[:last]
	delete(idx.idIndex, id)
	return true
}

// Search returns the top-k vectors most similar to query, ranked by
// descending inner product score. query must have length equal to dim.
// If k > Len(), returns all vectors sorted by score.
// Returns an empty slice if the index is empty or k <= 0.
func (idx *Index) Search(query []float32, k int) []SearchResult {
	if k <= 0 {
		return nil
	}
	idx.mu.RLock()
	n := len(idx.entries)
	if n == 0 {
		idx.mu.RUnlock()
		return nil
	}
	pq := idx.quantizer.PrepareQuery(query)
	h := &minHeap{}
	for i := 0; i < n; i++ {
		score := idx.quantizer.InnerProductPrepared(idx.entries[i].qv, pq)
		if h.Len() < k {
			heap.Push(h, SearchResult{ID: idx.entries[i].id, Score: score})
		} else if score > (*h)[0].Score {
			(*h)[0] = SearchResult{ID: idx.entries[i].id, Score: score}
			heap.Fix(h, 0)
		}
	}
	idx.mu.RUnlock()

	results := make([]SearchResult, h.Len())
	copy(results, *h)
	sort.Slice(results, func(a, b int) bool {
		return results[a].Score > results[b].Score
	})
	return results
}

// Len returns the number of vectors currently in the index.
func (idx *Index) Len() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// --- min-heap for top-k selection ---

type minHeap []SearchResult

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(SearchResult)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
