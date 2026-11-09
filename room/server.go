// Package room is a server for online multiplayer GoSX games. It runs
// server-authoritative rooms over hub WebSockets: each room steps a
// sim.Simulation (such as *game.Runtime) at a fixed tick, takes per-tick
// inputs from its players, and sends snapshots back as binary keyframes and
// deltas. Package game/netsync is the client.
//
// Every room is playable alone. A room starts its countdown as soon as one
// person is connected, fills every empty seat with a bot (mode.Bot), and lets
// later arrivals take over a bot's seat. A dropped player keeps their seat for
// a grace period and resumes it with a reconnect token; a bot covers the seat
// meanwhile.
//
// A Server is an http.Handler. Mount it under a prefix:
//
//	srv, _ := room.NewServer(room.Config{Modes: []mode.Mode{duel}, AllowedOrigins: []string{"https://play.example.com"}})
//	mux.Handle("/play/", http.StripPrefix("/play", srv))
//
// Clients connect to wss://host/play/ws with query parameters (see
// ParseJoin), and lobby pages may list public rooms from GET /play/rooms.
// See docs/rooms.md.
package room

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/ratelimit"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
)

// Config configures a Server. Zero values select the documented defaults.
type Config struct {
	// Modes lists the playable modes. The first is the default. Required.
	Modes []mode.Mode
	// AllowedOrigins lists the browser origins, besides the server's own, that
	// may connect: "https://play.example.com", "https://*.portal.example",
	// or "null" for sandboxed iframes. See hub.OriginAllowlist.
	AllowedOrigins []string
	// RequireOrigin refuses connections without an Origin header, which only
	// non-browser clients omit.
	RequireOrigin bool
	// MaxRooms caps live rooms. Default 256.
	MaxRooms int
	// MaxSpectators caps spectators per room. Default 8.
	MaxSpectators int
	// ReconnectGrace is how long a dropped player's seat waits for them.
	// Default 30 s.
	ReconnectGrace time.Duration
	// ConnectGrace is how long a newly reserved seat waits for its WebSocket
	// to open. Default 10 s.
	ConnectGrace time.Duration
	// IdleTimeout closes a room that has had no connected person for this
	// long. Default 60 s.
	IdleTimeout time.Duration
	// ResultsHold is how long the results phase lasts before the room closes.
	// Default 10 s.
	ResultsHold time.Duration
	// InputDelay is the input buffer, in ticks, that clients aim to keep at
	// the server: each client runs this many ticks ahead of the server's
	// timeline beyond its one-way latency, so jitter does not starve the
	// server of input. Default 2.
	InputDelay int
	// SnapshotEvery sends a snapshot every N ticks, at most 1000. Default 1.
	SnapshotEvery int
	// KeyframeEvery sends a keyframe every N snapshots and deltas between.
	// Default: one keyframe per second of each mode's snapshots.
	KeyframeEvery int
	// MaxCatchUp bounds the ticks one room runs per wake after a stall.
	// Default 8.
	MaxCatchUp int
	// MaxMessagesPerSecond bounds each client's inbound frames. Default: twice
	// the mode's tick rate plus 20, which leaves room for pings and bursts.
	MaxMessagesPerSecond int
	// ConnectsPerMinute bounds WebSocket connections per client address.
	// Default 30.
	ConnectsPerMinute int
	// CreatesPerMinute bounds new rooms per client address. Default 10.
	CreatesPerMinute int
	// TrustForwardedFor keys rate limits by the first X-Forwarded-For address.
	// Enable it only behind a proxy that sets the header.
	TrustForwardedFor bool
	// Manual stops rooms from running their own goroutines. The caller drives
	// time with Server.Advance or Room.AdvanceTo. For tests and custom loops.
	Manual bool
	// Now reads the server's monotonic clock. Nil uses the process clock.
	Now func() time.Duration
	// NewSeed picks each new room's simulation seed. Nil draws from
	// crypto/rand. Tests set it to reproduce a match.
	NewSeed func() uint64
	// Logf receives diagnostics. Nil uses log.Printf; use a no-op to silence.
	Logf func(format string, args ...any)
}

func (c Config) normalized() (Config, error) {
	if len(c.Modes) == 0 {
		return c, errors.New("room: Config.Modes is empty")
	}
	modes := make([]mode.Mode, len(c.Modes))
	seen := map[string]bool{}
	for i, m := range c.Modes {
		n, err := m.Normalize()
		if err != nil {
			return c, err
		}
		if seen[n.Name] {
			return c, fmt.Errorf("room: duplicate mode %q", n.Name)
		}
		seen[n.Name] = true
		modes[i] = n
	}
	c.Modes = modes
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	defDur := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.MaxRooms, 256)
	if c.MaxSpectators < 0 {
		c.MaxSpectators = 0
	} else if c.MaxSpectators == 0 {
		c.MaxSpectators = 8
	}
	defDur(&c.ReconnectGrace, 30*time.Second)
	defDur(&c.ConnectGrace, 10*time.Second)
	defDur(&c.IdleTimeout, 60*time.Second)
	defDur(&c.ResultsHold, 10*time.Second)
	if c.InputDelay < 0 {
		c.InputDelay = 0
	} else if c.InputDelay == 0 {
		c.InputDelay = 2
	}
	c.InputDelay = min(c.InputDelay, 32)
	def(&c.SnapshotEvery, 1)
	c.SnapshotEvery = min(c.SnapshotEvery, 1000)
	c.KeyframeEvery = max(c.KeyframeEvery, 0)
	def(&c.MaxCatchUp, 8)
	c.MaxMessagesPerSecond = max(c.MaxMessagesPerSecond, 0)
	def(&c.ConnectsPerMinute, 30)
	def(&c.CreatesPerMinute, 10)
	if c.Now == nil {
		start := time.Now()
		c.Now = func() time.Duration { return time.Since(start) }
	}
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	if c.NewSeed == nil {
		c.NewSeed = randomUint64
	}
	return c, nil
}

// Error is a refused join. Code is the WebSocket close code a browser client
// sees (CloseEvent.code).
type Error struct {
	Code   int
	Reason string
}

func (e *Error) Error() string { return "room: " + e.Reason }

// Join refusals.
var (
	ErrNotFound    = &Error{wire.CloseNotFound, "room not found"}
	ErrRoomFull    = &Error{wire.CloseRoomFull, "room full"}
	ErrRoomOver    = &Error{wire.CloseRoomOver, "match over"}
	ErrServerFull  = &Error{wire.CloseServerFull, "server full"}
	ErrUnknownMode = &Error{wire.CloseBadRequest, "unknown mode"}
	ErrRateLimited = &Error{wire.CloseRateLimited, "too many connections"}
	ErrShutdown    = &Error{wire.CloseShutdown, "server shutting down"}
)

// Server is a registry of rooms and the HTTP handler clients connect through.
type Server struct {
	cfg      Config
	modes    map[string]mode.Mode
	origins  *hub.OriginAllowlist
	limiter  *ratelimit.Keyed
	reject   websocket.Upgrader
	wg       sync.WaitGroup
	localIDs atomic.Uint64

	mu     sync.Mutex
	rooms  map[string]*Room
	tokens map[string]*Room
	closed bool
}

// NewServer validates cfg and returns a server with no rooms.
func NewServer(cfg Config) (*Server, error) {
	cfg, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	origins, err := hub.NewOriginAllowlist(cfg.AllowedOrigins...)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg: cfg, modes: make(map[string]mode.Mode, len(cfg.Modes)), origins: origins,
		limiter: ratelimit.NewKeyed(1<<14, nil),
		rooms:   make(map[string]*Room), tokens: make(map[string]*Room),
	}
	for _, m := range cfg.Modes {
		s.modes[m.Name] = m
	}
	s.reject.CheckOrigin = s.checkOrigin
	return s, nil
}

func (s *Server) now() time.Duration { return s.cfg.Now() }

// checkOrigin is the Origin policy for every upgrade: the server's own origin,
// an allowlisted origin, or (unless RequireOrigin) none.
func (s *Server) checkOrigin(r *http.Request) bool {
	if r.Header.Get("Origin") == "" {
		return !s.cfg.RequireOrigin
	}
	return s.origins.Check(r)
}

// JoinRequest says what a client wants: resume a seat, join a room by code,
// create a private room, or quick-match into a public room of a mode.
type JoinRequest struct {
	Mode     string // mode name; empty selects the first mode
	Code     string // join this room
	Private  bool   // create a private room instead of quick-matching
	Resume   string // reconnect token from a previous Welcome
	Name     string // display name, at most 24 characters
	Spectate bool   // watch instead of play
}

// ParseJoin reads a JoinRequest from WebSocket URL query parameters: mode,
// code, private, resume, name and spectate. netsync builds the same query.
func ParseJoin(q url.Values) JoinRequest {
	truthy := func(v string) bool { return v == "1" || v == "true" || v == "yes" }
	return JoinRequest{
		Mode: q.Get("mode"), Code: q.Get("code"), Private: truthy(q.Get("private")),
		Resume: q.Get("resume"), Name: q.Get("name"), Spectate: truthy(q.Get("spectate")),
	}
}

// Query encodes req as the query ParseJoin reads.
func (req JoinRequest) Query() url.Values {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("mode", req.Mode)
	set("code", req.Code)
	set("resume", req.Resume)
	set("name", req.Name)
	if req.Private {
		q.Set("private", "1")
	}
	if req.Spectate {
		q.Set("spectate", "1")
	}
	return q
}

// join admits req and reserves its seat. key identifies the client for rate
// limits (an address); empty skips them. HandleSocket and ConnectLocal call it.
func (s *Server) join(req JoinRequest, key string) (*Room, ticket, error) {
	req.Name = sanitizeName(req.Name)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ticket{}, ErrShutdown
	}
	if req.Resume != "" {
		if r := s.tokens[req.Resume]; r != nil {
			if t, ok := r.resume(req.Resume, now); ok {
				return r, t, nil
			}
		}
	}
	if req.Code != "" {
		r := s.rooms[NormalizeCode(req.Code)]
		if r == nil {
			return nil, ticket{}, ErrNotFound
		}
		return s.claimLocked(r, req, now)
	}
	m, ok := s.mode(req.Mode)
	if !ok {
		return nil, ticket{}, ErrUnknownMode
	}
	if !req.Private && !req.Spectate {
		if r := s.quickMatchLocked(m.Name, now); r != nil {
			if room, t, err := s.claimLocked(r, req, now); err == nil {
				return room, t, nil
			}
		}
	}
	if key != "" && !s.limiter.Allow("create:"+key, s.cfg.CreatesPerMinute, time.Minute) {
		return nil, ticket{}, ErrRateLimited
	}
	if len(s.rooms) >= s.cfg.MaxRooms {
		return nil, ticket{}, ErrServerFull
	}
	req.Spectate = false
	return s.claimLocked(s.createLocked(m, !req.Private, now), req, now)
}

func (s *Server) claimLocked(r *Room, req JoinRequest, now time.Duration) (*Room, ticket, error) {
	t, err := r.claim(req, now)
	if err != nil {
		return nil, ticket{}, err
	}
	if t.token != "" {
		s.tokens[t.token] = r
	}
	return r, t, nil
}

func (s *Server) mode(name string) (mode.Mode, bool) {
	if name == "" {
		return s.cfg.Modes[0], true
	}
	m, ok := s.modes[name]
	return m, ok
}

// quickMatchLocked picks the public room of mode a quick-matching person
// should join: a room still gathering players (most people first), else a
// running room where a bot holds a seat they can take over.
func (s *Server) quickMatchLocked(modeName string, now time.Duration) *Room {
	var best *Room
	bestScore := -1
	for _, r := range s.rooms {
		if !r.public || r.mode.Name != modeName {
			continue
		}
		r.mu.Lock()
		score := -1
		if !r.closed && r.freeSeatLocked() >= 0 {
			score = r.connectedHumansLocked()
			if r.phase == PhaseLobby || r.phase == PhaseCountdown {
				score += 1000
			}
		}
		r.mu.Unlock()
		if score > bestScore {
			best, bestScore = r, score
		}
	}
	return best
}

// Create makes an empty room of the named mode. Most rooms are created by
// joins; Create serves admin tools and tests.
func (s *Server) Create(modeName string, public bool) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrShutdown
	}
	m, ok := s.mode(modeName)
	if !ok {
		return nil, ErrUnknownMode
	}
	if len(s.rooms) >= s.cfg.MaxRooms {
		return nil, ErrServerFull
	}
	return s.createLocked(m, public, s.now()), nil
}

func (s *Server) createLocked(m mode.Mode, public bool, now time.Duration) *Room {
	code := s.newCodeLocked()
	r := newRoom(s, m, code, public, s.cfg.NewSeed(), now)
	s.rooms[code] = r
	if !s.cfg.Manual {
		s.wg.Add(1)
		go r.run()
	}
	return r
}

// Room returns the live room with code.
func (s *Server) Room(code string) (*Room, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[NormalizeCode(code)]
	return r, ok
}

// Rooms returns a summary of every live room. Public filters to rooms a lobby
// may list.
func (s *Server) Rooms(publicOnly bool) []Info {
	s.mu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		if !publicOnly || r.public {
			rooms = append(rooms, r)
		}
	}
	s.mu.Unlock()
	now := s.now()
	out := make([]Info, 0, len(rooms))
	for _, r := range rooms {
		r.mu.Lock()
		if !r.closed {
			out = append(out, r.infoLocked(now, false))
		}
		r.mu.Unlock()
	}
	return out
}

// Advance drives every room to the server clock's current time. Use it with
// Config.Manual.
func (s *Server) Advance() {
	s.mu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.mu.Unlock()
	now := s.now()
	for _, r := range rooms {
		r.AdvanceTo(now)
	}
}

func (s *Server) forgetTokens(tokens []string) {
	s.mu.Lock()
	for _, t := range tokens {
		delete(s.tokens, t)
	}
	s.mu.Unlock()
}

func (s *Server) forgetRoom(r *Room, tokens []string) {
	s.mu.Lock()
	if s.rooms[r.code] == r {
		delete(s.rooms, r.code)
	}
	for _, t := range tokens {
		if s.tokens[t] == r {
			delete(s.tokens, t)
		}
	}
	s.mu.Unlock()
}

// Close closes every room with wire.CloseShutdown, refuses new joins, and
// waits for room goroutines until ctx ends.
func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.mu.Unlock()
	for _, r := range rooms {
		r.Close(wire.CloseShutdown, "server shutting down")
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConnectLocal joins a room in-process, with the same rules as a WebSocket
// join but without rate limits or an Origin. See LocalConn.
func (s *Server) ConnectLocal(req JoinRequest) (*LocalConn, error) {
	r, t, err := s.join(req, "")
	if err != nil {
		return nil, err
	}
	c := &LocalConn{room: r, clientID: "local-" + strconv.FormatUint(s.localIDs.Add(1), 10)}
	r.attach(c, t.seat, t.token)
	return c, nil
}

// ServeHTTP routes a WebSocket upgrade to HandleSocket and GET .../rooms to
// HandleLobby.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case websocket.IsWebSocketUpgrade(r):
		s.HandleSocket(w, r)
	case strings.HasSuffix(r.URL.Path, "/rooms") || r.URL.Path == "rooms":
		s.HandleLobby(w, r)
	default:
		http.NotFound(w, r)
	}
}

// HandleSocket admits a WebSocket client and hands it to its room's hub. A
// refused join is answered with an upgrade and a close frame carrying the
// refusal's code, because a browser cannot read the HTTP status of a failed
// upgrade. Origin and rate-limit refusals stay plain HTTP errors (403, 429).
func (s *Server) HandleSocket(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(r) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	key := s.clientKey(r)
	if !s.limiter.Allow("connect:"+key, s.cfg.ConnectsPerMinute, time.Minute) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	room, t, err := s.join(ParseJoin(r.URL.Query()), key)
	if err != nil {
		s.refuse(w, r, err)
		return
	}
	room.hub.ServeHTTPWithMetadata(w, r, hub.ConnectionMetadata{"seat": strconv.Itoa(t.seat), "token": t.token})
}

func (s *Server) refuse(w http.ResponseWriter, r *http.Request, err error) {
	var re *Error
	if !errors.As(err, &re) {
		re = &Error{Code: 1011, Reason: "internal error"}
	}
	conn, uerr := s.reject.Upgrade(w, r, nil)
	if uerr != nil {
		return
	}
	defer conn.Close()
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(re.Code, re.Reason), time.Now().Add(time.Second))
}

// HandleLobby answers GET with the public rooms as JSON, optionally filtered
// by ?mode=. Allowed cross-origin pages may read it (CORS).
func (s *Server) HandleLobby(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		if !s.origins.Check(r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
	}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Methods", "GET")
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet, http.MethodHead:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	modeName := r.URL.Query().Get("mode")
	rooms := s.Rooms(true)
	list := rooms[:0]
	for _, info := range rooms {
		if modeName == "" || info.Mode == modeName {
			list = append(list, info)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"rooms": list})
}

func (s *Server) clientKey(r *http.Request) string {
	if s.cfg.TrustForwardedFor {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first, _, _ := strings.Cut(fwd, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// codeAlphabet is Crockford base32: digits and capitals without I, L, O and
// U. NormalizeCode reads I and L as 1 and O as 0, so a code read aloud or
// mistyped still resolves.
const (
	codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	codeLength   = 5
)

// NormalizeCode uppercases code, drops spaces and dashes, and maps the
// look-alike letters I, L and O to 1, 1 and 0.
func NormalizeCode(code string) string {
	var b strings.Builder
	b.Grow(len(code))
	for _, c := range strings.ToUpper(code) {
		switch c {
		case ' ', '-', '_':
			continue
		case 'I', 'L':
			c = '1'
		case 'O':
			c = '0'
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (s *Server) newCodeLocked() string {
	for {
		var b [codeLength]byte
		_, _ = rand.Read(b[:])
		for i := range b {
			b[i] = codeAlphabet[b[i]&31]
		}
		if _, taken := s.rooms[string(b[:])]; !taken {
			return string(b[:])
		}
	}
}

func newToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func randomUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:])
}

// sanitizeName keeps at most 24 printable characters.
func sanitizeName(name string) string {
	var b strings.Builder
	n := 0
	for _, c := range strings.TrimSpace(name) {
		if n == 24 {
			break
		}
		if unicode.IsPrint(c) {
			b.WriteRune(c)
			n++
		}
	}
	return b.String()
}
