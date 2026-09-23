//go:build js && wasm

package storage

import (
	"errors"
	"fmt"
	"syscall/js"
)

// ErrStorageUnavailable is returned by localStorageBackend when
// window.localStorage itself is not present (a JS environment with no DOM,
// for example).
var ErrStorageUnavailable = errors.New("storage: window.localStorage unavailable")

func defaultBackend() Backend { return newLocalStorageBackend() }

// localStorageBackend persists through window.localStorage. Every method
// recovers from a thrown JS exception — localStorage.setItem can throw
// QuotaExceededError, and reading or writing it can throw a SecurityError in
// some private-browsing modes — and reports the failure as a Go error or a
// "not found" instead of letting the panic escape.
type localStorageBackend struct{}

func newLocalStorageBackend() localStorageBackend { return localStorageBackend{} }

func (localStorageBackend) storage() (js.Value, bool) {
	global := js.Global()
	if global.IsUndefined() || global.IsNull() {
		return js.Undefined(), false
	}
	ls := global.Get("localStorage")
	if ls.IsUndefined() || ls.IsNull() {
		return js.Undefined(), false
	}
	return ls, true
}

func (b localStorageBackend) Get(key string) (value string, ok bool) {
	defer func() {
		if recover() != nil {
			value, ok = "", false
		}
	}()
	ls, present := b.storage()
	if !present {
		return "", false
	}
	item := ls.Call("getItem", key)
	if item.Type() != js.TypeString {
		return "", false
	}
	return item.String(), true
}

func (b localStorageBackend) Set(key, value string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("storage: setItem %q: %v", key, r)
		}
	}()
	ls, present := b.storage()
	if !present {
		return ErrStorageUnavailable
	}
	ls.Call("setItem", key, value)
	return nil
}

func (b localStorageBackend) Delete(key string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("storage: removeItem %q: %v", key, r)
		}
	}()
	ls, present := b.storage()
	if !present {
		return ErrStorageUnavailable
	}
	ls.Call("removeItem", key)
	return nil
}
