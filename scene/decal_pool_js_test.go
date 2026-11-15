//go:build js && wasm

package scene

import (
	"syscall/js"
	"testing"
)

func TestDecalPoolPublishWritesTheBrowserStream(t *testing.T) {
	js.Global().Set("__gosx_scene3d_decal_pools", js.Undefined())
	pool := NewDecalPool(DecalPoolConfig{Name: "hits", Capacity: 4})
	pool.Spawn(DecalSpawn{Position: Vec3(1, 2, 3), Layer: 2})
	pool.Spawn(DecalSpawn{Position: Vec3(4, 5, 6)})
	if !pool.Publish() {
		t.Fatal("Publish reported no browser")
	}
	store := js.Global().Get("__gosx_scene3d_decal_pools").Get("hits")
	if got := store.Get("count").Int(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	data := store.Get("data")
	if got := data.Get("length").Int(); got != 4*DecalRecordStride {
		t.Fatalf("data length = %d, want capacity * stride", got)
	}
	if got := data.Index(0).Float(); got != 1 {
		t.Fatalf("first record x = %v, want 1", got)
	}
	if got := data.Index(DecalRecordStride + 2).Float(); got != 6 {
		t.Fatalf("second record z = %v, want 6", got)
	}
	revision := store.Get("revision").Int()
	data.SetIndex(0, 99) // a second publish with no change must not copy over this
	if !pool.Publish() || data.Index(0).Float() != 99 {
		t.Fatal("an unchanged pool must not be copied again")
	}
	pool.Spawn(DecalSpawn{Position: Vec3(7, 8, 9)})
	pool.Publish()
	if store.Get("revision").Int() <= revision || store.Get("count").Int() != 3 || data.Index(0).Float() != 1 {
		t.Fatal("a changed pool must publish its records again")
	}
}
