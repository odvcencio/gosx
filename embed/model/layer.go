package model

import "math"

// LinearWeights holds weight + bias for a linear projection.
type LinearWeights struct {
	Weight []float32 // [out x in] row-major
	Bias   []float32 // [out]
}

// NormWeights holds gamma + beta for layer normalization.
type NormWeights struct {
	Gamma []float32
	Beta  []float32
}

// Layer is one transformer encoder layer (self-attention + FFN).
type Layer struct {
	Dim   int
	Heads int
	FFDim int

	QKV     LinearWeights // [3*dim x dim] fused Q/K/V projection
	AttnOut LinearWeights // [dim x dim] output projection
	LN1     NormWeights   // pre-attention layer norm
	FF1     LinearWeights // [ffDim x dim] first FFN layer
	FF2     LinearWeights // [dim x ffDim] second FFN layer
	LN2     NormWeights   // pre-FFN layer norm
}

// LayerScratch holds pre-allocated buffers for one forward pass.
type LayerScratch struct {
	normed    []float32
	qkv       []float32
	attn      []float32
	attnOut   []float32
	projected []float32
	ffHidden  []float32
	ffGelu    []float32
	ffOut     []float32
}

// NewLayerScratch allocates scratch buffers for the given dimensions.
func NewLayerScratch(maxSeq, dim, heads, ffDim int) *LayerScratch {
	return &LayerScratch{
		normed:    make([]float32, maxSeq*dim),
		qkv:       make([]float32, maxSeq*3*dim),
		attn:      make([]float32, heads*maxSeq*maxSeq),
		attnOut:   make([]float32, maxSeq*dim),
		projected: make([]float32, maxSeq*dim),
		ffHidden:  make([]float32, maxSeq*ffDim),
		ffGelu:    make([]float32, maxSeq*ffDim),
		ffOut:     make([]float32, maxSeq*dim),
	}
}

// Forward runs the layer: out = x + attn(LN1(x)) + ffn(LN2(x + attn(LN1(x))))
func (l *Layer) Forward(out, x []float32, seq int, s *LayerScratch) {
	dim := l.Dim
	heads := l.Heads
	headDim := dim / heads

	// Pre-attention layer norm
	layerNorm(s.normed[:seq*dim], x, l.LN1.Gamma, l.LN1.Beta, dim)

	// Fused QKV projection: [seq x dim] @ [dim x 3*dim]^T = [seq x 3*dim]
	matMulTransB(s.qkv[:seq*3*dim], s.normed[:seq*dim], l.QKV.Weight, seq, dim, 3*dim)
	addBias(s.qkv[:seq*3*dim], l.QKV.Bias, seq, 3*dim)

	// Multi-head attention with scaled dot-product
	scale := float32(1.0 / math.Sqrt(float64(headDim)))
	for h := 0; h < heads; h++ {
		for i := 0; i < seq; i++ {
			for j := 0; j < seq; j++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					qi := s.qkv[(i*3*dim)+(h*headDim)+d]
					ki := s.qkv[(j*3*dim)+dim+(h*headDim)+d]
					dot += qi * ki
				}
				s.attn[h*seq*seq+i*seq+j] = dot * scale
			}
		}
		for i := 0; i < seq; i++ {
			off := h*seq*seq + i*seq
			softmax(s.attn[off:off+seq], s.attn[off:off+seq], seq)
		}
		for i := 0; i < seq; i++ {
			for d := 0; d < headDim; d++ {
				var sum float32
				for j := 0; j < seq; j++ {
					vj := s.qkv[(j*3*dim)+2*dim+(h*headDim)+d]
					sum += s.attn[h*seq*seq+i*seq+j] * vj
				}
				s.attnOut[i*dim+h*headDim+d] = sum
			}
		}
	}

	// Output projection
	proj := s.projected[:seq*dim]
	matMulTransB(proj, s.attnOut[:seq*dim], l.AttnOut.Weight, seq, dim, dim)
	addBias(proj, l.AttnOut.Bias, seq, dim)

	// Residual connection
	for i := range proj[:seq*dim] {
		proj[i] += x[i]
	}

	// Pre-FFN layer norm
	layerNorm(s.normed[:seq*dim], proj[:seq*dim], l.LN2.Gamma, l.LN2.Beta, dim)

	// FFN
	matMulTransB(s.ffHidden[:seq*l.FFDim], s.normed[:seq*dim], l.FF1.Weight, seq, dim, l.FFDim)
	addBias(s.ffHidden[:seq*l.FFDim], l.FF1.Bias, seq, l.FFDim)
	gelu(s.ffGelu[:seq*l.FFDim], s.ffHidden[:seq*l.FFDim])
	matMulTransB(s.ffOut[:seq*dim], s.ffGelu[:seq*l.FFDim], l.FF2.Weight, seq, l.FFDim, dim)
	addBias(s.ffOut[:seq*dim], l.FF2.Bias, seq, dim)

	// Residual connection
	for i := 0; i < seq*dim; i++ {
		out[i] = proj[i] + s.ffOut[i]
	}
}
