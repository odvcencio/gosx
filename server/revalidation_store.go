package server

import (
	"sort"
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

// defaultRevalidationEntries bounds how many path or tag entries the default
// store keeps. An app that invalidates one entry per item — one per blog post,
// one per product — would otherwise grow the store without limit.
const defaultRevalidationEntries = 8192

// InMemoryRevalidationStore is the default process-local implementation of
// RevalidationStore.
//
// Lookup walks the path's own ancestors instead of every registered path, so
// the cost tracks the path depth, not the number of invalidated paths. The
// store also bounds both maps: past the entry limit it drops the oldest half
// and raises a version floor to the highest dropped version. A floor
// over-invalidates the dropped entries, which costs a re-render. It never
// under-invalidates, so no client receives a stale body.
type InMemoryRevalidationStore struct {
	mu           sync.RWMutex
	seq          atomic.Uint64
	maxEntries   int
	pathFloor    uint64
	tagFloor     uint64
	pathVersions map[string]uint64
	tagVersions  map[string]uint64
}

// NewInMemoryRevalidationStore creates an empty in-memory revalidation store.
func NewInMemoryRevalidationStore() *InMemoryRevalidationStore {
	return &InMemoryRevalidationStore{
		maxEntries:   defaultRevalidationEntries,
		pathVersions: make(map[string]uint64),
		tagVersions:  make(map[string]uint64),
	}
}

// SetMaxEntries changes how many path and tag entries the store keeps. A value
// below one restores the default. Lower it to cap memory; raise it to keep more
// exact per-item versions.
func (s *InMemoryRevalidationStore) SetMaxEntries(limit int) {
	if s == nil {
		return
	}
	if limit < 1 {
		limit = defaultRevalidationEntries
	}
	s.mu.Lock()
	s.maxEntries = limit
	s.pathFloor = compactVersions(s.pathVersions, s.pathFloor, limit)
	s.tagFloor = compactVersions(s.tagVersions, s.tagFloor, limit)
	s.mu.Unlock()
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
	if s.maxEntries > 0 && len(s.pathVersions) > s.maxEntries {
		s.pathFloor = compactVersions(s.pathVersions, s.pathFloor, s.maxEntries/2)
	}
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
	if s.maxEntries > 0 && len(s.tagVersions) > s.maxEntries {
		s.tagFloor = compactVersions(s.tagVersions, s.tagFloor, s.maxEntries/2)
	}
	s.mu.Unlock()
	return version
}

// PathVersion returns the newest known invalidation version that applies to
// requestPath.
//
// RevalidatePath("/blog") invalidates "/blog" and every path under it, so the
// paths that can apply to a request are exactly the request path itself and its
// ancestors. Walking that chain costs one map lookup per path segment. The old
// loop read every registered path and cleaned each one, so an app with many
// invalidated items paid for all of them on every cacheable request.
func (s *InMemoryRevalidationStore) PathVersion(requestPath string) uint64 {
	if s == nil {
		return 0
	}
	requestPath = cleanCachePath(requestPath)
	s.mu.RLock()
	version := s.pathFloor
	target := requestPath
	for {
		if candidate := s.pathVersions[target]; candidate > version {
			version = candidate
		}
		if target == "/" {
			break
		}
		cut := strings.LastIndexByte(target, '/')
		if cut <= 0 {
			target = "/"
			continue
		}
		target = target[:cut]
	}
	s.mu.RUnlock()
	return version
}

// pathVersionCount reports how many path entries the store holds. Tests use it
// to check that per-item invalidation does not grow the store without limit.
func (s *InMemoryRevalidationStore) pathVersionCount() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	count := len(s.pathVersions)
	s.mu.RUnlock()
	return count
}

// TagVersion returns the invalidation version for the provided tag.
func (s *InMemoryRevalidationStore) TagVersion(tag string) uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	version := s.tagFloor
	if candidate := s.tagVersions[strings.TrimSpace(tag)]; candidate > version {
		version = candidate
	}
	s.mu.RUnlock()
	return version
}

// compactVersions keeps the newest keep entries and returns the raised floor.
// The caller holds the write lock.
func compactVersions(versions map[string]uint64, floor uint64, keep int) uint64 {
	if keep < 0 {
		keep = 0
	}
	if len(versions) <= keep {
		return floor
	}
	type entry struct {
		key     string
		version uint64
	}
	entries := make([]entry, 0, len(versions))
	for key, version := range versions {
		entries = append(entries, entry{key: key, version: version})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].version > entries[j].version })
	for _, dropped := range entries[keep:] {
		if dropped.version > floor {
			floor = dropped.version
		}
		delete(versions, dropped.key)
	}
	return floor
}
