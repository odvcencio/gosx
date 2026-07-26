package meshoptim

// TipsifyCacheSize is the cache the Tipsify walk assumes. The published
// algorithm is not very sensitive to the value, and a size near the reported
// measurement cache keeps the strips narrow enough for small caches.
const TipsifyCacheSize = 16

// OptimizeVertexCacheTipsify reorders triangles with the Tipsify walk of
// Sander, Nehab and Barczak. It returns a new index slice and never changes the
// input.
//
// The walk emits every unemitted triangle around one fusion vertex, then picks
// the next fusion vertex from the ring it just touched. It prefers a vertex
// whose remaining triangles still fit in the cache beside it, which is what
// keeps the strips narrow.
func OptimizeVertexCacheTipsify(indices []uint32, vertexCount int) []uint32 {
	triangleCount := len(indices) / 3
	out := make([]uint32, 0, triangleCount*3)
	if triangleCount == 0 || vertexCount <= 0 {
		return append(out, indices[:triangleCount*3]...)
	}
	for _, index := range indices[:triangleCount*3] {
		if int(index) >= vertexCount {
			return append(out, indices[:triangleCount*3]...)
		}
	}

	offsets := make([]int32, vertexCount+1)
	for _, index := range indices[:triangleCount*3] {
		offsets[index+1]++
	}
	for i := 1; i <= vertexCount; i++ {
		offsets[i] += offsets[i-1]
	}
	adjacency := make([]int32, triangleCount*3)
	fill := make([]int32, vertexCount)
	live := make([]int32, vertexCount)
	for triangle := 0; triangle < triangleCount; triangle++ {
		for corner := 0; corner < 3; corner++ {
			vertex := indices[triangle*3+corner]
			adjacency[offsets[vertex]+fill[vertex]] = int32(triangle)
			fill[vertex]++
			live[vertex]++
		}
	}

	stamp := make([]int, vertexCount)
	for i := range stamp {
		stamp[i] = -1 << 30
	}
	emitted := make([]bool, triangleCount)
	deadEnd := make([]uint32, 0, triangleCount*3)
	candidates := make([]uint32, 0, 32)
	clock := 0
	scan := 0
	fusion := 0

	for fusion >= 0 {
		candidates = candidates[:0]
		for _, triangle := range adjacency[offsets[fusion]:offsets[fusion+1]] {
			if emitted[triangle] {
				continue
			}
			emitted[triangle] = true
			base := int(triangle) * 3
			for corner := 0; corner < 3; corner++ {
				vertex := indices[base+corner]
				out = append(out, vertex)
				deadEnd = append(deadEnd, vertex)
				candidates = append(candidates, vertex)
				live[vertex]--
				if clock-stamp[vertex] > TipsifyCacheSize {
					stamp[vertex] = clock
					clock++
				}
			}
		}
		fusion, deadEnd, scan = tipsifyNextVertex(candidates, deadEnd, live, stamp, clock, scan, vertexCount)
	}
	return out
}

// tipsifyNextVertex picks the fusion vertex for the next step. It prefers a
// candidate whose remaining triangles still fit in the cache beside it, then
// falls back to the recently touched vertices, then to a forward scan.
func tipsifyNextVertex(
	candidates, deadEnd []uint32,
	live []int32,
	stamp []int,
	clock, scan, vertexCount int,
) (int, []uint32, int) {
	best := -1
	bestPriority := -1
	for _, vertex := range candidates {
		if live[vertex] <= 0 {
			continue
		}
		priority := 0
		if clock-stamp[vertex]+2*int(live[vertex]) <= TipsifyCacheSize {
			priority = clock - stamp[vertex]
		}
		if priority > bestPriority {
			bestPriority = priority
			best = int(vertex)
		}
	}
	if best >= 0 {
		return best, deadEnd, scan
	}
	for len(deadEnd) > 0 {
		vertex := deadEnd[len(deadEnd)-1]
		deadEnd = deadEnd[:len(deadEnd)-1]
		if live[vertex] > 0 {
			return int(vertex), deadEnd, scan
		}
	}
	for scan < vertexCount {
		if live[scan] > 0 {
			return scan, deadEnd, scan
		}
		scan++
	}
	return -1, deadEnd, scan
}

// OptimizeVertexCacheBest returns the best order it can find, measured with the
// average cache miss ratio. It compares the authored order against both
// optimizers, so the pass can never make a mesh worse.
//
// The returned name says which order won, which keeps the report honest about
// where the gain came from.
func OptimizeVertexCacheBest(indices []uint32, vertexCount int) ([]uint32, string) {
	type candidate struct {
		name  string
		order []uint32
	}
	options := []candidate{
		{name: "authored", order: append([]uint32(nil), indices...)},
		{name: "forsyth", order: OptimizeVertexCache(indices, vertexCount)},
		{name: "tipsify", order: OptimizeVertexCacheTipsify(indices, vertexCount)},
	}
	best := 0
	bestMisses := CacheMisses(options[0].order, vertexCount, DefaultCacheSize)
	for i := 1; i < len(options); i++ {
		if len(options[i].order) != len(indices) {
			continue
		}
		misses := CacheMisses(options[i].order, vertexCount, DefaultCacheSize)
		if misses < bestMisses {
			bestMisses = misses
			best = i
		}
	}
	return options[best].order, options[best].name
}
