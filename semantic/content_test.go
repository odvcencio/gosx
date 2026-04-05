package semantic

import (
	"testing"

	"github.com/odvcencio/gosx/embed"
)

func TestContentIndex_AddSearch(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	ci := NewContentIndex(enc, ContentOptions{})

	ci.Add("page-1", "Introduction to Go programming", ContentMeta{
		Title: "Go Intro",
		Path:  "/docs/intro",
	})

	results := ci.Search("Introduction to Go programming", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].ID != "page-1" {
		t.Fatalf("expected page-1, got %s", results[0].ID)
	}
	if results[0].Meta.Title != "Go Intro" {
		t.Fatalf("expected 'Go Intro', got %q", results[0].Meta.Title)
	}
}

func TestContentIndex_Related(t *testing.T) {
	p := &controlledProvider{
		dim:     4,
		vectors: make(map[string][]float32),
	}
	p.vectors["Go concurrency patterns"] = []float32{0.8, 0.3, 0.1, 0.0}
	p.vectors["Rust ownership model"] = []float32{-0.2, 0.7, -0.5, 0.3}
	p.vectors["Go goroutines and channels"] = []float32{0.78, 0.32, 0.12, 0.01}
	// Query for related content.
	p.vectors["concurrency in Go"] = []float32{0.79, 0.31, 0.11, 0.01}

	enc := embed.NewProviderEncoder(p)
	ci := NewContentIndex(enc, ContentOptions{})

	ci.Add("go-concurrency", "Go concurrency patterns", ContentMeta{
		Title: "Concurrency in Go",
		Path:  "/docs/concurrency",
		Tags:  []string{"go", "concurrency"},
	})
	ci.Add("rust-ownership", "Rust ownership model", ContentMeta{
		Title: "Rust Ownership",
		Path:  "/docs/rust",
		Tags:  []string{"rust"},
	})
	ci.Add("go-goroutines", "Go goroutines and channels", ContentMeta{
		Title: "Goroutines",
		Path:  "/docs/goroutines",
		Tags:  []string{"go", "goroutines"},
	})

	results := ci.Related("concurrency in Go", 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The top results should be the Go-related pages.
	for _, r := range results {
		if r.ID == "rust-ownership" {
			t.Fatal("rust page should not be in top-2 related to Go concurrency")
		}
	}
}

func TestContentIndex_EmptyResults(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	ci := NewContentIndex(enc, ContentOptions{})

	results := ci.Search("anything", 10)
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}

	results = ci.Related("anything", 10)
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestContentIndex_Len(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	ci := NewContentIndex(enc, ContentOptions{})

	if ci.Len() != 0 {
		t.Fatalf("expected 0, got %d", ci.Len())
	}
	ci.Add("a", "alpha content", ContentMeta{Title: "Alpha"})
	ci.Add("b", "beta content", ContentMeta{Title: "Beta"})
	if ci.Len() != 2 {
		t.Fatalf("expected 2, got %d", ci.Len())
	}
}

func TestContentIndex_SearchMultiple(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	ci := NewContentIndex(enc, ContentOptions{})

	for i := 0; i < 10; i++ {
		id := string(rune('a' + i))
		ci.Add(id, "content for page "+id, ContentMeta{
			Title: "Page " + id,
			Path:  "/" + id,
		})
	}

	results := ci.Search("content for page a", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestContentIndex_MetaPreserved(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	ci := NewContentIndex(enc, ContentOptions{})

	ci.Add("tagged", "some content", ContentMeta{
		Title:       "Tagged Page",
		Description: "A page with tags",
		Path:        "/tagged",
		Tags:        []string{"alpha", "beta"},
	})

	results := ci.Search("some content", 1)
	if len(results) == 0 {
		t.Fatal("expected result")
	}
	m := results[0].Meta
	if m.Title != "Tagged Page" {
		t.Fatalf("title: got %q", m.Title)
	}
	if m.Description != "A page with tags" {
		t.Fatalf("desc: got %q", m.Description)
	}
	if m.Path != "/tagged" {
		t.Fatalf("path: got %q", m.Path)
	}
	if len(m.Tags) != 2 || m.Tags[0] != "alpha" || m.Tags[1] != "beta" {
		t.Fatalf("tags: got %v", m.Tags)
	}
}
