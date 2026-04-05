package embed

// Provider wraps an external embedding API (OpenAI, Cohere, etc.).
// Implementations must be safe for concurrent use.
type Provider interface {
	// Encode converts a single text to an L2-normalized float32 embedding vector.
	Encode(text string) ([]float32, error)

	// EncodeBatch converts multiple texts to embeddings.
	EncodeBatch(texts []string) ([][]float32, error)

	// Dim returns the embedding dimension (e.g., 384, 1536).
	Dim() int
}
