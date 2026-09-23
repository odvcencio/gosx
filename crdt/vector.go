package crdt

import (
	"sync"

	"m31labs.dev/turboquant"
)

// vectorQuantSeed is a fixed seed shared by all CRDT vector quantizers.
// Every replica using the same (dim, bitWidth, seed) produces byte-identical
// compressed output. Changing this value is a breaking protocol change.
//
// turboquant v0.2.1 also changed NewWithSeed's default rotation from a single
// Walsh-Hadamard round to three rounds, fixing a codebook defect at dim >=
// 1024 (a one-hot input stayed exactly zero outside its power-of-two block).
// The fix changes the quantizer's output for the same seed, so it is itself a
// breaking protocol change: a VectorPacked value that Quantize wrote with
// turboquant < v0.2.1 decodes to the wrong vector under this version, because
// Dequantize applies the new three-round rotation to bytes the old one-round
// rotation produced. VectorValue stores only the packed codes and the norm,
// not the source float32 vector, so there is no way to re-derive a correct
// three-round encoding from an old payload after the fact.
//
// This only matters for a persisted crdt.Doc (workspace.Workspace.Save,
// crdt.Doc.Save) that already contains a ValueKindVector entry written before
// this upgrade. Loading that snapshot after the bump silently dequantizes to
// an incorrect vector instead of failing closed. There is no in-place
// migration: a workspace carrying pre-v0.2.1 vector values must re-embed and
// re-write each one (WriteVector) from its original source, not from the
// stored snapshot, after upgrading past this commit.
const vectorQuantSeed int64 = 0x676f73785f637264 // "gosx_crd"

var vectorQuantCache sync.Map // key: vectorCacheKey -> *turboquant.Quantizer

type vectorCacheKey struct {
	dim  int
	bits int
}

func vectorQuantizer(dim, bitWidth int) *turboquant.Quantizer {
	key := vectorCacheKey{dim, bitWidth}
	if q, ok := vectorQuantCache.Load(key); ok {
		return q.(*turboquant.Quantizer)
	}
	q := turboquant.NewWithSeed(dim, bitWidth, vectorQuantSeed)
	actual, _ := vectorQuantCache.LoadOrStore(key, q)
	return actual.(*turboquant.Quantizer)
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
