package scene

import (
	"container/list"
	"strings"
	"sync"
)

// GeometryCacheLimits bounds retained vertex storage and the number of keys.
// Nonpositive limits disable retention. Key storage is bounded by MaxEntries;
// callers should use small keys containing all geometry construction inputs.
type GeometryCacheLimits struct {
	MaxEntries int
	MaxBytes   int64
}

// GeometryCache retains generated immutable meshes for one scene or renderer.
// Keys and revisions identify geometry, never placement, material or user state.
// Returned streams are read-only; replace the key or revision to change them.
// Clear releases retained storage when the renderer is disposed.
//
// Cached streams are independently owned, trimmed copies, so a small subslice
// cannot keep a large source allocation alive. Cold concurrent misses may bake
// twice; the callback runs outside the lock and should be deterministic.
type GeometryCache[K comparable] struct {
	mu      sync.Mutex
	limits  GeometryCacheLimits
	bytes   int64
	epoch   uint64
	entries map[K]*list.Element
	order   list.List
}

type geometryCacheEntry[K comparable] struct {
	key      K
	revision uint64
	geometry BufferGeometry
	bytes    int64
}

func NewGeometryCache[K comparable](limits GeometryCacheLimits) *GeometryCache[K] {
	return &GeometryCache[K]{limits: limits, entries: make(map[K]*list.Element)}
}

// Get bakes on a miss. Dynamic, mutable, empty, revisionless and oversized
// results pass through without retention. bake must be non-nil on a miss.
func (c *GeometryCache[K]) Get(key K, revision uint64, bake func() BufferGeometry) BufferGeometry {
	if c == nil || c.limits.MaxEntries <= 0 || c.limits.MaxBytes <= 0 || revision == 0 {
		return bake()
	}
	c.mu.Lock()
	epoch := c.epoch
	if element := c.entries[key]; element != nil {
		entry := element.Value.(*geometryCacheEntry[K])
		if entry.revision == revision {
			c.order.MoveToFront(element)
			c.mu.Unlock()
			return entry.geometry
		}
	}
	c.mu.Unlock()
	g := bake()
	bytes := geometryStorageBytes(g)
	if !g.Immutable || g.Dynamic || g.Revision != revision || len(g.Positions) == 0 || bytes > c.limits.MaxBytes {
		return g
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.epoch {
		return g
	}
	if element := c.entries[key]; element != nil {
		entry := element.Value.(*geometryCacheEntry[K])
		if entry.revision == revision {
			c.order.MoveToFront(element)
			return entry.geometry
		}
		if entry.revision > revision {
			return g // An older concurrent bake must not evict a newer revision.
		}
		c.remove(element)
	}
	for c.order.Len() >= c.limits.MaxEntries || c.bytes+bytes > c.limits.MaxBytes {
		c.remove(c.order.Back())
	}
	g = ownGeometry(g)
	entry := &geometryCacheEntry[K]{key: key, revision: revision, geometry: g, bytes: bytes}
	c.entries[key] = c.order.PushFront(entry)
	c.bytes += bytes
	return g
}

func (c *GeometryCache[K]) remove(element *list.Element) {
	entry := element.Value.(*geometryCacheEntry[K])
	delete(c.entries, entry.key)
	c.bytes -= entry.bytes
	c.order.Remove(element)
}

func (c *GeometryCache[K]) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[K]*list.Element)
	c.order.Init()
	c.bytes = 0
	c.epoch++
}

func geometryStorageBytes(g BufferGeometry) int64 {
	bytes := int64(len(g.Positions)+len(g.Normals)+len(g.UVs)+len(g.Tangents)) * 8
	// Conservatively account native-width indices as 64-bit on every target.
	bytes += int64(len(g.Indices)) * 8
	//gosx:allow map-range sums or copies each entry independently; order does not matter
	for name, attribute := range g.Attributes {
		bytes += int64(len(name)) + int64(len(attribute.Data))*8
	}
	return bytes
}

func ownGeometry(g BufferGeometry) BufferGeometry {
	copyFloats := func(values []float64) []float64 {
		if values == nil {
			return nil
		}
		owned := make([]float64, len(values))
		copy(owned, values)
		return owned
	}
	g.Positions = copyFloats(g.Positions)
	g.Normals = copyFloats(g.Normals)
	g.UVs = copyFloats(g.UVs)
	g.Tangents = copyFloats(g.Tangents)
	if g.Indices != nil {
		indices := make([]int, len(g.Indices))
		copy(indices, g.Indices)
		g.Indices = indices
	}
	if g.Attributes != nil {
		attributes := make(map[string]BufferAttribute, len(g.Attributes))
		//gosx:allow map-range sums or copies each entry independently; order does not matter
		for name, attribute := range g.Attributes {
			attribute.Data = copyFloats(attribute.Data)
			attributes[strings.Clone(name)] = attribute
		}
		g.Attributes = attributes
	}
	return g
}
