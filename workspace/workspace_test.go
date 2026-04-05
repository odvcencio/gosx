package workspace

import (
	"testing"
)

func TestWorkspaceCreation(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})
	if ws.Name() != "test" {
		t.Errorf("name = %q want %q", ws.Name(), "test")
	}
	if ws.Doc() == nil {
		t.Fatal("doc is nil")
	}
	if ws.Index() == nil {
		t.Fatal("index is nil")
	}
}

func TestWorkspaceWriteAndQuery(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	vec := []float32{1, 0, 0, 0}
	ws.WriteVector("finding-1", vec)

	results := ws.Query(vec, 1)
	if len(results) != 1 {
		t.Fatalf("query returned %d results want 1", len(results))
	}
	if results[0].ID != "finding-1" {
		t.Errorf("result ID = %q want %q", results[0].ID, "finding-1")
	}
}

func TestWorkspaceWriteWithMetadata(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	vec := []float32{0, 1, 0, 0}
	ws.WriteVector("finding-1", vec)
	ws.WriteMeta("finding-1", "text", "discovered a pattern")
	ws.WriteMeta("finding-1", "agent", "cedar")

	text, ok := ws.ReadMeta("finding-1", "text")
	if !ok || text != "discovered a pattern" {
		t.Errorf("meta text = %q, %v", text, ok)
	}
	agent, ok := ws.ReadMeta("finding-1", "agent")
	if !ok || agent != "cedar" {
		t.Errorf("meta agent = %q, %v", agent, ok)
	}
}

func TestWorkspaceSaveLoad(t *testing.T) {
	ws := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	vec := []float32{1, 0, 0, 0}
	ws.WriteVector("finding-1", vec)

	data, err := ws.Save()
	if err != nil {
		t.Fatal(err)
	}

	ws2 := New(Options{Name: "test", Dim: 4, BitWidth: 2})
	if err := ws2.Load(data); err != nil {
		t.Fatal(err)
	}

	results := ws2.Query(vec, 1)
	if len(results) != 1 {
		t.Fatalf("after load: query returned %d results want 1", len(results))
	}
}

func TestWorkspaceMerge(t *testing.T) {
	ws1 := New(Options{Name: "test", Dim: 4, BitWidth: 2})
	ws2 := New(Options{Name: "test", Dim: 4, BitWidth: 2})

	ws1.WriteVector("finding-a", []float32{1, 0, 0, 0})
	ws2.WriteVector("finding-b", []float32{0, 1, 0, 0})

	data1, _ := ws1.Save()
	ws2.Load(data1)

	if ws2.Index().Len() != 2 {
		t.Fatalf("after merge: index len = %d want 2", ws2.Index().Len())
	}
}
