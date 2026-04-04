package quant

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	mrand "math/rand"
	"sync"
)

// IPQuantizer provides unbiased inner-product-preserving quantization.
// Uses TurboQuant_mse at bit-width (b-1) + 1-bit QJL on the residual.
// Safe for concurrent use after construction.
type IPQuantizer struct {
	dim      int
	bitWidth int
	seed     int64
	mse      *Quantizer
	proj     []float32 // d×d Gaussian matrix for QJL
	pool     sync.Pool
}

// IPQuantized holds the two-stage quantization result.
type IPQuantized struct {
	MSE     []byte  // packed MSE indices (b-1 bits per coordinate)
	Signs   []byte  // packed QJL sign bits (1 bit per coordinate)
	ResNorm float32 // L2 norm of residual
}

// PreparedQuery precomputes S^T * y for amortized IP queries.
type PreparedQuery struct {
	projY []float32 // precomputed <s_i, y> for each row i of S
	mseY  []float32 // clone of raw y for MSE dequant dot product
}

type ipScratch struct {
	residual []float32
}

// NewIP creates an inner-product-optimal quantizer with random seed.
func NewIP(dim, bitWidth int) *IPQuantizer {
	var seedBytes [8]byte
	rand.Read(seedBytes[:])
	seed := int64(binary.LittleEndian.Uint64(seedBytes[:]))
	return NewIPWithSeed(dim, bitWidth, seed)
}

// NewIPWithSeed creates a deterministic inner-product-optimal quantizer.
func NewIPWithSeed(dim, bitWidth int, seed int64) *IPQuantizer {
	if dim < 2 {
		panic("quant: dim must be >= 2")
	}
	if bitWidth < 2 {
		panic("quant: IP quantizer bitWidth must be >= 2")
	}
	// CRITICAL: MSE rotation and QJL projection must be independent.
	mseSeed := seed
	qjlSeed := seed ^ 0x5DEECE66D
	mseQ := NewWithSeed(dim, bitWidth-1, mseSeed)
	qjlRng := mrand.New(mrand.NewSource(qjlSeed))
	proj := generateGaussianMatrix(dim, qjlRng)
	q := &IPQuantizer{
		dim:      dim,
		bitWidth: bitWidth,
		seed:     seed,
		mse:      mseQ,
		proj:     proj,
	}
	q.pool.New = func() any {
		return &ipScratch{
			residual: make([]float32, dim),
		}
	}
	return q
}

func (q *IPQuantizer) Dim() int      { return q.dim }
func (q *IPQuantizer) BitWidth() int { return q.bitWidth }
func (q *IPQuantizer) Seed() int64   { return q.seed }

// Quantize returns the two-stage quantization result.
func (q *IPQuantizer) Quantize(vec []float32) IPQuantized {
	// Stage 1: MSE quantize at b-1 bits
	msePacked, mseNorm := q.mse.Quantize(vec)

	// Dequantize to compute residual
	recon := q.mse.Dequantize(msePacked)

	buf := q.pool.Get().(*ipScratch)
	defer q.pool.Put(buf)

	// residual = normalized(vec) - recon
	scale := float32(1.0)
	if mseNorm > 1e-12 {
		scale = 1.0 / mseNorm
	}
	for i := range vec {
		buf.residual[i] = vec[i]*scale - recon[i]
	}

	// Stage 2: QJL on residual
	signs := make([]byte, (q.dim+7)/8)
	resNorm := qjlProject(signs, buf.residual, q.proj, q.dim)

	return IPQuantized{
		MSE:     msePacked,
		Signs:   signs,
		ResNorm: resNorm,
	}
}

// Dequantize reconstructs an approximate vector (primarily for debugging).
func (q *IPQuantizer) Dequantize(qx IPQuantized) []float32 {
	mseRecon := q.mse.Dequantize(qx.MSE)
	// Add QJL reconstruction: mse_recon + sqrt(pi/2)/d * resNorm * S^T * sign_vector
	scale := float32(math.Sqrt(math.Pi/2.0)) / float32(q.dim) * qx.ResNorm
	for i := 0; i < q.dim; i++ {
		var sum float32
		for j := 0; j < q.dim; j++ {
			signBit := qx.Signs[j/8] & (1 << uint(j%8))
			s := float32(-1.0)
			if signBit != 0 {
				s = 1.0
			}
			sum += q.proj[j*q.dim+i] * s // S^T[i][j] = S[j][i]
		}
		mseRecon[i] += scale * sum
	}
	return mseRecon
}

// InnerProduct estimates <x, y> from quantized x and raw y.
// Asymmetric: does NOT reconstruct the full vector.
// Unbiased: E[InnerProduct(Quantize(x), y)] = <x, y>
func (q *IPQuantizer) InnerProduct(qx IPQuantized, y []float32) float32 {
	// MSE part: <dequant_mse(idx), y>
	mseRecon := q.mse.Dequantize(qx.MSE)
	var mseDot float32
	for i := range mseRecon {
		mseDot += mseRecon[i] * y[i]
	}
	// QJL part
	qjlDot := qjlInnerProduct(qx.Signs, qx.ResNorm, y, q.proj, q.dim)
	return mseDot + qjlDot
}

// PrepareQuery precomputes S^T * y for efficient repeated IP queries.
func (q *IPQuantizer) PrepareQuery(y []float32) PreparedQuery {
	projY := make([]float32, q.dim)
	for i := 0; i < q.dim; i++ {
		row := i * q.dim
		var dot float32
		for j := 0; j < q.dim; j++ {
			dot += q.proj[row+j] * y[j]
		}
		projY[i] = dot
	}
	yClone := make([]float32, len(y))
	copy(yClone, y)
	return PreparedQuery{projY: projY, mseY: yClone}
}

// InnerProductPrepared estimates <x, y> using a precomputed query projection.
func (q *IPQuantizer) InnerProductPrepared(qx IPQuantized, pq PreparedQuery) float32 {
	// MSE part
	mseRecon := q.mse.Dequantize(qx.MSE)
	var mseDot float32
	for i := range mseRecon {
		mseDot += mseRecon[i] * pq.mseY[i]
	}
	// QJL part using precomputed projections
	scale := float32(math.Sqrt(math.Pi/2.0)) / float32(q.dim) * qx.ResNorm
	var sum float32
	for i := 0; i < q.dim; i++ {
		signBit := qx.Signs[i/8] & (1 << uint(i%8))
		if signBit != 0 {
			sum += pq.projY[i]
		} else {
			sum -= pq.projY[i]
		}
	}
	return mseDot + scale*sum
}
