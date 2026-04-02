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

func readUntilEvent(t *testing.T, conn *websocket.Conn, event string) Message {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read %s: %v", event, err)
		}
		if msg.Event == event {
			return msg
		}
	}
}

func TestHubBasic(t *testing.T) {
	h := New("test")
	if h.Name() != "test" {
		t.Fatal("wrong name")
	}
	if h.ClientCount() != 0 {
		t.Fatal("expected 0 clients")
	}
}

func TestHubState(t *testing.T) {
	h := New("test")
	h.SetState("count", 42)

	val, ok := h.GetState("count")
	if !ok {
		t.Fatal("expected state")
	}
	if val.(int) != 42 {
		t.Fatalf("expected 42, got %v", val)
	}
}

func TestHubPresence(t *testing.T) {
	h := New("test")
	h.presence.add("client-1")
	h.presence.add("client-2")

	if h.presence.Count() != 2 {
		t.Fatalf("expected 2 members, got %d", h.presence.Count())
	}

	h.presence.remove("client-1")
	if h.presence.Count() != 1 {
		t.Fatalf("expected 1 member after remove, got %d", h.presence.Count())
	}

	list := h.presence.List()
	if len(list) != 1 || list[0].ID != "client-2" {
		t.Fatalf("expected client-2 in list, got %+v", list)
	}
}

func TestHubWebSocket(t *testing.T) {
	h := New("chat")

	var mu sync.Mutex
	var received []string

	h.On("join", func(ctx *Context) {
		h.Broadcast("memberJoined", map[string]string{"id": ctx.Client.ID})
	})

	h.On("message", func(ctx *Context) {
		var text string
		json.Unmarshal(ctx.Data, &text)
		mu.Lock()
		received = append(received, text)
		mu.Unlock()
		h.Broadcast("newMessage", text)
	})

	// Start test server
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect two clients
	c1, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()

	c2, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	// Wait for connections to register
	time.Sleep(100 * time.Millisecond)

	if h.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", h.ClientCount())
	}

	// Read welcome message without assuming it beats every broadcast after both joins.
	msg := readUntilEvent(t, c1, "__welcome")
	if msg.Event != "__welcome" {
		t.Fatalf("expected __welcome, got %s", msg.Event)
	}
	t.Logf("Client 1 welcome: %s", msg.Data)

	// Client 1 sends a message
	c1.WriteJSON(Message{Event: "message", Data: mustMarshal("hello from c1")})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) != 1 || received[0] != "hello from c1" {
		t.Fatalf("expected [hello from c1], got %v", received)
	}
	mu.Unlock()

	t.Logf("Hub test passed: 2 clients, message sent and received")
}

func TestHubOriginCheck(t *testing.T) {
	h := New("secure")
	server := httptest.NewServer(h)
	defer server.Close()

	// Cross-origin request should be rejected (default check)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Origin": []string{"http://evil.com"}}
	_, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected cross-origin rejection")
	}
}

func TestHubMaxClients(t *testing.T) {
	h := New("limited")
	h.MaxClients = 2
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	c1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()
	c2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	time.Sleep(100 * time.Millisecond)

	// Third connection should fail
	_, _, err = websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected rejection when hub is full")
	}
}

func TestHubBroadcast(t *testing.T) {
	h := New("broadcast")

	var broadcastCount int
	var mu sync.Mutex

	h.On("ping", func(ctx *Context) {
		mu.Lock()
		broadcastCount++
		mu.Unlock()
		h.Broadcast("pong", "ok")
	})

	server := httptest.NewServer(h)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect 3 clients
	clients := make([]*websocket.Conn, 3)
	for i := range clients {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{})
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		defer c.Close()
		clients[i] = c
	}

	time.Sleep(100 * time.Millisecond)

	if h.ClientCount() != 3 {
		t.Fatalf("expected 3 clients, got %d", h.ClientCount())
	}

	// Read welcome messages
	for _, c := range clients {
		readUntilEvent(t, c, "__welcome")
	}

	// Client 0 sends ping
	clients[0].WriteJSON(Message{Event: "ping"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if broadcastCount != 1 {
		t.Fatalf("expected 1 ping handler call, got %d", broadcastCount)
	}
	mu.Unlock()

	t.Logf("Broadcast test passed: 3 clients, ping/pong")
}
