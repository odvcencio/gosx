package meshoptim

import (
	"math/rand"
	"sort"
	"testing"
)

// gridMesh builds an n by n quad grid as a triangle list in row order.
func gridMesh(n int) ([]uint32, []float32, int) {
	vertexCount := (n + 1) * (n + 1)
	positions := make([]float32, 0, vertexCount*3)
	for y := 0; y <= n; y++ {
		for x := 0; x <= n; x++ {
			positions = append(positions, float32(x), 0, float32(y))
		}
	}
	indices := make([]uint32, 0, n*n*6)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			a := uint32(y*(n+1) + x)
			b := a + 1
			c := a + uint32(n+1)
			d := c + 1
			indices = append(indices, a, c, b, b, c, d)
		}
	}
	return indices, positions, vertexCount
}

// shuffleTriangles reorders whole triangles, which leaves the mesh unchanged
// but destroys every cache locality the author built in.
func shuffleTriangles(indices []uint32, seed int64) []uint32 {
	triangles := len(indices) / 3
	order := make([]int, triangles)
	for i := range order {
		order[i] = i
	}
	random := rand.New(rand.NewSource(seed))
	random.Shuffle(triangles, func(i, j int) { order[i], order[j] = order[j], order[i] })
	out := make([]uint32, 0, len(indices))
	for _, triangle := range order {
		out = append(out, indices[triangle*3:triangle*3+3]...)
	}
	return out
}

// triangleSet keys every triangle by its sorted corner set, so a comparison
// ignores order but still catches a lost or invented triangle.
func triangleSet(indices []uint32) map[[3]uint32]int {
	out := map[[3]uint32]int{}
	for i := 0; i+2 < len(indices); i += 3 {
		key := [3]uint32{indices[i], indices[i+1], indices[i+2]}
		sorted := key[:]
		sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
		out[[3]uint32{sorted[0], sorted[1], sorted[2]}]++
	}
	return out
}

func TestOptimizeVertexCacheKeepsEveryTriangle(t *testing.T) {
	indices, _, vertexCount := gridMesh(24)
	shuffled := shuffleTriangles(indices, 7)
	optimized := OptimizeVertexCache(shuffled, vertexCount)

	if len(optimized) != len(shuffled) {
		t.Fatalf("index count changed: %d, want %d", len(optimized), len(shuffled))
	}
	before := triangleSet(shuffled)
	after := triangleSet(optimized)
	if len(before) != len(after) {
		t.Fatalf("distinct triangles changed: %d, want %d", len(after), len(before))
	}
	for key, count := range before {
		if after[key] != count {
			t.Fatalf("triangle %v appears %d times, want %d", key, after[key], count)
		}
	}
}

func TestOptimizeVertexCacheKeepsWinding(t *testing.T) {
	indices, _, vertexCount := gridMesh(12)
	shuffled := shuffleTriangles(indices, 11)
	optimized := OptimizeVertexCache(shuffled, vertexCount)

	// A triangle keeps its winding when its corner sequence is a rotation of
	// the source sequence. A swap would flip the face.
	source := map[[3]uint32]int{}
	for i := 0; i+2 < len(shuffled); i += 3 {
		source[rotateToLowest(shuffled[i], shuffled[i+1], shuffled[i+2])]++
	}
	for i := 0; i+2 < len(optimized); i += 3 {
		key := rotateToLowest(optimized[i], optimized[i+1], optimized[i+2])
		if source[key] == 0 {
			t.Fatalf("triangle %v %v %v changed winding", optimized[i], optimized[i+1], optimized[i+2])
		}
		source[key]--
	}
}

func rotateToLowest(a, b, c uint32) [3]uint32 {
	if b < a && b <= c {
		return [3]uint32{b, c, a}
	}
	if c < a && c < b {
		return [3]uint32{c, a, b}
	}
	return [3]uint32{a, b, c}
}

func TestOptimizeVertexCacheLowersACMR(t *testing.T) {
	indices, _, vertexCount := gridMesh(48)
	shuffled := shuffleTriangles(indices, 3)
	floor := float64(vertexCount) / float64(len(indices)/3)
	before := ACMR(shuffled, vertexCount, DefaultCacheSize)

	for name, order := range map[string][]uint32{
		"forsyth": OptimizeVertexCache(shuffled, vertexCount),
		"tipsify": OptimizeVertexCacheTipsify(shuffled, vertexCount),
	} {
		after := ACMR(order, vertexCount, DefaultCacheSize)
		atvr := ATVR(order, vertexCount, DefaultCacheSize)
		t.Logf("%s: ACMR %.3f -> %.3f (floor %.3f), ATVR %.3f", name, before, after, floor, atvr)
		if after >= before {
			t.Fatalf("%s did not lower ACMR: %.3f -> %.3f", name, before, after)
		}
		// A grid shares every interior vertex between six triangles, so a good
		// order stays within forty percent of the theoretical floor.
		if after > floor*1.4 {
			t.Fatalf("%s ACMR %.3f is more than 1.4 times the floor %.3f", name, after, floor)
		}
		if atvr > 1.35 {
			t.Fatalf("%s ATVR %.3f means the cache refetched too many vertices", name, atvr)
		}
	}

	best, winner := OptimizeVertexCacheBest(shuffled, vertexCount)
	bestACMR := ACMR(best, vertexCount, DefaultCacheSize)
	t.Logf("best order is %s at ACMR %.3f", winner, bestACMR)
	if bestACMR > ACMR(OptimizeVertexCache(shuffled, vertexCount), vertexCount, DefaultCacheSize) {
		t.Fatal("the selector must never pick a worse order than one it compared")
	}
	if len(best) != len(shuffled) {
		t.Fatalf("the selected order changed the index count: %d, want %d", len(best), len(shuffled))
	}
}

func TestOptimizeVertexCacheBestNeverRegresses(t *testing.T) {
	// A mesh whose authored order is already ideal must survive untouched in
	// the metric, whichever candidate wins.
	indices, _, vertexCount := gridMesh(8)
	authored := ACMR(indices, vertexCount, DefaultCacheSize)
	best, winner := OptimizeVertexCacheBest(indices, vertexCount)
	after := ACMR(best, vertexCount, DefaultCacheSize)
	t.Logf("authored ACMR %.3f, selected %s at %.3f", authored, winner, after)
	if after > authored {
		t.Fatalf("selector regressed ACMR: %.3f -> %.3f", authored, after)
	}
}

func TestTipsifyKeepsEveryTriangle(t *testing.T) {
	indices, _, vertexCount := gridMesh(20)
	shuffled := shuffleTriangles(indices, 21)
	optimized := OptimizeVertexCacheTipsify(shuffled, vertexCount)
	if len(optimized) != len(shuffled) {
		t.Fatalf("index count changed: %d, want %d", len(optimized), len(shuffled))
	}
	before := triangleSet(shuffled)
	after := triangleSet(optimized)
	for key, count := range before {
		if after[key] != count {
			t.Fatalf("triangle %v appears %d times, want %d", key, after[key], count)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("distinct triangles changed: %d, want %d", len(after), len(before))
	}
}

func TestOptimizeVertexCacheHandlesAlreadyGoodOrder(t *testing.T) {
	indices, _, vertexCount := gridMesh(32)
	before := ACMR(indices, vertexCount, DefaultCacheSize)
	optimized := OptimizeVertexCache(indices, vertexCount)
	after := ACMR(optimized, vertexCount, DefaultCacheSize)
	t.Logf("row order grid ACMR %.3f -> %.3f", before, after)
	if after > before+0.05 {
		t.Fatalf("optimizer made a good order worse: %.3f -> %.3f", before, after)
	}
}

func TestOptimizeVertexCacheRejectsBadVertexCount(t *testing.T) {
	indices := []uint32{0, 1, 2}
	optimized := OptimizeVertexCache(indices, 2)
	if len(optimized) != 3 || optimized[2] != 2 {
		t.Fatalf("an out of range index must leave the list unchanged: %v", optimized)
	}
}

func TestCacheMissesMatchesADirectFIFO(t *testing.T) {
	indices, _, vertexCount := gridMesh(10)
	shuffled := shuffleTriangles(indices, 5)
	for _, size := range []int{4, 8, 16, 32} {
		want := directFIFOMisses(shuffled, size)
		got := CacheMisses(shuffled, vertexCount, size)
		if got != want {
			t.Fatalf("cache size %d: misses %d, want %d", size, got, want)
		}
	}
}

// directFIFOMisses is a plain queue simulation. It shares no code with the
// timestamp model CacheMisses uses, so it checks that model rather than
// repeating it.
func directFIFOMisses(indices []uint32, size int) int {
	queue := make([]uint32, 0, size)
	misses := 0
	for _, index := range indices {
		found := false
		for _, resident := range queue {
			if resident == index {
				found = true
				break
			}
		}
		if found {
			continue
		}
		misses++
		if len(queue) == size {
			queue = queue[1:]
		}
		queue = append(queue, index)
	}
	return misses
}

func TestOptimizeVertexFetchOrdersTheVertexBuffer(t *testing.T) {
	indices, positions, vertexCount := gridMesh(16)
	shuffled := shuffleTriangles(indices, 9)
	optimized := OptimizeVertexCache(shuffled, vertexCount)

	beforeFetch := FetchOverfetch(optimized, vertexCount, 12, 64)
	newIndices, remap, used := OptimizeVertexFetch(optimized, vertexCount)
	if used != vertexCount {
		t.Fatalf("a closed grid uses every vertex: %d of %d", used, vertexCount)
	}
	newPositions := ApplyRemapFloat32(positions, 3, remap, used)
	afterFetch := FetchOverfetch(newIndices, used, 12, 64)
	t.Logf("fetch overfetch %.3f -> %.3f", beforeFetch, afterFetch)
	if afterFetch > beforeFetch {
		t.Fatalf("vertex fetch got worse: %.3f -> %.3f", beforeFetch, afterFetch)
	}

	// The renumbering must not move a single vertex position.
	for i := 0; i+2 < len(newIndices); i += 3 {
		for corner := 0; corner < 3; corner++ {
			oldIndex := optimized[i+corner]
			newIndex := newIndices[i+corner]
			for axis := 0; axis < 3; axis++ {
				if newPositions[int(newIndex)*3+axis] != positions[int(oldIndex)*3+axis] {
					t.Fatalf("vertex %d moved after the fetch pass", oldIndex)
				}
			}
		}
	}
	if ACMR(newIndices, used, DefaultCacheSize) != ACMR(optimized, vertexCount, DefaultCacheSize) {
		t.Fatal("the fetch pass must not change the cache order")
	}
}

func TestOptimizeVertexFetchDropsUnusedVertices(t *testing.T) {
	indices := []uint32{0, 1, 2, 2, 1, 3}
	_, remap, used := OptimizeVertexFetch(indices, 6)
	if used != 4 {
		t.Fatalf("used vertices = %d, want 4", used)
	}
	if remap[4] != -1 || remap[5] != -1 {
		t.Fatalf("unused vertices must map to -1: %v", remap)
	}
}
