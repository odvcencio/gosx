package playground

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/odvcencio/gosx/examples/gosx-docs/app/demos/democtl"
)

// realClock is a local implementation of democtl.Clock that delegates to
// time.Now. democtl's own realClock is unexported, so we define our own
// here — structural interface match is sufficient.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// cacheEntry holds a single cached compile result.
type cacheEntry struct {
	key        string
	result     CompileResult
	insertedAt time.Time
}

// compileCache is a bounded LRU cache for compile results. It is
// concurrency-safe.
type compileCache struct {
	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List // front = most recently used
	cap   int
	ttl   time.Duration
	clock democtl.Clock
}

// newCompileCache creates a compileCache with the given capacity, TTL, and
// clock. Zero values fall back to package defaults.
func newCompileCache(cap int, ttl time.Duration, clock democtl.Clock) *compileCache {
	if cap <= 0 {
		cap = defaultCacheCapacity
	}
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if clock == nil {
		clock = realClock{}
	}
	return &compileCache{
		items: make(map[string]*list.Element, cap),
		order: list.New(),
		cap:   cap,
		ttl:   ttl,
		clock: clock,
	}
}

// cacheKeyFor hashes source bytes to a stable hex string used as the cache key.
func cacheKeyFor(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

// Get returns a cached result if present and not expired.
func (c *compileCache) Get(key string) (CompileResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return CompileResult{}, false
	}
	entry := elem.Value.(*cacheEntry)
	if c.clock.Now().Sub(entry.insertedAt) > c.ttl {
		// Expired — evict.
		c.order.Remove(elem)
		delete(c.items, key)
		return CompileResult{}, false
	}
	// Bump MRU.
	c.order.MoveToFront(elem)
	return entry.result, true
}

// Put stores a result, evicting the LRU entry if at capacity.
func (c *compileCache) Put(key string, result CompileResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*cacheEntry)
		entry.result = result
		entry.insertedAt = c.clock.Now()
		c.order.MoveToFront(elem)
		return
	}
	entry := &cacheEntry{
		key:        key,
		result:     result,
		insertedAt: c.clock.Now(),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
	for c.order.Len() > c.cap {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		oldEntry := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.items, oldEntry.key)
	}
}

// Len returns the current number of entries (for tests).
func (c *compileCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
