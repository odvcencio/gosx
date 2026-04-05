package model

import (
	"math"
	"math/rand"
	"testing"
)

func randomWeights(rng *rand.Rand, n int) []float32 {
	w := make([]float32, n)
	for i := range w {
		w[i] = float32(rng.NormFloat64()) * 0.02
	}
	return w
}

func TestLayerForwardShapePreserved(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	dim := 64
	heads := 4
	ffDim := 256
	seq := 8

	layer := &Layer{
		Dim:     dim,
		Heads:   heads,
		FFDim:   ffDim,
		QKV:     LinearWeights{Weight: randomWeights(rng, 3*dim*dim), Bias: make([]float32, 3*dim)},
		AttnOut: LinearWeights{Weight: randomWeights(rng, dim*dim), Bias: make([]float32, dim)},
		LN1:     NormWeights{Gamma: ones(dim), Beta: make([]float32, dim)},
		FF1:     LinearWeights{Weight: randomWeights(rng, ffDim*dim), Bias: make([]float32, ffDim)},
		FF2:     LinearWeights{Weight: randomWeights(rng, dim*ffDim), Bias: make([]float32, dim)},
		LN2:     NormWeights{Gamma: ones(dim), Beta: make([]float32, dim)},
	}

	input := randomWeights(rng, seq*dim)
	scratch := NewLayerScratch(seq, dim, heads, ffDim)
	output := make([]float32, seq*dim)

	layer.Forward(output, input, seq, scratch)

	if len(output) != seq*dim {
		t.Fatalf("output len %d want %d", len(output), seq*dim)
	}
	same := true
	for i := range output {
		if math.Abs(float64(output[i]-input[i])) > 1e-6 {
			same = false
			break
		}
	}
	if same {
		t.Error("output identical to input — layer did nothing")
	}
}

func TestLayerForwardDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	dim := 32
	heads := 2
	ffDim := 128
	seq := 4

	layer := &Layer{
		Dim:     dim,
		Heads:   heads,
		FFDim:   ffDim,
		QKV:     LinearWeights{Weight: randomWeights(rng, 3*dim*dim), Bias: make([]float32, 3*dim)},
		AttnOut: LinearWeights{Weight: randomWeights(rng, dim*dim), Bias: make([]float32, dim)},
		LN1:     NormWeights{Gamma: ones(dim), Beta: make([]float32, dim)},
		FF1:     LinearWeights{Weight: randomWeights(rng, ffDim*dim), Bias: make([]float32, ffDim)},
		FF2:     LinearWeights{Weight: randomWeights(rng, dim*ffDim), Bias: make([]float32, dim)},
		LN2:     NormWeights{Gamma: ones(dim), Beta: make([]float32, dim)},
	}

	input := randomWeights(rand.New(rand.NewSource(99)), seq*dim)
	scratch := NewLayerScratch(seq, dim, heads, ffDim)
	out1 := make([]float32, seq*dim)
	out2 := make([]float32, seq*dim)

	layer.Forward(out1, input, seq, scratch)
	layer.Forward(out2, input, seq, scratch)

	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("non-deterministic at %d: %f vs %f", i, out1[i], out2[i])
		}
	}
}

func ones(n int) []float32 {
	o := make([]float32, n)
	for i := range o {
		o[i] = 1
	}
	return o
}
