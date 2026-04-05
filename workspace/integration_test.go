package workspace

import (
	"math"
	"testing"
)

func TestIntegrationFullRoundTrip(t *testing.T) {
	ws := New(Options{Name: "integration", Dim: 8, BitWidth: 2, Seed: 42})

	// Three agents write findings
	findings := []FindingMessage{
		{ID: "f1", Vector: normalize([]float32{1, 1, 0, 0, 0, 0, 0, 0}), Text: "PERFORM THRU expansion pattern", Agent: "cedar"},
		{ID: "f2", Vector: normalize([]float32{1, 0.5, 0.5, 0, 0, 0, 0, 0}), Text: "PERFORM range normalization", Agent: "oak"},
		{ID: "f3", Vector: normalize([]float32{0, 0, 0, 0, 1, 1, 0, 0}), Text: "EXEC SQL cursor loop", Agent: "birch"},
	}

	for _, f := range findings {
		if err := ws.HandleWriteFinding(f); err != nil {
			t.Fatalf("write %s: %v", f.ID, err)
		}
	}

	// Query for PERFORM-related findings — f1 and f2 should rank higher than f3
	results, err := ws.HandleQuery(QueryMessage{
		Vector: normalize([]float32{1, 1, 0, 0, 0, 0, 0, 0}),
		K:      3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results want 3", len(results))
	}
	// f1 should be top result (exact match direction)
	if results[0].ID != "f1" {
		t.Errorf("top result = %q want f1", results[0].ID)
	}
	// f3 should be last (orthogonal)
	if results[2].ID != "f3" {
		t.Errorf("bottom result = %q want f3", results[2].ID)
	}

	// Save and reload
	data, err := ws.Save()
	if err != nil {
		t.Fatal(err)
	}

	ws2 := New(Options{Name: "integration", Dim: 8, BitWidth: 2, Seed: 42})
	if err := ws2.Load(data); err != nil {
		t.Fatal(err)
	}

	// Verify index rebuilt with same ranking
	results2, _ := ws2.HandleQuery(QueryMessage{
		Vector: normalize([]float32{1, 1, 0, 0, 0, 0, 0, 0}),
		K:      3,
	})
	if len(results2) != 3 {
		t.Fatalf("after load: got %d results want 3", len(results2))
	}
	if results2[0].ID != "f1" {
		t.Errorf("after load: top result = %q want f1", results2[0].ID)
	}

	// Verify metadata survived
	text, ok := ws2.ReadMeta("f1", "text")
	if !ok || text != "PERFORM THRU expansion pattern" {
		t.Errorf("after load: f1 text = %q, %v", text, ok)
	}
	agent, ok := ws2.ReadMeta("f1", "agent")
	if !ok || agent != "cedar" {
		t.Errorf("after load: f1 agent = %q, %v", agent, ok)
	}
}

func TestIntegrationTwoWorkspacesMerge(t *testing.T) {
	ws1 := New(Options{Name: "merge1", Dim: 4, BitWidth: 2, Seed: 42})
	ws2 := New(Options{Name: "merge2", Dim: 4, BitWidth: 2, Seed: 42})

	ws1.HandleWriteFinding(FindingMessage{
		ID: "a1", Vector: []float32{1, 0, 0, 0}, Text: "finding from session 1", Agent: "cedar",
	})
	ws2.HandleWriteFinding(FindingMessage{
		ID: "b1", Vector: []float32{0, 1, 0, 0}, Text: "finding from session 2", Agent: "oak",
	})

	// Merge session 1 into session 2
	data1, _ := ws1.Save()
	ws2.Load(data1)

	// Both findings should be searchable
	if ws2.Index().Len() != 2 {
		t.Fatalf("merged index len = %d want 2", ws2.Index().Len())
	}

	// Metadata from both sessions survives
	text1, ok := ws2.ReadMeta("a1", "text")
	if !ok || text1 != "finding from session 1" {
		t.Errorf("a1 text = %q, %v", text1, ok)
	}
	text2, ok := ws2.ReadMeta("b1", "text")
	if !ok || text2 != "finding from session 2" {
		t.Errorf("b1 text = %q, %v", text2, ok)
	}
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	scale := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * scale
	}
	return out
}
