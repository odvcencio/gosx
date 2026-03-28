package crdt

import (
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
