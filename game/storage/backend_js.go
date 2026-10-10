//go:build js && wasm

package storage

import (
	"fmt"
	"strings"
	"syscall/js"
)

func defaultBackend() Backend { return Local() }

func (s BrowserStore) value() (js.Value, error) {
	if s.name != "localStorage" && s.name != "sessionStorage" {
		return js.Undefined(), ErrStorageUnavailable
	}
	// Call catches policy getter exceptions; syscall/js.Get would let them
	// escape directly through the WASM host before Go recovery can run.
	value := js.Global().Get("Reflect").Call("get", js.Global(), s.name)
	if value.IsNull() || value.IsUndefined() {
		return js.Undefined(), ErrStorageUnavailable
	}
	return value, nil
}

// Read returns a stored string, whether it exists, and any access error.
func (s BrowserStore) Read(key string) (value string, found bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value, found = "", false
			err = fmt.Errorf("storage: read %q: %v", key, recovered)
		}
	}()
	store, err := s.value()
	if err != nil {
		return "", false, err
	}
	item := store.Call("getItem", key)
	if item.IsNull() || item.IsUndefined() {
		return "", false, nil
	}
	if item.Type() != js.TypeString {
		return "", false, fmt.Errorf("storage: read %q returned a non-string value", key)
	}
	return item.String(), true, nil
}

// Set writes one value, reporting quota, policy, and unavailable-store errors.
func (s BrowserStore) Set(key, value string) (err error) {
	defer storageRecover("set", &err)
	store, err := s.value()
	if err != nil {
		return err
	}
	store.Call("setItem", key, value)
	return nil
}

// Delete removes one key. An absent key is not an error.
func (s BrowserStore) Delete(key string) (err error) {
	defer storageRecover("delete", &err)
	store, err := s.value()
	if err != nil {
		return err
	}
	store.Call("removeItem", key)
	return nil
}

// Keys snapshots keys with prefix, scanning at most limit storage entries.
// A nonpositive limit or a larger store returns ErrKeyLimit without a partial
// list. Snapshot before deleting: deleting during index traversal skips keys.
func (s BrowserStore) Keys(prefix string, limit int) (keys []string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			keys = nil
			err = fmt.Errorf("storage: enumerate: %v", recovered)
		}
	}()
	if limit <= 0 {
		return nil, ErrKeyLimit
	}
	store, err := s.value()
	if err != nil {
		return nil, err
	}
	length := store.Get("length").Int()
	if length < 0 || length > limit {
		return nil, ErrKeyLimit
	}
	for i := 0; i < length; i++ {
		key := store.Call("key", i)
		if key.Type() == js.TypeString {
			text := key.String()
			if strings.HasPrefix(text, prefix) {
				keys = append(keys, text)
			}
		}
	}
	return keys, nil
}

func storageRecover(operation string, err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("storage: %s: %v", operation, recovered)
	}
}
