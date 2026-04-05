package server

import (
	"strings"
	"sync"
	"sync/atomic"
)

// RevalidationStore tracks path and tag revisions used to invalidate cache
// validators across requests and instances.
type RevalidationStore interface {
	RevalidatePath(target string) uint64
	RevalidateTag(tag string) uint64
	PathVersion(requestPath string) uint64
	TagVersion(tag string) uint64
}

// InMemoryRevalidationStore is the default process-local implementation of
// RevalidationStore.
type InMemoryRevalidationStore struct {
	mu           sync.RWMutex
	seq          atomic.Uint64
	pathVersions map[string]uint64
	tagVersions  map[string]uint64
}

// NewInMemoryRevalidationStore creates an empty in-memory revalidation store.
func NewInMemoryRevalidationStore() *InMemoryRevalidationStore {
	return &InMemoryRevalidationStore{
		pathVersions: make(map[string]uint64),
		tagVersions:  make(map[string]uint64),
	}
}

// RevalidatePath invalidates cached responses for the provided path prefix.
func (s *InMemoryRevalidationStore) RevalidatePath(target string) uint64 {
	if s == nil {
		return 0
	}
	target = cleanCachePath(target)
	version := s.seq.Add(1)
	s.mu.Lock()
	s.pathVersions[target] = version
	s.mu.Unlock()
	return version
}

// RevalidateTag invalidates cached responses associated with the provided tag.
func (s *InMemoryRevalidationStore) RevalidateTag(tag string) uint64 {
	if s == nil {
		return 0
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return 0
	}
	version := s.seq.Add(1)
	s.mu.Lock()
	s.tagVersions[tag] = version
	s.mu.Unlock()
	return version
}

// PathVersion returns the newest known invalidation version that applies to
// requestPath.
func (s *InMemoryRevalidationStore) PathVersion(requestPath string) uint64 {
	if s == nil {
		return 0
	}
	requestPath = cleanCachePath(requestPath)
	var version uint64
	s.mu.RLock()
	for target, candidate := range s.pathVersions {
		if cachePathMatches(target, requestPath) && candidate > version {
			version = candidate
		}
	}
	s.mu.RUnlock()
	return version
}

// TagVersion returns the invalidation version for the provided tag.
func (s *InMemoryRevalidationStore) TagVersion(tag string) uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	version := s.tagVersions[strings.TrimSpace(tag)]
	s.mu.RUnlock()
	return version
}
