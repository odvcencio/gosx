package surface

import "sync"

// surfaceRegistry is the in-memory store for discovered surface components.
// It is populated by Discover and read by Renderer.Mount.
type surfaceRegistry struct {
	mu      sync.RWMutex
	entries map[string]*registryEntry
}

func (r *surfaceRegistry) register(component string, entry *registryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]*registryEntry)
	}
	r.entries[component] = entry
}

func (r *surfaceRegistry) lookup(component string) (*registryEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[component]
	return e, ok
}

// injectRegistryEntry is used by tests to seed the registry without running Discover.
func injectRegistryEntry(component string, entry *registryEntry) {
	registry.register(component, entry)
}
