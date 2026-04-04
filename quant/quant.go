package quant

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand"
	"sync"
)

// Quantizer compresses float32 vectors to packed bit representations using
// the TurboQuant MSE-optimal algorithm. Safe for concurrent use after construction.
type Quantizer struct {
	dim      int
	bitWidth int
	seed     int64
	rotation []float32
	cb       codebook
	pool     sync.Pool
}

type scratchBuf struct {
	rotated []float32
	tmp     []float32
	indices []int
}

// New creates a TurboQuant MSE-optimal quantizer with a random seed.
func New(dim, bitWidth int) *Quantizer {
	var seedBytes [8]byte
	rand.Read(seedBytes[:])
	seed := int64(binary.LittleEndian.Uint64(seedBytes[:]))
	return NewWithSeed(dim, bitWidth, seed)
}

// NewWithSeed creates a deterministic MSE-optimal quantizer.
// Two quantizers with the same dim, bitWidth, and seed produce identical output.
func NewWithSeed(dim, bitWidth int, seed int64) *Quantizer {
	if dim < 2 {
		panic("quant: dim must be >= 2")
	}
	if bitWidth < 1 || bitWidth > 8 {
		panic("quant: bitWidth must be 1-8")
	}
	rng := mrand.New(mrand.NewSource(seed))
	q := &Quantizer{
		dim:      dim,
		bitWidth: bitWidth,
		seed:     seed,
		rotation: generateRotation(dim, rng),
		cb:       cachedCodebook(dim, bitWidth),
	}
	q.pool.New = func() any {
		return &scratchBuf{
			rotated: make([]float32, dim),
			tmp:     make([]float32, dim),
			indices: make([]int, dim),
		}
	}
	return q
}

// Dim returns the vector dimension.
func (q *Quantizer) Dim() int { return q.dim }

// BitWidth returns the bits per coordinate.
func (q *Quantizer) BitWidth() int { return q.bitWidth }

// Seed returns the seed used to construct this quantizer.
func (q *Quantizer) Seed() int64 { return q.seed }

// Quantize compresses a float32 vector to packed bytes.
// Returns packed indices and the original vector norm (for rescaling on dequantize).
func (q *Quantizer) Quantize(vec []float32) ([]byte, float32) {
	norm := vecNorm(vec)
	buf := q.pool.Get().(*scratchBuf)
	defer q.pool.Put(buf)

	// Normalize into tmp
	scale := float32(1.0)
	if norm > 1e-12 {
		scale = 1.0 / norm
	}
	for i, v := range vec {
		buf.tmp[i] = v * scale
	}

	// Rotate: rotated = R · normalized
	rotate(buf.rotated, buf.tmp, q.rotation, q.dim)

	// Quantize each coordinate to nearest centroid
	for i := 0; i < q.dim; i++ {
		buf.indices[i] = q.cb.nearestCentroid(buf.rotated[i])
	}

	// Pack indices
	packed := make([]byte, packedSize(q.dim, q.bitWidth))
	packIndices(packed, buf.indices, q.bitWidth)
	return packed, norm
}

// Dequantize reconstructs an approximate unit-norm float32 vector.
// Multiply by the norm from Quantize to recover original scale.
func (q *Quantizer) Dequantize(packed []byte) []float32 {
	buf := q.pool.Get().(*scratchBuf)
	defer q.pool.Put(buf)

	// Unpack indices and look up centroids
	unpackIndices(buf.indices, packed, q.dim, q.bitWidth)
	for i := 0; i < q.dim; i++ {
		buf.rotated[i] = q.cb.centroidValue(buf.indices[i])
	}

	// Rotate back: result = Rᵀ · centroids
	result := make([]float32, q.dim)
	rotateInverse(result, buf.rotated, q.rotation, q.dim)
	return result
}

// InnerProduct estimates <x, y> from MSE-quantized x and raw y.
// Computes in rotated domain without allocating a full dequantized vector.
func (q *Quantizer) InnerProduct(packed []byte, norm float32, y []float32) float32 {
	buf := q.pool.Get().(*scratchBuf)
	defer q.pool.Put(buf)

	// Rotate y into quantized domain: rotY = R · y
	rotate(buf.rotated, y, q.rotation, q.dim)

	// Unpack and dot with centroids directly
	unpackIndices(buf.indices, packed, q.dim, q.bitWidth)
	var dot float32
	for i := 0; i < q.dim; i++ {
		dot += q.cb.centroidValue(buf.indices[i]) * buf.rotated[i]
	}
	return dot * norm
}

// QuantizeBatch compresses multiple vectors.
func (q *Quantizer) QuantizeBatch(vecs [][]float32) ([][]byte, []float32) {
	packed := make([][]byte, len(vecs))
	norms := make([]float32, len(vecs))
	for i, v := range vecs {
		packed[i], norms[i] = q.Quantize(v)
	}
	return packed, norms
}

// DequantizeBatch reconstructs multiple vectors.
func (q *Quantizer) DequantizeBatch(packed [][]byte) [][]float32 {
	result := make([][]float32, len(packed))
	for i, p := range packed {
		result[i] = q.Dequantize(p)
	}
	return result
}
