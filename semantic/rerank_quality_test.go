package semantic

import (
	"fmt"
	"sort"
	"testing"

	"m31labs.dev/gosx/embed"
	"m31labs.dev/gosx/vecdb"
)

// TestQuantizedRecallAgainstExactRanking measures how often the quantized index
// alone returns the same top-k as an exact cosine scan, and how much an
// over-fetch plus exact re-rank recovers. The numbers justify the over-fetch
// factor that searchVec uses.
func TestQuantizedRecallAgainstExactRanking(t *testing.T) {
	const (
		docs    = 1000
		queries = 200
		dim     = 128
	)
	provider := &hashProvider{dim: dim}
	encoder := embed.NewProviderEncoder(provider)

	vectors := make(map[string][]float32, docs)
	ids := make([]string, 0, docs)
	index := vecdb.NewWithSeed(dim, 3, 42)
	for i := 0; i < docs; i++ {
		id := fmt.Sprintf("page-%d", i)
		vec, err := encoder.Encode(fmt.Sprintf("content about topic %d with details", i))
		if err != nil {
			t.Fatal(err)
		}
		vectors[id] = vec
		ids = append(ids, id)
		index.Add(id, vec)
	}

	exactTopK := func(query []float32, k int) []string {
		type scored struct {
			id    string
			score float32
		}
		all := make([]scored, 0, len(ids))
		for _, id := range ids {
			all = append(all, scored{id: id, score: cosineSimilarity(query, vectors[id])})
		}
		sort.Slice(all, func(a, b int) bool {
			if all[a].score == all[b].score {
				return all[a].id < all[b].id
			}
			return all[a].score > all[b].score
		})
		out := make([]string, 0, k)
		for i := 0; i < k && i < len(all); i++ {
			out = append(out, all[i].id)
		}
		return out
	}

	rerankTopK := func(query []float32, k, fetch int) []string {
		type scored struct {
			id    string
			score float32
		}
		results := index.Search(query, fetch)
		all := make([]scored, 0, len(results))
		for _, r := range results {
			all = append(all, scored{id: r.ID, score: cosineSimilarity(query, vectors[r.ID])})
		}
		sort.Slice(all, func(a, b int) bool {
			if all[a].score == all[b].score {
				return all[a].id < all[b].id
			}
			return all[a].score > all[b].score
		})
		out := make([]string, 0, k)
		for i := 0; i < k && i < len(all); i++ {
			out = append(out, all[i].id)
		}
		return out
	}

	for _, k := range []int{1, 5, 10} {
		var quantHits, quantTop1, rerankHits, rerankTop1 int
		fetchHits := map[int]int{}
		fetchTop1 := map[int]int{}
		factors := []int{2, 4, 8, 16}
		for q := 0; q < queries; q++ {
			query, err := encoder.Encode(fmt.Sprintf("search for topic %d", q))
			if err != nil {
				t.Fatal(err)
			}
			truth := exactTopK(query, k)
			truthSet := map[string]bool{}
			for _, id := range truth {
				truthSet[id] = true
			}

			// Quantized only, no re-rank.
			for _, r := range index.Search(query, k) {
				if truthSet[r.ID] {
					quantHits++
				}
			}
			if got := index.Search(query, k); len(got) > 0 && got[0].ID == truth[0] {
				quantTop1++
			}

			// Full store fetch plus exact re-rank: what the code does today.
			full := rerankTopK(query, k, docs)
			for _, id := range full {
				if truthSet[id] {
					rerankHits++
				}
			}
			if len(full) > 0 && full[0] == truth[0] {
				rerankTop1++
			}

			// Bounded over-fetch plus exact re-rank.
			for _, factor := range factors {
				got := rerankTopK(query, k, k*factor)
				for _, id := range got {
					if truthSet[id] {
						fetchHits[factor]++
					}
				}
				if len(got) > 0 && got[0] == truth[0] {
					fetchTop1[factor]++
				}
			}
		}
		total := float64(queries * k)
		t.Logf("k=%d quantized-only recall=%.3f top1=%.3f", k,
			float64(quantHits)/total, float64(quantTop1)/float64(queries))
		t.Logf("k=%d full-fetch re-rank recall=%.3f top1=%.3f", k,
			float64(rerankHits)/total, float64(rerankTop1)/float64(queries))
		for _, factor := range factors {
			t.Logf("k=%d over-fetch x%d re-rank recall=%.3f top1=%.3f", k, factor,
				float64(fetchHits[factor])/total, float64(fetchTop1[factor])/float64(queries))
		}
	}
}

// TestBoundedCandidateSetMatchesWholeStoreRerank locks the shipped candidate
// sizing. The bounded two-stage search must return nearly the same top-k as a
// re-rank over the whole store.
func TestBoundedCandidateSetMatchesWholeStoreRerank(t *testing.T) {
	const (
		docs    = 1000
		queries = 200
		k       = 5
	)
	encoder := embed.NewProviderEncoder(&hashProvider{dim: 128})
	index := NewContentIndex(encoder, ContentOptions{})
	for i := 0; i < docs; i++ {
		index.Add(fmt.Sprintf("page-%d", i), fmt.Sprintf("content about topic %d with details", i), ContentMeta{})
	}

	var agree, top1 int
	for q := 0; q < queries; q++ {
		query := fmt.Sprintf("search for topic %d", q)
		vec, err := encoder.Encode(query)
		if err != nil {
			t.Fatal(err)
		}

		bounded := index.Search(query, k)
		truth := wholeStoreRerank(t, index, vec, k)
		if len(bounded) != k || len(truth) != k {
			t.Fatalf("expected %d results, got bounded=%d truth=%d", k, len(bounded), len(truth))
		}
		truthSet := map[string]bool{}
		for _, r := range truth {
			truthSet[r.ID] = true
		}
		for _, r := range bounded {
			if truthSet[r.ID] {
				agree++
			}
		}
		if bounded[0].ID == truth[0].ID {
			top1++
		}
	}

	recall := float64(agree) / float64(queries*k)
	top1Rate := float64(top1) / float64(queries)
	t.Logf("bounded candidate set: recall@%d=%.3f top1=%.3f", k, recall, top1Rate)
	if recall < 0.99 {
		t.Fatalf("recall@%d = %.3f, want at least 0.99", k, recall)
	}
	if top1Rate < 0.99 {
		t.Fatalf("top1 = %.3f, want at least 0.99", top1Rate)
	}
}

// wholeStoreRerank reproduces the old behaviour: fetch every document, then
// re-rank all of them exactly.
func wholeStoreRerank(t *testing.T, ci *ContentIndex, vec []float32, k int) []ContentResult {
	t.Helper()
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	results := ci.index.Search(vec, len(ci.meta))
	out := make([]ContentResult, 0, len(results))
	for _, r := range results {
		embedding, ok := ci.embeddings[r.ID]
		if !ok {
			continue
		}
		out = append(out, ContentResult{ID: r.ID, Score: cosineSimilarity(vec, embedding)})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Score == out[b].Score {
			return out[a].ID < out[b].ID
		}
		return out[a].Score > out[b].Score
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}
