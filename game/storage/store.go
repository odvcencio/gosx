package storage

import (
	"encoding/json"
	"errors"
	"strings"
)

// ErrNoBackend is returned by Set and Delete on a Namespaced constructed
// with a nil Backend. Get on such a Namespaced simply reports "not found"
// rather than erroring, matching how a full storage quota or a disabled
// localStorage should look to game code: a normal cache miss, not a fatal
// error.
var ErrNoBackend = errors.New("storage: no backend")

// Backend is a raw string key/value store. Every method reports failure
// through its return value; a Backend must never panic, even when the
// underlying storage is unavailable (private browsing, quota exceeded,
// disabled by the user).
type Backend interface {
	// Get returns the stored value and true, or "" and false if key is
	// absent or the backend could not be read.
	Get(key string) (string, bool)
	// Set stores value under key, returning a non-nil error if the write
	// failed.
	Set(key, value string) error
	// Delete removes key. Deleting an absent key is not an error.
	Delete(key string) error
}

// Namespaced wraps a Backend with a fixed key prefix so independent features
// sharing one Backend cannot collide.
type Namespaced struct {
	prefix  string
	backend Backend
}

// New wraps backend with prefix. A nil backend is accepted: every read
// reports "not found" and every write returns ErrNoBackend, so a caller can
// construct a Namespaced unconditionally and only decide whether storage is
// wired up in one place.
func New(prefix string, backend Backend) *Namespaced {
	return &Namespaced{prefix: strings.TrimSpace(prefix), backend: backend}
}

// NewDefault wraps prefix around the platform default backend: localStorage
// under GOOS=js/GOARCH=wasm, an in-memory Backend everywhere else (including
// native tests).
func NewDefault(prefix string) *Namespaced {
	return New(prefix, defaultBackend())
}

func (n *Namespaced) key(key string) string {
	if n.prefix == "" {
		return key
	}
	return n.prefix + "." + key
}

// Get returns the stored string value for key.
func (n *Namespaced) Get(key string) (string, bool) {
	if n == nil || n.backend == nil {
		return "", false
	}
	return n.backend.Get(n.key(key))
}

// Set stores value for key.
func (n *Namespaced) Set(key, value string) error {
	if n == nil {
		return ErrNoBackend
	}
	if n.backend == nil {
		return ErrNoBackend
	}
	return n.backend.Set(n.key(key), value)
}

// Delete removes key.
func (n *Namespaced) Delete(key string) error {
	if n == nil {
		return ErrNoBackend
	}
	if n.backend == nil {
		return ErrNoBackend
	}
	return n.backend.Delete(n.key(key))
}

// GetJSON decodes the JSON value stored at key into T. It returns false if
// the key is absent or the stored value fails to decode.
func GetJSON[T any](n *Namespaced, key string) (T, bool) {
	var value T
	raw, ok := n.Get(key)
	if !ok {
		return value, false
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return value, false
	}
	return value, true
}

// SetJSON encodes value as JSON and stores it at key.
func SetJSON[T any](n *Namespaced, key string, value T) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return n.Set(key, string(raw))
}
