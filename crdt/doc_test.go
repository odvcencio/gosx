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
	if hash := doc.Commit("set title"); hash == (ChangeHash{}) {
		t.Fatal("expected non-zero change hash")
	}

	saved := doc.Save()
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
	base.Commit("seed")

	left := base.Fork()
	right := base.Fork()
	actor, err := NewActorID()
	if err != nil {
		t.Fatalf("new actor id: %v", err)
	}
	right.actorID = actor

	if err := left.Put(Root, "title", StringValue("left")); err != nil {
		t.Fatalf("left put: %v", err)
	}
	left.Commit("left")

	if err := right.Put(Root, "title", StringValue("right")); err != nil {
		t.Fatalf("right put: %v", err)
	}
	right.Commit("right")

	leftMerged := left.Fork()
	rightMerged := right.Fork()
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
	server.Commit("seed")

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
	client.Commit("client update")

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
	base.Commit("init list")

	left := base.Fork()
	right := base.Fork()
	actor, err := NewActorID()
	if err != nil {
		t.Fatalf("new actor id: %v", err)
	}
	right.actorID = actor

	if err := left.InsertAt(items, 0, StringValue("left")); err != nil {
		t.Fatalf("left insert: %v", err)
	}
	left.Commit("left insert")

	if err := right.InsertAt(items, 0, StringValue("right")); err != nil {
		t.Fatalf("right insert: %v", err)
	}
	right.Commit("right insert")

	leftMerged := left.Fork()
	rightMerged := right.Fork()
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
