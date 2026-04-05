package semantic

import (
	"sync"
	"testing"
	"time"

	"github.com/odvcencio/gosx/embed"
)

func TestCache_SetGet(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	c.Set("how do I reset my password", "Go to settings > security", 0)

	val, ok := c.Get("how do I reset my password")
	if !ok {
		t.Fatal("expected cache hit for exact key")
	}
	if val != "Go to settings > security" {
		t.Fatalf("unexpected value: %v", val)
	}
}

func TestCache_GetMiss(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	val, ok := c.Get("anything")
	if ok {
		t.Fatalf("expected miss on empty cache, got %v", val)
	}
}

func TestCache_SimilarKeyHit(t *testing.T) {
	// Use a controlled provider where we dictate exact vectors.
	p := &controlledProvider{
		dim:     4,
		vectors: make(map[string][]float32),
	}
	// Two similar keys with near-identical vectors (cosine ~0.99).
	p.vectors["What is the weather today"] = []float32{0.9, 0.1, 0.3, 0.1}
	p.vectors["Tell me today's weather"] = []float32{0.89, 0.12, 0.31, 0.09}
	// A dissimilar query.
	p.vectors["How to bake a cake"] = []float32{-0.5, 0.8, -0.2, 0.1}

	enc := embed.NewProviderEncoder(p)
	c := NewCache(enc, CacheOptions{Threshold: 0.5})

	c.Set("What is the weather today", "sunny", 0)

	// Similar query should hit.
	val, ok := c.Get("Tell me today's weather")
	if !ok {
		t.Fatal("expected cache hit for similar key")
	}
	if val != "sunny" {
		t.Fatalf("unexpected value: %v", val)
	}

	// Dissimilar query should miss.
	_, ok = c.Get("How to bake a cake")
	if ok {
		t.Fatal("expected cache miss for dissimilar key")
	}
}

func TestCache_TTLExpiration(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	// Use SetWithEmbedding to control timing — manually set a past CreatedAt.
	vec, _ := enc.Encode("expired-key")
	c.mu.Lock()
	c.store["expired-key"] = CacheEntry{
		Key:       "expired-key",
		Value:     "old-value",
		Embedding: vec,
		CreatedAt: time.Now().Add(-2 * time.Second),
		TTL:       1 * time.Second,
	}
	c.index.Add("expired-key", vec)
	c.mu.Unlock()

	val, ok := c.Get("expired-key")
	if ok {
		t.Fatalf("expected miss for expired entry, got %v", val)
	}
}

func TestCache_TTLNotExpired(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	c.Set("live-key", "live-value", 10*time.Minute)

	val, ok := c.Get("live-key")
	if !ok {
		t.Fatal("expected hit for non-expired entry")
	}
	if val != "live-value" {
		t.Fatalf("unexpected value: %v", val)
	}
}

func TestCache_ZeroTTLNeverExpires(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	c.Set("forever", "here", 0)

	val, ok := c.Get("forever")
	if !ok {
		t.Fatal("expected hit for zero-TTL entry")
	}
	if val != "here" {
		t.Fatalf("unexpected value: %v", val)
	}
}

func TestCache_Invalidate(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	c.Set("remove-me", "value", 0)
	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}

	c.Invalidate("remove-me")
	if c.Len() != 0 {
		t.Fatalf("expected len 0 after invalidate, got %d", c.Len())
	}

	_, ok := c.Get("remove-me")
	if ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestCache_ConcurrentSetGet(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		key := string(rune('a'+i%26)) + "-key"
		go func() {
			defer wg.Done()
			c.Set(key, "val", 0)
		}()
		go func() {
			defer wg.Done()
			c.Get(key)
		}()
	}
	wg.Wait()
	// No panic or race detector failure is the success condition.
}

func TestCache_SetWithEmbedding(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 4})
	c := NewCache(enc, CacheOptions{Threshold: 0.5})

	vec := []float32{0.5, 0.5, 0.5, 0.5}
	c.SetWithEmbedding("manual", "manual-value", vec, 0)

	if c.Len() != 1 {
		t.Fatalf("expected len 1, got %d", c.Len())
	}
}

func TestCache_Len(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	c := NewCache(enc, CacheOptions{})

	if c.Len() != 0 {
		t.Fatalf("expected 0, got %d", c.Len())
	}
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	if c.Len() != 2 {
		t.Fatalf("expected 2, got %d", c.Len())
	}
}
