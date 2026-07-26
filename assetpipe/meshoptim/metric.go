package meshoptim

// DefaultCacheSize is the cache size the reported ACMR uses. Hardware vertex
// caches differ, so the number is a model, not a measurement of one GPU. A
// 16-entry FIFO is the usual reporting size, and it separates a good order
// from a bad one clearly.
const DefaultCacheSize = 16

// CacheMisses counts how many vertices a first-in-first-out post-transform
// cache of cacheSize entries must fetch for this index order.
//
// The simulation keeps the position at which the cache last saw a vertex. A
// vertex stays resident while fewer than cacheSize other vertices entered
// after it, which is exactly first-in-first-out behaviour.
func CacheMisses(indices []uint32, vertexCount, cacheSize int) int {
	if cacheSize <= 0 {
		cacheSize = DefaultCacheSize
	}
	if vertexCount <= 0 || len(indices) == 0 {
		return 0
	}
	stamp := make([]int, vertexCount)
	for i := range stamp {
		stamp[i] = -1 << 30
	}
	misses := 0
	position := 0
	for _, index := range indices {
		if int(index) >= vertexCount {
			continue
		}
		// stamp holds the insertion count at the moment the cache took the
		// vertex. The vertex leaves once cacheSize later insertions pushed past
		// it, so the difference must exceed cacheSize, not reach it.
		if position-stamp[index] > cacheSize {
			misses++
			stamp[index] = position
			position++
		}
	}
	return misses
}

// ACMR reports the average cache miss ratio, that is misses per triangle. A
// perfect order approaches 0.5 and an unordered mesh approaches 3.0.
func ACMR(indices []uint32, vertexCount, cacheSize int) float64 {
	triangles := len(indices) / 3
	if triangles == 0 {
		return 0
	}
	return float64(CacheMisses(indices, vertexCount, cacheSize)) / float64(triangles)
}

// ATVR reports the average transform to vertex ratio, that is misses per
// unique vertex. A perfect order reaches 1.0.
func ATVR(indices []uint32, vertexCount, cacheSize int) float64 {
	if vertexCount == 0 {
		return 0
	}
	used := make([]bool, vertexCount)
	unique := 0
	for _, index := range indices {
		if int(index) >= vertexCount || used[index] {
			continue
		}
		used[index] = true
		unique++
	}
	if unique == 0 {
		return 0
	}
	return float64(CacheMisses(indices, vertexCount, cacheSize)) / float64(unique)
}

// FetchOverfetch reports how many bytes of vertex data a linear reader must
// touch, divided by the bytes the mesh holds. It models a cache line of
// lineBytes over a tightly packed vertex buffer of vertexSize bytes. A value
// of 1.0 means the reader touched every byte exactly once.
func FetchOverfetch(indices []uint32, vertexCount, vertexSize, lineBytes int) float64 {
	if vertexCount <= 0 || vertexSize <= 0 || lineBytes <= 0 || len(indices) == 0 {
		return 0
	}
	lines := (vertexCount*vertexSize + lineBytes - 1) / lineBytes
	// A small cache of 16 lines models the memory system in front of the
	// vertex fetch unit.
	const cacheLines = 16
	stamp := make([]int, lines)
	for i := range stamp {
		stamp[i] = -1 << 30
	}
	fetched := 0
	position := 0
	for _, index := range indices {
		if int(index) >= vertexCount {
			continue
		}
		first := int(index) * vertexSize / lineBytes
		last := (int(index)*vertexSize + vertexSize - 1) / lineBytes
		for line := first; line <= last && line < lines; line++ {
			if position-stamp[line] > cacheLines {
				fetched++
				stamp[line] = position
				position++
			}
		}
	}
	return float64(fetched*lineBytes) / float64(vertexCount*vertexSize)
}
