package server

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrISRArtifactNotFound reports that no cached ISR artifact exists for the
// requested page in the configured store.
var ErrISRArtifactNotFound = errors.New("isr artifact not found")

// ISRArtifactInfo describes a stored ISR artifact without loading its body.
type ISRArtifactInfo struct {
	ModTime time.Time
}

// ISRArtifact contains a stored ISR artifact body and its modification time.
type ISRArtifact struct {
	Body    []byte
	ModTime time.Time
}

// ISRPageState tracks the freshness metadata associated with an ISR page.
type ISRPageState struct {
	GeneratedAt time.Time
	PathVersion uint64
	TagVersions map[string]uint64
}

// ISRRefreshLease represents an acquired refresh lock.
type ISRRefreshLease interface {
	Release() error
}

// ISRStore provides the storage and coordination surface used by ISR.
type ISRStore interface {
	StatArtifact(staticDir, pagePath, file string) (ISRArtifactInfo, error)
	ReadArtifact(staticDir, pagePath, file string) (ISRArtifact, error)
	WriteArtifact(staticDir, pagePath, file string, body []byte) (ISRArtifactInfo, error)
	LoadState(bundleRoot, pagePath string, fallbackGeneratedAt time.Time) (ISRPageState, error)
	SaveState(bundleRoot, pagePath string, state ISRPageState) error
	AcquireRefresh(bundleRoot, pagePath string) (ISRRefreshLease, bool, error)
}

// InMemoryISRStore is the default ISR store. Artifacts live on the local
// filesystem, while page freshness state and refresh leases are process-local.
type InMemoryISRStore struct {
	mu         sync.Mutex
	state      map[string]map[string]ISRPageState
	refreshing map[string]map[string]bool
}

// NewInMemoryISRStore creates the default local ISR store.
func NewInMemoryISRStore() *InMemoryISRStore {
	return &InMemoryISRStore{
		state:      make(map[string]map[string]ISRPageState),
		refreshing: make(map[string]map[string]bool),
	}
}

// StatArtifact returns the modification time for an artifact without loading it.
func (s *InMemoryISRStore) StatArtifact(staticDir, pagePath, file string) (ISRArtifactInfo, error) {
	target, ok := safeArtifactPath(staticDir, file)
	if !ok {
		return ISRArtifactInfo{}, errors.New("isr artifact path is invalid")
	}
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return ISRArtifactInfo{}, ErrISRArtifactNotFound
	}
	if err != nil {
		return ISRArtifactInfo{}, err
	}
	return ISRArtifactInfo{ModTime: info.ModTime().UTC()}, nil
}

// ReadArtifact loads an artifact body and its modification time.
func (s *InMemoryISRStore) ReadArtifact(staticDir, pagePath, file string) (ISRArtifact, error) {
	target, ok := safeArtifactPath(staticDir, file)
	if !ok {
		return ISRArtifact{}, errors.New("isr artifact path is invalid")
	}
	body, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return ISRArtifact{}, ErrISRArtifactNotFound
	}
	if err != nil {
		return ISRArtifact{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ISRArtifact{}, err
	}
	return ISRArtifact{
		Body:    body,
		ModTime: info.ModTime().UTC(),
	}, nil
}

// WriteArtifact stores an artifact body and returns its modification time.
func (s *InMemoryISRStore) WriteArtifact(staticDir, pagePath, file string, body []byte) (ISRArtifactInfo, error) {
	target, ok := safeArtifactPath(staticDir, file)
	if !ok {
		return ISRArtifactInfo{}, errors.New("isr artifact path is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return ISRArtifactInfo{}, err
	}
	temp := target + ".tmp"
	if err := os.WriteFile(temp, body, 0644); err != nil {
		return ISRArtifactInfo{}, err
	}
	if err := os.Rename(temp, target); err != nil {
		return ISRArtifactInfo{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return ISRArtifactInfo{}, err
	}
	return ISRArtifactInfo{ModTime: info.ModTime().UTC()}, nil
}

// LoadState returns the stored ISR freshness state for a page or initializes it
// from the provided fallback timestamp when no state exists yet.
func (s *InMemoryISRStore) LoadState(bundleRoot, pagePath string, fallbackGeneratedAt time.Time) (ISRPageState, error) {
	if s == nil {
		return ISRPageState{GeneratedAt: fallbackGeneratedAt.UTC(), TagVersions: map[string]uint64{}}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pages := s.state[bundleRoot]
	if pages == nil {
		pages = make(map[string]ISRPageState)
		s.state[bundleRoot] = pages
	}
	state, ok := pages[pagePath]
	if ok {
		return cloneISRPageState(state), nil
	}
	if fallbackGeneratedAt.IsZero() {
		fallbackGeneratedAt = time.Now().UTC()
	}
	state = ISRPageState{
		GeneratedAt: fallbackGeneratedAt.UTC(),
		TagVersions: map[string]uint64{},
	}
	pages[pagePath] = state
	return cloneISRPageState(state), nil
}

// SaveState persists freshness state for a page.
func (s *InMemoryISRStore) SaveState(bundleRoot, pagePath string, state ISRPageState) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pages := s.state[bundleRoot]
	if pages == nil {
		pages = make(map[string]ISRPageState)
		s.state[bundleRoot] = pages
	}
	pages[pagePath] = cloneISRPageState(state)
	return nil
}

// AcquireRefresh acquires a process-local refresh lease for the page.
func (s *InMemoryISRStore) AcquireRefresh(bundleRoot, pagePath string) (ISRRefreshLease, bool, error) {
	if s == nil {
		return noopISRRefreshLease{}, true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pages := s.refreshing[bundleRoot]
	if pages == nil {
		pages = make(map[string]bool)
		s.refreshing[bundleRoot] = pages
	}
	if pages[pagePath] {
		return nil, false, nil
	}
	pages[pagePath] = true
	return &memoryISRRefreshLease{
		store:      s,
		bundleRoot: bundleRoot,
		pagePath:   pagePath,
	}, true, nil
}

type memoryISRRefreshLease struct {
	store      *InMemoryISRStore
	bundleRoot string
	pagePath   string
	once       sync.Once
}

func (l *memoryISRRefreshLease) Release() error {
	if l == nil || l.store == nil {
		return nil
	}
	l.once.Do(func() {
		l.store.mu.Lock()
		defer l.store.mu.Unlock()
		if pages := l.store.refreshing[l.bundleRoot]; pages != nil {
			delete(pages, l.pagePath)
			if len(pages) == 0 {
				delete(l.store.refreshing, l.bundleRoot)
			}
		}
	})
	return nil
}

type noopISRRefreshLease struct{}

func (noopISRRefreshLease) Release() error { return nil }

func cloneISRPageState(state ISRPageState) ISRPageState {
	cloned := ISRPageState{
		GeneratedAt: state.GeneratedAt,
		PathVersion: state.PathVersion,
		TagVersions: make(map[string]uint64, len(state.TagVersions)),
	}
	for tag, version := range state.TagVersions {
		cloned.TagVersions[tag] = version
	}
	return cloned
}
