package semantic

import (
	"fmt"
	"testing"
	"time"

	"m31labs.dev/gosx/embed"
)

// fastestCall reports the shortest per-call time across several batches. The
// shortest batch resists interference from other work on the same machine.
func fastestCall(batches, iterations int, fn func()) time.Duration {
	fn()
	best := time.Duration(1 << 62)
	for b := 0; b < batches; b++ {
		start := time.Now()
		for i := 0; i < iterations; i++ {
			fn()
		}
		if elapsed := time.Since(start) / time.Duration(iterations); elapsed < best {
			best = elapsed
		}
	}
	return best
}

// TestContentIndexSearchScalesWithK proves that a small k must not cost a full
// pass over the store. Passing the whole store size as k defeats the index: the
// heap never prunes and the caller re-ranks every document exactly.
func TestContentIndexSearchScalesWithK(t *testing.T) {
	if raceEnabled {
		t.Skip("wall clock budget does not hold under the race detector")
	}
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	index := NewContentIndex(encoder, ContentOptions{})
	for i := 0; i < 1000; i++ {
		index.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d with details", i), ContentMeta{
			Title: fmt.Sprintf("Page %d", i),
			Path:  fmt.Sprintf("/page/%d", i),
		})
	}

	fastest := fastestCall(8, 50, func() {
		if got := index.Search("search for topic 7", 5); len(got) != 5 {
			t.Fatalf("expected 5 results, got %d", len(got))
		}
	})
	t.Logf("ContentIndex.Search(k=5) over 1000 docs = %v per call", fastest)
	if fastest > 250*time.Microsecond {
		t.Fatalf("Search(k=5) takes %v per call, want at most 250µs", fastest)
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
