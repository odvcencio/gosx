package model

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed weights/minilm-l6-v2.gsxt
var embeddedWeights []byte

// HasWeights reports whether the embedded weight file is available.
func HasWeights() bool {
	return len(embeddedWeights) > 0
}

// New creates a Model loaded with the default MiniLM-L6-v2 weights.
func New() (*Model, error) {
	if !HasWeights() {
		return nil, fmt.Errorf("embed/model: no embedded weights")
	}
	return LoadFromBytes(embeddedWeights)
}

// LoadFromBytes creates a Model from a tensor file in memory.
func LoadFromBytes(data []byte) (*Model, error) {
	tensors, err := ReadTensorFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read tensor file: %w", err)
	}
	return buildModel(tensors)
}

func buildModel(tensors []Tensor) (*Model, error) {
	byName := make(map[string]Tensor, len(tensors))
	for _, t := range tensors {
		byName[t.Name] = t
	}
	get := func(name string) ([]float32, error) {
		t, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("missing tensor %q", name)
		}
		return t.Data, nil
	}

	tokEmb, err := get("embeddings.word_embeddings.weight")
	if err != nil {
		return nil, err
	}
	posEmb, err := get("embeddings.position_embeddings.weight")
	if err != nil {
		return nil, err
	}

	embTensor := byName["embeddings.word_embeddings.weight"]
	vocab := embTensor.Shape[0]
	dim := embTensor.Shape[1]
	posTensor := byName["embeddings.position_embeddings.weight"]
	maxSeq := posTensor.Shape[0]

	// Count layers
	numLayers := 0
	for name := range byName {
		if strings.HasPrefix(name, "encoder.layer.") && strings.Contains(name, ".attention.self.query.weight") {
			numLayers++
		}
	}

	ffWeight := byName[fmt.Sprintf("encoder.layer.0.intermediate.dense.weight")]
	ffDim := ffWeight.Shape[0]

	heads := 12
	if dim <= 128 {
		heads = dim / 16
		if heads == 0 {
			heads = 1
		}
	}

	m := &Model{
		dim:       dim,
		maxSeq:    maxSeq,
		vocabSize: vocab,
		tokenEmb:  tokEmb,
		posEmb:    posEmb,
	}

	for i := 0; i < numLayers; i++ {
		p := fmt.Sprintf("encoder.layer.%d.", i)
		layer, err := loadLayer(get, p, dim, heads, ffDim)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		m.layers = append(m.layers, layer)
	}

	fnGamma, err := get("embeddings.LayerNorm.weight")
	if err != nil {
		return nil, err
	}
	fnBeta, err := get("embeddings.LayerNorm.bias")
	if err != nil {
		return nil, err
	}
	m.finalNorm = NormWeights{Gamma: fnGamma, Beta: fnBeta}

	m.tokenize = defaultTokenize
	return m, nil
}

func loadLayer(get func(string) ([]float32, error), prefix string, dim, heads, ffDim int) (Layer, error) {
	var l Layer
	l.Dim = dim
	l.Heads = heads
	l.FFDim = ffDim

	qw, err := get(prefix + "attention.self.query.weight")
	if err != nil {
		return l, err
	}
	kw, err := get(prefix + "attention.self.key.weight")
	if err != nil {
		return l, err
	}
	vw, err := get(prefix + "attention.self.value.weight")
	if err != nil {
		return l, err
	}
	qb, err := get(prefix + "attention.self.query.bias")
	if err != nil {
		return l, err
	}
	kb, err := get(prefix + "attention.self.key.bias")
	if err != nil {
		return l, err
	}
	vb, err := get(prefix + "attention.self.value.bias")
	if err != nil {
		return l, err
	}

	l.QKV.Weight = make([]float32, 3*dim*dim)
	copy(l.QKV.Weight[0:dim*dim], qw)
	copy(l.QKV.Weight[dim*dim:2*dim*dim], kw)
	copy(l.QKV.Weight[2*dim*dim:3*dim*dim], vw)
	l.QKV.Bias = make([]float32, 3*dim)
	copy(l.QKV.Bias[0:dim], qb)
	copy(l.QKV.Bias[dim:2*dim], kb)
	copy(l.QKV.Bias[2*dim:3*dim], vb)

	ow, err := get(prefix + "attention.output.dense.weight")
	if err != nil {
		return l, err
	}
	ob, err := get(prefix + "attention.output.dense.bias")
	if err != nil {
		return l, err
	}
	l.AttnOut = LinearWeights{Weight: ow, Bias: ob}

	ln1g, err := get(prefix + "attention.output.LayerNorm.weight")
	if err != nil {
		return l, err
	}
	ln1b, err := get(prefix + "attention.output.LayerNorm.bias")
	if err != nil {
		return l, err
	}
	l.LN1 = NormWeights{Gamma: ln1g, Beta: ln1b}

	ff1w, err := get(prefix + "intermediate.dense.weight")
	if err != nil {
		return l, err
	}
	ff1b, err := get(prefix + "intermediate.dense.bias")
	if err != nil {
		return l, err
	}
	l.FF1 = LinearWeights{Weight: ff1w, Bias: ff1b}

	ff2w, err := get(prefix + "output.dense.weight")
	if err != nil {
		return l, err
	}
	ff2b, err := get(prefix + "output.dense.bias")
	if err != nil {
		return l, err
	}
	l.FF2 = LinearWeights{Weight: ff2w, Bias: ff2b}

	ln2g, err := get(prefix + "output.LayerNorm.weight")
	if err != nil {
		return l, err
	}
	ln2b, err := get(prefix + "output.LayerNorm.bias")
	if err != nil {
		return l, err
	}
	l.LN2 = NormWeights{Gamma: ln2g, Beta: ln2b}

	return l, nil
}

func defaultTokenize(text string) []int {
	words := strings.Fields(text)
	tokens := make([]int, 0, len(words)+2)
	tokens = append(tokens, 101) // [CLS]
	for range words {
		tokens = append(tokens, 1000) // placeholder
	}
	tokens = append(tokens, 102) // [SEP]
	return tokens
}
