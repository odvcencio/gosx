package workspace

import (
	"testing"

	"github.com/odvcencio/gosx/crdt"
	"github.com/odvcencio/gosx/vecdb"
)

func TestObserverIndexesVectorOnPut(t *testing.T) {
	doc := crdt.NewDoc()
	idx := vecdb.NewWithSeed(4, 2, 42)
	obs := newObserver(doc, idx, 4)
	_ = obs

	vec := []float32{1, 0, 0, 0}
	doc.Put(crdt.Root, "finding-1", crdt.VectorValue(vec, 4, 3))
	doc.Commit("add finding")

	if idx.Len() != 1 {
		t.Fatalf("index len = %d want 1", idx.Len())
	}

	results := idx.Search(vec, 1)
	if len(results) != 1 {
		t.Fatalf("search returned %d results want 1", len(results))
	}
	if results[0].ID != "finding-1" {
		t.Errorf("result ID = %q want %q", results[0].ID, "finding-1")
	}
}

func TestObserverRemovesVectorOnDelete(t *testing.T) {
	doc := crdt.NewDoc()
	idx := vecdb.NewWithSeed(4, 2, 42)
	obs := newObserver(doc, idx, 4)
	_ = obs

	vec := []float32{1, 0, 0, 0}
	doc.Put(crdt.Root, "finding-1", crdt.VectorValue(vec, 4, 3))
	doc.Commit("add")

	if idx.Len() != 1 {
		t.Fatalf("after add: index len = %d want 1", idx.Len())
	}

	doc.Delete(crdt.Root, "finding-1")
	doc.Commit("delete")

	if idx.Len() != 0 {
		t.Fatalf("after delete: index len = %d want 0", idx.Len())
	}
}

func TestObserverIgnoresNonVectorValues(t *testing.T) {
	doc := crdt.NewDoc()
	idx := vecdb.NewWithSeed(4, 2, 42)
	obs := newObserver(doc, idx, 4)
	_ = obs

	doc.Put(crdt.Root, "name", crdt.StringValue("alice"))
	doc.Put(crdt.Root, "count", crdt.IntValue(42))
	doc.Commit("add strings")

	if idx.Len() != 0 {
		t.Fatalf("index len = %d want 0 (should ignore non-vectors)", idx.Len())
	}
}

func TestObserverHandlesRemoteSync(t *testing.T) {
	doc1 := crdt.NewDoc()
	doc2 := crdt.NewDoc()
	idx := vecdb.NewWithSeed(4, 2, 42)
	obs := newObserver(doc2, idx, 4)
	_ = obs

	vec := []float32{0, 1, 0, 0}
	doc1.Put(crdt.Root, "remote-finding", crdt.VectorValue(vec, 4, 3))
	doc1.Commit("remote add")

	doc2.Merge(doc1)

	if idx.Len() != 1 {
		t.Fatalf("after merge: index len = %d want 1", idx.Len())
	}
}
