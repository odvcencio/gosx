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

const (
	maxMessageSize = 64 * 1024
	readWait       = 60 * time.Second
	writeWait      = 10 * time.Second
	pingPeriod     = (readWait * 9) / 10
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

	syncMu      sync.RWMutex
	syncDocs    map[byte]*syncedDoc
	syncDocName map[string]byte
	nextSyncDoc byte

	latched map[string][]byte
	// latchedMu protects latched. Never held while acquiring h.mu or any
	// other hub-level mutex — release it before taking other locks.
	latchedMu sync.RWMutex

	// MaxClients limits the number of concurrent connections. 0 = unlimited.
	MaxClients int
}

// Client represents a connected WebSocket client.
type Client struct {
	ID         string
	Hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	binarySend chan []byte
	syncStates *peerSyncState
	mu         sync.Mutex
}

// HandlerFunc handles an event from a client.
type HandlerFunc func(ctx *Context)

// Context is passed to hub event handlers.
type Context struct {
	Client *Client
	Hub    *Hub
	Event  string
	Data   json.RawMessage
}

// Presence tracks connected clients.
type Presence struct {
	mu      sync.RWMutex
	members map[string]*ClientInfo
}

// ClientInfo describes a connected client.
type ClientInfo struct {
	ID       string            `json:"id"`
	JoinedAt time.Time         `json:"joinedAt"`
	Meta     map[string]string `json:"meta,omitempty"`
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
		name:        name,
		clients:     make(map[string]*Client),
		handlers:    make(map[string]HandlerFunc),
		state:       make(map[string]any),
		presence:    &Presence{members: make(map[string]*ClientInfo)},
		syncDocs:    make(map[byte]*syncedDoc),
		syncDocName: make(map[string]byte),
		latched:     make(map[string][]byte),
	}
}

// Latch declares that this hub should remember the last payload
// broadcast on the given topic and replay it to any client that joins
// after the broadcast. Idempotent. Latch has no effect until the first
// Broadcast on the topic fires.
//
// Latched payloads are in-memory only. They do not persist across
// server restart. Latching an unbounded set of topic names grows
// memory without bound — use Latch for fixed, known topics only.
func (h *Hub) Latch(topic string) {
	h.latchedMu.Lock()
	defer h.latchedMu.Unlock()
	if _, ok := h.latched[topic]; !ok {
		h.latched[topic] = nil // declared, not yet populated
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
	msg, err := encodeMessage(event, data)
	if err != nil {
		return
	}

	// Snapshot the message for latched topics so late joiners can
	// replay it. Capture before fanout so zero-client broadcasts
	// still populate the latch.
	h.latchedMu.Lock()
	if _, ok := h.latched[event]; ok {
		clone := make([]byte, len(msg))
		copy(clone, msg)
		h.latched[event] = clone
	}
	h.latchedMu.Unlock()

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

	msg, err := encodeMessage(event, data)
	if err != nil {
		return
	}

	select {
	case client.send <- msg:
	default:
	}
}

// encodeMessage serializes a hub Message into a single JSON byte slice.
//
// The previous implementation encoded Data with json.Marshal into a
// json.RawMessage, then encoded the outer Message with a second
// json.Marshal call — two marshal passes, two intermediate byte buffers,
// and an interface boxing of the RawMessage each call.
//
// Using a single json.Encoder over a pre-sized bytes.Buffer encodes the
// full message in one walk, one allocation for the payload buffer, and
// one for the final byte slice copy.
func encodeMessage(event string, data any) ([]byte, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	// `{"event":"","data":}` + event + payload + envelope headroom.
	buf := make([]byte, 0, 16+len(event)+len(payload))
	buf = append(buf, `{"event":`...)
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	buf = append(buf, eventJSON...)
	if len(payload) > 0 && string(payload) != "null" {
		buf = append(buf, `,"data":`...)
		buf = append(buf, payload...)
	}
	buf = append(buf, '}')
	return buf, nil
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
	readLimit := int64(maxMessageSize)
	if h.hasSyncDocs() {
		readLimit = 16 * 1024 * 1024
	}
	conn.SetReadLimit(readLimit)
	if err := conn.SetReadDeadline(time.Now().Add(readWait)); err != nil {
		log.Printf("[hub/%s] set read deadline error: %v", h.name, err)
		conn.Close()
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readWait))
	})

	clientID := generateClientID(h.name)
	client := &Client{
		ID:         clientID,
		Hub:        h,
		conn:       conn,
		send:       make(chan []byte, 256),
		binarySend: make(chan []byte, 256),
		syncStates: newPeerSyncState(),
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
	h.initClientSync(client)

	// Replay latched payloads to this client so it sees current state
	// for any topic declared via Latch. Topic ordering doesn't matter —
	// latched topics are independent of each other.
	//
	// Snapshot latched payload pointers under the read lock; send them
	// after unlocking. Matches Broadcast's non-blocking-send pattern so
	// a slow consumer can't deadlock the join path while holding
	// latchedMu.
	h.latchedMu.RLock()
	payloads := make([][]byte, 0, len(h.latched))
	for _, payload := range h.latched {
		if payload == nil {
			continue // declared but never broadcast
		}
		payloads = append(payloads, payload)
	}
	h.latchedMu.RUnlock()

	for _, payload := range payloads {
		select {
		case client.send <- payload:
		default:
			// Send buffer full — drop the replay for this topic.
			// Consistent with Broadcast's behavior on a saturated client.
		}
	}

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
		if err := c.conn.SetReadDeadline(time.Now().Add(readWait)); err != nil {
			break
		}
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[hub/%s] read error: %v", c.Hub.name, err)
			}
			break
		}

		if msgType == websocket.BinaryMessage {
			c.Hub.handleBinaryMessage(c, data)
			continue
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
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			c.mu.Lock()
			if !ok {
				// The send channel was closed.
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				c.mu.Unlock()
				return
			}

			err := c.conn.WriteMessage(websocket.TextMessage, msg)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case msg, ok := <-c.binarySend:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			c.mu.Lock()
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				c.mu.Unlock()
				return
			}

			err := c.conn.WriteMessage(websocket.BinaryMessage, msg)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			c.mu.Lock()
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (h *Hub) removeClient(c *Client) {
	h.mu.Lock()
	delete(h.clients, c.ID)
	h.mu.Unlock()

	h.presence.remove(c.ID)
	close(c.send)
	close(c.binarySend)

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
