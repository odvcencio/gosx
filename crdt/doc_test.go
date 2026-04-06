package crdt

import (
	"strconv"
	"testing"

	crdtsync "github.com/odvcencio/gosx/crdt/sync"
)

func TestDocPutCommitSaveLoad(t *testing.T) {
	doc := NewDoc()
	if err := doc.Put(Root, "title", StringValue("hello")); err != nil {
		t.Fatalf("put title: %v", err)
	}
	hash, err := doc.Commit("set title")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if hash == (ChangeHash{}) {
		t.Fatal("expected non-zero change hash")
	}

	saved, err := doc.Save()
	if err != nil {
		t.Fatalf("save doc: %v", err)
	}
	loaded, err := Load(saved)
	if err != nil {
		t.Fatalf("load saved doc: %v", err)
	}

	value, _, err := loaded.Get(Root, "title")
	if err != nil {
		t.Fatalf("get loaded title: %v", err)
	}
	if value.Str != "hello" {
		t.Fatalf("expected title hello, got %#v", value)
	}
}

func TestDocMergeConvergesAcrossForks(t *testing.T) {
	base := NewDoc()
	if err := base.Put(Root, "title", StringValue("base")); err != nil {
		t.Fatalf("seed title: %v", err)
	}
	if _, err := base.Commit("seed"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	left, err := base.Fork()
	if err != nil {
		t.Fatalf("fork left: %v", err)
	}
	right, err := base.Fork()
	if err != nil {
		t.Fatalf("fork right: %v", err)
	}
	actor, err := NewActorID()
	if err != nil {
		t.Fatalf("new actor id: %v", err)
	}
	right.actorID = actor

	if err := left.Put(Root, "title", StringValue("left")); err != nil {
		t.Fatalf("left put: %v", err)
	}
	if _, err := left.Commit("left"); err != nil {
		t.Fatalf("commit left: %v", err)
	}

	if err := right.Put(Root, "title", StringValue("right")); err != nil {
		t.Fatalf("right put: %v", err)
	}
	if _, err := right.Commit("right"); err != nil {
		t.Fatalf("commit right: %v", err)
	}

	leftMerged, err := left.Fork()
	if err != nil {
		t.Fatalf("fork left merged: %v", err)
	}
	rightMerged, err := right.Fork()
	if err != nil {
		t.Fatalf("fork right merged: %v", err)
	}
	if err := leftMerged.Merge(right); err != nil {
		t.Fatalf("merge right into left: %v", err)
	}
	if err := rightMerged.Merge(left); err != nil {
		t.Fatalf("merge left into right: %v", err)
	}

	leftValue, _, err := leftMerged.Get(Root, "title")
	if err != nil {
		t.Fatalf("left merged title: %v", err)
	}
	rightValue, _, err := rightMerged.Get(Root, "title")
	if err != nil {
		t.Fatalf("right merged title: %v", err)
	}
	if leftValue.Str != rightValue.Str {
		t.Fatalf("expected merge convergence, got left=%q right=%q", leftValue.Str, rightValue.Str)
	}
}

func TestDocSyncMessagesConverge(t *testing.T) {
	server := NewDoc()
	client := NewDoc()
	serverState := crdtsync.NewState()
	clientState := crdtsync.NewState()

	if err := server.Put(Root, "title", StringValue("server")); err != nil {
		t.Fatalf("server seed put: %v", err)
	}
	if _, err := server.Commit("seed"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	exchangeDocs(t, server, serverState, client, clientState)

	clientValue, _, err := client.Get(Root, "title")
	if err != nil {
		t.Fatalf("client synced title: %v", err)
	}
	if clientValue.Str != "server" {
		t.Fatalf("expected client title server, got %q", clientValue.Str)
	}

	if err := client.Put(Root, "subtitle", StringValue("from client")); err != nil {
		t.Fatalf("client subtitle put: %v", err)
	}
	if _, err := client.Commit("client update"); err != nil {
		t.Fatalf("commit client update: %v", err)
	}

	exchangeDocs(t, server, serverState, client, clientState)

	serverValue, _, err := server.Get(Root, "subtitle")
	if err != nil {
		t.Fatalf("server synced subtitle: %v", err)
	}
	if serverValue.Str != "from client" {
		t.Fatalf("expected server subtitle from client, got %q", serverValue.Str)
	}

	if _, ok := server.GenerateSyncMessage(serverState); ok {
		t.Fatal("expected synced server to have no further messages")
	}
	if _, ok := client.GenerateSyncMessage(clientState); ok {
		t.Fatal("expected synced client to have no further messages")
	}
}

func TestDocListMergeConvergesAcrossForks(t *testing.T) {
	base := NewDoc()
	items, err := base.MakeList(Root, "items")
	if err != nil {
		t.Fatalf("make list: %v", err)
	}
	if _, err := base.Commit("init list"); err != nil {
		t.Fatalf("commit init list: %v", err)
	}

	left, err := base.Fork()
	if err != nil {
		t.Fatalf("fork left: %v", err)
	}
	right, err := base.Fork()
	if err != nil {
		t.Fatalf("fork right: %v", err)
	}
	actor, err := NewActorID()
	if err != nil {
		t.Fatalf("new actor id: %v", err)
	}
	right.actorID = actor

	if err := left.InsertAt(items, 0, StringValue("left")); err != nil {
		t.Fatalf("left insert: %v", err)
	}
	if _, err := left.Commit("left insert"); err != nil {
		t.Fatalf("commit left insert: %v", err)
	}

	if err := right.InsertAt(items, 0, StringValue("right")); err != nil {
		t.Fatalf("right insert: %v", err)
	}
	if _, err := right.Commit("right insert"); err != nil {
		t.Fatalf("commit right insert: %v", err)
	}

	leftMerged, err := left.Fork()
	if err != nil {
		t.Fatalf("fork left merged: %v", err)
	}
	rightMerged, err := right.Fork()
	if err != nil {
		t.Fatalf("fork right merged: %v", err)
	}
	if err := leftMerged.Merge(right); err != nil {
		t.Fatalf("merge right into left: %v", err)
	}
	if err := rightMerged.Merge(left); err != nil {
		t.Fatalf("merge left into right: %v", err)
	}

	leftValues := listStrings(t, leftMerged, items)
	rightValues := listStrings(t, rightMerged, items)
	if len(leftValues) != 2 {
		t.Fatalf("expected 2 merged list values, got %#v", leftValues)
	}
	if len(rightValues) != 2 {
		t.Fatalf("expected 2 merged list values, got %#v", rightValues)
	}
	if leftValues[0] != rightValues[0] || leftValues[1] != rightValues[1] {
		t.Fatalf("expected list convergence, got left=%#v right=%#v", leftValues, rightValues)
	}
	if !containsString(leftValues, "left") || !containsString(leftValues, "right") {
		t.Fatalf("expected merged list to contain both inserts, got %#v", leftValues)
	}
}

func TestMakeTextAndInsert(t *testing.T) {
	doc := NewDoc()
	textID, err := doc.MakeText(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if textID == "" {
		t.Fatal("expected non-empty text object ID")
	}

	// Insert characters
	if err := doc.InsertAt(textID, 0, StringValue("H")); err != nil {
		t.Fatal(err)
	}
	if err := doc.InsertAt(textID, 1, StringValue("i")); err != nil {
		t.Fatal(err)
	}

	_, err = doc.Commit("add text")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the text object exists and has the right kind
	val, _, err := doc.Get(Root, "content")
	if err != nil {
		t.Fatal(err)
	}
	if val.Kind != ValueKindText {
		t.Fatalf("expected text kind, got %s", val.Kind)
	}
}

func exchangeDocs(t *testing.T, left *Doc, leftState *crdtsync.State, right *Doc, rightState *crdtsync.State) {
	t.Helper()
	for i := 0; i < 8; i++ {
		progress := false
		if msg, ok := left.GenerateSyncMessage(leftState); ok {
			progress = true
			if err := right.ReceiveSyncMessage(rightState, msg); err != nil {
				t.Fatalf("receive right sync message: %v", err)
			}
		}
		if msg, ok := right.GenerateSyncMessage(rightState); ok {
			progress = true
			if err := left.ReceiveSyncMessage(leftState, msg); err != nil {
				t.Fatalf("receive left sync message: %v", err)
			}
		}
		if !progress {
			return
		}
	}
	t.Fatal("sync exchange did not converge")
}

func listStrings(t *testing.T, doc *Doc, obj ObjID) []string {
	t.Helper()
	values := []string{}
	for index := 0; ; index += 1 {
		value, _, err := doc.Get(obj, Prop(strconv.Itoa(index)))
		if err != nil {
			return values
		}
		values = append(values, value.Str)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
