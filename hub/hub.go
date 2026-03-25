// Package hub provides long-lived server-side stateful coordinators
// for realtime features: chat, presence, multiplayer, subscriptions, fanout.
//
// A hub is a fifth primitive in the GoSX platform model:
//
//	server:  request/response, routes, SSR
//	action:  mutations
//	island:  constrained DOM interactivity
//	engine:  heavy browser compute/render
//	hub:     long-lived realtime server state
package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Hub is a long-lived server-side coordinator for realtime state.
type Hub struct {
	name    string
	clients map[string]*Client
	mu      sync.RWMutex

	// Event handlers registered via On()
	handlers map[string]HandlerFunc

	// Shared state
	state   map[string]any
	stateMu sync.RWMutex

	// Presence tracking
	presence *Presence

	// MaxClients limits the number of concurrent connections. 0 = unlimited.
	MaxClients int
}

// Client represents a connected WebSocket client.
type Client struct {
	ID   string
	Hub  *Hub
	conn *websocket.Conn
	send chan []byte
	mu   sync.Mutex
}

// HandlerFunc handles an event from a client.
type HandlerFunc func(ctx *Context)

// Context is passed to hub event handlers.
type Context struct {
	Client  *Client
	Hub     *Hub
	Event   string
	Data    json.RawMessage
}

// Presence tracks connected clients.
type Presence struct {
	mu      sync.RWMutex
	members map[string]*ClientInfo
}

// ClientInfo describes a connected client.
type ClientInfo struct {
	ID        string    `json:"id"`
	JoinedAt  time.Time `json:"joinedAt"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Message is the WebSocket wire format.
type Message struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

var upgrader = websocket.Upgrader{
	// CheckOrigin: reject cross-origin requests by default.
	// Use SetCheckOrigin to override for development or trusted origins.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

// SetCheckOrigin overrides the default origin check for WebSocket upgrades.
func SetCheckOrigin(fn func(*http.Request) bool) {
	upgrader.CheckOrigin = fn
}

// generateClientID produces a cryptographically random client ID.
func generateClientID(hubName string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return hubName + "-" + hex.EncodeToString(b)
}

// New creates a new hub instance.
func New(name string) *Hub {
	return &Hub{
		name:     name,
		clients:  make(map[string]*Client),
		handlers: make(map[string]HandlerFunc),
		state:    make(map[string]any),
		presence: &Presence{members: make(map[string]*ClientInfo)},
	}
}

// On registers a handler for an event.
func (h *Hub) On(event string, handler HandlerFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[event] = handler
}

// State gets a shared state value.
func (h *Hub) GetState(key string) (any, bool) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	v, ok := h.state[key]
	return v, ok
}

// SetState sets a shared state value.
func (h *Hub) SetState(key string, value any) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	h.state[key] = value
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(event string, data any) {
	msg, err := json.Marshal(Message{
		Event: event,
		Data:  mustMarshal(data),
	})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, client := range h.clients {
		select {
		case client.send <- msg:
		default:
			// Client send buffer full — skip
		}
	}
}

// Send sends a message to a specific client.
func (h *Hub) Send(clientID string, event string, data any) {
	h.mu.RLock()
	client, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return
	}

	msg, err := json.Marshal(Message{
		Event: event,
		Data:  mustMarshal(data),
	})
	if err != nil {
		return
	}

	select {
	case client.send <- msg:
	default:
	}
}

// Presence returns the hub's presence tracker.
func (h *Hub) Presence() *Presence {
	return h.presence
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Name returns the hub name.
func (h *Hub) Name() string {
	return h.name
}

// ServeHTTP handles WebSocket upgrade requests.
// Mount at: mux.Handle("/gosx/hub/{name}", hub)
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.MaxClients > 0 && h.ClientCount() >= h.MaxClients {
		http.Error(w, "hub full", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[hub/%s] upgrade error: %v", h.name, err)
		return
	}

	clientID := generateClientID(h.name)
	client := &Client{
		ID:   clientID,
		Hub:  h,
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Register client
	h.mu.Lock()
	h.clients[clientID] = client
	h.mu.Unlock()

	// Track presence
	h.presence.add(clientID)

	// Send client their ID first — before any broadcast from join handler
	welcome, _ := json.Marshal(Message{
		Event: "__welcome",
		Data:  mustMarshal(map[string]string{"clientId": clientID}),
	})
	client.send <- welcome

	// Fire join handler (may broadcast to all clients including this one)
	if handler, ok := h.handlers["join"]; ok {
		handler(&Context{
			Client: client,
			Hub:    h,
			Event:  "join",
		})
	}

	log.Printf("[hub/%s] client %s connected (%d total)", h.name, clientID, h.ClientCount())

	// Start read/write pumps
	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.Hub.removeClient(c)
		c.conn.Close()
	}()

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		c.Hub.mu.RLock()
		handler, ok := c.Hub.handlers[msg.Event]
		c.Hub.mu.RUnlock()

		if ok {
			handler(&Context{
				Client: c,
				Hub:    c.Hub,
				Event:  msg.Event,
				Data:   msg.Data,
			})
		}
	}
}

func (c *Client) writePump() {
	defer c.conn.Close()

	for msg := range c.send {
		c.mu.Lock()
		err := c.conn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()
		if err != nil {
			break
		}
	}
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	delete(h.clients, c.ID)
	h.mu.Unlock()

	h.presence.remove(c.ID)
	close(c.send)

	// Fire leave handler
	if handler, ok := h.handlers["leave"]; ok {
		handler(&Context{
			Client: c,
			Hub:    h,
			Event:  "leave",
		})
	}

	log.Printf("[hub/%s] client %s disconnected (%d remaining)", h.name, c.ID, h.ClientCount())
}

// Presence methods

func (p *Presence) add(clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.members[clientID] = &ClientInfo{
		ID:       clientID,
		JoinedAt: time.Now(),
	}
}

func (p *Presence) remove(clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.members, clientID)
}

// Count returns the number of present members.
func (p *Presence) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.members)
}

// List returns all present members.
func (p *Presence) List() []*ClientInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*ClientInfo, 0, len(p.members))
	for _, m := range p.members {
		result = append(result, m)
	}
	return result
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
