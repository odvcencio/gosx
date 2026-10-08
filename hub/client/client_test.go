package hubclient

import (
	"sync"
	"testing"
)

func TestClientSetResumeTokenPersistsToStore(t *testing.T) {
	store := newFakeStore()
	c := New(Options{URL: "ws://unused", Store: store, StoreKey: "room-1"})
	if err := c.SetResumeToken("abc"); err != nil {
		t.Fatalf("SetResumeToken: %v", err)
	}
	if got, ok := store.Get("room-1"); !ok || got != "abc" {
		t.Fatalf("store value = %q ok=%v, want abc/true", got, ok)
	}
	if c.ResumeToken() != "abc" {
		t.Fatalf("ResumeToken() = %q, want abc", c.ResumeToken())
	}
}

func TestNewSeedsResumeTokenFromStore(t *testing.T) {
	store := newFakeStore()
	_ = store.Set("room-1", "seeded")
	c := New(Options{URL: "ws://unused", Store: store, StoreKey: "room-1", ResumeToken: "ignored"})
	if c.ResumeToken() != "seeded" {
		t.Fatalf("ResumeToken() = %q, want seeded", c.ResumeToken())
	}
}

func TestSendWithoutConnectionReturnsErrNotConnected(t *testing.T) {
	c := New(Options{URL: "ws://unused"})
	if err := c.Send("ping", nil); err != ErrNotConnected {
		t.Fatalf("Send err = %v, want ErrNotConnected", err)
	}
}

func TestCloseBeforeConnectIsSafeAndIdempotent(t *testing.T) {
	c := New(Options{URL: "ws://unused"})
	closeClient(t, c)
	closeClient(t, c)
	if got := c.State(); got != StateClosed {
		t.Fatalf("State() = %s, want closed", got)
	}
}

type fakeStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{values: make(map[string]string)}
}

func (s *fakeStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}

func (s *fakeStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}
