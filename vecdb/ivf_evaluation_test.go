package vecdb

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// This file evaluates whether an approximate nearest neighbour index belongs in
// vecdb. It builds an inverted file index, the cheapest useful approximation for
// a pure Go package with no new dependency, and measures it against the exact
// scan that Index.Search runs today.
//
// The measurement copies semantic/rerank_quality_test.go: 128-dimension unit
// vectors from a hash provider, 3-bit quantization, an exact cosine scan as the
// truth, and recall at k plus top-1 agreement as the score. The corpus sizes are
// the sizes GoSX targets, which are route tables and per-site content indexes.
//
// The evaluation is a test, not shipped code. Read the report in
// TestIVFAgainstExactScan before you add an approximate index.

// hashVector reproduces the deterministic embeddings of the semantic harness.
func hashVector(text string, dim int) []float32 {
	h := fnv.New64a()
	h.Write([]byte(text))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rng.Float32()*2 - 1
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	scale := float32(1.0 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}
	return vec
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// ivfIndex is a prototype inverted file index over the same quantized entries
// that Index holds. It scores the query against nlist centroids, then scans only
// the entries of the nprobe nearest lists.
type ivfIndex struct {
	base      *Index
	centroids [][]float32
	lists     [][]int // entry positions per centroid
	nprobe    int
}

// buildIVF clusters the vectors with a bounded k-means run and fills the lists.
func buildIVF(base *Index, vectors [][]float32, nlist, nprobe, iterations int, seed int64) *ivfIndex {
	dim := len(vectors[0])
	rng := rand.New(rand.NewSource(seed))
	centroids := make([][]float32, nlist)
	for i := range centroids {
		centroids[i] = append([]float32(nil), vectors[rng.Intn(len(vectors))]...)
	}
	assign := make([]int, len(vectors))
	for iter := 0; iter < iterations; iter++ {
		for i, vec := range vectors {
			best, bestScore := 0, float32(-2)
			for c := range centroids {
				if score := cosine(vec, centroids[c]); score > bestScore {
					best, bestScore = c, score
				}
			}
			assign[i] = best
		}
		sums := make([][]float64, nlist)
		counts := make([]int, nlist)
		for c := range sums {
			sums[c] = make([]float64, dim)
		}
		for i, vec := range vectors {
			c := assign[i]
			counts[c]++
			for d := range vec {
				sums[c][d] += float64(vec[d])
			}
		}
		for c := range centroids {
			if counts[c] == 0 {
				continue
			}
			for d := 0; d < dim; d++ {
				centroids[c][d] = float32(sums[c][d] / float64(counts[c]))
			}
		}
	}
	lists := make([][]int, nlist)
	for i := range vectors {
		lists[assign[i]] = append(lists[assign[i]], i)
	}
	return &ivfIndex{base: base, centroids: centroids, lists: lists, nprobe: nprobe}
}

// search returns the top-k identities and the number of entries it scored.
func (ix *ivfIndex) search(query []float32, k int) ([]SearchResult, int) {
	type probe struct {
		list  int
		score float32
	}
	probes := make([]probe, len(ix.centroids))
	for c := range ix.centroids {
		probes[c] = probe{list: c, score: cosine(query, ix.centroids[c])}
	}
	sort.Slice(probes, func(a, b int) bool { return probes[a].score > probes[b].score })

	pq := ix.base.quantizer.PrepareQuery(query)
	out := make([]SearchResult, 0, k)
	scored := 0
	for p := 0; p < ix.nprobe && p < len(probes); p++ {
		for _, pos := range ix.lists[probes[p].list] {
			score := ix.base.quantizer.InnerProductPrepared(ix.base.entries[pos].qv, pq)
			out = append(out, SearchResult{ID: ix.base.entries[pos].id, Score: score})
			scored++
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out, scored
}

// TestIVFAgainstExactScan reports recall and latency for the inverted file index
// against the exact scan, at the corpus sizes GoSX targets.
func TestIVFAgainstExactScan(t *testing.T) {
	if testing.Short() {
		t.Skip("evaluation runs a k-means build")
	}
	const (
		dim     = 128
		bits    = 3
		k       = 5
		queries = 200
	)
	for _, docs := range []int{1_000, 10_000} {
		t.Run(fmt.Sprintf("docs=%d", docs), func(t *testing.T) {
			ids := make([]string, docs)
			vectors := make([][]float32, docs)
			byID := make(map[string][]float32, docs)
			for i := 0; i < docs; i++ {
				ids[i] = fmt.Sprintf("page-%d", i)
				vectors[i] = hashVector(fmt.Sprintf("content about topic %d with details", i), dim)
				byID[ids[i]] = vectors[i]
			}
			base := NewWithSeed(dim, bits, 42)
			base.AddBatch(ids, vectors)

			queryVecs := make([][]float32, queries)
			truth := make([][]string, queries)
			for q := 0; q < queries; q++ {
				queryVecs[q] = hashVector(fmt.Sprintf("search for topic %d", q), dim)
				truth[q] = exactTopIDs(queryVecs[q], ids, byID, k)
			}

			fetch := k * 16

			// Exact scan plus exact re-rank: the shipped behaviour.
			start := time.Now()
			var exactHits, exactTop1 int
			for q := 0; q < queries; q++ {
				got := rerank(queryVecs[q], base.Search(queryVecs[q], fetch), byID, k)
				exactHits += overlap(got, truth[q])
				if len(got) > 0 && got[0] == truth[q][0] {
					exactTop1++
				}
			}
			exactTime := time.Since(start) / queries
			t.Logf("exact scan + re-rank: recall@%d=%.3f top1=%.3f %v/query scored=%d",
				k, float64(exactHits)/float64(queries*k),
				float64(exactTop1)/float64(queries), exactTime, docs)

			nlist := int(math.Sqrt(float64(docs)))
			for _, nprobe := range []int{1, 2, 4, 8, 16} {
				if nprobe > nlist {
					continue
				}
				buildStart := time.Now()
				ivf := buildIVF(base, vectors, nlist, nprobe, 8, 7)
				buildTime := time.Since(buildStart)

				start := time.Now()
				var hits, top1, scored int
				for q := 0; q < queries; q++ {
					results, n := ivf.search(queryVecs[q], fetch)
					scored += n
					got := rerank(queryVecs[q], results, byID, k)
					hits += overlap(got, truth[q])
					if len(got) > 0 && got[0] == truth[q][0] {
						top1++
					}
				}
				elapsed := time.Since(start) / queries
				t.Logf("ivf nlist=%d nprobe=%2d: recall@%d=%.3f top1=%.3f %v/query scored=%d build=%v",
					nlist, nprobe, k, float64(hits)/float64(queries*k),
					float64(top1)/float64(queries), elapsed, scored/queries, buildTime)
			}
		})
	}
}

func exactTopIDs(query []float32, ids []string, byID map[string][]float32, k int) []string {
	type scored struct {
		id    string
		score float32
	}
	all := make([]scored, 0, len(ids))
	for _, id := range ids {
		all = append(all, scored{id: id, score: cosine(query, byID[id])})
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

func rerank(query []float32, candidates []SearchResult, byID map[string][]float32, k int) []string {
	type scored struct {
		id    string
		score float32
	}
	all := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		all = append(all, scored{id: c.ID, score: cosine(query, byID[c.ID])})
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

func overlap(got, want []string) int {
	set := make(map[string]bool, len(want))
	for _, id := range want {
		set[id] = true
	}
	n := 0
	for _, id := range got {
		if set[id] {
			n++
		}
	}
	return n
}
