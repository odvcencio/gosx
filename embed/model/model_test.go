package model

import (
	"math"
	"math/rand"
	"testing"
)

func TestModelForwardShape(t *testing.T) {
	m := newTestModel(t, 32, 2, 128, 2, 100)
	tokens := []int{1, 5, 10, 2}
	vec, err := m.forward(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 32 {
		t.Fatalf("output dim %d want 32", len(vec))
	}
	var normSq float64
	for _, v := range vec {
		normSq += float64(v) * float64(v)
	}
	if math.Abs(normSq-1.0) > 1e-3 {
		t.Errorf("norm^2 = %f want 1.0", normSq)
	}
}

func TestModelDeterministic(t *testing.T) {
	m := newTestModel(t, 32, 2, 128, 2, 100)
	tokens := []int{1, 5, 10, 2}
	v1, _ := m.forward(tokens)
	v2, _ := m.forward(tokens)
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
}

func TestModelProviderInterface(t *testing.T) {
	m := newTestModel(t, 32, 2, 128, 2, 100)
	var _ interface {
		Encode(string) ([]float32, error)
		EncodeBatch([]string) ([][]float32, error)
		Dim() int
	} = m
	if m.Dim() != 32 {
		t.Errorf("Dim() = %d want 32", m.Dim())
	}
}

func newTestModel(t *testing.T, dim, heads, ffDim, layers, vocab int) *Model {
	t.Helper()
	rng := rand.New(rand.NewSource(42))
	rw := func(n int) []float32 { return randomWeights(rng, n) }
	zw := func(n int) []float32 { return make([]float32, n) }

	m := &Model{
		dim:       dim,
		maxSeq:    128,
		tokenEmb:  rw(vocab * dim),
		posEmb:    rw(128 * dim),
		vocabSize: vocab,
		finalNorm: NormWeights{Gamma: ones(dim), Beta: zw(dim)},
	}

	for i := 0; i < layers; i++ {
		m.layers = append(m.layers, Layer{
			Dim:     dim,
			Heads:   heads,
			FFDim:   ffDim,
			QKV:     LinearWeights{Weight: rw(3 * dim * dim), Bias: zw(3 * dim)},
			AttnOut: LinearWeights{Weight: rw(dim * dim), Bias: zw(dim)},
			LN1:     NormWeights{Gamma: ones(dim), Beta: zw(dim)},
			FF1:     LinearWeights{Weight: rw(ffDim * dim), Bias: zw(ffDim)},
			FF2:     LinearWeights{Weight: rw(dim * ffDim), Bias: zw(dim)},
			LN2:     NormWeights{Gamma: ones(dim), Beta: zw(dim)},
		})
	}

	m.tokenize = func(text string) []int { return []int{1, 5, 10, 2} }
	return m
}
