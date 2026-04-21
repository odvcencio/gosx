package semantic

import (
	"sort"
	"sync"

	"github.com/odvcencio/gosx/embed"
	"github.com/odvcencio/gosx/vecdb"
)

// ContentOptions configures a content index.
type ContentOptions struct {
	// BitWidth is the quantization bit-width for the underlying vector index.
	// Default: 3.
	BitWidth int
}

// ContentMeta holds metadata about an indexed page or document.
type ContentMeta struct {
	Title       string
	Description string
	Path        string
	Tags        []string
}

// ContentResult is a single result from a content similarity query.
type ContentResult struct {
	ID    string
	Meta  ContentMeta
	Score float32
}

// ContentIndex indexes page content for semantic similarity queries.
// It enables "related pages" and semantic search over a collection of
// documents. Safe for concurrent use.
type ContentIndex struct {
	index      *vecdb.Index
	encoder    *embed.Encoder
	meta       map[string]ContentMeta
	embeddings map[string][]float32
	mu         sync.RWMutex
}

// NewContentIndex creates a content index backed by the given encoder.
func NewContentIndex(encoder *embed.Encoder, opts ContentOptions) *ContentIndex {
	if opts.BitWidth <= 0 {
		opts.BitWidth = 3
	}
	return &ContentIndex{
		index:      vecdb.New(encoder.Dim(), opts.BitWidth),
		meta:       make(map[string]ContentMeta),
		embeddings: make(map[string][]float32),
		encoder:    encoder,
	}
}

// Add indexes a page's content. The content string is embedded via the
// encoder and stored for similarity queries. The meta is returned in
// results but not used for matching.
func (ci *ContentIndex) Add(id string, content string, meta ContentMeta) {
	vec, err := ci.encoder.Encode(content)
	if err != nil {
		return
	}
	ci.mu.Lock()
	defer ci.mu.Unlock()
	ci.meta[id] = meta
	ci.embeddings[id] = cloneFloat32s(vec)
	ci.index.Add(id, vec)
}

// Related finds pages semantically related to the given content.
// It embeds the content and returns the top k nearest neighbors,
// excluding an exact ID match if the content itself is indexed.
func (ci *ContentIndex) Related(content string, k int) []ContentResult {
	vec, err := ci.encoder.Encode(content)
	if err != nil {
		return nil
	}
	return ci.searchVec(vec, k)
}

// Search finds pages matching a search query. It embeds the query
// and returns the top k nearest neighbors.
func (ci *ContentIndex) Search(query string, k int) []ContentResult {
	vec, err := ci.encoder.Encode(query)
	if err != nil {
		return nil
	}
	return ci.searchVec(vec, k)
}

func (ci *ContentIndex) searchVec(vec []float32, k int) []ContentResult {
	if k <= 0 {
		return nil
	}
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	results := ci.index.Search(vec, len(ci.meta))
	out := make([]ContentResult, 0, len(results))
	for _, r := range results {
		meta, ok := ci.meta[r.ID]
		if !ok {
			continue
		}
		embedding, ok := ci.embeddings[r.ID]
		if !ok {
			continue
		}
		out = append(out, ContentResult{
			ID:    r.ID,
			Meta:  meta,
			Score: cosineSimilarity(vec, embedding),
		})
	}
	sort.Slice(out, func(left, right int) bool {
		if out[left].Score == out[right].Score {
			return out[left].ID < out[right].ID
		}
		return out[left].Score > out[right].Score
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

// Len returns the number of indexed documents.
func (ci *ContentIndex) Len() int {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return len(ci.meta)
}
