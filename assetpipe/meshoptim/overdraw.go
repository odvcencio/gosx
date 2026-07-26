package meshoptim

import (
	"math"
	"sort"
)

// OptimizeOverdraw reorders triangles front to back while it keeps most of the
// vertex cache gain. It works on a cache-optimized index list.
//
// The pass first cuts the index list into clusters. A cluster ends when the
// running average cache miss ratio would rise above threshold times the ratio
// of the whole list, so a cluster is a run of triangles that already share
// vertices. The pass then sorts whole clusters, never single triangles, so the
// cache order inside a cluster survives.
//
// A cluster sorts by how far it faces away from the mesh centre. A surface
// that faces outward is the surface an outside viewer sees first, so it draws
// first and fills the depth buffer for the surfaces behind it.
func OptimizeOverdraw(indices []uint32, positions []float32, vertexCount int, threshold float64) []uint32 {
	triangleCount := len(indices) / 3
	if triangleCount < 2 || vertexCount <= 0 || len(positions) < vertexCount*3 {
		return append([]uint32(nil), indices[:triangleCount*3]...)
	}
	if threshold < 1 {
		threshold = 1
	}

	clusters := buildClusters(indices, vertexCount, threshold)
	order := sortClusters(indices, positions, clusters, triangleCount)

	out := make([]uint32, 0, triangleCount*3)
	for _, cluster := range order {
		start := clusters[cluster]
		end := triangleCount
		if cluster+1 < len(clusters) {
			end = clusters[cluster+1]
		}
		out = append(out, indices[start*3:end*3]...)
	}
	return out
}

// buildClusters returns the first triangle of every cluster.
func buildClusters(indices []uint32, vertexCount int, threshold float64) []int {
	triangleCount := len(indices) / 3
	target := ACMR(indices, vertexCount, DefaultCacheSize) * threshold
	if target <= 0 {
		target = 3
	}

	clusters := []int{0}
	stamp := make([]int, vertexCount)
	for i := range stamp {
		stamp[i] = -1 << 30
	}
	misses := 0
	position := 0
	clusterTriangles := 0
	for triangle := 0; triangle < triangleCount; triangle++ {
		for corner := 0; corner < 3; corner++ {
			index := indices[triangle*3+corner]
			if int(index) >= vertexCount {
				continue
			}
			if position-stamp[index] > DefaultCacheSize {
				misses++
				stamp[index] = position
				position++
			}
		}
		clusterTriangles++
		// Start a new cluster once this run holds enough triangles and its
		// ratio already sits at the target. A short cluster would break the
		// cache order for no gain.
		if clusterTriangles >= 8 && float64(misses)/float64(clusterTriangles) <= target {
			clusters = append(clusters, triangle+1)
			misses = 0
			position = 0
			clusterTriangles = 0
			for i := range stamp {
				stamp[i] = -1 << 30
			}
		}
	}
	if clusters[len(clusters)-1] >= triangleCount {
		clusters = clusters[:len(clusters)-1]
	}
	return clusters
}

// sortClusters returns cluster indices in draw order.
func sortClusters(indices []uint32, positions []float32, clusters []int, triangleCount int) []int {
	var centre [3]float64
	total := 0.0
	for i := 0; i+2 < len(indices); i += 3 {
		for corner := 0; corner < 3; corner++ {
			index := int(indices[i+corner]) * 3
			if index+2 >= len(positions) {
				continue
			}
			centre[0] += float64(positions[index])
			centre[1] += float64(positions[index+1])
			centre[2] += float64(positions[index+2])
			total++
		}
	}
	if total > 0 {
		centre[0] /= total
		centre[1] /= total
		centre[2] /= total
	}

	keys := make([]float64, len(clusters))
	for cluster := range clusters {
		start := clusters[cluster]
		end := triangleCount
		if cluster+1 < len(clusters) {
			end = clusters[cluster+1]
		}
		var normal, centroid [3]float64
		area := 0.0
		for triangle := start; triangle < end; triangle++ {
			a := vertexAt(positions, indices[triangle*3])
			b := vertexAt(positions, indices[triangle*3+1])
			c := vertexAt(positions, indices[triangle*3+2])
			cross := crossProduct(sub(b, a), sub(c, a))
			weight := math.Sqrt(cross[0]*cross[0]+cross[1]*cross[1]+cross[2]*cross[2]) * 0.5
			for axis := 0; axis < 3; axis++ {
				normal[axis] += cross[axis]
				centroid[axis] += (a[axis] + b[axis] + c[axis]) / 3 * weight
			}
			area += weight
		}
		if area > 0 {
			for axis := 0; axis < 3; axis++ {
				centroid[axis] /= area
			}
		}
		length := math.Sqrt(normal[0]*normal[0] + normal[1]*normal[1] + normal[2]*normal[2])
		if length > 0 {
			for axis := 0; axis < 3; axis++ {
				normal[axis] /= length
			}
		}
		direction := sub(centroid, centre)
		reach := math.Sqrt(direction[0]*direction[0] + direction[1]*direction[1] + direction[2]*direction[2])
		if reach > 0 {
			for axis := 0; axis < 3; axis++ {
				direction[axis] /= reach
			}
		}
		// A large key means the cluster faces away from the mesh centre, so an
		// outside viewer meets it first.
		keys[cluster] = direction[0]*normal[0] + direction[1]*normal[1] + direction[2]*normal[2]
	}

	order := make([]int, len(clusters))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return keys[order[i]] > keys[order[j]] })
	return order
}

func vertexAt(positions []float32, index uint32) [3]float64 {
	base := int(index) * 3
	if base+2 >= len(positions) {
		return [3]float64{}
	}
	return [3]float64{float64(positions[base]), float64(positions[base+1]), float64(positions[base+2])}
}

func sub(a, b [3]float64) [3]float64 {
	return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func crossProduct(a, b [3]float64) [3]float64 {
	return [3]float64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}
