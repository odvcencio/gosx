package crdt

import (
	"sync"

	"github.com/odvcencio/gosx/quant"
)

// vectorQuantSeed is a fixed seed shared by all CRDT vector quantizers.
// Every replica using the same (dim, bitWidth, seed) produces byte-identical
// compressed output. Changing this value is a breaking protocol change.
const vectorQuantSeed int64 = 0x676f73785f637264 // "gosx_crd"

var vectorQuantCache sync.Map // key: vectorCacheKey -> *quant.Quantizer

type vectorCacheKey struct {
	dim  int
	bits int
}

func vectorQuantizer(dim, bitWidth int) *quant.Quantizer {
	key := vectorCacheKey{dim, bitWidth}
	if q, ok := vectorQuantCache.Load(key); ok {
		return q.(*quant.Quantizer)
	}
	q := quant.NewWithSeed(dim, bitWidth, vectorQuantSeed)
	actual, _ := vectorQuantCache.LoadOrStore(key, q)
	return actual.(*quant.Quantizer)
}

// VectorValue quantizes vec and returns a Value containing the compressed form.
// dim must equal len(vec). bitWidth controls compression (1-8, lower = smaller).
func VectorValue(vec []float32, dim, bitWidth int) Value {
	q := vectorQuantizer(dim, bitWidth)
	packed, norm := q.Quantize(vec)
	return Value{
		Kind:         ValueKindVector,
		VectorPacked: packed,
		VectorNorm:   norm,
		VectorDim:    dim,
		VectorBits:   bitWidth,
	}
}

// Vector dequantizes a vector value back to float32.
// Returns nil if the value is not ValueKindVector.
func (v Value) Vector() []float32 {
	if v.Kind != ValueKindVector || len(v.VectorPacked) == 0 {
		return nil
	}
	q := vectorQuantizer(v.VectorDim, v.VectorBits)
	unit := q.Dequantize(v.VectorPacked)
	for i := range unit {
		unit[i] *= v.VectorNorm
	}
	return unit
}
