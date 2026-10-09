package storage

import "errors"

var (
	// ErrStorageUnavailable means the selected browser store cannot be accessed.
	ErrStorageUnavailable = errors.New("storage: browser storage unavailable")
	// ErrKeyLimit means enumeration would exceed the caller's scan budget.
	// No keys are returned; destructive callers cannot mistake a partial list
	// for a complete enumeration.
	ErrKeyLimit = errors.New("storage: key enumeration limit exceeded")
)

// BrowserStore is a typed handle to local or session storage. It resolves the
// underlying object per operation, including potentially throwing property
// getters. Read distinguishes inaccessible data from an absent key. The zero
// value and non-browser hosts return ErrStorageUnavailable; NewDefault still
// selects in-memory storage on native hosts.
type BrowserStore struct{ name string }

// Local returns persistent storage for the current origin.
func Local() BrowserStore { return BrowserStore{name: "localStorage"} }

// Session returns storage for the current browsing session.
func Session() BrowserStore { return BrowserStore{name: "sessionStorage"} }

// Get implements Backend. Use Read when unreadable data must not be replaced.
func (s BrowserStore) Get(key string) (string, bool) {
	value, found, err := s.Read(key)
	return value, found && err == nil
}

var _ Backend = BrowserStore{}
