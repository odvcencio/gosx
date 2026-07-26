package meshoptim

import (
	"encoding/binary"
	"hash/maphash"
	"math"
)

// Stream is one vertex attribute the welder compares.
type Stream struct {
	// Values holds Components entries per vertex.
	Values []float64
	// Components is the component count of one vertex.
	Components int
	// Quantum snaps a value to a grid before the comparison. Zero compares the
	// exact bits, which is the safe default.
	Quantum float64
}

// Weld merges vertices whose every stream matches. It returns remap[old] with
// the surviving vertex index and the count of surviving vertices.
//
// A non-zero Quantum snaps values to a grid before the comparison. Two
// vertices that sit on opposite sides of a grid line do not merge, even when
// they are closer than the quantum. Run Weld after quantization to avoid that
// case: quantized values already sit on the grid, so equal values are bitwise
// equal.
func Weld(vertexCount int, streams []Stream) (remap []uint32, unique int) {
	remap = make([]uint32, vertexCount)
	if vertexCount <= 0 {
		return remap, 0
	}
	if len(streams) == 0 {
		for i := range remap {
			remap[i] = uint32(i)
		}
		return remap, vertexCount
	}

	seed := maphash.MakeSeed()
	buckets := make(map[uint64][]int, vertexCount)
	key := make([]byte, 0, 64)
	canonical := make([]float64, 0, 16)

	for vertex := 0; vertex < vertexCount; vertex++ {
		canonical = canonical[:0]
		key = key[:0]
		for _, stream := range streams {
			for c := 0; c < stream.Components; c++ {
				index := vertex*stream.Components + c
				value := 0.0
				if index < len(stream.Values) {
					value = stream.Values[index]
				}
				value = snap(value, stream.Quantum)
				canonical = append(canonical, value)
				var scratch [8]byte
				binary.LittleEndian.PutUint64(scratch[:], math.Float64bits(value))
				key = append(key, scratch[:]...)
			}
		}
		hash := maphash.Bytes(seed, key)
		match := -1
		for _, candidate := range buckets[hash] {
			if sameVertex(candidate, canonical, streams) {
				match = candidate
				break
			}
		}
		if match >= 0 {
			remap[vertex] = remap[match]
			continue
		}
		buckets[hash] = append(buckets[hash], vertex)
		remap[vertex] = uint32(unique)
		unique++
	}
	return remap, unique
}

// sameVertex compares one candidate vertex against a canonical value list. The
// hash can collide, so the welder still checks the values.
func sameVertex(candidate int, canonical []float64, streams []Stream) bool {
	offset := 0
	for _, stream := range streams {
		for c := 0; c < stream.Components; c++ {
			index := candidate*stream.Components + c
			value := 0.0
			if index < len(stream.Values) {
				value = stream.Values[index]
			}
			if snap(value, stream.Quantum) != canonical[offset] {
				return false
			}
			offset++
		}
	}
	return true
}

// snap moves a value onto a grid and removes the two representations of zero.
func snap(value, quantum float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	if quantum > 0 {
		value = math.Round(value/quantum) * quantum
	}
	if value == 0 {
		return 0
	}
	return value
}

// ApplyWeld rewrites an index list through a weld remap.
func ApplyWeld(indices []uint32, remap []uint32) []uint32 {
	out := make([]uint32, len(indices))
	for i, index := range indices {
		if int(index) < len(remap) {
			out[i] = remap[index]
			continue
		}
		out[i] = index
	}
	return out
}

// CollapseWeld builds the surviving vertex stream for a weld remap. It keeps
// the value of the first vertex that reached each surviving slot.
func CollapseWeld(values []float64, components int, remap []uint32, unique int) []float64 {
	out := make([]float64, unique*components)
	written := make([]bool, unique)
	for old, next := range remap {
		if int(next) >= unique || written[next] {
			continue
		}
		written[next] = true
		src := old * components
		dst := int(next) * components
		if src+components > len(values) {
			continue
		}
		copy(out[dst:dst+components], values[src:src+components])
	}
	return out
}

// DropDegenerate removes triangles that name the same vertex twice. Welding
// can collapse a thin triangle into a line, and a line draws nothing.
func DropDegenerate(indices []uint32) []uint32 {
	out := make([]uint32, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		if a == b || b == c || a == c {
			continue
		}
		out = append(out, a, b, c)
	}
	return out
}
