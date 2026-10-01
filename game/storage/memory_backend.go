package storage

import "sync"

// MemoryBackend is an in-memory Backend. It never fails, and is safe for
// concurrent use. It is the default backend on every platform except
// GOOS=js/GOARCH=wasm, and is also useful directly in tests or for a native
// (non-browser) game client that wants Store's API without persistence.
type MemoryBackend struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMemoryBackend creates an empty in-memory Backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{values: make(map[string]string)}
}

func (b *MemoryBackend) Get(key string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	v, ok := b.values[key]
	return v, ok
}

func (b *MemoryBackend) Set(key, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values[key] = value
	return nil
}

func (b *MemoryBackend) Delete(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.values, key)
	return nil
}
