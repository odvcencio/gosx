package embed

import (
	"errors"
	"math"
	"testing"
)

// mockProvider is a test double that returns deterministic embeddings.
type mockProvider struct {
	dim    int
	calls  int
	batchN int
	failAt int // if > 0, Encode fails on the nth call
}

func (m *mockProvider) Encode(text string) ([]float32, error) {
	m.calls++
	if m.failAt > 0 && m.calls >= m.failAt {
		return nil, errors.New("mock: simulated failure")
	}
	vec := make([]float32, m.dim)
	// Deterministic: hash the text length into the first component, normalize.
	vec[0] = float32(len(text) % 100)
	norm := float32(math.Sqrt(float64(vec[0] * vec[0])))
	if norm > 0 {
		vec[0] /= norm
	}
	return vec, nil
}

func (m *mockProvider) EncodeBatch(texts []string) ([][]float32, error) {
	m.batchN = len(texts)
	results := make([][]float32, len(texts))
	for i, t := range texts {
		vec, err := m.Encode(t)
		if err != nil {
			return nil, err
		}
		results[i] = vec
	}
	return results, nil
}

func (m *mockProvider) Dim() int { return m.dim }

func TestProviderEncoderEncode(t *testing.T) {
	p := &mockProvider{dim: 384}
	enc := NewProviderEncoder(p)

	vec, err := enc.Encode("hello world")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(vec) != 384 {
		t.Fatalf("got len %d, want 384", len(vec))
	}
	if p.calls != 1 {
		t.Fatalf("provider called %d times, want 1", p.calls)
	}
}

func TestProviderEncoderDim(t *testing.T) {
	p := &mockProvider{dim: 1536}
	enc := NewProviderEncoder(p)
	if enc.Dim() != 1536 {
		t.Fatalf("Dim() = %d, want 1536", enc.Dim())
	}
}

func TestProviderEncoderBatch(t *testing.T) {
	p := &mockProvider{dim: 384}
	enc := NewProviderEncoder(p)

	texts := []string{"alpha", "beta", "gamma"}
	vecs, err := enc.EncodeBatch(texts)
	if err != nil {
		t.Fatalf("EncodeBatch: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d results, want 3", len(vecs))
	}
	if p.batchN != 3 {
		t.Fatalf("provider batch saw %d texts, want 3", p.batchN)
	}
	for i, v := range vecs {
		if len(v) != 384 {
			t.Fatalf("vec[%d] len %d, want 384", i, len(v))
		}
	}
}

func TestProviderEncoderErrorPropagation(t *testing.T) {
	p := &mockProvider{dim: 384, failAt: 1}
	enc := NewProviderEncoder(p)

	_, err := enc.Encode("fail")
	if err == nil {
		t.Fatal("expected error from failing provider")
	}
	if err.Error() != "mock: simulated failure" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProviderEncoderBatchErrorPropagation(t *testing.T) {
	p := &mockProvider{dim: 384, failAt: 2}
	enc := NewProviderEncoder(p)

	_, err := enc.EncodeBatch([]string{"ok", "fail", "never"})
	if err == nil {
		t.Fatal("expected error from failing batch provider")
	}
}

func TestEncoderNoProvider(t *testing.T) {
	enc := &Encoder{}
	_, err := enc.Encode("orphan")
	if err == nil {
		t.Fatal("expected error with no provider")
	}
	_, err = enc.EncodeBatch([]string{"orphan"})
	if err == nil {
		t.Fatal("expected error with no provider")
	}
	if enc.Dim() != 0 {
		t.Fatalf("Dim() = %d, want 0 with no provider", enc.Dim())
	}
}

// Tests matching the user's specified interface for simple mock usage.

func TestProviderEncoder(t *testing.T) {
	enc := NewProviderEncoder(&simpleMock{dim: 384})
	if enc.Dim() != 384 {
		t.Errorf("Dim() = %d want 384", enc.Dim())
	}
	vec, err := enc.Encode("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 384 {
		t.Errorf("vec len %d want 384", len(vec))
	}
}

func TestProviderEncoderBatchSimple(t *testing.T) {
	enc := NewProviderEncoder(&simpleMock{dim: 128})
	vecs, err := enc.EncodeBatch([]string{"hello", "world", "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 3 {
		t.Errorf("got %d vecs want 3", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 128 {
			t.Errorf("vec[%d] len %d want 128", i, len(v))
		}
	}
}

// simpleMock produces vectors where every element is len(text)/dim.
type simpleMock struct {
	dim int
}

func (m *simpleMock) Encode(text string) ([]float32, error) {
	v := make([]float32, m.dim)
	for i := range v {
		v[i] = float32(len(text)) / float32(m.dim)
	}
	return v, nil
}

func (m *simpleMock) EncodeBatch(texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Encode(t)
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

func (m *simpleMock) Dim() int { return m.dim }
