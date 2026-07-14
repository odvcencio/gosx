package crdt

import "testing"

func TestCommitAssignsStableChangeGroupIDs(t *testing.T) {
	doc := NewDoc()
	if err := doc.Put(Root, "first", StringValue("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.CommitWithGroup("first edit", "edit-42"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "second", StringValue("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.CommitWithGroup("second edit", "edit-42"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Put(Root, "third", StringValue("c")); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("default group"); err != nil {
		t.Fatal(err)
	}

	if len(doc.changes) != 3 {
		t.Fatalf("changes=%d", len(doc.changes))
	}
	if doc.changes[0].ChangeGroupID != "edit-42" || doc.changes[1].ChangeGroupID != "edit-42" {
		t.Fatalf("explicit groups=%q,%q", doc.changes[0].ChangeGroupID, doc.changes[1].ChangeGroupID)
	}
	wantDefault := doc.actorID.String() + ":3"
	if doc.changes[2].ChangeGroupID != wantDefault {
		t.Fatalf("default group=%q want=%q", doc.changes[2].ChangeGroupID, wantDefault)
	}
	chunk, _, err := EncodeChangeChunk(doc.changes[0])
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeChangeChunk(chunk)
	if err != nil || decoded.ChangeGroupID != "edit-42" {
		t.Fatalf("decoded group=%q err=%v", decoded.ChangeGroupID, err)
	}
}
