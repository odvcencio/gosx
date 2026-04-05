package workspace

import (
	"encoding/json"
	"testing"
)

func TestWriteFindingMessage(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	msg := FindingMessage{
		ID:     "f1",
		Vector: []float32{1, 0, 0, 0},
		Text:   "discovered pattern X",
		Agent:  "cedar",
	}

	if err := ws.HandleWriteFinding(msg); err != nil {
		t.Fatal(err)
	}

	results := ws.Query([]float32{1, 0, 0, 0}, 1)
	if len(results) != 1 {
		t.Fatalf("query returned %d results want 1", len(results))
	}

	text, ok := ws.ReadMeta("f1", "text")
	if !ok || text != "discovered pattern X" {
		t.Errorf("text meta = %q, %v", text, ok)
	}

	agent, ok := ws.ReadMeta("f1", "agent")
	if !ok || agent != "cedar" {
		t.Errorf("agent meta = %q, %v", agent, ok)
	}
}

func TestQueryMessage(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})
	ws.HandleWriteFinding(FindingMessage{
		ID: "f1", Vector: []float32{1, 0, 0, 0}, Text: "pattern A", Agent: "oak",
	})
	ws.HandleWriteFinding(FindingMessage{
		ID: "f2", Vector: []float32{0, 1, 0, 0}, Text: "pattern B", Agent: "birch",
	})

	results, err := ws.HandleQuery(QueryMessage{
		Vector: []float32{1, 0, 0, 0},
		K:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("query returned %d results want 1", len(results))
	}
	if results[0].ID != "f1" {
		t.Errorf("top result ID = %q want %q", results[0].ID, "f1")
	}
}

func TestAgentJoinMessage(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	join := AgentJoinMessage{
		Name: "cedar",
		Role: "translator",
	}

	data, _ := json.Marshal(join)
	_ = data

	agents := ws.Agents()
	if len(agents) != 0 {
		t.Fatalf("agents = %d want 0 before join", len(agents))
	}

	ws.HandleAgentJoin(join)
	agents = ws.Agents()
	if len(agents) != 1 {
		t.Fatalf("agents = %d want 1 after join", len(agents))
	}
	if agents[0].Name != "cedar" {
		t.Errorf("agent name = %q want %q", agents[0].Name, "cedar")
	}
}
