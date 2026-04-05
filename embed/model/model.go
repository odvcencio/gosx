package model

import (
	"fmt"
	"sync"
)

// Model is a BERT-style transformer encoder for text embeddings.
// Safe for concurrent use after construction.
type Model struct {
	dim       int
	maxSeq    int
	vocabSize int
	tokenEmb  []float32 // [vocab x dim]
	posEmb    []float32 // [maxSeq x dim]
	layers    []Layer
	finalNorm NormWeights
	tokenize  func(string) []int
	pool      sync.Pool
}

type modelScratch struct {
	hidden  []float32
	scratch *LayerScratch
	output  []float32
	pooled  []float32
}

// Dim returns the embedding dimension.
func (m *Model) Dim() int { return m.dim }

// Encode converts text to an L2-normalized embedding vector.
func (m *Model) Encode(text string) ([]float32, error) {
	tokens := m.tokenize(text)
	return m.forward(tokens)
}

// EncodeBatch converts multiple texts to embeddings.
func (m *Model) EncodeBatch(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		var err error
		results[i], err = m.Encode(text)
		if err != nil {
			return nil, fmt.Errorf("text %d: %w", i, err)
		}
	}
	return results, nil
}

func (m *Model) forward(tokens []int) ([]float32, error) {
	seq := len(tokens)
	if seq == 0 {
		return nil, fmt.Errorf("empty token sequence")
	}
	if seq > m.maxSeq {
		seq = m.maxSeq
		tokens = tokens[:seq]
	}

	s := m.getScratch(seq)
	defer m.putScratch(s)

	dim := m.dim
	hidden := s.hidden[:seq*dim]

	// Token embeddings + positional embeddings
	for i, tok := range tokens {
		if tok < 0 || tok >= m.vocabSize {
			tok = 0
		}
		tokOff := tok * dim
		posOff := i * dim
		hidOff := i * dim
		for j := 0; j < dim; j++ {
			hidden[hidOff+j] = m.tokenEmb[tokOff+j] + m.posEmb[posOff+j]
		}
	}

	// Transformer layers
	buf := s.output[:seq*dim]
	for i := range m.layers {
		m.layers[i].Forward(buf, hidden, seq, s.scratch)
		hidden, buf = buf, hidden
	}

	// Final layer norm (in-place safe: reads x[off+j] before writing out[off+j])
	layerNorm(hidden, hidden, m.finalNorm.Gamma, m.finalNorm.Beta, dim)

	// Mean pooling
	pooled := s.pooled[:dim]
	meanPool(pooled, hidden, seq, dim)

	// L2 normalize
	result := make([]float32, dim)
	l2Normalize(result, pooled)
	return result, nil
}

func (m *Model) getScratch(seq int) *modelScratch {
	if v := m.pool.Get(); v != nil {
		s := v.(*modelScratch)
		if len(s.hidden) >= seq*m.dim {
			return s
		}
	}
	ffDim := 0
	heads := 0
	if len(m.layers) > 0 {
		ffDim = m.layers[0].FFDim
		heads = m.layers[0].Heads
	}
	return &modelScratch{
		hidden:  make([]float32, seq*m.dim),
		scratch: NewLayerScratch(seq, m.dim, heads, ffDim),
		output:  make([]float32, seq*m.dim),
		pooled:  make([]float32, m.dim),
	}
}

func (m *Model) putScratch(s *modelScratch) {
	m.pool.Put(s)
}
