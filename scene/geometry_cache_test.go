package scene

import (
	"reflect"
	"sync"
	"testing"
)

func cacheTestGeometry(revision uint64) BufferGeometry {
	return BufferGeometry{Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}, Normals: []float64{0, 0, 1, 0, 0, 1, 0, 0, 1}, UVs: []float64{0, 0, 1, 0, 0, 1}, Indices: []int{0, 1, 2}, Immutable: true, Revision: revision}
}

func TestGeometryCacheOwnsStreamsAndReusesMatchingRevision(t *testing.T) {
	c := NewGeometryCache[string](GeometryCacheLimits{MaxEntries: 2, MaxBytes: 2048})
	g := cacheTestGeometry(1)
	g.Positions = append(make([]float64, 0, 4096), g.Positions...)
	g.Attributes = map[string]BufferAttribute{"heat": {Data: []float64{1, 2, 3}, ItemSize: 1}}
	calls := 0
	bake := func() BufferGeometry { calls++; return g }
	first := c.Get("shape", 1, bake)
	if !reflect.DeepEqual(first, g) || &first.Positions[0] == &g.Positions[0] || cap(first.Positions) != len(first.Positions) {
		t.Fatal("cache must retain equivalent, independently owned, trimmed geometry")
	}
	g.Positions[0], g.Indices[0], g.Attributes["heat"].Data[0] = 9, 2, 7
	second := c.Get("shape", 1, bake)
	if calls != 1 || &first.Positions[0] != &second.Positions[0] || second.Positions[0] != 0 || second.Indices[0] != 0 || second.Attributes["heat"].Data[0] != 1 {
		t.Fatal("immutable cache hit rebaked or changed with the source")
	}
	g.Revision = 2
	third := c.Get("shape", 2, bake)
	if calls != 2 || third.Positions[0] != 9 || third.Revision != 2 {
		t.Fatal("new revision reused stale geometry")
	}
	c.Clear()
	c.Get("shape", 2, bake)
	if calls != 3 {
		t.Fatal("cleared geometry was retained")
	}
}

func TestGeometryCacheBoundsAndMutableBypass(t *testing.T) {
	g := cacheTestGeometry(1)
	size := geometryStorageBytes(g)
	c := NewGeometryCache[string](GeometryCacheLimits{MaxEntries: 2, MaxBytes: 2 * size})
	calls := map[string]int{}
	get := func(key string) { c.Get(key, 1, func() BufferGeometry { calls[key]++; return g }) }
	get("a")
	get("b")
	get("a")
	get("c")
	get("b")
	if calls["a"] != 1 || calls["b"] != 2 || c.bytes > 2*size || c.order.Len() > 2 {
		t.Fatal("LRU or storage limits failed")
	}
	for name, limits := range map[string]GeometryCacheLimits{
		"disabled": {}, "entry-limit": {MaxEntries: 1, MaxBytes: 4 * size}, "byte-limit": {MaxEntries: 8, MaxBytes: size},
	} {
		t.Run(name, func(t *testing.T) {
			cache := NewGeometryCache[int](limits)
			for n := 0; n < 4; n++ {
				cache.Get(n, 1, func() BufferGeometry { return g })
			}
			if cache.bytes > max(0, limits.MaxBytes) || cache.order.Len() > max(0, limits.MaxEntries) {
				t.Fatal("limit exceeded")
			}
		})
	}
	for _, geometry := range []BufferGeometry{
		{Positions: g.Positions}, {Positions: g.Positions, Immutable: true},
		{Positions: g.Positions, Immutable: true, Dynamic: true, Revision: 1},
		{Positions: g.Positions, Immutable: true, Revision: 2},
		{Positions: make([]float64, 512), Immutable: true, Revision: 1},
	} {
		cache := NewGeometryCache[string](GeometryCacheLimits{MaxEntries: 2, MaxBytes: size})
		calls := 0
		for n := 0; n < 2; n++ {
			cache.Get("bypass", 1, func() BufferGeometry { calls++; return geometry })
		}
		if calls != 2 || cache.order.Len() != 0 {
			t.Fatal("ineligible geometry was cached")
		}
	}
}

func TestGeometryCacheClearDiscardsConcurrentBake(t *testing.T) {
	c := NewGeometryCache[string](GeometryCacheLimits{MaxEntries: 2, MaxBytes: 2048})
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		c.Get("late", 1, func() BufferGeometry { close(started); <-finish; return cacheTestGeometry(1) })
	}()
	<-started
	c.Clear()
	close(finish)
	<-done
	if c.order.Len() != 0 || c.bytes != 0 {
		t.Fatal("late bake repopulated a cleared cache")
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Go(func() {
			for i := 0; i < 10; i++ {
				c.Get("shared", 1, func() BufferGeometry { return cacheTestGeometry(1) })
			}
		})
	}
	wg.Wait()
	if c.order.Len() != 1 {
		t.Fatal("concurrent hits retained duplicate entries")
	}
}
