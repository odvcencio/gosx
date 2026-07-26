// Package quantize turns floating point vertex attributes into small integers
// and reports the error the change introduces.
//
// The package implements what KHR_mesh_quantization allows. A quantized
// accessor is an ordinary glTF accessor with a small component type, so a
// loader reads it with no decoder. Positions carry the scale and the offset in
// a node transform, and the other attributes use the normalized flag.
package quantize

import "math"

// PositionGrid maps positions to unsigned integers on a uniform lattice.
//
// The grid uses one scale for all three axes. A per-axis scale would pack a
// little tighter, but it would also make the node transform non-uniform, which
// skews normals and tangents. A uniform scale keeps the normal matrix a pure
// rotation.
type PositionGrid struct {
	// Offset is the lattice origin in source units.
	Offset [3]float64
	// Scale is the size of one lattice step in source units. One value covers
	// all three axes.
	//
	// A sibling implementation at m31labs.dev/turboquant/mesh keeps a per-axis
	// scale in PositionParams.Scale. The two encodings are not interchangeable:
	// a mesh this package encodes decodes to the wrong shape through the
	// per-axis reader, and the reverse holds too. The uniform choice here is
	// deliberate. KHR_mesh_quantization folds the dequantization into the glTF
	// node transform, and a non-uniform node scale corrupts the normal matrix
	// for every consumer of the file. This grid serves glTF. The per-axis grid
	// serves a stream format that carries its own decoder.
	Scale float64
	// Bits is the width of one component.
	Bits int
}

// FitPositionGrid builds a grid that covers every position. bits selects the
// component width, 8 or 16.
func FitPositionGrid(positions []float64, bits int) PositionGrid {
	if bits != 8 {
		bits = 16
	}
	grid := PositionGrid{Bits: bits}
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+2 < len(positions); i += 3 {
		for axis := 0; axis < 3; axis++ {
			value := positions[i+axis]
			if math.IsNaN(value) {
				value = 0
			}
			low[axis] = math.Min(low[axis], value)
			high[axis] = math.Max(high[axis], value)
		}
	}
	if math.IsInf(low[0], 1) {
		return PositionGrid{Scale: 1, Bits: bits}
	}
	extent := 0.0
	for axis := 0; axis < 3; axis++ {
		grid.Offset[axis] = low[axis]
		extent = math.Max(extent, high[axis]-low[axis])
	}
	steps := float64(int(1)<<bits - 1)
	if extent <= 0 {
		grid.Scale = 1
		return grid
	}
	// Round the scale up to the next representable float32 step. A scale that
	// float32 rounds down would place the far corner one step outside the
	// lattice, so the decoded bounds would no longer contain the source.
	grid.Scale = nextFloat32Up(extent / steps)
	return grid
}

// Encode returns the lattice coordinate of one position.
func (g PositionGrid) Encode(x, y, z float64) [3]int32 {
	steps := float64(int(1)<<g.Bits - 1)
	source := [3]float64{x, y, z}
	var out [3]int32
	for axis := 0; axis < 3; axis++ {
		value := source[axis]
		if math.IsNaN(value) {
			value = 0
		}
		step := math.Round((value - g.Offset[axis]) / g.Scale)
		out[axis] = int32(math.Max(0, math.Min(steps, step)))
	}
	return out
}

// Decode returns the position a lattice coordinate stands for.
func (g PositionGrid) Decode(q [3]int32) [3]float64 {
	var out [3]float64
	for axis := 0; axis < 3; axis++ {
		out[axis] = float64(q[axis])*g.Scale + g.Offset[axis]
	}
	return out
}

// EncodeStream quantizes a VEC3 position stream. It returns the lattice
// coordinates in component order.
func (g PositionGrid) EncodeStream(positions []float64) []int32 {
	out := make([]int32, 0, len(positions))
	for i := 0; i+2 < len(positions); i += 3 {
		q := g.Encode(positions[i], positions[i+1], positions[i+2])
		out = append(out, q[0], q[1], q[2])
	}
	return out
}

// PositionError reports how far a decoded position sits from its source.
type PositionError struct {
	// Max is the largest distance in source units.
	Max float64
	// RMS is the root mean square distance in source units.
	RMS float64
	// Bound is half the lattice step times the square root of three, which is
	// the largest distance rounding can produce. A measured Max above Bound
	// means the encoder is wrong.
	Bound float64
	// SourceLow and SourceHigh hold the source bounding box.
	SourceLow, SourceHigh [3]float64
	// DecodedLow and DecodedHigh hold the bounding box after the round trip.
	DecodedLow, DecodedHigh [3]float64
	// Step is the size of one lattice step in source units.
	Step float64
}

// Contains reports whether the decoded bounding box still holds every source
// corner, allowing half a lattice step on each face.
//
// The tolerance is not slack. Rounding to the nearest lattice point can pull an
// extreme vertex inward by up to half a step, and no uniform lattice avoids
// that. The check therefore catches the failure this stage must never ship: a
// scale or an offset that shrinks the whole mesh. It does not fight the
// half-step that rounding owns.
func (e PositionError) Contains() bool {
	tolerance := 0.5*e.Step + 1e-12
	for axis := 0; axis < 3; axis++ {
		if e.DecodedLow[axis] > e.SourceLow[axis]+tolerance {
			return false
		}
		if e.DecodedHigh[axis] < e.SourceHigh[axis]-tolerance {
			return false
		}
	}
	return true
}

// ExtentDelta reports how much each axis of the bounding box grew or shrank
// through the round trip. A correct grid keeps every axis within one lattice
// step. A wrong step count shows up here as a large negative number, which is
// the systematic shrink Contains alone could hide.
func (e PositionError) ExtentDelta() [3]float64 {
	var out [3]float64
	for axis := 0; axis < 3; axis++ {
		source := e.SourceHigh[axis] - e.SourceLow[axis]
		decoded := e.DecodedHigh[axis] - e.DecodedLow[axis]
		out[axis] = decoded - source
	}
	return out
}

// MeasurePositionError runs the round trip and reports the error against the
// source stream. float32 storage is part of the round trip, because a glTF node
// transform holds float32 numbers.
func (g PositionGrid) MeasurePositionError(positions []float64) PositionError {
	out := PositionError{Bound: 0.5 * g.Scale * math.Sqrt(3), Step: g.Scale}
	for axis := 0; axis < 3; axis++ {
		out.SourceLow[axis] = math.Inf(1)
		out.SourceHigh[axis] = math.Inf(-1)
		out.DecodedLow[axis] = math.Inf(1)
		out.DecodedHigh[axis] = math.Inf(-1)
	}
	// A glTF node stores the scale and the translation as float32, so the
	// measurement must use the same values the loader will see.
	stored := PositionGrid{Scale: float64(float32(g.Scale)), Bits: g.Bits}
	for axis := 0; axis < 3; axis++ {
		stored.Offset[axis] = float64(float32(g.Offset[axis]))
	}
	total := 0.0
	count := 0
	for i := 0; i+2 < len(positions); i += 3 {
		q := g.Encode(positions[i], positions[i+1], positions[i+2])
		decoded := stored.Decode(q)
		squared := 0.0
		for axis := 0; axis < 3; axis++ {
			source := positions[i+axis]
			out.SourceLow[axis] = math.Min(out.SourceLow[axis], source)
			out.SourceHigh[axis] = math.Max(out.SourceHigh[axis], source)
			out.DecodedLow[axis] = math.Min(out.DecodedLow[axis], decoded[axis])
			out.DecodedHigh[axis] = math.Max(out.DecodedHigh[axis], decoded[axis])
			delta := decoded[axis] - source
			squared += delta * delta
		}
		out.Max = math.Max(out.Max, math.Sqrt(squared))
		total += squared
		count++
	}
	if count > 0 {
		out.RMS = math.Sqrt(total / float64(count))
	}
	return out
}

// nextFloat32Up returns the smallest float32 that is not below value.
func nextFloat32Up(value float64) float64 {
	rounded := float64(float32(value))
	if rounded >= value {
		return rounded
	}
	return float64(math.Nextafter32(float32(rounded), float32(math.Inf(1))))
}
