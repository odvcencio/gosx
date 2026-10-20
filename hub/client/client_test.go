package hubclient

import (
	"sync"
	"testing"
)

func TestParseGoroutineID(t *testing.T) {
	for _, test := range []struct {
		header string
		id     uint64
		ok     bool
	}{
		{header: ""},
		{header: "goroutine "},
		{header: "goroutine x"},
		{header: "other 42 [running]:"},
		{header: "goroutine 42 [running]:\nexample()\n", id: 42, ok: true},
		{header: "goroutine 7", id: 7, ok: true},
		{header: "goroutine 0", ok: true},
		{header: "goroutine 18446744073709551615 [running]:", id: ^uint64(0), ok: true},
		{header: "goroutine 18446744073709551616 [running]:"},
	} {
		t.Run(test.header, func(t *testing.T) {
			id, ok := parseGoroutineID([]byte(test.header))
			if id != test.id || ok != test.ok {
				t.Fatalf("parseGoroutineID(%q) = (%d, %v), want (%d, %v)", test.header, id, ok, test.id, test.ok)
			}
		})
	}
}

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
