package textmodel

import "testing"

type testOpSource struct {
	local  []Operation
	remote func(Operation)
}

func (s *testOpSource) ApplyLocal(operation Operation) { s.local = append(s.local, operation) }
func (s *testOpSource) SubscribeRemote(remote func(Operation)) func() {
	s.remote = remote
	return func() { s.remote = nil }
}

func TestCRDTDocumentAppliesRemoteOpsWithoutEchoAndMapsPositions(t *testing.T) {
	source := &testOpSource{}
	document := NewCRDTDocument("hello world", source)
	defer document.Close()
	var changes []DocumentChange
	document.Subscribe(func(change DocumentChange) { changes = append(changes, change) })

	document.Insert(Position{Line: 0, Col: 5}, "!")
	if len(source.local) != 1 || document.Version() != 1 {
		t.Fatalf("local=%d version=%d", len(source.local), document.Version())
	}
	source.remote(Operation{Kind: OpReplace, Range: Range{Start: Position{0, 7}, End: Position{0, 12}}, Content: []byte("gophers"), Actor: "agent"})
	if len(source.local) != 1 {
		t.Fatal("remote operation echoed through local source")
	}
	if document.Content() != "hello! gophers" || document.Version() != 2 || !changes[1].Remote {
		t.Fatalf("content=%q version=%d changes=%+v", document.Content(), document.Version(), changes)
	}
	if mapped := changes[1].MapPosition(Position{Line: 0, Col: 12}); mapped != (Position{Line: 0, Col: 14}) {
		t.Fatalf("mapped end = %+v", mapped)
	}
}

func TestCRDTDocumentVersionMonotonicAcrossInterleavedSources(t *testing.T) {
	source := &testOpSource{}
	document := NewCRDTDocument("", source)
	versions := []int{}
	document.Subscribe(func(change DocumentChange) { versions = append(versions, change.Version) })
	document.Insert(Position{}, "a")
	source.remote(Operation{Kind: OpInsert, Range: Range{Start: Position{0, 1}, End: Position{0, 1}}, Content: []byte("b")})
	document.Replace(Range{Start: Position{0, 0}, End: Position{0, 1}}, "A")
	for index, version := range versions {
		if version != index+1 {
			t.Fatalf("versions = %v", versions)
		}
	}
}
