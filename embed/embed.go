package embed

import "errors"

// Encoder converts text to embedding vectors.
//
// An Encoder is backed by either an external Provider or a local model.
// Use NewProviderEncoder for external APIs. NewEncoder (local model) is
// not yet implemented and will return an error.
type Encoder struct {
	provider Provider
}

// NewProviderEncoder creates an Encoder backed by an external Provider.
// The returned encoder delegates all calls to the provider.
// This is the escape hatch for production deployments that need
// high-quality embeddings from a hosted API.
func NewProviderEncoder(p Provider) *Encoder {
	return &Encoder{provider: p}
}

// Encode converts a single text to an L2-normalized float32 embedding vector.
func (e *Encoder) Encode(text string) ([]float32, error) {
	if e.provider != nil {
		return e.provider.Encode(text)
	}
	return nil, errors.New("embed: no provider or model configured")
}

// EncodeBatch converts multiple texts to embeddings.
func (e *Encoder) EncodeBatch(texts []string) ([][]float32, error) {
	if e.provider != nil {
		return e.provider.EncodeBatch(texts)
	}
	return nil, errors.New("embed: no provider or model configured")
}

// Dim returns the embedding dimension.
func (e *Encoder) Dim() int {
	if e.provider != nil {
		return e.provider.Dim()
	}
	return 0
}
