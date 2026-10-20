//go:build !(js && wasm)

package storage

import (
	"errors"
	"testing"
)

func TestBrowserStoresUnavailableOnNative(t *testing.T) {
	for _, store := range []BrowserStore{Local(), Session(), {}} {
		if value, found, err := store.Read("key"); value != "" || found || !errors.Is(err, ErrStorageUnavailable) {
			t.Fatal(value, found, err)
		}
		if _, found := store.Get("key"); found {
			t.Fatal("native browser store reported a value")
		}
		if !errors.Is(store.Set("key", "value"), ErrStorageUnavailable) || !errors.Is(store.Delete("key"), ErrStorageUnavailable) {
			t.Fatal("native store mutation")
		}
		if keys, err := store.Keys("", 10); keys != nil || !errors.Is(err, ErrStorageUnavailable) {
			t.Fatal(keys, err)
		}
	}
	store := NewDefault("native")
	if err := store.Set("key", "value"); err != nil {
		t.Fatal(err)
	}
	if value, found := store.Get("key"); !found || value != "value" {
		t.Fatal("native default memory backend changed")
	}
}
