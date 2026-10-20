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
// vectorQuantFormatV1 guards against that silently: VectorValue prepends it
// as a one-byte tag before the packed codes, and Vector checks both the tag
// and the tagged payload's exact expected length before it trusts the bytes.
// A payload from before this tag existed is always one byte short of that
// expected length, so Vector treats it (and any other unrecognized payload)
// as undecodable and fails closed to nil instead of returning a silently
// wrong vector. A persisted crdt.Doc (workspace.Workspace.Save, crdt.Doc.Save)
// carrying pre-v0.2.1 vector values has no in-place migration: re-embed and
// re-write each one (WriteVector) from its original source, not from the
// stored snapshot, after upgrading past this commit.
const vectorQuantSeed int64 = 0x676f73785f637264 // "gosx_crd"

// vectorQuantFormatV1 tags a VectorPacked payload produced by the three-round
// Hadamard rotation (turboquant >= v0.2.1). Bump this, and the length check in
// Vector, if the quantizer's output format ever changes again.
const vectorQuantFormatV1 byte = 1

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
	tagged := make([]byte, len(packed)+1)
	tagged[0] = vectorQuantFormatV1
	copy(tagged[1:], packed)
	return Value{
		Kind:         ValueKindVector,
		VectorPacked: tagged,
		VectorNorm:   norm,
		VectorDim:    dim,
		VectorBits:   bitWidth,
	}
}

// Vector dequantizes a vector value back to float32.
// Returns nil if the value is not ValueKindVector, or if VectorPacked does
// not carry the current quantizer format tag: that happens for a value a
// pre-v0.2.1 turboquant wrote (see vectorQuantFormatV1), which this version
// cannot decode correctly, and for any other payload this version does not
// recognize. Either way, Vector fails closed instead of returning a silently
// wrong vector.
func (v Value) Vector() []float32 {
	if v.Kind != ValueKindVector || len(v.VectorPacked) == 0 {
		return nil
	}
	want := turboquant.PackedSize(v.VectorDim, v.VectorBits) + 1
	if len(v.VectorPacked) != want || v.VectorPacked[0] != vectorQuantFormatV1 {
		return nil
	}
	q := vectorQuantizer(v.VectorDim, v.VectorBits)
	unit := q.Dequantize(v.VectorPacked[1:])
	for i := range unit {
		unit[i] *= v.VectorNorm
	}
	return unit
}
