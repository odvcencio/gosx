package crdt

import (
	"fmt"
	"strings"
	"testing"

	crdtsync "m31labs.dev/gosx/crdt/sync"
)

func TestDocConvergesAcrossPartitionedConcurrentTextEdits(t *testing.T) {
	base := NewDoc()
	textID, err := base.MakeText(Root, "body")
	if err != nil {
		t.Fatal(err)
	}
	for i, ch := range "abcdefghij" {
		if err := base.InsertAt(textID, uint64(i), StringValue(string(ch))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := base.Commit("seed text"); err != nil {
		t.Fatal(err)
	}

	left := forkWithActor(t, base, 1)
	right := forkWithActor(t, base, 2)
	third := forkWithActor(t, base, 3)

	if err := left.DeleteAt(textID, 2); err != nil {
		t.Fatal(err)
	}
	if err := left.InsertAt(textID, 0, StringValue("L")); err != nil {
		t.Fatal(err)
	}
	if _, err := left.Commit("left partition edits"); err != nil {
		t.Fatal(err)
	}

	if err := right.DeleteAt(textID, 2); err != nil {
		t.Fatal(err)
	}
	if err := right.InsertAt(textID, 9, StringValue("R")); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Commit("right partition edits"); err != nil {
		t.Fatal(err)
	}

	if err := third.InsertAt(textID, 5, StringValue("T")); err != nil {
		t.Fatal(err)
	}
	if _, err := third.Commit("third partition edits"); err != nil {
		t.Fatal(err)
	}

	mergeAll(t, []*Doc{left, right, third})
	assertSameText(t, textID, left, right, third)

	got, err := left.TextToString(textID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"L", "R", "T"} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged text %q missing concurrent insert %q", got, want)
		}
	}
	if strings.Contains(got, "c") {
		t.Fatalf("merged text %q kept concurrently deleted character c", got)
	}
}

func TestDocSyncConvergesAfterLargePartitionedHistory(t *testing.T) {
	base := NewDoc()
	items, err := base.MakeList(Root, "items")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Commit("seed list"); err != nil {
		t.Fatal(err)
	}

	left := forkWithActor(t, base, 10)
	right := forkWithActor(t, base, 20)
	for i := 0; i < 750; i++ {
		if err := left.InsertAt(items, uint64(i), StringValue(fmt.Sprintf("left-%03d", i))); err != nil {
			t.Fatal(err)
		}
		if err := left.Put(Root, Prop(fmt.Sprintf("left-key-%03d", i)), IntValue(int64(i))); err != nil {
			t.Fatal(err)
		}
		if err := right.InsertAt(items, uint64(i), StringValue(fmt.Sprintf("right-%03d", i))); err != nil {
			t.Fatal(err)
		}
		if err := right.Put(Root, Prop(fmt.Sprintf("right-key-%03d", i)), IntValue(int64(i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := left.Commit("left large partition"); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Commit("right large partition"); err != nil {
		t.Fatal(err)
	}

	leftState := crdtsync.NewState()
	rightState := crdtsync.NewState()
	exchangeDocs(t, left, leftState, right, rightState)

	if got, err := left.ListLen(items); err != nil || got != 1500 {
		t.Fatalf("left list len = %d, %v; want 1500", got, err)
	}
	if got, err := right.ListLen(items); err != nil || got != 1500 {
		t.Fatalf("right list len = %d, %v; want 1500", got, err)
	}
	assertSameList(t, items, left, right)
	for _, key := range []Prop{"left-key-000", "left-key-749", "right-key-000", "right-key-749"} {
		leftValue, _, leftErr := left.Get(Root, key)
		rightValue, _, rightErr := right.Get(Root, key)
		if leftErr != nil || rightErr != nil || leftValue.Int != rightValue.Int {
			t.Fatalf("key %s diverged: left=%#v/%v right=%#v/%v", key, leftValue, leftErr, rightValue, rightErr)
		}
	}
}

func TestDocSaveLoadPreservesTombstonesForFutureMerges(t *testing.T) {
	base := NewDoc()
	items, err := base.MakeList(Root, "items")
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []string{"a", "b", "c"} {
		if err := base.InsertAt(items, uint64(i), StringValue(value)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := base.Commit("seed list"); err != nil {
		t.Fatal(err)
	}

	deleter := forkWithActor(t, base, 4)
	inserter := forkWithActor(t, base, 5)
	if err := deleter.DeleteAt(items, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := deleter.Commit("delete b"); err != nil {
		t.Fatal(err)
	}
	if err := inserter.InsertAt(items, 2, StringValue("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := inserter.Commit("insert after b"); err != nil {
		t.Fatal(err)
	}

	saved, err := deleter.Save()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Merge(inserter); err != nil {
		t.Fatal(err)
	}
	if err := inserter.Merge(reloaded); err != nil {
		t.Fatal(err)
	}
	assertSameList(t, items, reloaded, inserter)
	values := listStrings(t, reloaded, items)
	if containsString(values, "b") {
		t.Fatalf("deleted tombstoned value reappeared after save/load merge: %#v", values)
	}
	if !containsString(values, "x") {
		t.Fatalf("concurrent insert after tombstone was lost: %#v", values)
	}
}

func forkWithActor(t *testing.T, doc *Doc, suffix byte) *Doc {
	t.Helper()
	fork, err := doc.Fork()
	if err != nil {
		t.Fatal(err)
	}
	fork.actorID = testActorID(suffix)
	return fork
}

func testActorID(suffix byte) ActorID {
	var actor ActorID
	actor[15] = suffix
	return actor
}

func mergeAll(t *testing.T, docs []*Doc) {
	t.Helper()
	for _, dst := range docs {
		for _, src := range docs {
			if dst == src {
				continue
			}
			if err := dst.Merge(src); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertSameText(t *testing.T, text ObjID, docs ...*Doc) {
	t.Helper()
	first, err := docs[0].TextToString(text)
	if err != nil {
		t.Fatal(err)
	}
	for i, doc := range docs[1:] {
		got, err := doc.TextToString(text)
		if err != nil {
			t.Fatal(err)
		}
		if got != first {
			t.Fatalf("text diverged at doc %d: %q != %q", i+1, got, first)
		}
	}
}

func assertSameList(t *testing.T, list ObjID, docs ...*Doc) {
	t.Helper()
	first := listStrings(t, docs[0], list)
	for i, doc := range docs[1:] {
		got := listStrings(t, doc, list)
		if strings.Join(got, "\x00") != strings.Join(first, "\x00") {
			t.Fatalf("list diverged at doc %d: len=%d want len=%d", i+1, len(got), len(first))
		}
	}
}
