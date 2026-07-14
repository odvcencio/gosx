package crdt

import (
	"strings"
	"testing"

	crdtsync "m31labs.dev/gosx/crdt/sync"
)

func TestSpliceTextRunEncodesProductionSizedEditBelowHubFrameBudget(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("text object"); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("func generated() { return }\n", 700)
	inserted, deleted, err := doc.SpliceText(textID, 0, 0, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != len([]rune(content)) || len(deleted) != 0 {
		t.Fatalf("inserted=%d deleted=%d", len(inserted), len(deleted))
	}
	if len(doc.pending) != 1 || doc.pending[0].Action != "splice" || doc.pending[0].Run != content {
		t.Fatalf("splice expanded into %#v", doc.pending)
	}
	if _, err := doc.Commit("agent write"); err != nil {
		t.Fatal(err)
	}
	state := crdtsync.NewState()
	message, ok := doc.GenerateSyncMessage(state)
	if !ok {
		t.Fatal("expected sync message")
	}
	if len(message) >= 64*1024 {
		t.Fatalf("run-encoded sync frame=%d bytes, want below 64 KiB", len(message))
	}
	replica := NewDoc()
	if err := replica.ReceiveSyncMessage(crdtsync.NewState(), message); err != nil {
		t.Fatal(err)
	}
	if got, err := replica.TextToString(textID); err != nil || got != content {
		t.Fatalf("replica length=%d err=%v", len(got), err)
	}

	if _, _, err := doc.SpliceText(textID, 100, 10000, "replacement"); err != nil {
		t.Fatal(err)
	}
	if len(doc.pending) != 1 || len(doc.pending[0].DeleteRuns) != 1 {
		t.Fatalf("large deletion was not one compact ID run: %#v", doc.pending)
	}
}

func TestSpliceTextReturnsStableElementIdentities(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := doc.SpliceText(textID, 0, 0, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	inserted, deleted, err := doc.SpliceText(textID, 1, 3, "i")
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 1 || len(deleted) != 3 {
		t.Fatalf("inserted=%v deleted=%v", inserted, deleted)
	}
	if _, err := doc.Commit("splice"); err != nil {
		t.Fatal(err)
	}
	if got, _ := doc.TextToString(textID); got != "hio" {
		t.Fatalf("text = %q, want hio", got)
	}
	if err := doc.DeleteByElemID(textID, inserted[0]); err != nil {
		t.Fatal(err)
	}
	if err := doc.ReviveElem(textID, deleted[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Commit("identity edits"); err != nil {
		t.Fatal(err)
	}
	if got, _ := doc.TextToString(textID); got != "heo" {
		t.Fatalf("text = %q, want heo", got)
	}
}

func TestElementVisibilityConvergesByOperationIdentity(t *testing.T) {
	base := NewDoc()
	textID, err := base.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := base.SpliceText(textID, 0, 0, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	elemID, err := base.ElementIDAt(textID, 0)
	if err != nil {
		t.Fatal(err)
	}
	left := forkWithActor(t, base, 70)
	right := forkWithActor(t, base, 71)
	if err := left.DeleteByElemID(textID, elemID); err != nil {
		t.Fatal(err)
	}
	if _, err := left.Commit("delete"); err != nil {
		t.Fatal(err)
	}
	if err := right.ReviveElem(textID, elemID); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Commit("revive"); err != nil {
		t.Fatal(err)
	}
	exchangeDocs(t, left, crdtsync.NewState(), right, crdtsync.NewState())
	leftText, _ := left.TextToString(textID)
	rightText, _ := right.TextToString(textID)
	if leftText != rightText {
		t.Fatalf("visibility did not converge: left=%q right=%q", leftText, rightText)
	}
}
