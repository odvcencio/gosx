package island

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// SceneEntryPathPrefix is where the server serves deferred Scene3D props.
const SceneEntryPathPrefix = "/gosx/scene3d/"

const (
	sceneEntryMaxEntries = 64
	sceneEntryMaxBytes   = 64 << 20
	sceneEntryHashLen    = 32 // hex characters of SHA-256; 128 bits is enough for a cache key
)

// SceneEntryStore keeps the props of deferred Scene3D engines by content
// hash, so a page can name them with an immutable URL and the browser and
// CDN can cache them. It is a bounded LRU: oldest entries go first.
type SceneEntryStore struct {
	mu         sync.Mutex
	entries    map[string][]byte
	order      []string // least recently used first
	maxEntries int
	maxBytes   int64
	bytes      int64
}

// NewSceneEntryStore returns a store bounded by entry count and total bytes.
func NewSceneEntryStore(maxEntries int, maxBytes int64) *SceneEntryStore {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &SceneEntryStore{entries: map[string][]byte{}, maxEntries: maxEntries, maxBytes: maxBytes}
}

// SceneEntryHash returns the content hash used in a scene entry URL.
func SceneEntryHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:sceneEntryHashLen]
}

// ValidSceneEntryHash reports whether s is a well-formed scene entry hash.
func ValidSceneEntryHash(s string) bool {
	if len(s) != sceneEntryHashLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// SceneEntryPath returns the URL path that serves the entry with this hash.
func SceneEntryPath(hash string) string { return SceneEntryPathPrefix + hash + ".json" }

func (s *SceneEntryStore) touch(hash string) {
	for i, h := range s.order {
		if h == hash {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, hash)
}

// Put stores a copy of body and returns its hash. A repeat refreshes its position.
func (s *SceneEntryStore) Put(body []byte) string {
	hash := SceneEntryHash(body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[hash]; !ok {
		s.entries[hash] = append([]byte(nil), body...)
		s.bytes += int64(len(body))
	}
	s.touch(hash)
	for (len(s.order) > s.maxEntries || s.bytes > s.maxBytes) && len(s.order) > 1 {
		oldest := s.order[0]
		s.order = s.order[1:]
		s.bytes -= int64(len(s.entries[oldest]))
		delete(s.entries, oldest)
	}
	return hash
}

// Get returns the body for hash and marks it most recently used.
func (s *SceneEntryStore) Get(hash string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.entries[hash]
	if ok {
		s.touch(hash)
	}
	return body, ok
}

var defaultSceneEntryStore atomic.Pointer[SceneEntryStore]

func init() {
	defaultSceneEntryStore.Store(sceneEntryStoreFromEnv(os.Getenv))
}

// sceneEntryStoreFromEnv returns the process store, or nil during `gosx
// export`: a static site has no server for /gosx/scene3d/, so deferred props
// stay inline in each page's manifest (the Defer.Inline behaviour).
func sceneEntryStoreFromEnv(getenv func(string) string) *SceneEntryStore {
	if getenv("GOSX_STATIC_EXPORT") == "1" {
		return nil
	}
	return NewSceneEntryStore(sceneEntryMaxEntries, sceneEntryMaxBytes)
}

// DefaultSceneEntryStore returns the process store; nil when disabled, in which
// case deferred props stay inline.
func DefaultSceneEntryStore() *SceneEntryStore { return defaultSceneEntryStore.Load() }

// SetSceneEntryStore replaces the process store; nil disables it.
func SetSceneEntryStore(s *SceneEntryStore) { defaultSceneEntryStore.Store(s) }

// ValidSceneEntryRoute reports whether route can be named in a props URL and
// re-rendered later: a rooted request path, bounded, and never under /gosx/
// (which would let a props URL point back at the framework's own routes).
func ValidSceneEntryRoute(route string) bool {
	if len(route) == 0 || len(route) > 2048 || route[0] != '/' || strings.HasPrefix(route, "//") || strings.HasPrefix(route, "/gosx/") || strings.HasPrefix(route, "/\\") {
		return false
	}
	for i := 0; i < len(route); i++ {
		if route[i] < 0x20 || route[i] == 0x7f {
			return false
		}
	}
	return true
}
