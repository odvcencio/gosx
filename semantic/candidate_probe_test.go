package semantic

import (
	"fmt"
	"runtime"
	"testing"

	"m31labs.dev/gosx/embed"
)

// searchProbeQuery is the query every probe in this file sends. One constant
// keeps every measurement comparable.
const searchProbeQuery = "search for topic 7"

// probeIndex builds a content index that holds size documents of one shape.
func probeIndex(size int) *ContentIndex {
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	index := NewContentIndex(encoder, ContentOptions{})
	for i := 0; i < size; i++ {
		index.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d with details", i), ContentMeta{
			Title: fmt.Sprintf("Page %d", i),
			Path:  fmt.Sprintf("/page/%d", i),
		})
	}
	return index
}

// searchWork reports the memory one Search call allocates, in bytes and in
// allocation count.
//
// The two-stage search scans the quantized vectors without allocating, then
// allocates one result row for each candidate it re-ranks exactly. So allocated
// memory counts the exact re-rank, which is the work the candidate bound exists
// to cap. A busy machine changes the clock. It does not change this number, so
// this probe replaces the wall clock budget the test carried before.
func searchWork(index *ContentIndex, k, runs int) (bytes, allocs float64) {
	index.Search(searchProbeQuery, k) // warm the lazy state, then measure
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < runs; i++ {
		index.Search(searchProbeQuery, k)
	}
	runtime.ReadMemStats(&after)
	return float64(after.TotalAlloc-before.TotalAlloc) / float64(runs),
		float64(after.Mallocs-before.Mallocs) / float64(runs)
}

// TestContentIndexSearchScalesWithK proves that a small k must not cost a full
// pass over the store. Passing the whole store size as the candidate count
// defeats the index: the heap never prunes and the caller re-ranks every
// document exactly.
//
// The test measures that property directly, with two ratios taken inside one
// process:
//
//  1. Grow the corpus 8 times at a fixed k. The work must stay flat. A search
//     that re-ranks the whole store shows about 8 times the work here.
//  2. Grow k 10 times at a fixed corpus. The work must grow. This proves the
//     probe reacts to the candidate set at all, so the flat result in step 1
//     means something.
//
// Both numbers are ratios of two measurements of the same code in the same
// process, so machine speed and machine load cancel. An earlier version of this
// test asserted an absolute 250 microsecond budget for one call. It passed alone
// and failed under load (measured 286, 354 and 101 microseconds on one machine),
// which teaches a reader to ignore a red build.
func TestContentIndexSearchScalesWithK(t *testing.T) {
	const (
		smallCorpus = 1000
		largeCorpus = 8000
		smallK      = 5
		largeK      = 50
		runs        = 200
	)

	// Check the sizing algebra first. candidateCount is pure, so these two
	// assertions cost nothing and they name the contract the measurement below
	// then confirms end to end.
	if flat, grown := candidateCount(smallK, smallCorpus), candidateCount(smallK, largeCorpus); flat != grown {
		t.Fatalf("candidateCount(%d, %d)=%d but candidateCount(%d, %d)=%d; the candidate set must not track the store size",
			smallK, smallCorpus, flat, smallK, largeCorpus, grown)
	}
	if low, high := candidateCount(smallK, smallCorpus), candidateCount(largeK, smallCorpus); high <= low {
		t.Fatalf("candidateCount(%d, %d)=%d and candidateCount(%d, %d)=%d; the candidate set must track k",
			smallK, smallCorpus, low, largeK, smallCorpus, high)
	}

	small := probeIndex(smallCorpus)
	large := probeIndex(largeCorpus)

	if got := small.Search(searchProbeQuery, smallK); len(got) != smallK {
		t.Fatalf("expected %d results, got %d", smallK, len(got))
	}

	smallBytes, smallAllocs := searchWork(small, smallK, runs)
	largeBytes, largeAllocs := searchWork(large, smallK, runs)
	t.Logf("Search(k=%d): %d docs = %.0f bytes / %.1f allocs per call; %d docs = %.0f bytes / %.1f allocs per call",
		smallK, smallCorpus, smallBytes, smallAllocs, largeCorpus, largeBytes, largeAllocs)

	// The corpus grows by this factor, so an unbounded re-rank grows the work by
	// the same factor. Allow a wide margin: the failure it must catch is 8x.
	const corpusToleranceRatio = 2.0
	corpusFactor := float64(largeCorpus) / float64(smallCorpus)
	if ratio := largeAllocs / smallAllocs; ratio > corpusToleranceRatio {
		t.Fatalf("Search(k=%d) allocation count grew %.2fx when the corpus grew %.0fx; "+
			"the exact re-rank must stay bounded by candidateCount(k, size), not by the store size",
			smallK, ratio, corpusFactor)
	}
	if ratio := largeBytes / smallBytes; ratio > corpusToleranceRatio {
		t.Fatalf("Search(k=%d) allocated bytes grew %.2fx when the corpus grew %.0fx; "+
			"the exact re-rank must stay bounded by candidateCount(k, size), not by the store size",
			smallK, ratio, corpusFactor)
	}

	// Step 2: prove the probe tracks the candidate set. The expected growth comes
	// from the caller's k, never from candidateCount, because a candidateCount
	// that ignored k would then lower its own target and pass.
	bigKBytes, bigKAllocs := searchWork(small, largeK, runs)
	t.Logf("Search over %d docs: k=%d = %.1f allocs per call; k=%d = %.1f allocs per call",
		smallCorpus, smallK, smallAllocs, largeK, bigKAllocs)
	kFactor := float64(largeK) / float64(smallK)
	// The margin absorbs the fixed per-call cost and the candidate floor, which
	// both flatten the measured growth below the growth in k.
	const growthMargin = 3.0
	minGrowth := kFactor / growthMargin
	if ratio := bigKAllocs / smallAllocs; ratio < minGrowth {
		t.Fatalf("raising k from %d to %d moved the allocation count by only %.2fx (want at least %.2fx, k grows %.2fx); "+
			"search no longer buys more exact re-rank for a larger k, so the corpus check above proves nothing",
			smallK, largeK, ratio, minGrowth, kFactor)
	}
	if bigKBytes <= smallBytes {
		t.Fatalf("raising k from %d to %d did not raise allocated bytes (%.0f then %.0f); "+
			"the probe no longer tracks the candidate set", smallK, largeK, smallBytes, bigKBytes)
	}
}

// TestContentIndexSearchAllocationsScaleWithK proves the same in allocations:
// the result path must not build a row for every document in the store.
func TestContentIndexSearchAllocationsScaleWithK(t *testing.T) {
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	index := NewContentIndex(encoder, ContentOptions{})
	for i := 0; i < 1000; i++ {
		index.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d", i), ContentMeta{})
	}

	allocs := testing.AllocsPerRun(50, func() {
		index.Search("search for topic 7", 5)
	})
	t.Logf("ContentIndex.Search(k=5) over 1000 docs = %.0f allocations", allocs)
	if allocs > 200 {
		t.Fatalf("Search(k=5) allocated %.0f objects, want at most 200", allocs)
	}
}

// TestRouterMatchScalesWithRouteCount proves the router must not re-rank every
// route exactly on every request.
func TestRouterMatchScalesWithRouteCount(t *testing.T) {
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	router := NewRouter(encoder, RouterOptions{Threshold: 0.5})
	for i := 0; i < 2000; i++ {
		name := fmt.Sprintf("route-%d", i)
		router.Handle(name, fmt.Sprintf("description for route %d", i), func(string) (any, error) {
			return nil, nil
		})
	}

	allocs := testing.AllocsPerRun(50, func() {
		router.Match("query about topic 7")
	})
	t.Logf("Router.Match over 2000 routes = %.0f allocations", allocs)
	if allocs > 200 {
		t.Fatalf("Router.Match allocated %.0f objects, want at most 200", allocs)
	}
}

// TestCacheGetScalesWithStoreSize proves the cache must not re-rank every entry
// exactly on every lookup.
func TestCacheGetScalesWithStoreSize(t *testing.T) {
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	cache := NewCache(encoder, CacheOptions{Threshold: 0.85})
	for i := 0; i < 2000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i, 0)
	}

	allocs := testing.AllocsPerRun(50, func() {
		cache.Get("query-7")
	})
	t.Logf("Cache.Get over 2000 entries = %.0f allocations", allocs)
	if allocs > 200 {
		t.Fatalf("Cache.Get allocated %.0f objects, want at most 200", allocs)
	}
}
