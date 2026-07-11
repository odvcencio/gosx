package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type metadataObservation struct {
	id      string
	actor   string
	role    string
	okActor bool
	okRole  bool
}

func TestHubServeHTTPCompatibilityUsesEmptyMetadata(t *testing.T) {
	h := New("metadata-compat")
	joined := make(chan metadataObservation, 1)
	h.On("join", func(ctx *Context) {
		actor, okActor := ctx.Client.Metadata("actor")
		role, okRole := ctx.Client.Metadata("role")
		joined <- metadataObservation{
			id:      ctx.Client.ID,
			actor:   actor,
			role:    role,
			okActor: okActor,
			okRole:  okRole,
		}
	})

	server := httptest.NewServer(h)
	defer server.Close()
	conn := dialHubMetadataTestClient(t, server, "/")
	defer conn.Close()
	readUntilEvent(t, conn, "__welcome")

	select {
	case observation := <-joined:
		if observation.okActor || observation.okRole || observation.actor != "" || observation.role != "" {
			t.Fatalf("ServeHTTP compatibility wrapper supplied metadata: %+v", observation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for join observation")
	}
}

func TestHubConnectionMetadataLifecycleAndWireIsolation(t *testing.T) {
	h := New("metadata-lifecycle")
	h.Latch("snapshot")
	h.Broadcast("snapshot", map[string]string{"value": "ready"})

	joined := make(chan metadataObservation, 2)
	leaves := make(chan metadataObservation, 2)
	h.On("join", func(ctx *Context) {
		actor, okActor := ctx.Client.Metadata("actor")
		role, okRole := ctx.Client.Metadata("role")
		joined <- metadataObservation{
			id:      ctx.Client.ID,
			actor:   actor,
			role:    role,
			okActor: okActor,
			okRole:  okRole,
		}
		// This event gives the test an explicit barrier after latch replay and
		// proves metadata was available to the generic join handler.
		ctx.Hub.Broadcast("joinObserved", map[string]string{"actor": actor})
	})
	h.On("leave", func(ctx *Context) {
		actor, okActor := ctx.Client.Metadata("actor")
		role, okRole := ctx.Client.Metadata("role")
		leaves <- metadataObservation{
			id:      ctx.Client.ID,
			actor:   actor,
			role:    role,
			okActor: okActor,
			okRole:  okRole,
		}
	})

	aliceMetadata := ConnectionMetadata{"actor": "alice", "role": "editor"}
	bobMetadata := ConnectionMetadata{"actor": "bob", "role": "viewer"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alice":
			h.ServeHTTPWithMetadata(w, r, aliceMetadata)
		case "/bob":
			h.ServeHTTPWithMetadata(w, r, bobMetadata)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	alice := dialHubMetadataTestClient(t, server, "/alice")
	bob := dialHubMetadataTestClient(t, server, "/bob")

	// The welcome envelope is intentionally the only server-generated identity
	// payload. Connection metadata must not leak into it or any replay payload.
	welcome := readUntilEvent(t, alice, "__welcome")
	if strings.Contains(string(welcome.Data), "alice") || strings.Contains(string(welcome.Data), "editor") {
		t.Fatalf("connection metadata leaked into welcome payload: %s", welcome.Data)
	}
	snapshot := readUntilEvent(t, alice, "snapshot")
	if strings.Contains(string(snapshot.Data), "alice") || strings.Contains(string(snapshot.Data), "editor") {
		t.Fatalf("connection metadata leaked into latch replay: %s", snapshot.Data)
	}
	joinObserved := readUntilEvent(t, alice, "joinObserved")
	var joinPayload map[string]string
	if err := json.Unmarshal(joinObserved.Data, &joinPayload); err != nil {
		t.Fatalf("decode join observation: %v", err)
	}
	if joinPayload["actor"] != "alice" {
		t.Fatalf("join handler observed actor=%q, want alice", joinPayload["actor"])
	}
	bobWelcome := readUntilEvent(t, bob, "__welcome")
	if strings.Contains(string(bobWelcome.Data), "bob") || strings.Contains(string(bobWelcome.Data), "viewer") {
		t.Fatalf("connection metadata leaked into bob welcome payload: %s", bobWelcome.Data)
	}
	bobSnapshot := readUntilEvent(t, bob, "snapshot")
	if strings.Contains(string(bobSnapshot.Data), "bob") || strings.Contains(string(bobSnapshot.Data), "viewer") {
		t.Fatalf("connection metadata leaked into bob latch replay: %s", bobSnapshot.Data)
	}
	readUntilEvent(t, bob, "joinObserved")

	observations := make(map[string]metadataObservation, 2)
	for range 2 {
		select {
		case observation := <-joined:
			observations[observation.id] = observation
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for join metadata")
		}
	}
	if len(observations) != 2 {
		t.Fatalf("join observations=%d, want 2", len(observations))
	}
	var aliceID, bobID string
	for id, observation := range observations {
		if !observation.okActor || !observation.okRole {
			t.Fatalf("join metadata missing for %s: %+v", id, observation)
		}
		switch observation.actor {
		case "alice":
			if observation.role != "editor" {
				t.Fatalf("alice role=%q, want editor", observation.role)
			}
			aliceID = id
		case "bob":
			if observation.role != "viewer" {
				t.Fatalf("bob role=%q, want viewer", observation.role)
			}
			bobID = id
		default:
			t.Fatalf("unexpected join metadata: %+v", observation)
		}
	}
	if aliceID == "" || bobID == "" || aliceID == bobID {
		t.Fatalf("distinct connection IDs not observed: alice=%q bob=%q", aliceID, bobID)
	}

	// Mutating the host-owned input maps after join must not change the
	// per-connection values visible to leave handlers.
	aliceMetadata["actor"] = "mallory"
	aliceMetadata["role"] = "admin"
	bobMetadata["actor"] = "intruder"
	bobMetadata["role"] = "owner"

	_ = alice.Close()
	_ = bob.Close()
	leaveObservations := make(map[string]metadataObservation, 2)
	for len(leaveObservations) < 2 {
		select {
		case observation := <-leaves:
			leaveObservations[observation.id] = observation
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for leave metadata: got %d", len(leaveObservations))
		}
	}
	if got := leaveObservations[aliceID]; got.actor != "alice" || got.role != "editor" || !got.okActor || !got.okRole {
		t.Fatalf("alice leave metadata=%+v, want immutable alice/editor", got)
	}
	if got := leaveObservations[bobID]; got.actor != "bob" || got.role != "viewer" || !got.okActor || !got.okRole {
		t.Fatalf("bob leave metadata=%+v, want immutable bob/viewer", got)
	}

}

func TestHubConnectionMetadataConcurrentUpgradesAndDisconnects(t *testing.T) {
	h := New("metadata-race")
	joined := make(chan string, 64)
	leaves := make(chan string, 64)
	h.On("join", func(ctx *Context) {
		actor, ok := ctx.Client.Metadata("actor")
		if !ok || actor == "" {
			t.Errorf("join metadata missing: actor=%q ok=%v", actor, ok)
			return
		}
		joined <- actor
	})
	h.On("leave", func(ctx *Context) {
		actor, ok := ctx.Client.Metadata("actor")
		if !ok || actor == "" {
			t.Errorf("leave metadata missing: actor=%q ok=%v", actor, ok)
			return
		}
		leaves <- actor
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The route is host-selected; no query-string identity is consumed by
		// the hub. Each request receives a fresh map before the call.
		actor := strings.TrimPrefix(r.URL.Path, "/")
		h.ServeHTTPWithMetadata(w, r, ConnectionMetadata{"actor": actor})
	}))
	defer server.Close()
	const clients = 32
	connections := make([]*websocket.Conn, clients)
	var wg sync.WaitGroup
	for i := 0; i < clients; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			connections[i] = dialHubMetadataTestClient(t, server, "/actor-")
		}()
	}
	wg.Wait()
	for _, conn := range connections {
		if conn == nil {
			t.Fatal("concurrent dial returned nil connection")
		}
		_ = conn.Close()
	}

	deadline := time.After(5 * time.Second)
	joinedCount, leaveCount := 0, 0
	for joinedCount < clients || leaveCount < clients {
		select {
		case <-joined:
			joinedCount++
		case <-leaves:
			leaveCount++
		case <-deadline:
			t.Fatalf("lifecycle counts joined=%d/%d leaves=%d/%d", joinedCount, clients, leaveCount, clients)
		}
	}
}

func dialHubMetadataTestClient(t *testing.T, server *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return conn
}

func TestConnectionMetadataIsNotInJSON(t *testing.T) {
	client := &Client{metadata: ConnectionMetadata{"actor": "alice", "role": "editor"}}
	encoded, err := json.Marshal(client)
	if err != nil {
		t.Fatalf("marshal client: %v", err)
	}
	if strings.Contains(string(encoded), "alice") || strings.Contains(string(encoded), "editor") {
		t.Fatalf("connection metadata serialized through Client: %s", encoded)
	}
}
