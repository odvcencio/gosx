package semantic

import (
	"fmt"
	"testing"
	"time"

	"github.com/odvcencio/gosx/embed"
)

func BenchmarkCache_Set(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	c := NewCache(enc, CacheOptions{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, 0)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	c := NewCache(enc, CacheOptions{Threshold: 0.85})
	for i := 0; i < 1000; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(fmt.Sprintf("query-%d", i%1000))
	}
}

func BenchmarkCache_SetWithTTL(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	c := NewCache(enc, CacheOptions{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), i, 5*time.Minute)
	}
}

func BenchmarkRouter_Match(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	r := NewRouter(enc, RouterOptions{Threshold: 0.5})
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("route-%d", i)
		r.Handle(name, fmt.Sprintf("description for route %d", i), func(q string) (any, error) {
			return nil, nil
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match(fmt.Sprintf("query about topic %d", i%100))
	}
}

func BenchmarkContentIndex_Search(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	ci := NewContentIndex(enc, ContentOptions{})
	for i := 0; i < 1000; i++ {
		ci.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d with details", i), ContentMeta{
			Title: fmt.Sprintf("Page %d", i),
			Path:  fmt.Sprintf("/page/%d", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ci.Search(fmt.Sprintf("search for topic %d", i%1000), 5)
	}
}

func BenchmarkContentIndex_Add(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	ci := NewContentIndex(enc, ContentOptions{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ci.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content %d", i), ContentMeta{
			Title: fmt.Sprintf("Page %d", i),
		})
	}
}

func BenchmarkContentIndex_Related(b *testing.B) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 128})
	ci := NewContentIndex(enc, ContentOptions{})
	for i := 0; i < 500; i++ {
		ci.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d", i), ContentMeta{
			Title: fmt.Sprintf("Page %d", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ci.Related(fmt.Sprintf("related topic %d", i%500), 5)
	}
}
