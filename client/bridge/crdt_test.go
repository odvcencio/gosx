//go:build !gosx_tiny_runtime || gosx_runtime_collab || gosx_runtime_full

package bridge

import (
	"testing"

	"m31labs.dev/gosx/crdt"
)

func TestCRDTBridgeInitPutAndGet(t *testing.T) {
	b := NewCRDTBridge()
	if err := b.Put(crdt.Root, "title", `"hello"`); err != nil {
		t.Fatalf("put crdt title: %v", err)
	}

	got, err := b.Get(crdt.Root, "title")
	if err != nil {
		t.Fatalf("get crdt title: %v", err)
	}
	if got != `"hello"` {
		t.Fatalf("expected JSON string hello, got %s", got)
	}

	saved, err := b.Doc().Save()
	if err != nil {
		t.Fatalf("save crdt doc: %v", err)
	}
	other := NewCRDTBridge()
	if err := other.InitDoc(saved); err != nil {
		t.Fatalf("init saved crdt doc: %v", err)
	}
	got, err = other.Get(crdt.Root, "title")
	if err != nil {
		t.Fatalf("get restored crdt title: %v", err)
	}
	if got != `"hello"` {
		t.Fatalf("expected restored JSON string hello, got %s", got)
	}
}
