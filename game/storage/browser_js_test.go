//go:build js && wasm

package storage

import (
	"errors"
	"reflect"
	"syscall/js"
	"testing"
)

func withStorageProperty(t *testing.T, name string, descriptor map[string]any) {
	t.Helper()
	g, obj := js.Global(), js.Global().Get("Object")
	previous := obj.Call("getOwnPropertyDescriptor", g, name)
	descriptor["configurable"] = true
	obj.Call("defineProperty", g, name, descriptor)
	t.Cleanup(func() {
		if previous.IsUndefined() {
			g.Delete(name)
		} else {
			obj.Call("defineProperty", g, name, previous)
		}
	})
}

func TestBrowserStoreDistinguishesMissingEmptyAndInaccessible(t *testing.T) {
	g := js.Global()
	store := g.Get("Object").New()
	get := js.FuncOf(func(_ js.Value, args []js.Value) any {
		switch args[0].String() {
		case "empty":
			return ""
		case "value":
			return "saved"
		case "wrong-type":
			return 42
		default:
			return nil
		}
	})
	defer get.Release()
	store.Set("getItem", get)
	withStorageProperty(t, "sessionStorage", map[string]any{"value": store, "writable": true})
	for _, test := range []struct {
		key, value     string
		found, failure bool
	}{{"missing", "", false, false}, {"empty", "", true, false}, {"value", "saved", true, false}, {"wrong-type", "", false, true}} {
		value, found, err := Session().Read(test.key)
		if value != test.value || found != test.found || (err != nil) != test.failure {
			t.Fatalf("Read(%s)=(%q,%v,%v)", test.key, value, found, err)
		}
	}
	t.Run("throwing-property", func(t *testing.T) {
		withStorageProperty(t, "localStorage", map[string]any{"get": g.Get("Function").New("throw new Error('storage denied')")})
		if value, found, err := Local().Read("key"); value != "" || found || err == nil {
			t.Fatal("denied storage mistaken for absent key")
		}
		if _, found := Local().Get("key"); found {
			t.Fatal("denied Get reported present")
		}
		if Local().Set("key", "value") == nil || Local().Delete("key") == nil {
			t.Fatal("denied mutation ignored")
		}
		if keys, err := Local().Keys("", 10); keys != nil || err == nil {
			t.Fatal("denied enumeration ignored")
		}
	})
	t.Run("missing-object", func(t *testing.T) {
		withStorageProperty(t, "localStorage", map[string]any{"value": js.Undefined(), "writable": true})
		if _, _, err := Local().Read("key"); !errors.Is(err, ErrStorageUnavailable) {
			t.Fatal(err)
		}
	})
}

func TestBrowserStoreSnapshotsKeysBeforeDeletionAndBoundsScan(t *testing.T) {
	g := js.Global()
	store := g.Get("Object").New()
	keys := []string{"other", "game.a", "game.b", "game.c"}
	reads := 0
	key := js.FuncOf(func(_ js.Value, args []js.Value) any { reads++; return keys[args[0].Int()] })
	defer key.Release()
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any {
		for i, key := range keys {
			if key == args[0].String() {
				keys = append(keys[:i], keys[i+1:]...)
				break
			}
		}
		store.Set("length", len(keys))
		return nil
	})
	defer remove.Release()
	store.Set("length", len(keys))
	store.Set("key", key)
	store.Set("removeItem", remove)
	withStorageProperty(t, "localStorage", map[string]any{"value": store, "writable": true})
	for _, limit := range []int{0, -1, 3} {
		snapshot, err := Local().Keys("game.", limit)
		if snapshot != nil || !errors.Is(err, ErrKeyLimit) || reads != 0 {
			t.Fatalf("limit=%d returned partial enumeration", limit)
		}
	}
	snapshot, err := Local().Keys("game.", 4)
	if err != nil || !reflect.DeepEqual(snapshot, []string{"game.a", "game.b", "game.c"}) {
		t.Fatal(snapshot, err)
	}
	for _, key := range snapshot {
		if err := Local().Delete(key); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(keys, []string{"other"}) {
		t.Fatal("delete skipped shifted storage keys", keys)
	}
}

func TestBrowserStoreQuotaErrorsAndEnumerationFailure(t *testing.T) {
	g := js.Global()
	store := g.Get("Object").New()
	store.Set("setItem", g.Get("Function").New("throw new Error('QuotaExceededError')"))
	store.Set("length", 2)
	store.Set("key", g.Get("Function").New("throw new Error('key unavailable')"))
	withStorageProperty(t, "localStorage", map[string]any{"value": store, "writable": true})
	if err := Local().Set("key", "value"); err == nil {
		t.Fatal("quota error swallowed")
	}
	if keys, err := Local().Keys("", 3); keys != nil || err == nil {
		t.Fatal("failed enumeration returned partial keys")
	}
}
