package semantic

import (
	"sync"
	"time"

	"github.com/odvcencio/gosx/embed"
	"github.com/odvcencio/gosx/vecdb"
)

// CacheOptions configures a semantic cache.
type CacheOptions struct {
	// BitWidth is the quantization bit-width for the underlying vector index.
	// Default: 3.
	BitWidth int

	// Threshold is the minimum cosine similarity for a cache hit.
	// Queries whose best match scores below this are treated as misses.
	// Default: 0.85.
	Threshold float32
}

// CacheEntry holds a cached value together with its embedding and metadata.
type CacheEntry struct {
	Key       string
	Value     any
	Embedding []float32
	CreatedAt time.Time
	TTL       time.Duration
}

// Cache stores responses keyed by embedding similarity.
// Two semantically similar keys can resolve to the same cached value.
// Safe for concurrent use.
type Cache struct {
	index     *vecdb.Index
	store     map[string]CacheEntry
	encoder   *embed.Encoder
	threshold float32
	mu        sync.RWMutex
}

// NewCache creates a semantic cache backed by the given encoder.
func NewCache(encoder *embed.Encoder, opts CacheOptions) *Cache {
	if opts.BitWidth <= 0 {
		opts.BitWidth = 3
	}
	if opts.Threshold <= 0 {
		opts.Threshold = 0.85
	}
	return &Cache{
		index:     vecdb.New(encoder.Dim(), opts.BitWidth),
		store:     make(map[string]CacheEntry),
		encoder:   encoder,
		threshold: opts.Threshold,
	}
}

// Get finds the most similar cached entry above the threshold.
// Returns the cached value and true on a hit, or nil and false on a miss.
// Expired entries (past their TTL) are treated as misses.
func (c *Cache) Get(query string) (any, bool) {
	vec, err := c.encoder.Encode(query)
	if err != nil {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	results := c.index.Search(vec, 1)
	if len(results) == 0 {
		return nil, false
	}
	best := results[0]
	if best.Score < c.threshold {
		return nil, false
	}
	entry, ok := c.store[best.ID]
	if !ok {
		return nil, false
	}
	if entry.TTL > 0 && time.Since(entry.CreatedAt) > entry.TTL {
		return nil, false
	}
	return entry.Value, true
}

// Set stores a value with its text key, embedding it automatically.
// The key text is embedded via the encoder and used for similarity matching.
func (c *Cache) Set(key string, value any, ttl time.Duration) {
	vec, err := c.encoder.Encode(key)
	if err != nil {
		return
	}
	c.SetWithEmbedding(key, value, vec, ttl)
}

// SetWithEmbedding stores a value with a pre-computed embedding.
func (c *Cache) SetWithEmbedding(key string, value any, embedding []float32, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = CacheEntry{
		Key:       key,
		Value:     value,
		Embedding: embedding,
		CreatedAt: time.Now(),
		TTL:       ttl,
	}
	c.index.Add(key, embedding)
}

// Invalidate removes a cached entry by key.
func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
	c.index.Remove(key)
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.store)
}
