package field

// QuantizeOptions configures the wire format produced by Field.Quantize.
type QuantizeOptions struct {
	// BitWidth is the bits per coordinate, 4..8. Lower means smaller wire,
	// larger reconstruction error.
	BitWidth int

	// PreviewBits, if > 0 and < BitWidth, also emits a preview at PreviewBits
	// for progressive loading. The preview is stored in Quantized.Preview.
	PreviewBits int

	// DeltaAgainst, if non-nil, encodes the field as a delta against the
	// supplied previous field. Both fields must have identical Resolution
	// and Components.
	DeltaAgainst *Field
}

// Quantized is the wire format for a Field.
type Quantized struct {
	Resolution  [3]int
	Components  int
	Bounds      AABB
	BitWidth    int
	Mins        []float32 // length = Components, per-component minimum value
	Maxs        []float32 // length = Components, per-component maximum value
	Packed      []byte    // packed indices, deinterleaved by component
	Preview     []byte    // optional lower-bit preview
	PreviewBits int
	IsDelta     bool // true when Packed encodes deltas instead of values
}

// Quantize compresses a Field to its wire form.
func (f *Field) Quantize(opts QuantizeOptions) *Quantized {
	if opts.BitWidth < 4 || opts.BitWidth > 8 {
		panic("field.Quantize: BitWidth must be 4..8")
	}

	source := f
	isDelta := false
	if opts.DeltaAgainst != nil {
		if opts.DeltaAgainst.Resolution != f.Resolution || opts.DeltaAgainst.Components != f.Components {
			panic("field.Quantize: DeltaAgainst shape mismatch")
		}
		source = subtractFields(f, opts.DeltaAgainst)
		isDelta = true
	}

	mins, maxs := perComponentRange(source)
	indices := quantizeIndices(source, mins, maxs, opts.BitWidth)
	packed := packBits(indices, opts.BitWidth)

	q := &Quantized{
		Resolution: f.Resolution,
		Components: f.Components,
		Bounds:     f.Bounds,
		BitWidth:   opts.BitWidth,
		Mins:       mins,
		Maxs:       maxs,
		Packed:     packed,
		IsDelta:    isDelta,
	}
	if opts.PreviewBits > 0 && opts.PreviewBits < opts.BitWidth {
		previewIdx := quantizeIndices(source, mins, maxs, opts.PreviewBits)
		q.Preview = packBits(previewIdx, opts.PreviewBits)
		q.PreviewBits = opts.PreviewBits
	}
	return q
}

// Decompress reconstructs a Field from a Quantized wire form.
// For delta-encoded Quantized values, callers must apply the delta to the
// reference field separately via ApplyDelta.
func (q *Quantized) Decompress() *Field {
	totalVoxels := q.Resolution[0] * q.Resolution[1] * q.Resolution[2]
	indices := unpackBits(q.Packed, totalVoxels*q.Components, q.BitWidth)
	out := New(q.Resolution, q.Components, q.Bounds)
	dequantize(indices, q.Mins, q.Maxs, q.BitWidth, q.Components, out.Data)
	return out
}

// WireSize returns the total bytes consumed by the packed payload (excluding
// the small Mins/Maxs/header overhead, which is constant per field).
func (q *Quantized) WireSize() int {
	return len(q.Packed) + len(q.Preview)
}

// ApplyDelta reconstructs the current field by adding a delta-encoded
// Quantized to a reference field. Panics if shapes mismatch or q is not
// a delta.
func ApplyDelta(reference *Field, q *Quantized) *Field {
	if !q.IsDelta {
		panic("field.ApplyDelta: Quantized is not a delta")
	}
	if reference.Resolution != q.Resolution || reference.Components != q.Components {
		panic("field.ApplyDelta: shape mismatch")
	}
	delta := q.Decompress()
	out := New(reference.Resolution, reference.Components, reference.Bounds)
	for i := range out.Data {
		out.Data[i] = reference.Data[i] + delta.Data[i]
	}
	return out
}

// perComponentRange returns the min and max for each component across the field.
func perComponentRange(f *Field) (mins, maxs []float32) {
	mins = make([]float32, f.Components)
	maxs = make([]float32, f.Components)
	for c := 0; c < f.Components; c++ {
		mins[c] = f.Data[c]
		maxs[c] = f.Data[c]
	}
	for i := 0; i < len(f.Data); i += f.Components {
		for c := 0; c < f.Components; c++ {
			v := f.Data[i+c]
			if v < mins[c] {
				mins[c] = v
			}
			if v > maxs[c] {
				maxs[c] = v
			}
		}
	}
	return
}

// quantizeIndices maps each value to its bin index. Output is deinterleaved:
// all component-0 indices, then all component-1 indices, etc.
func quantizeIndices(f *Field, mins, maxs []float32, bitWidth int) []int {
	levels := (1 << uint(bitWidth)) - 1
	totalVoxels := f.Resolution[0] * f.Resolution[1] * f.Resolution[2]
	out := make([]int, totalVoxels*f.Components)
	for c := 0; c < f.Components; c++ {
		rangeVal := maxs[c] - mins[c]
		if rangeVal < 1e-12 {
			rangeVal = 1e-12
		}
		scale := float32(levels) / rangeVal
		base := c * totalVoxels
		for v := 0; v < totalVoxels; v++ {
			val := f.Data[v*f.Components+c]
			idx := int((val-mins[c])*scale + 0.5)
			if idx < 0 {
				idx = 0
			}
			if idx > levels {
				idx = levels
			}
			out[base+v] = idx
		}
	}
	return out
}

// dequantize reverses quantizeIndices, writing reinterleaved values to dst.
func dequantize(indices []int, mins, maxs []float32, bitWidth, components int, dst []float32) {
	levels := (1 << uint(bitWidth)) - 1
	totalVoxels := len(indices) / components
	for c := 0; c < components; c++ {
		rangeVal := maxs[c] - mins[c]
		step := rangeVal / float32(levels)
		base := c * totalVoxels
		for v := 0; v < totalVoxels; v++ {
			dst[v*components+c] = mins[c] + float32(indices[base+v])*step
		}
	}
}

// packBits packs indices into a byte slice at bitWidth bits per index.
func packBits(indices []int, bitWidth int) []byte {
	totalBits := len(indices) * bitWidth
	out := make([]byte, (totalBits+7)/8)
	bitPos := 0
	for _, idx := range indices {
		for b := 0; b < bitWidth; b++ {
			if idx&(1<<uint(b)) != 0 {
				bytePos := bitPos / 8
				bitInByte := bitPos % 8
				out[bytePos] |= 1 << uint(bitInByte)
			}
			bitPos++
		}
	}
	return out
}

// unpackBits reverses packBits.
func unpackBits(packed []byte, count, bitWidth int) []int {
	out := make([]int, count)
	bitPos := 0
	for i := 0; i < count; i++ {
		v := 0
		for b := 0; b < bitWidth; b++ {
			bytePos := bitPos / 8
			bitInByte := bitPos % 8
			if packed[bytePos]&(1<<uint(bitInByte)) != 0 {
				v |= 1 << uint(b)
			}
			bitPos++
		}
		out[i] = v
	}
	return out
}

// subtractFields returns a new field equal to a - b (used for delta encoding).
func subtractFields(a, b *Field) *Field {
	out := New(a.Resolution, a.Components, a.Bounds)
	for i := range a.Data {
		out.Data[i] = a.Data[i] - b.Data[i]
	}
	return out
}
