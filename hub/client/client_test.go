package hubclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

// waitFor polls until fn returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for condition")
}

func TestClientConnectsReceivesWelcomeAndDispatchesHandlers(t *testing.T) {
	h := hub.New("welcome")
	joined := make(chan string, 1)
	h.On("join", func(ctx *hub.Context) {
		joined <- ctx.Client.ID
	})
	server := httptest.NewServer(h)
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()

	var mu sync.Mutex
	var welcomeID string
	c.On("__welcome", func(data json.RawMessage) {
		var payload struct {
			ClientID string `json:"clientId"`
		}
		_ = json.Unmarshal(data, &payload)
		mu.Lock()
		welcomeID = payload.ClientID
		mu.Unlock()
	})
	c.Connect()

	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	select {
	case id := <-joined:
		waitFor(t, time.Second, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return welcomeID == id
		})
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for join")
	}
}

func TestClientSendReachesHubHandler(t *testing.T) {
	h := hub.New("echo")
	received := make(chan string, 1)
	h.On("ping", func(ctx *hub.Context) {
		var payload struct {
			Value string `json:"value"`
		}
		_ = json.Unmarshal(ctx.Data, &payload)
		received <- payload.Value
	})
	server := httptest.NewServer(h)
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	if err := c.Send("ping", map[string]string{"value": "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case v := <-received:
		if v != "hello" {
			t.Fatalf("received %q, want hello", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive send")
	}
}

func TestClientTypedOnDecodesPayload(t *testing.T) {
	h := hub.New("typed")
	h.Latch("state")
	server := httptest.NewServer(h)
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL)})
	defer c.Close()

	type statePayload struct {
		Frame uint64 `json:"frame"`
	}
	got := make(chan statePayload, 1)
	On(c, "state", func(v statePayload) { got <- v })
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	h.Broadcast("state", statePayload{Frame: 42})

	select {
	case v := <-got:
		if v.Frame != 42 {
			t.Fatalf("frame = %d, want 42", v.Frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed broadcast")
	}
}

func TestClientReconnectsAfterServerDrop(t *testing.T) {
	h := hub.New("drop")
	var joinCount int
	var mu sync.Mutex
	joinSeen := make(chan int, 8)
	h.On("join", func(ctx *hub.Context) {
		mu.Lock()
		joinCount++
		n := joinCount
		mu.Unlock()
		joinSeen <- n
	})
	server := httptest.NewServer(h)
	defer server.Close()

	var stateMu sync.Mutex
	var states []State
	c := New(Options{
		URL:     wsURL(server.URL),
		Backoff: Backoff{Base: 10 * time.Millisecond, Max: 40 * time.Millisecond, Factor: 2},
		OnStateChange: func(s State) {
			stateMu.Lock()
			states = append(states, s)
			stateMu.Unlock()
		},
	})
	defer c.Close()
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	select {
	case n := <-joinSeen:
		if n != 1 {
			t.Fatalf("first join count = %d, want 1", n)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first join")
	}

	// Disconnect the connected client from the server side; the hub client
	// should notice and dial again on its own without any help from the
	// test.
	members := h.Presence().List()
	if len(members) != 1 {
		t.Fatalf("connected members = %d, want 1", len(members))
	}
	h.Disconnect(members[0].ID, "test drop")

	select {
	case n := <-joinSeen:
		if n != 2 {
			t.Fatalf("second join count = %d, want 2", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect join")
	}
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	// The reconnect must have passed through StateReconnecting on its way
	// back to StateConnected — this is the state signal a game's UI would
	// drive a "reconnecting..." indicator off.
	stateMu.Lock()
	defer stateMu.Unlock()
	sawReconnecting := false
	for _, s := range states {
		if s == StateReconnecting {
			sawReconnecting = true
			break
		}
	}
	if !sawReconnecting {
		t.Fatalf("state sequence = %v, want it to include reconnecting", states)
	}
}

func TestClientResumeTokenTravelsOnQueryString(t *testing.T) {
	var gotResume string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotResume = r.URL.Query().Get("resume")
		mu.Unlock()
		h := hub.New("resume")
		h.ServeHTTP(w, r)
	}))
	defer server.Close()

	c := New(Options{URL: wsURL(server.URL), ResumeToken: "tok-123"})
	defer c.Close()
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })

	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotResume == "tok-123"
	})
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
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := c.State(); got != StateClosed {
		t.Fatalf("State() = %s, want closed", got)
	}
}

func TestStateChangesObservedInOrder(t *testing.T) {
	h := hub.New("states")
	server := httptest.NewServer(h)
	defer server.Close()

	var mu sync.Mutex
	var seen []State
	c := New(Options{URL: wsURL(server.URL), OnStateChange: func(s State) {
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	}})
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	_ = c.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 || seen[0] != StateConnecting {
		t.Fatalf("state sequence = %v, want it to start with connecting", seen)
	}
	if seen[len(seen)-1] != StateClosed {
		t.Fatalf("state sequence = %v, want it to end with closed", seen)
	}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
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
