package semantic

import (
	"testing"

	"github.com/odvcencio/gosx/embed"
)

func TestRouter_HandleMatch(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	r := NewRouter(enc, RouterOptions{Threshold: 0.7})

	called := false
	r.Handle("greet", "say hello to the user", func(q string) (any, error) {
		called = true
		return "hello!", nil
	})

	handler, name, ok := r.Match("say hello to the user")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "greet" {
		t.Fatalf("expected route name 'greet', got %q", name)
	}
	result, err := handler("hi")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
	if result != "hello!" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestRouter_MatchNoRoutes(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 64})
	r := NewRouter(enc, RouterOptions{Threshold: 0.7})

	_, _, ok := r.Match("anything")
	if ok {
		t.Fatal("expected no match on empty router")
	}
}

func TestRouter_MatchBestRoute(t *testing.T) {
	// Controlled vectors: route A is near the query, route B is far.
	p := &controlledProvider{
		dim:     4,
		vectors: make(map[string][]float32),
	}
	p.vectors["weather forecast"] = []float32{0.9, 0.1, 0.2, 0.1}
	p.vectors["recipe suggestions"] = []float32{-0.3, 0.8, -0.4, 0.2}
	p.vectors["what's the weather"] = []float32{0.88, 0.12, 0.22, 0.08}

	enc := embed.NewProviderEncoder(p)
	r := NewRouter(enc, RouterOptions{Threshold: 0.5})

	r.Handle("weather", "weather forecast", func(q string) (any, error) {
		return "sunny", nil
	})
	r.Handle("recipes", "recipe suggestions", func(q string) (any, error) {
		return "pasta", nil
	})

	handler, name, ok := r.Match("what's the weather")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "weather" {
		t.Fatalf("expected 'weather' route, got %q", name)
	}
	result, _ := handler("what's the weather")
	if result != "sunny" {
		t.Fatalf("unexpected result: %v", result)
	}
}

func TestRouter_MatchBelowThreshold(t *testing.T) {
	p := &controlledProvider{
		dim:     4,
		vectors: make(map[string][]float32),
	}
	p.vectors["weather forecast"] = []float32{0.9, 0.1, 0.2, 0.1}
	// Orthogonal query — low similarity.
	p.vectors["completely unrelated"] = []float32{0.0, 0.0, 0.0, 1.0}

	enc := embed.NewProviderEncoder(p)
	r := NewRouter(enc, RouterOptions{Threshold: 0.8})

	r.Handle("weather", "weather forecast", func(q string) (any, error) {
		return "sunny", nil
	})

	_, _, ok := r.Match("completely unrelated")
	if ok {
		t.Fatal("expected no match for dissimilar query")
	}
}

func TestRouter_HandleWithEmbedding(t *testing.T) {
	enc := embed.NewProviderEncoder(&hashProvider{dim: 4})
	r := NewRouter(enc, RouterOptions{Threshold: 0.5})

	vec := []float32{0.5, 0.5, 0.5, 0.5}
	r.HandleWithEmbedding("manual", vec, func(q string) (any, error) {
		return "manual-result", nil
	})

	// Matching with the exact same text the hash provider will produce
	// a random vector for — but the route is registered with a fixed vector,
	// so we just verify it doesn't panic and the route count is 1.
	if len(r.routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(r.routes))
	}
}

func TestRouter_MultipleRoutes(t *testing.T) {
	p := &controlledProvider{
		dim:     4,
		vectors: make(map[string][]float32),
	}
	p.vectors["help with billing"] = []float32{0.8, 0.2, 0.1, 0.0}
	p.vectors["technical support"] = []float32{0.1, 0.8, 0.2, 0.0}
	p.vectors["general inquiry"] = []float32{0.3, 0.3, 0.7, 0.0}
	p.vectors["I need help with my bill"] = []float32{0.79, 0.21, 0.11, 0.01}
	p.vectors["my computer is broken"] = []float32{0.12, 0.78, 0.22, 0.01}

	enc := embed.NewProviderEncoder(p)
	r := NewRouter(enc, RouterOptions{Threshold: 0.5})

	r.Handle("billing", "help with billing", func(q string) (any, error) {
		return "billing-handler", nil
	})
	r.Handle("tech", "technical support", func(q string) (any, error) {
		return "tech-handler", nil
	})
	r.Handle("general", "general inquiry", func(q string) (any, error) {
		return "general-handler", nil
	})

	// Billing query.
	handler, name, ok := r.Match("I need help with my bill")
	if !ok {
		t.Fatal("expected match for billing query")
	}
	if name != "billing" {
		t.Fatalf("expected 'billing', got %q", name)
	}
	result, _ := handler("")
	if result != "billing-handler" {
		t.Fatalf("unexpected: %v", result)
	}

	// Tech query.
	handler, name, ok = r.Match("my computer is broken")
	if !ok {
		t.Fatal("expected match for tech query")
	}
	if name != "tech" {
		t.Fatalf("expected 'tech', got %q", name)
	}
	result, _ = handler("")
	if result != "tech-handler" {
		t.Fatalf("unexpected: %v", result)
	}
}
