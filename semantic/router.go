package semantic

import (
	"sync"

	"github.com/odvcencio/gosx/embed"
	"github.com/odvcencio/gosx/vecdb"
)

// RouterOptions configures a semantic router.
type RouterOptions struct {
	// BitWidth is the quantization bit-width for the underlying vector index.
	// Default: 3.
	BitWidth int

	// Threshold is the minimum similarity score for a route match.
	// Default: 0.7.
	Threshold float32
}

// SemanticRoute describes a registered route.
type SemanticRoute struct {
	Description string
	Handler     func(query string) (any, error)
	Embedding   []float32
}

// Router matches requests to handlers by semantic similarity
// instead of exact URL patterns. Safe for concurrent use.
type Router struct {
	index     *vecdb.Index
	encoder   *embed.Encoder
	routes    map[string]SemanticRoute
	threshold float32
	mu        sync.RWMutex
}

// NewRouter creates a semantic router backed by the given encoder.
func NewRouter(encoder *embed.Encoder, opts RouterOptions) *Router {
	if opts.BitWidth <= 0 {
		opts.BitWidth = 3
	}
	if opts.Threshold <= 0 {
		opts.Threshold = 0.7
	}
	return &Router{
		index:     vecdb.New(encoder.Dim(), opts.BitWidth),
		routes:    make(map[string]SemanticRoute),
		encoder:   encoder,
		threshold: opts.Threshold,
	}
}

// Handle registers a semantic route. The description is embedded via the
// encoder and used for similarity matching against incoming queries.
func (r *Router) Handle(name, description string, handler func(string) (any, error)) {
	vec, err := r.encoder.Encode(description)
	if err != nil {
		return
	}
	r.HandleWithEmbedding(name, vec, handler)
}

// HandleWithEmbedding registers a route with a pre-computed embedding.
func (r *Router) HandleWithEmbedding(name string, embedding []float32, handler func(string) (any, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[name] = SemanticRoute{
		Handler:   handler,
		Embedding: cloneFloat32s(embedding),
	}
	r.index.Add(name, embedding)
}

// Match finds the best matching route for a query.
// Returns the handler, the route name, and true if a match above the
// threshold was found. Returns nil, "", false otherwise.
func (r *Router) Match(query string) (func(string) (any, error), string, bool) {
	vec, err := r.encoder.Encode(query)
	if err != nil {
		return nil, "", false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	results := r.index.Search(vec, len(r.routes))
	if len(results) == 0 {
		return nil, "", false
	}
	var bestRoute SemanticRoute
	var bestName string
	var bestScore float32
	var matched bool
	for _, result := range results {
		route, ok := r.routes[result.ID]
		if !ok {
			continue
		}
		score := cosineSimilarity(vec, route.Embedding)
		if !matched || score > bestScore || (score == bestScore && result.ID < bestName) {
			bestRoute = route
			bestName = result.ID
			bestScore = score
			matched = true
		}
	}
	if !matched || bestScore < r.threshold {
		return nil, "", false
	}
	return bestRoute.Handler, bestName, true
}
