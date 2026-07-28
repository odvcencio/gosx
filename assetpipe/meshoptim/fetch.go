package meshoptim

// OptimizeVertexFetch renumbers vertices so the index list reads the vertex
// buffer front to back. It returns the new index list and a remap table.
//
// remap[old] holds the new index of an old vertex, or -1 when no triangle uses
// it. The caller must reorder every vertex stream with the same table. The
// used count is the length of the new vertex streams, so the pass also drops
// vertices no triangle reaches.
func OptimizeVertexFetch(indices []uint32, vertexCount int) (newIndices []uint32, remap []int32, used int) {
	remap = make([]int32, vertexCount)
	for i := range remap {
		remap[i] = -1
	}
	newIndices = make([]uint32, 0, len(indices))
	for _, index := range indices {
		if int(index) >= vertexCount {
			// Keep an illegal index visible rather than silently moving it.
			newIndices = append(newIndices, index)
			continue
		}
		if remap[index] < 0 {
			remap[index] = int32(used)
			used++
		}
		newIndices = append(newIndices, uint32(remap[index]))
	}
	return newIndices, remap, used
}

// ApplyRemapFloat32 builds a new attribute stream in the order remap names.
// components is the component count of one vertex.
func ApplyRemapFloat32(values []float32, components int, remap []int32, used int) []float32 {
	out := make([]float32, used*components)
	for old, next := range remap {
		if next < 0 {
			continue
		}
		src := old * components
		dst := int(next) * components
		if src+components > len(values) {
			continue
		}
		copy(out[dst:dst+components], values[src:src+components])
	}
	return out
}

// ApplyRemapFloat64 is ApplyRemapFloat32 for float64 streams.
func ApplyRemapFloat64(values []float64, components int, remap []int32, used int) []float64 {
	out := make([]float64, used*components)
	for old, next := range remap {
		if next < 0 {
			continue
		}
		src := old * components
		dst := int(next) * components
		if src+components > len(values) {
			continue
		}
		copy(out[dst:dst+components], values[src:src+components])
	}
	return out
}
