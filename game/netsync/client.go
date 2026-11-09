// Package netsync is the client side of package room: it connects a GoSX game
// to a server-authoritative room, predicts the local player on a local copy of
// the simulation, and reconciles with the server's snapshots.
//
// A Client owns a predicted sim.Simulation (such as *game.Runtime) built from
// the same mode.Mode the server runs. Call Frame once per render frame. Frame
// reads the network, restores the newest authoritative snapshot when one
// arrived and replays the local inputs the server has not applied yet, steers
// its clock so the local inputs reach the server just before they are due, runs
// the due fixed ticks with a game.Clock under game.CatchUpLag (it never drops
// a tick, so its tick count stays aligned with the server's), and sends the
// newest inputs with the previous few repeated for redundancy.
//
// When the server cannot be reached, or the connection drops and does not come
// back, the Client plays on locally with bots in every other seat, so a game is
// always playable alone.
//
// The package builds for js/wasm without importing net/http: the browser
// transport goes through hub/socket.
package netsync

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"net/url"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// Status is the client's connection state.
type Status uint8

const (
	// StatusIdle: Start has not been called.
	StatusIdle Status = iota
	// StatusConnecting: dialing the server for the first time.
	StatusConnecting
	// StatusOnline: connected and welcomed into a room.
	StatusOnline
	// StatusReconnecting: the connection dropped; prediction continues while
	// the client tries to resume its seat.
	StatusReconnecting
	// StatusOffline: playing locally with bots.
	StatusOffline
	// StatusClosed: the match ended, the seat moved to another connection, or
	// Close was called.
	StatusClosed
)

func (s Status) String() string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusConnecting:
		return "connecting"
	case StatusOnline:
		return "online"
	case StatusReconnecting:
		return "reconnecting"
	case StatusOffline:
		return "offline"
	case StatusClosed:
		return "closed"
	}
	return "unknown"
}

// TokenStore persists the reconnect token across page reloads.
// game/storage.Namespaced satisfies it.
type TokenStore interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

// Join says which room to enter.
type Join struct {
	Code     string // join this room
	Private  bool   // create a private room
	Name     string // display name
	Spectate bool   // watch instead of play
}

// Config configures a Client. Zero values select the documented defaults.
type Config struct {
	// URL is the room server's WebSocket endpoint, such as
	// "wss://play.example.com/play/ws". Empty plays offline at once.
	URL string
	// Mode is the mode to play; it must match a mode the server registers.
	// Required.
	Mode mode.Mode
	// Join selects the room.
	Join Join
	// Input samples the local player's held input for tick and appends it to
	// dst. Nil sends empty inputs.
	Input func(tick uint64, dst []byte) []byte
	// Store and StoreKey persist the reconnect token. StoreKey defaults to
	// "netsync.<mode>.token".
	Store    TokenStore
	StoreKey string
	// Redundancy is how many recent ticks every input frame carries.
	// Default 8, at most wire.MaxRedundancy.
	Redundancy int
	// ConnectTimeout bounds the first connection before falling back to
	// offline play. Default 5 s.
	ConnectTimeout time.Duration
	// ReconnectTimeout bounds attempts to resume after a drop before falling
	// back. Default 10 s; keep it below the server's ReconnectGrace.
	ReconnectTimeout time.Duration
	// StallTimeout treats a connection that delivered nothing for this long as
	// dropped. Default 5 s.
	StallTimeout time.Duration
	// NoFallback ends in StatusClosed instead of playing offline.
	NoFallback bool
	// CatchUpBudget bounds the wall time one Frame spends on fixed ticks; see
	// game.LoopConfig. Zero selects game.DefaultCatchUpBudget; negative removes
	// the bound.
	CatchUpBudget time.Duration
	// MaxDelta clamps one frame's delta before it becomes game time. Default
	// 250 ms. Time beyond it is recovered by a resync to the server.
	MaxDelta time.Duration
	// MaxReplay bounds the ticks one reconciliation replays. Default two
	// seconds of ticks.
	MaxReplay int
	// OnSnapshot, when set, is called with the simulation restored to each new
	// authoritative snapshot, before the local inputs are replayed. Read
	// remote entities from it into an Interpolator. It must not keep s.
	OnSnapshot func(tick uint64, s sim.Simulation)
	// OnStatus, when set, is called on every status change.
	OnStatus func(status Status, reason string)
	// Dial opens connections. Nil selects DialWebSocket.
	Dial Dialer
	// Now reads a monotonic clock. Nil uses the process clock. Tests inject a
	// virtual clock.
	Now func() time.Duration
	// Seed seeds offline matches. Zero picks a random seed.
	Seed uint64
}

// RoomView describes the room the client is in.
type RoomView struct {
	Code      string
	Mode      string
	Phase     wire.Phase
	Remaining time.Duration
	Seats     []wire.SeatInfo
	Results   []byte
	Offline   bool
}

// Stats reports connection and prediction health.
type Stats struct {
	Status Status
	// RTT is the smoothed round trip; Jitter its mean deviation; MinRTT the
	// lowest seen.
	RTT, Jitter, MinRTT time.Duration
	// ServerTick is the estimated server timeline now, in ticks.
	ServerTick float64
	// Tick is the newest predicted tick; AuthTick the newest authoritative
	// one. Ahead is their difference.
	Tick, AuthTick uint64
	Ahead          int
	// Lead is the server-reported input buffer for this seat, in ticks;
	// TargetLead is what the client aims for.
	Lead       int
	TargetLead float64
	// Dilation is the clock speed factor applied in the last frame.
	Dilation float64
	// Reconciles counts snapshot restores; Replayed the ticks re-run after
	// them; Resyncs the hard clock corrections.
	Reconciles, Replayed, Resyncs uint64
	// Keyframes, Deltas and Stale count snapshots applied and ignored.
	Keyframes, Deltas, Stale, DecodeErrors uint64
	// BytesIn and BytesOut count frame bytes.
	BytesIn, BytesOut uint64
	// InputFrames counts input frames sent.
	InputFrames uint64
	// Dials counts connection attempts.
	Dials uint64
}

// Client is a netsync connection to one room. It is not safe for concurrent
// use: call it from the game loop.
type Client struct {
	cfg    Config
	mode   mode.Mode
	period time.Duration
	now    func() time.Duration
	dial   Dialer

	status     Status
	reason     string
	conn       Conn
	connAt     time.Duration
	deadline   time.Duration // connect or reconnect deadline
	nextDial   time.Duration
	backoff    time.Duration
	lastRecv   time.Duration
	everOnline bool
	token      string

	welcome     wire.Welcome
	haveWelcome bool
	seat        int
	room        wire.Room
	roomAt      time.Duration
	closedFrom  wire.Phase // the phase the room was in when it closed
	results     []byte

	// Prediction.
	sim      sim.Simulation
	simSeed  uint64
	clock    *game.Clock
	tick     uint64
	alpha    float64
	inputs   *mode.Inputs
	hist     history
	frame    game.Frame
	dilation float64

	// Authoritative state.
	snap      wire.Snapshot
	keyEpoch  uint64
	keyState  []byte
	authState []byte
	authTick  uint64
	authAt    time.Duration
	haveAuth  bool
	newAuth   bool
	authSeats [wire.MaxSeats]remoteSeat
	nAuthSeat int

	// Clock steering.
	sync      clockSync
	extraLead float64
	pingSeq   uint32
	nextPing  time.Duration
	pings     int

	// Offline play.
	bots     []mode.Bot
	localEnd uint64

	sendBuf []byte
	stats   Stats

	onFrameFn func([]byte)
	stepFn    func(uint64)
	pollFn    func([]byte)
}

type remoteSeat struct {
	flags uint8
	lead  uint32
	n     int
	input [wire.MaxInputSize]byte
}

// New validates cfg and returns an idle Client. Call Start, or Frame, which
// starts it.
func New(cfg Config) (*Client, error) {
	m, err := cfg.Mode.Normalize()
	if err != nil {
		return nil, err
	}
	if cfg.Redundancy <= 0 {
		cfg.Redundancy = 8
	}
	cfg.Redundancy = min(cfg.Redundancy, wire.MaxRedundancy)
	defDur := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	defDur(&cfg.ConnectTimeout, 5*time.Second)
	defDur(&cfg.ReconnectTimeout, 10*time.Second)
	defDur(&cfg.StallTimeout, 5*time.Second)
	defDur(&cfg.MaxDelta, 250*time.Millisecond)
	if cfg.MaxReplay <= 0 {
		cfg.MaxReplay = 2 * m.TickRate
	}
	cfg.MaxReplay = min(cfg.MaxReplay, historySize-1)
	if cfg.StoreKey == "" {
		cfg.StoreKey = "netsync." + m.Name + ".token"
	}
	c := &Client{cfg: cfg, mode: m, period: m.Period(), now: cfg.Now, dial: cfg.Dial, seat: -1, dilation: 1}
	if c.now == nil {
		start := time.Now()
		c.now = func() time.Duration { return time.Since(start) }
	}
	if c.dial == nil {
		c.dial = DialWebSocket
	}
	if cfg.Store != nil {
		c.token, _ = cfg.Store.Get(cfg.StoreKey)
	}
	c.inputs = mode.NewInputs(m.Seats, m.InputSize)
	c.onFrameFn = c.onFrame
	c.stepFn = c.step
	c.pollFn = c.onFrame
	c.newClock()
	return c, nil
}

func (c *Client) newClock() {
	c.clock = game.NewClock(game.LoopConfig{
		FixedStep:     c.period,
		MaxDelta:      c.cfg.MaxDelta,
		CatchUp:       game.CatchUpLag,
		CatchUpBudget: c.cfg.CatchUpBudget,
		WallClock:     c.now,
	})
	c.alpha = 0
}

// Start begins connecting, or starts offline play when Config.URL is empty.
func (c *Client) Start() {
	if c.status != StatusIdle {
		return
	}
	if c.cfg.URL == "" {
		c.fallback("no server configured")
		return
	}
	now := c.now()
	c.setStatus(StatusConnecting, "")
	c.deadline = now + c.cfg.ConnectTimeout
	c.backoff = 250 * time.Millisecond
	c.dialNow(now)
}

// Close leaves the room and stops the client.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close(1000, "client closed")
		c.conn = nil
	}
	c.setStatus(StatusClosed, "closed by client")
}

// GoOffline switches to local play now, keeping the current world.
func (c *Client) GoOffline(reason string) {
	if c.status == StatusOffline || c.status == StatusClosed {
		return
	}
	c.fallback(reason)
}

// Status returns the connection state.
func (c *Client) Status() Status { return c.status }

// Reason explains the latest status change.
func (c *Client) Reason() string { return c.reason }

// Sim returns the simulation to render: predicted online, local offline. It
// is nil until a match starts.
func (c *Client) Sim() sim.Simulation { return c.sim }

// Tick returns the newest predicted (or local) tick.
func (c *Client) Tick() uint64 { return c.tick }

// Alpha returns the fraction of a tick between the last tick and the next, for
// render interpolation.
func (c *Client) Alpha() float64 { return c.alpha }

// Seat returns the local seat, or -1 for a spectator or before Welcome.
func (c *Client) Seat() int { return c.seat }

// Token returns the reconnect token of the current seat.
func (c *Client) Token() string { return c.token }

// Phase returns the room phase.
func (c *Client) Phase() wire.Phase { return c.room.Phase }

// Room returns the room the client is in. Seats is shared; copy it to keep it.
func (c *Client) Room() RoomView {
	v := RoomView{Code: c.welcome.Code, Mode: c.mode.Name, Phase: c.room.Phase, Seats: c.room.Seats,
		Results: c.results, Offline: c.status == StatusOffline}
	if c.room.Remaining > 0 {
		v.Remaining = max(0, c.room.Remaining-(c.now()-c.roomAt))
	}
	return v
}

// ServerTick estimates the server's tick timeline now. Offline it is Tick.
func (c *Client) ServerTick() float64 {
	if c.status == StatusOffline {
		return float64(c.tick) + c.alpha
	}
	if t, ok := c.estimateServer(c.now()); ok {
		return t
	}
	return float64(c.authTick)
}

// Stats returns connection and prediction counters.
func (c *Client) Stats() Stats {
	s := c.stats
	s.Status = c.status
	s.RTT, s.Jitter, s.MinRTT = c.sync.rtt(), c.sync.jitter(), c.sync.minRTT
	s.ServerTick = c.ServerTick()
	s.Tick, s.AuthTick = c.tick, c.authTick
	s.Ahead = int(int64(c.tick) - int64(c.authTick))
	if c.seat >= 0 && c.seat < c.nAuthSeat {
		s.Lead = int(c.authSeats[c.seat].lead)
	}
	s.TargetLead = float64(c.welcome.InputDelay) + c.extraLead
	s.Dilation = c.dilation
	return s
}

// Frame advances the client by one render frame of delta and returns the
// fixed-step frame it ran. Call it once per render frame.
func (c *Client) Frame(delta time.Duration) game.Frame {
	if c.status == StatusIdle {
		c.Start()
	}
	delta = max(delta, 0)
	now := c.now()
	if c.conn != nil {
		c.pump(now)
	}
	c.maintain(now)
	c.frame = game.Frame{}
	switch c.status {
	case StatusOffline:
		c.runOffline(delta)
	case StatusOnline, StatusReconnecting:
		if c.room.Phase == wire.PhaseRunning && c.haveAuth {
			c.runOnline(delta, now)
		}
	}
	if c.status == StatusOnline && c.conn != nil {
		c.sendInputs()
		c.maybePing(now)
	}
	return c.frame
}

func (c *Client) setStatus(s Status, reason string) {
	if c.status == s && c.reason == reason {
		return
	}
	c.status, c.reason = s, reason
	if c.cfg.OnStatus != nil {
		c.cfg.OnStatus(s, reason)
	}
}

func (c *Client) joinURL() string {
	q := url.Values{}
	q.Set("mode", c.mode.Name)
	if c.token != "" {
		q.Set("resume", c.token)
	}
	j := c.cfg.Join
	code := j.Code
	if code == "" && c.welcome.Code != "" && c.everOnline {
		code = c.welcome.Code // rejoin the same room if the token expired
	}
	if code != "" {
		q.Set("code", code)
	}
	if j.Private && code == "" {
		q.Set("private", "1")
	}
	if j.Name != "" {
		q.Set("name", j.Name)
	}
	if j.Spectate {
		q.Set("spectate", "1")
	}
	sep := "?"
	for i := 0; i < len(c.cfg.URL); i++ {
		if c.cfg.URL[i] == '?' {
			sep = "&"
			break
		}
	}
	return c.cfg.URL + sep + q.Encode()
}

func (c *Client) dialNow(now time.Duration) {
	c.stats.Dials++
	conn, err := c.dial(c.joinURL())
	if err != nil || conn == nil {
		c.nextDial = now + c.backoff
		c.backoff = min(2*c.backoff, 2*time.Second)
		return
	}
	c.conn, c.connAt, c.lastRecv = conn, now, now
	c.haveWelcome = false
	c.keyEpoch, c.keyState = 0, c.keyState[:0]
	c.sync.reset()
	c.pings, c.nextPing = 0, now
}

// pump reads the connection.
func (c *Client) pump(now time.Duration) {
	before := c.stats.BytesIn
	conn := c.conn
	state := conn.Poll(c.pollFn)
	if c.stats.BytesIn != before {
		c.lastRecv = now
	}
	// A frame handler may have dropped the connection (a protocol mismatch
	// falls back to offline play) or replaced it.
	if c.conn == conn && state == wire.ConnClosed {
		c.onClosed(now)
	}
}

func (c *Client) maintain(now time.Duration) {
	switch c.status {
	case StatusConnecting, StatusReconnecting:
		if now >= c.deadline {
			c.dropConn(1000, "giving up")
			if c.status == StatusConnecting {
				c.fallback("server unreachable")
			} else {
				c.fallback("connection lost")
			}
			return
		}
		if c.conn == nil && now >= c.nextDial {
			c.dialNow(now)
		} else if c.conn != nil && !c.haveWelcome && now-c.connAt > 3*time.Second {
			c.dropConn(1000, "handshake timeout") // try again
			c.nextDial = now
		}
	case StatusOnline:
		if now-c.lastRecv > c.cfg.StallTimeout {
			c.dropConn(1000, "stalled")
			c.beginReconnect(now)
		}
	}
}

func (c *Client) dropConn(code int, reason string) {
	if c.conn != nil {
		c.conn.Close(code, reason)
		c.conn = nil
	}
}

func (c *Client) beginReconnect(now time.Duration) {
	c.setStatus(StatusReconnecting, "connection lost")
	c.deadline = now + c.cfg.ReconnectTimeout
	c.backoff = 250 * time.Millisecond
	c.nextDial = now
}

func (c *Client) onClosed(now time.Duration) {
	code, reason := c.conn.CloseStatus()
	c.conn = nil
	switch {
	case c.status == StatusClosed || c.status == StatusOffline:
	case code == wire.CloseRoomOver:
		c.setStatus(StatusClosed, "match over")
	case code == wire.CloseReplaced:
		c.setStatus(StatusClosed, "seat resumed elsewhere")
	case code == wire.CloseSeatExpired:
		c.token = ""
		c.fallback(closeReason(reason, "seat expired"))
	case !wire.Retryable(code):
		c.fallback(closeReason(reason, "refused by server"))
	case c.status == StatusOnline:
		c.beginReconnect(now)
	default:
		c.nextDial = now + c.backoff
		c.backoff = min(2*c.backoff, 2*time.Second)
	}
}

func closeReason(reason, fallback string) string {
	if reason != "" {
		return reason
	}
	return fallback
}

// onFrame handles one server frame.
func (c *Client) onFrame(frame []byte) {
	c.stats.BytesIn += uint64(len(frame))
	kind, ok := wire.KindOf(frame)
	if !ok || c.status == StatusOffline || c.status == StatusClosed {
		return // frames still queued after a fallback or close are not ours
	}
	now := c.now()
	switch kind {
	case wire.KindWelcome:
		var w wire.Welcome
		if w.Decode(frame) != nil || w.Version != wire.Version || w.Mode != c.mode.Name || w.Seats != c.mode.Seats {
			c.stats.DecodeErrors++
			c.dropConn(1000, "protocol mismatch")
			c.fallback("server runs another protocol or mode")
			return
		}
		if c.haveAuth && c.welcome.Code != w.Code {
			c.haveAuth = false // a different room: its ticks are not ours
		}
		c.welcome, c.haveWelcome, c.seat = w, true, w.Seat
		if w.Token != "" && w.Token != c.token {
			c.token = w.Token
			if c.cfg.Store != nil {
				_ = c.cfg.Store.Set(c.cfg.StoreKey, w.Token)
			}
		}
		c.everOnline = true
		c.setStatus(StatusOnline, "")
	case wire.KindRoom:
		prev := c.room.Phase
		if c.room.Decode(frame) != nil {
			c.stats.DecodeErrors++
			return
		}
		if c.room.Phase == wire.PhaseClosed && prev != wire.PhaseClosed {
			c.closedFrom = prev
		}
		c.roomAt = now
		if len(c.room.Results) > 0 {
			c.results = append(c.results[:0], c.room.Results...)
		}
		c.room.Results = nil
	case wire.KindPong:
		var p wire.Pong
		if p.Decode(frame) == nil {
			c.sync.add(now, &p, c.period)
		}
	case wire.KindKeyframe, wire.KindDelta:
		c.onSnapshot(frame, now)
	}
}

func (c *Client) onSnapshot(frame []byte, now time.Duration) {
	if err := c.snap.Decode(frame); err != nil {
		c.stats.DecodeErrors++
		return
	}
	s := &c.snap
	newer := !c.haveAuth || s.Tick > c.authTick
	if s.Keyframe {
		if s.Epoch < c.keyEpoch {
			c.stats.Stale++
			return
		}
		c.keyEpoch = s.Epoch
		c.keyState = append(c.keyState[:0], s.Body...)
		if !newer {
			c.stats.Stale++
			return
		}
		c.authState = append(c.authState[:0], s.Body...)
		c.stats.Keyframes++
	} else {
		if s.Epoch != c.keyEpoch || !newer {
			c.stats.Stale++
			return
		}
		state, err := wire.ApplyDelta(c.authState, c.keyState, s.Body, 0)
		if err != nil {
			c.stats.DecodeErrors++
			return
		}
		c.authState = state
		c.stats.Deltas++
	}
	c.authTick, c.authAt, c.haveAuth, c.newAuth = s.Tick, now, true, true
	c.nAuthSeat = min(len(s.Seats), c.mode.Seats)
	for i := 0; i < c.nAuthSeat; i++ {
		r := &c.authSeats[i]
		r.flags, r.lead = s.Seats[i].Flags, s.Seats[i].Lead
		r.n = copy(r.input[:], s.Seats[i].Input)
	}
	c.adaptLead()
}

// adaptLead nudges the target lead so the server holds about InputDelay ticks
// of this seat's input: up quickly when the buffer runs dry, down slowly when
// it is deeper than needed.
func (c *Client) adaptLead() {
	if c.seat < 0 || c.seat >= c.nAuthSeat || c.authSeats[c.seat].flags&wire.FlagConnected == 0 || c.authTick < 2 {
		return
	}
	lead := float64(c.authSeats[c.seat].lead)
	want := float64(c.welcome.InputDelay)
	switch {
	case lead < 1:
		c.extraLead += 0.25
	default:
		c.extraLead += 0.02 * (want - lead)
	}
	c.extraLead = min(max(c.extraLead, -want), 16)
}

func (c *Client) estimateServer(now time.Duration) (float64, bool) {
	if t, ok := c.sync.serverTick(now, c.period); ok {
		return t, true
	}
	if !c.haveAuth {
		return 0, false
	}
	// Before the first timed pong: the newest snapshot left the server about
	// half a round trip ago.
	return float64(c.authTick) + float64(now-c.authAt+c.sync.rtt()/2)/float64(c.period), true
}

// targetTick is where the predicted clock should be now: the server's
// timeline plus the upstream delay plus the input buffer.
func (c *Client) targetTick(now time.Duration) (float64, bool) {
	est, ok := c.estimateServer(now)
	if !ok {
		return 0, false
	}
	up := float64(c.sync.rtt()/2) / float64(c.period)
	return est + up + float64(c.welcome.InputDelay) + c.extraLead, true
}

func (c *Client) runOnline(delta time.Duration, now time.Duration) {
	if c.sim == nil || c.simSeed != c.welcome.Seed {
		c.sim, c.simSeed = c.mode.NewSim(c.welcome.Seed), c.welcome.Seed
		c.tick = c.authTick
		c.hist.reset()
		c.newAuth = true
	}
	if c.newAuth {
		c.reconcile()
	}
	scale := 1.0
	if target, ok := c.targetTick(now); ok {
		err := target - (float64(c.tick) + c.alpha)
		snap := max(4, float64(c.mode.TickRate)/5)
		if math.Abs(err) > snap {
			c.resync(int64(math.Round(target)))
		} else {
			scale = 1 + min(max(err*0.08, -0.15), 0.15)
		}
	}
	c.dilation = scale
	c.frame = c.clock.AdvanceFunc(time.Duration(float64(delta)*scale), c.stepFn)
	c.alpha = c.frame.Alpha
}

// restoreAuth loads the newest authoritative state into the simulation.
func (c *Client) restoreAuth() bool {
	var err error
	if r, ok := c.sim.(sim.CheckedRestorer); ok {
		err = r.RestoreSnapshot(c.authState)
	} else {
		c.sim.Restore(c.authState)
	}
	if err != nil {
		c.stats.DecodeErrors++
		return false
	}
	if c.cfg.OnSnapshot != nil {
		c.cfg.OnSnapshot(c.authTick, c.sim)
	}
	return true
}

// reconcile restores the newest snapshot and replays the predicted ticks
// after it with the local inputs from history.
func (c *Client) reconcile() {
	c.newAuth = false
	if !c.restoreAuth() {
		return
	}
	c.stats.Reconciles++
	if c.tick < c.authTick || c.tick-c.authTick > uint64(c.cfg.MaxReplay) {
		c.tick = c.authTick // behind the server, or too far ahead: start over here
		return
	}
	for t := c.authTick + 1; t <= c.tick; t++ {
		c.simulate(t)
		c.stats.Replayed++
	}
}

// resync jumps the predicted clock to target: restore the newest snapshot and
// run forward to target.
func (c *Client) resync(target int64) {
	c.stats.Resyncs++
	if target < int64(c.authTick) {
		target = int64(c.authTick)
	}
	if !c.restoreAuth() {
		return
	}
	c.tick = c.authTick
	for n := 0; int64(c.tick) < target && n < c.cfg.MaxReplay; n++ {
		c.tick++
		c.simulate(c.tick)
	}
	c.newClock()
}

// step runs one new predicted tick (the game.Clock callback).
func (c *Client) step(uint64) {
	c.tick++
	c.simulate(c.tick)
}

// simulate runs tick t on the predicted simulation: the local seat's input
// from history (sampled and recorded the first time t runs), every other seat's
// last authoritative input.
func (c *Client) simulate(t uint64) {
	for i := 0; i < c.mode.Seats; i++ {
		if i == c.seat {
			c.inputs.Set(i, c.localInput(t))
			continue
		}
		if i < c.nAuthSeat {
			r := &c.authSeats[i]
			c.inputs.Set(i, r.input[:r.n])
		} else {
			c.inputs.Set(i, nil)
		}
	}
	c.sim.Tick(c.inputs.Map())
}

func (c *Client) localInput(t uint64) []byte {
	if data, ok := c.hist.get(t); ok {
		return data
	}
	var data []byte
	if c.cfg.Input != nil {
		data = c.cfg.Input(t, c.inputs.Buffer(max(c.seat, 0)))
	}
	if len(data) > c.mode.InputSize {
		data = data[:c.mode.InputSize]
	}
	c.hist.put(t, data)
	data, _ = c.hist.get(t)
	return data
}

// sendInputs sends the newest local inputs, with up to Redundancy earlier ones
// the server has not confirmed.
func (c *Client) sendInputs() {
	if c.seat < 0 || c.room.Phase != wire.PhaseRunning || !c.haveAuth || c.tick <= c.hist.sent {
		return
	}
	first := uint64(1)
	if c.tick > uint64(c.cfg.Redundancy) {
		first = c.tick - uint64(c.cfg.Redundancy) + 1
	}
	if c.seat < c.nAuthSeat {
		if acked := c.authTick + uint64(c.authSeats[c.seat].lead); acked+1 > first {
			first = min(acked+1, c.tick)
		}
	}
	var in wire.Input
	in.First = first
	for t := first; t <= c.tick && in.Count < wire.MaxRedundancy; t++ {
		data, _ := c.hist.get(t)
		in.Inputs[in.Count] = data
		in.Count++
	}
	c.sendBuf = in.Append(c.sendBuf[:0])
	if c.conn.Send(c.sendBuf) == nil {
		c.stats.InputFrames++
		c.stats.BytesOut += uint64(len(c.sendBuf))
		c.hist.sent = c.tick
	}
}

func (c *Client) maybePing(now time.Duration) {
	if !c.haveWelcome || now < c.nextPing {
		return
	}
	c.pingSeq++
	p := wire.Ping{Seq: c.pingSeq, Sent: int64(now)}
	c.sendBuf = p.Append(c.sendBuf[:0])
	if c.conn.Send(c.sendBuf) == nil {
		c.stats.BytesOut += uint64(len(c.sendBuf))
	}
	c.pings++
	if c.pings < 5 {
		c.nextPing = now + 100*time.Millisecond // fast first samples
	} else {
		c.nextPing = now + 500*time.Millisecond
	}
}

// fallback switches to offline play. A running match continues from its
// current state with bots in every other seat; otherwise a fresh local match
// starts.
func (c *Client) fallback(reason string) {
	if c.cfg.NoFallback {
		c.dropConn(1000, "closing")
		c.setStatus(StatusClosed, reason)
		return
	}
	c.dropConn(1000, "playing offline")
	running := c.room.Phase == wire.PhaseRunning || (c.room.Phase == wire.PhaseClosed && c.closedFrom == wire.PhaseRunning)
	if c.sim == nil || !running {
		seed := c.cfg.Seed
		if seed == 0 {
			var b [8]byte
			_, _ = rand.Read(b[:])
			seed = binary.LittleEndian.Uint64(b[:])
		}
		c.sim, c.simSeed, c.tick = c.mode.NewSim(seed), seed, 0
		c.hist.reset()
		c.seat = 0
		c.welcome.Code = ""
	}
	if c.seat < 0 {
		c.seat = 0
	}
	c.bots = make([]mode.Bot, c.mode.Seats)
	seats := make([]wire.SeatInfo, c.mode.Seats)
	for i := range c.bots {
		seats[i] = wire.SeatInfo{Kind: wire.SeatBot, Role: c.mode.Role(i), Name: "Bot " + itoa(i+1)}
		if i != c.seat {
			c.bots[i] = c.mode.Bot(i)
		}
	}
	seats[c.seat] = wire.SeatInfo{Kind: wire.SeatHuman, Connected: true, Role: c.mode.Role(c.seat), Name: c.cfg.Join.Name}
	c.room = wire.Room{Phase: wire.PhaseRunning, Tick: c.tick, Seats: seats}
	c.localEnd = 0
	if c.mode.Duration > 0 {
		c.localEnd = c.tick + uint64(c.mode.Duration/c.period)
		c.room.Remaining = c.mode.Duration
		c.roomAt = c.now()
	}
	c.newClock()
	c.setStatus(StatusOffline, reason)
}

func (c *Client) runOffline(delta time.Duration) {
	if c.room.Phase != wire.PhaseRunning {
		return
	}
	c.frame = c.clock.AdvanceFunc(delta, c.offlineStep)
	c.alpha = c.frame.Alpha
}

func (c *Client) offlineStep(uint64) {
	if c.room.Phase != wire.PhaseRunning {
		return
	}
	c.tick++
	t := c.tick
	for i := 0; i < c.mode.Seats; i++ {
		if i == c.seat {
			c.inputs.Set(i, c.localInput(t))
			continue
		}
		ctx := mode.BotContext{Tick: t, Seat: i, Role: c.mode.Role(i), Seed: c.simSeed, Sim: c.sim}
		c.inputs.Set(i, c.bots[i].Input(ctx, c.inputs.Buffer(i)))
	}
	c.sim.Tick(c.inputs.Map())
	c.room.Tick = t
	over := c.mode.Over != nil && c.mode.Over(c.sim, t)
	if over || (c.localEnd > 0 && t >= c.localEnd) {
		c.room.Phase = wire.PhaseResults
		if c.mode.Results != nil {
			c.results = c.mode.Results(c.sim)
		}
	}
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}

// historySize is how many ticks of local input the client keeps.
const historySize = 256

// history keeps the local seat's input per tick in fixed storage.
type history struct {
	ticks [historySize]uint64
	lens  [historySize]uint8
	data  [historySize][wire.MaxInputSize]byte
	sent  uint64 // newest tick sent to the server
}

func (h *history) put(t uint64, in []byte) {
	i := t % historySize
	h.ticks[i] = t
	h.lens[i] = uint8(copy(h.data[i][:], in))
}

func (h *history) get(t uint64) ([]byte, bool) {
	i := t % historySize
	if t == 0 || h.ticks[i] != t {
		return nil, false
	}
	return h.data[i][:h.lens[i]], true
}

func (h *history) reset() {
	clear(h.ticks[:])
	h.sent = 0
}
