// Package meshoptim reorders and merges mesh index and vertex data. Every
// function here changes only the order or the identity of vertices. No
// function changes a vertex value, so the rendered result stays the same.
//
// The package holds no glTF knowledge. It takes plain slices, so a test can
// drive it with a synthetic mesh and check a metric directly.
package meshoptim

import "math"

// Post-transform vertex cache model. Forsyth's linear-speed optimizer scores a
// vertex by how recently the cache saw it and by how many triangles still need
// it. The constants come from the published algorithm.
const (
	forsythCacheSize         = 32
	forsythDecayPower        = 1.5
	forsythLastTriScore      = 0.75
	forsythValenceBoostScale = 2.0
	forsythValenceBoostPower = 0.5
	// forsythValenceTable caps the precomputed valence score table. A vertex
	// with more triangles falls back to the direct computation.
	forsythValenceTable = 64
)

var (
	cachePositionScore [forsythCacheSize]float32
	valenceScore       [forsythValenceTable]float32
)

func init() {
	for position := 0; position < forsythCacheSize; position++ {
		switch {
		case position < 3:
			cachePositionScore[position] = forsythLastTriScore
		default:
			scaler := 1.0 / float64(forsythCacheSize-3)
			value := 1.0 - float64(position-3)*scaler
			cachePositionScore[position] = float32(math.Pow(value, forsythDecayPower))
		}
	}
	for live := 1; live < forsythValenceTable; live++ {
		valenceScore[live] = float32(forsythValenceBoostScale * math.Pow(float64(live), -forsythValenceBoostPower))
	}
}

// vertexScore rates one vertex. cachePosition is -1 when the cache does not
// hold the vertex. A vertex with no remaining triangle scores -1, so no
// triangle picks it up again.
func vertexScore(cachePosition int, live int32) float32 {
	if live <= 0 {
		return -1
	}
	var score float32
	if cachePosition >= 0 && cachePosition < forsythCacheSize {
		score = cachePositionScore[cachePosition]
	}
	if int(live) < forsythValenceTable {
		return score + valenceScore[live]
	}
	return score + float32(forsythValenceBoostScale*math.Pow(float64(live), -forsythValenceBoostPower))
}

// OptimizeVertexCache reorders triangles so the post-transform vertex cache
// reuses a transformed vertex more often. It returns a new index slice and
// never changes the input. The triangle set stays the same, and the winding of
// every triangle stays the same.
//
// The function runs Tom Forsyth's linear-speed vertex cache optimization.
func OptimizeVertexCache(indices []uint32, vertexCount int) []uint32 {
	triangleCount := len(indices) / 3
	out := make([]uint32, 0, triangleCount*3)
	if triangleCount == 0 || vertexCount <= 0 {
		return append(out, indices[:triangleCount*3]...)
	}

	// Build the vertex-to-triangle adjacency in one flat array.
	offsets := make([]int32, vertexCount+1)
	for _, index := range indices[:triangleCount*3] {
		if int(index) >= vertexCount {
			// An out-of-range index means the caller lied about vertexCount.
			// Return the input unchanged rather than corrupt the mesh.
			return append(out, indices[:triangleCount*3]...)
		}
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

	cachePosition := make([]int32, vertexCount)
	scores := make([]float32, vertexCount)
	for vertex := 0; vertex < vertexCount; vertex++ {
		cachePosition[vertex] = -1
		scores[vertex] = vertexScore(-1, live[vertex])
	}
	triangleScore := make([]float32, triangleCount)
	emitted := make([]bool, triangleCount)
	best := -1
	bestScore := float32(-1)
	for triangle := 0; triangle < triangleCount; triangle++ {
		score := scores[indices[triangle*3]] + scores[indices[triangle*3+1]] + scores[indices[triangle*3+2]]
		triangleScore[triangle] = score
		if score > bestScore {
			bestScore = score
			best = triangle
		}
	}

	// The cache keeps three extra slots. A vertex that just fell out still
	// needs a score update, so the optimizer must still see it.
	cache := make([]uint32, 0, forsythCacheSize+3)
	scratch := make([]uint32, 0, forsythCacheSize+3)
	deadEnd := make([]uint32, 0, triangleCount*3)
	scan := 0

	for emittedCount := 0; emittedCount < triangleCount; emittedCount++ {
		if best < 0 {
			best, deadEnd, scan = nextTriangle(emitted, live, triangleScore, adjacency, offsets, deadEnd, scan, triangleCount)
			if best < 0 {
				break
			}
		}
		triangle := best
		emitted[triangle] = true
		v0, v1, v2 := indices[triangle*3], indices[triangle*3+1], indices[triangle*3+2]
		out = append(out, v0, v1, v2)
		live[v0]--
		live[v1]--
		live[v2]--
		deadEnd = append(deadEnd, v0, v1, v2)

		// Move the three vertices to the front of the cache, keeping the rest
		// in order.
		scratch = scratch[:0]
		scratch = append(scratch, v0, v1, v2)
		for _, vertex := range cache {
			if vertex == v0 || vertex == v1 || vertex == v2 {
				continue
			}
			if len(scratch) == forsythCacheSize+3 {
				break
			}
			scratch = append(scratch, vertex)
		}
		// Vertices that leave the tracked window lose their cache position and
		// drop back to the uncached score.
		for _, vertex := range cache {
			cachePosition[vertex] = -1
			scores[vertex] = vertexScore(-1, live[vertex])
		}
		cache = append(cache[:0], scratch...)
		for position, vertex := range cache {
			cachePosition[vertex] = int32(position)
		}

		// Rescore every vertex the cache still tracks and every triangle that
		// touches one of them.
		best = -1
		bestScore = -1
		for _, vertex := range cache {
			scores[vertex] = vertexScore(int(cachePosition[vertex]), live[vertex])
		}
		for _, vertex := range cache {
			for _, candidate := range adjacency[offsets[vertex]:offsets[vertex+1]] {
				if emitted[candidate] {
					continue
				}
				score := scores[indices[candidate*3]] + scores[indices[candidate*3+1]] + scores[indices[candidate*3+2]]
				triangleScore[candidate] = score
				if score > bestScore {
					bestScore = score
					best = int(candidate)
				}
			}
		}
	}
	return out
}

// nextTriangle finds work after a dead end. It first retries vertices the
// optimizer touched recently, then walks the triangle list once from the
// front. The scan cursor never moves backwards, so the whole search stays
// linear.
func nextTriangle(
	emitted []bool,
	live []int32,
	triangleScore []float32,
	adjacency []int32,
	offsets []int32,
	deadEnd []uint32,
	scan int,
	triangleCount int,
) (int, []uint32, int) {
	for len(deadEnd) > 0 {
		vertex := deadEnd[len(deadEnd)-1]
		deadEnd = deadEnd[:len(deadEnd)-1]
		if live[vertex] <= 0 {
			continue
		}
		best := -1
		bestScore := float32(-1)
		for _, candidate := range adjacency[offsets[vertex]:offsets[vertex+1]] {
			if emitted[candidate] {
				continue
			}
			if triangleScore[candidate] > bestScore {
				bestScore = triangleScore[candidate]
				best = int(candidate)
			}
		}
		if best >= 0 {
			return best, deadEnd, scan
		}
	}
	for scan < triangleCount {
		if !emitted[scan] {
			return scan, deadEnd, scan
		}
		scan++
	}
	return -1, deadEnd, scan
}
