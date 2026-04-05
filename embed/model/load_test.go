package model

import (
	"testing"
)

func TestNewDefaultModel(t *testing.T) {
	if !HasWeights() {
		t.Skip("no embedded weights — run convert tool first")
	}
	m, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if m.Dim() != 384 {
		t.Errorf("dim = %d want 384", m.Dim())
	}
	vec, err := m.Encode("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 384 {
		t.Fatalf("output dim = %d want 384", len(vec))
	}
}
