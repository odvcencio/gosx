package semantic

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
)

// hashProvider produces deterministic vectors by hashing the input text.
// Same text always produces the same normalized vector. Different texts
// produce essentially random vectors (no semantic similarity).
// Useful for testing mechanics (set/get, invalidate, concurrency).
type hashProvider struct{ dim int }

func (p *hashProvider) Encode(text string) ([]float32, error) {
	h := fnv.New64a()
	h.Write([]byte(text))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	vec := make([]float32, p.dim)
	for i := range vec {
		vec[i] = rng.Float32()*2 - 1
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	scale := float32(1.0 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}
	return vec, nil
}

func (p *hashProvider) EncodeBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := p.Encode(t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (p *hashProvider) Dim() int { return p.dim }

// controlledProvider returns pre-registered vectors for known texts.
// Unknown texts return an error. This lets tests control exact similarity
// scores between queries and stored items.
type controlledProvider struct {
	dim     int
	vectors map[string][]float32
}

func (p *controlledProvider) Encode(text string) ([]float32, error) {
	v, ok := p.vectors[text]
	if !ok {
		return nil, fmt.Errorf("controlledProvider: unknown text %q", text)
	}
	// Return a normalized copy.
	out := make([]float32, len(v))
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	scale := float32(1.0 / math.Sqrt(norm))
	for i, x := range v {
		out[i] = x * scale
	}
	return out, nil
}

func (p *controlledProvider) EncodeBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := p.Encode(t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (p *controlledProvider) Dim() int { return p.dim }
