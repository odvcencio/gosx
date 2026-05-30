package scheduled

import (
	"sync"
	"time"
)

// TaskStatus records the observable state of a task.
type TaskStatus struct {
	Name                 string    `json:"name"`
	Schedule             string    `json:"schedule,omitempty"`
	LastRunAt            time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt        time.Time `json:"last_success_at,omitempty"`
	NextDueAt            time.Time `json:"next_due_at,omitempty"`
	CurrentAttempt       int       `json:"current_attempt,omitempty"`
	CurrentProgress      string    `json:"current_progress,omitempty"`
	CurrentProgressAgeMs *int64    `json:"current_progress_age_ms,omitempty"`
	ProgressTimeoutMs    *int64    `json:"progress_timeout_ms,omitempty"`
	RecentError          string    `json:"recent_error,omitempty"`
}

// Store is the persistence seam for task status records.
type Store interface {
	// Save persists or updates a TaskStatus record.
	Save(status TaskStatus) error
	// Load retrieves the TaskStatus for the named task.
	// Returns (_, false) if no record exists.
	Load(name string) (TaskStatus, bool)
	// List returns all stored TaskStatus records in unspecified order.
	List() []TaskStatus
}

// memStore is a concurrency-safe in-memory Store implementation.
type memStore struct {
	mu      sync.RWMutex
	records map[string]TaskStatus
}

// newMemStore returns a new in-memory Store.
func newMemStore() Store {
	return &memStore{
		records: make(map[string]TaskStatus),
	}
}

// Save stores or replaces the TaskStatus record keyed by Name.
func (m *memStore) Save(status TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[status.Name] = status
	return nil
}

// Load retrieves the TaskStatus for name. Returns (zero, false) if not found.
func (m *memStore) Load(name string) (TaskStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.records[name]
	return s, ok
}

// List returns a snapshot of all stored TaskStatus records.
func (m *memStore) List() []TaskStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]TaskStatus, 0, len(m.records))
	for _, s := range m.records {
		result = append(result, s)
	}
	return result
}
