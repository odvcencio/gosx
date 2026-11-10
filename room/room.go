package room

import (
	"context"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// Phase is a room lifecycle phase.
type Phase = wire.Phase

// Lifecycle phases, in order.
const (
	PhaseLobby     = wire.PhaseLobby
	PhaseCountdown = wire.PhaseCountdown
	PhaseRunning   = wire.PhaseRunning
	PhaseResults   = wire.PhaseResults
	PhaseClosed    = wire.PhaseClosed
)

// SeatKind says who plays a seat.
type SeatKind = wire.SeatKind

// Seat kinds.
const (
	SeatEmpty = wire.SeatEmpty
	SeatHuman = wire.SeatHuman
	SeatBot   = wire.SeatBot
)

// Room is one match: its seats, its lifecycle, and the authoritative
// simulation. A Room runs on its own goroutine unless Config.Manual is set.
type Room struct {
	srv    *Server
	code   string
	mode   mode.Mode
	public bool
	period time.Duration
	seed   uint64
	hub    *hub.Hub
	// keyEvery is the snapshots per keyframe.
	keyEvery int

	mu         sync.Mutex
	phase      wire.Phase
	phaseEnd   time.Duration // end of a timed phase; zero when open-ended
	created    time.Duration
	lastHuman  time.Duration // last time a person was connected
	firstHuman time.Duration
	haveHuman  bool
	forceStart bool
	seats      []*seat
	links      map[string]attachment
	spectators int

	sim        sim.Simulation
	tick       uint64
	runStart   time.Duration
	inputs     *mode.Inputs
	seatStates []wire.SeatState
	epoch      uint64
	keyframe   []byte
	sinceKey   int
	denc       wire.DeltaEncoder
	results    []byte
	stats      Stats

	dirty  bool // seats or phase changed: broadcast a Room frame
	closed bool
	done   chan struct{}
	after  []func() // work that must run after Room.mu is released
	frame  []byte   // scratch for small replies
}

type attachment struct {
	link link
	seat int // -1 for a spectator
}

// Stats counts a room's traffic and input health since it was created.
type Stats struct {
	Ticks         uint64
	Keyframes     uint64
	Deltas        uint64
	KeyframeBytes uint64
	DeltaBytes    uint64
	// LateInputs counts inputs that arrived after their tick ran.
	LateInputs uint64
	// MissedInputs counts ticks where a connected person's input had not
	// arrived, so the server repeated their previous input.
	MissedInputs uint64
}

// Info is a summary of a room for lobby lists and tests.
type Info struct {
	Code       string        `json:"code"`
	Mode       string        `json:"mode"`
	Public     bool          `json:"public"`
	Phase      string        `json:"phase"`
	Tick       uint64        `json:"tick"`
	Humans     int           `json:"humans"`
	Bots       int           `json:"bots"`
	Seats      int           `json:"seats"`
	Spectators int           `json:"spectators"`
	Joinable   bool          `json:"joinable"`
	SeatList   []SeatInfo    `json:"seatList,omitempty"`
	Remaining  time.Duration `json:"remainingNanos,omitempty"`
}

// SeatInfo describes one seat.
type SeatInfo struct {
	Index     int    `json:"index"`
	Role      string `json:"role,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	Connected bool   `json:"connected,omitempty"`
}

func newRoom(s *Server, m mode.Mode, code string, public bool, seed uint64, now time.Duration) *Room {
	r := &Room{
		srv: s, code: code, mode: m, public: public, period: m.Period(), seed: seed,
		phase: PhaseLobby, created: now, lastHuman: now,
		links: make(map[string]attachment), done: make(chan struct{}),
		seatStates: make([]wire.SeatState, m.Seats),
		inputs:     mode.NewInputs(m.Seats, m.InputSize),
	}
	r.seats = make([]*seat, m.Seats)
	for i := range r.seats {
		r.seats[i] = &seat{index: i, role: m.Role(i), bot: m.Bot(i)}
	}
	h := hub.New("room-" + code)
	h.MaxClients = m.Seats + s.cfg.MaxSpectators
	h.RequireOrigin = s.cfg.RequireOrigin
	h.CheckOrigin = s.checkOrigin
	h.MaxMessagesPerSecond = s.cfg.MaxMessagesPerSecond
	if h.MaxMessagesPerSecond == 0 {
		h.MaxMessagesPerSecond = 2*m.TickRate + 20
	}
	r.keyEvery = s.cfg.KeyframeEvery
	if r.keyEvery == 0 {
		r.keyEvery = max(1, m.TickRate/s.cfg.SnapshotEvery)
	}
	h.SetBinaryMessageHandler(func(c *hub.Client, data []byte) bool {
		r.onFrame(c.ID, data)
		return true
	})
	h.On("join", func(ctx *hub.Context) {
		seatText, _ := ctx.Client.Metadata("seat")
		token, _ := ctx.Client.Metadata("token")
		index, err := strconv.Atoi(seatText)
		if err != nil {
			index = -1
		}
		r.attach(hubLink{h: h, clientID: ctx.Client.ID}, index, token)
	})
	h.On("leave", func(ctx *hub.Context) { r.detach(ctx.Client.ID) })
	r.hub = h
	return r
}

// Code returns the room's join code.
func (r *Room) Code() string { return r.code }

// Mode returns the room's mode name.
func (r *Room) Mode() string { return r.mode.Name }

// Seed returns the seed of the room's simulation.
func (r *Room) Seed() uint64 { return r.seed }

// Phase returns the current lifecycle phase.
func (r *Room) Phase() Phase {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.phase
}

// Tick returns the number of simulation ticks run in the current match.
func (r *Room) Tick() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tick
}

// Stats returns the room's counters.
func (r *Room) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

// Done is closed when the room has closed.
func (r *Room) Done() <-chan struct{} { return r.done }

// Start skips the rest of the lobby wait: the countdown starts at the next
// advance, with bots in every empty seat.
func (r *Room) Start() {
	r.mu.Lock()
	r.forceStart = true
	r.mu.Unlock()
}

// With calls fn with the authoritative simulation and its tick, under the
// room's lock. fn must not keep s or call into the room. s is nil outside a
// running or finished match.
func (r *Room) With(fn func(s sim.Simulation, tick uint64)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.sim, r.tick)
}

// Info returns a summary of the room.
func (r *Room) Info() Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.infoLocked(r.srv.now(), true)
}

func (r *Room) infoLocked(now time.Duration, seats bool) Info {
	info := Info{Code: r.code, Mode: r.mode.Name, Public: r.public, Phase: r.phase.String(), Tick: r.tick,
		Seats: len(r.seats), Spectators: r.spectators}
	for _, s := range r.seats {
		switch s.kind {
		case SeatHuman:
			info.Humans++
		case SeatBot:
			info.Bots++
		}
		if seats {
			info.SeatList = append(info.SeatList, SeatInfo{Index: s.index, Role: s.role, Kind: s.kind.String(), Name: s.name, Connected: s.connected()})
		}
	}
	info.Joinable = r.freeSeatLocked() >= 0
	if r.phaseEnd > now {
		info.Remaining = r.phaseEnd - now
	}
	return info
}

// Close ends the room at once and disconnects every client with code.
func (r *Room) Close(code int, reason string) {
	r.mu.Lock()
	r.closeLocked(code, reason)
	r.unlock()
}

// AdvanceTo runs the room's lifecycle and every tick due by now, the time on
// the server's clock (Config.Now). With Config.Manual set, call it (or
// Server.Advance) to drive the room; otherwise the room's goroutine does.
//
// If the game's code panics (the simulation, a bot, or a Mode callback),
// AdvanceTo recovers, closes this room with wire.CloseInternalError (1011),
// and reports the panic to Config.Logf and Config.OnPanic. Other rooms and
// the server keep running.
func (r *Room) AdvanceTo(now time.Duration) {
	r.mu.Lock()
	r.advanceRecoverLocked(now)
	r.unlock()
}

// advanceRecoverLocked is advanceLocked behind a recover. Room.mu stays held
// through the panic, so the deferred close runs with the lock it expects.
func (r *Room) advanceRecoverLocked(now time.Duration) {
	defer func() {
		if v := recover(); v != nil {
			r.failLocked(v, debug.Stack())
		}
	}()
	r.advanceLocked(now)
}

// failLocked closes a room whose game code panicked. Its state may be half
// updated, so nothing of it is kept: clients get 1011 and the server forgets
// the room. The report runs after Room.mu is released, because Logf and
// OnPanic are the app's code.
func (r *Room) failLocked(v any, stack []byte) {
	p := Panic{Room: r.code, Mode: r.mode.Name, Tick: r.tick, Value: v, Stack: stack}
	r.closeLocked(wire.CloseInternalError, "internal error")
	srv := r.srv
	r.after = append(r.after, func() { srv.reportPanic(p) })
}

// unlock releases Room.mu, then runs the work deferred while it was held.
func (r *Room) unlock() {
	after := r.after
	r.after = nil
	r.mu.Unlock()
	for _, fn := range after {
		fn()
	}
}

func (r *Room) run() {
	defer r.srv.wg.Done()
	interval := min(r.period, 50*time.Millisecond)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-t.C:
			r.AdvanceTo(r.srv.now())
		}
	}
}

func (r *Room) advanceLocked(now time.Duration) {
	if r.closed {
		return
	}
	switch r.phase {
	case PhaseLobby:
		if r.connectedHumansLocked() > 0 && !r.haveHuman {
			r.haveHuman, r.firstHuman = true, now
		}
		if r.forceStart || (r.haveHuman && now >= r.firstHuman+r.mode.LobbyWait) {
			r.enterCountdownLocked(now)
		}
	case PhaseCountdown:
		if now >= r.phaseEnd {
			r.enterRunningLocked(now)
		}
	case PhaseRunning:
		r.runTicksLocked(now)
	case PhaseResults:
		if now >= r.phaseEnd {
			r.closeLocked(wire.CloseRoomOver, "match over")
			return
		}
	}
	r.expireSeatsLocked(now)
	if r.connectedHumansLocked() > 0 || r.spectators > 0 {
		r.lastHuman = now
	} else if now-r.lastHuman >= r.srv.cfg.IdleTimeout {
		r.closeLocked(wire.CloseIdle, "no players connected")
		return
	}
	if r.dirty {
		r.broadcastRoomLocked(now)
	}
}

func (r *Room) enterCountdownLocked(now time.Duration) {
	r.phase, r.phaseEnd, r.dirty = PhaseCountdown, now+r.mode.Countdown, true
	if r.mode.Countdown <= 0 {
		r.enterRunningLocked(now)
	}
}

func (r *Room) enterRunningLocked(now time.Duration) {
	for _, s := range r.seats {
		if s.kind == SeatEmpty {
			s.kind, s.fill, s.name = SeatBot, true, botName(s.index)
		}
		s.resetMatch()
	}
	r.sim = r.mode.NewSim(r.seed)
	r.tick, r.runStart = 0, now
	r.phase, r.dirty = PhaseRunning, true
	r.phaseEnd = 0
	if r.mode.Duration > 0 {
		r.phaseEnd = now + r.mode.Duration
	}
	r.keyframe, r.sinceKey = nil, 0
	r.snapshotLocked() // tick 0: every client starts from the same state
}

func (r *Room) enterResultsLocked(now time.Duration) {
	if r.mode.Results != nil {
		r.results = r.mode.Results(r.sim)
	}
	r.phase, r.phaseEnd, r.dirty = PhaseResults, now+r.srv.cfg.ResultsHold, true
}

// runTicksLocked runs every tick due by now on the room's fixed timeline,
// at most MaxCatchUp per call. A backlog beyond one second is dropped by
// moving the timeline, so a stalled server does not fast-forward the match;
// clients realign from their next Pong.
func (r *Room) runTicksLocked(now time.Duration) {
	due := uint64(0)
	if now > r.runStart {
		due = uint64((now - r.runStart) / r.period)
	}
	if limit := r.tick + uint64(r.mode.TickRate); due > limit {
		r.runStart += time.Duration(due-limit) * r.period
		due = limit
	}
	for ran := 0; r.tick < due && ran < r.srv.cfg.MaxCatchUp; ran++ {
		r.stepLocked()
		if r.overLocked(now) {
			r.enterResultsLocked(now)
			return
		}
	}
}

func (r *Room) overLocked(now time.Duration) bool {
	if r.mode.Over != nil && r.mode.Over(r.sim, r.tick) {
		return true
	}
	return r.mode.Duration > 0 && r.tick >= uint64(r.mode.Duration/r.period)
}

// stepLocked runs one authoritative tick. It allocates nothing of its own;
// see snapshotLocked for the per-snapshot frame.
func (r *Room) stepLocked() {
	r.tick++
	t := r.tick
	for i, s := range r.seats {
		data, fromBot := r.seatInputLocked(s, t)
		r.inputs.Set(i, data)
		s.lastN = copy(s.last[:], r.inputs.Get(i))
		s.usedBot = fromBot
	}
	r.sim.Tick(r.inputs.Map())
	r.stats.Ticks++
	for _, s := range r.seats {
		if s.acked < t {
			s.acked = t
		}
		for {
			if _, ok := s.ring.get(s.acked + 1); !ok {
				break
			}
			s.acked++
		}
	}
	if t%uint64(r.srv.cfg.SnapshotEvery) == 0 {
		r.snapshotLocked()
	}
}

// seatInputLocked picks seat's input for tick t: the person's buffered input;
// their previous input when it is missing (they are connected but it is late);
// or the bot's, when no person plays or the person is away.
func (r *Room) seatInputLocked(s *seat, t uint64) ([]byte, bool) {
	if s.human() {
		if data, ok := s.ring.get(t); ok {
			return data, false
		}
		if s.client != "" {
			s.missed++
			r.stats.MissedInputs++
			return s.last[:s.lastN], false
		}
	}
	ctx := mode.BotContext{Tick: t, Seat: s.index, Role: s.role, Seed: r.seed, Sim: r.sim}
	return s.bot.Input(ctx, r.inputs.Buffer(s.index)), true
}

// snapshotLocked offers the state after the current tick to every client: a
// new keyframe every KeyframeEvery snapshots (or when a delta would not save
// much), and otherwise a delta against the current keyframe. Each snapshot
// allocates its one frame, because the hub keeps offered frames without
// copying and shares them across clients.
func (r *Room) snapshotLocked() {
	state := r.sim.Snapshot()
	for i, s := range r.seats {
		lead := uint64(0)
		if s.acked > r.tick {
			lead = s.acked - r.tick
		}
		r.seatStates[i] = wire.SeatState{Flags: s.flags(), Lead: uint32(lead), Input: s.last[:s.lastN]}
	}
	header := 24 + len(r.seats)*(3+r.mode.InputSize)
	keyframe := r.keyframe == nil || r.sinceKey >= r.keyEvery
	var delta []byte
	if !keyframe {
		buf := make([]byte, 0, header+16+len(state)/4)
		buf = wire.AppendSnapshotHeader(buf, false, r.epoch, r.tick, r.seatStates)
		head := len(buf)
		buf = r.denc.Append(buf, state)
		if len(buf)-head > len(state)/2+16 {
			keyframe = true
		} else {
			delta = buf
		}
	}
	if keyframe {
		r.epoch++
		buf := make([]byte, 0, header+len(state))
		buf = wire.AppendSnapshotHeader(buf, true, r.epoch, r.tick, r.seatStates)
		head := len(buf)
		buf = append(buf, state...)
		r.keyframe, r.sinceKey = buf, 0
		r.denc.SetBase(buf[head:])
		r.stats.Keyframes++
		r.stats.KeyframeBytes += uint64(len(buf))
	} else {
		r.stats.Deltas++
		r.stats.DeltaBytes += uint64(len(delta))
	}
	r.sinceKey++
	r.hub.BroadcastBinarySnapshot(r.epoch, r.keyframe, delta)
	for _, a := range r.links {
		if c := a.link.local(); c != nil {
			c.offerSnapshot(r.epoch, r.keyframe, delta)
		}
	}
}

// expireSeatsLocked ends reservations whose person did not come back in time.
// In the lobby the seat empties; in a match a bot keeps it.
func (r *Room) expireSeatsLocked(now time.Duration) {
	var tokens []string
	for _, s := range r.seats {
		if !s.human() || s.client != "" || s.expires == 0 || now < s.expires {
			continue
		}
		tokens = append(tokens, s.token)
		s.token, s.expires, r.dirty = "", 0, true
		if r.phase == PhaseLobby || r.phase == PhaseCountdown {
			s.kind, s.name = SeatEmpty, ""
		} else {
			s.kind, s.fill, s.name = SeatBot, true, botName(s.index)
		}
	}
	if len(tokens) > 0 {
		r.after = append(r.after, func() { r.srv.forgetTokens(tokens) })
	}
}

func (r *Room) connectedHumansLocked() int {
	n := 0
	for _, s := range r.seats {
		if s.connected() {
			n++
		}
	}
	return n
}

// freeSeatLocked returns the seat a joining person would take, or -1.
func (r *Room) freeSeatLocked() int {
	switch r.phase {
	case PhaseLobby, PhaseCountdown:
		for _, s := range r.seats {
			if s.kind == SeatEmpty {
				return s.index
			}
		}
	case PhaseRunning:
		if r.mode.NoHotJoin {
			return -1
		}
		for _, s := range r.seats {
			if s.kind == SeatBot && s.fill {
				return s.index
			}
		}
	}
	return -1
}

// ticket is an admitted join: the seat to attach to (-1 for a spectator) and
// the token that proves it.
type ticket struct {
	seat  int
	token string
}

// claim reserves a seat (or a spectator place) for req.
//
// The server calls claim and resume with Server.mu held, so they must not
// queue deferred work (which may take Server.mu); they release with a plain
// Unlock.
func (r *Room) claim(req JoinRequest, now time.Duration) (ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.phase >= PhaseResults {
		return ticket{}, ErrRoomOver
	}
	if !req.Spectate {
		if i := r.freeSeatLocked(); i >= 0 {
			s := r.seats[i]
			s.kind, s.fill, s.client = SeatHuman, false, ""
			s.name = req.Name
			if s.name == "" {
				s.name = "Player " + strconv.Itoa(i+1)
			}
			s.token = newToken()
			s.expires = now + r.srv.cfg.ConnectGrace
			r.dirty = true
			return ticket{seat: i, token: s.token}, nil
		}
	}
	if r.spectators < r.srv.cfg.MaxSpectators {
		return ticket{seat: -1}, nil
	}
	return ticket{}, ErrRoomFull
}

// resume re-reserves the seat that token names, if it is still the person's.
func (r *Room) resume(token string, now time.Duration) (ticket, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.phase >= PhaseResults {
		return ticket{}, false
	}
	for _, s := range r.seats {
		if s.human() && s.token == token {
			if s.client == "" {
				s.expires = max(s.expires, now+r.srv.cfg.ConnectGrace)
			}
			return ticket{seat: s.index, token: token}, true
		}
	}
	return ticket{}, false
}

// attach binds a connected link to its reserved seat and greets it.
func (r *Room) attach(l link, index int, token string) {
	r.mu.Lock()
	defer r.unlock()
	if r.closed {
		r.after = append(r.after, func() { l.close(wire.CloseRoomOver, "room closed") })
		return
	}
	if index >= 0 {
		if index >= len(r.seats) || !r.seats[index].human() || r.seats[index].token != token {
			r.after = append(r.after, func() { l.close(wire.CloseSeatExpired, "seat no longer reserved") })
			return
		}
		s := r.seats[index]
		if old, ok := r.links[s.client]; ok && s.client != l.id() {
			delete(r.links, s.client)
			r.after = append(r.after, func() { old.link.close(wire.CloseReplaced, "seat resumed elsewhere") })
		}
		s.client, s.expires = l.id(), 0
	} else {
		r.spectators++
	}
	r.links[l.id()] = attachment{link: l, seat: index}
	r.dirty = true
	now := r.srv.now()
	welcome := wire.Welcome{
		Version: wire.Version, Seat: index, Seats: len(r.seats), TickRate: r.mode.TickRate,
		InputDelay: r.srv.cfg.InputDelay, SnapshotEvery: r.srv.cfg.SnapshotEvery, Seed: r.seed,
		Mode: r.mode.Name, Code: r.code, Token: token,
	}
	r.frame = welcome.Append(r.frame[:0])
	l.sendReliable(r.frame)
	r.frame = r.roomFrameLocked(r.frame[:0], now)
	l.sendReliable(r.frame)
	if c := l.local(); c != nil && r.keyframe != nil {
		c.offerSnapshot(r.epoch, r.keyframe, nil)
	}
}

// detach records that a link went away. A person's seat stays reserved for
// the reconnect grace; a bot covers it during a match.
func (r *Room) detach(id string) {
	r.mu.Lock()
	defer r.unlock()
	a, ok := r.links[id]
	if !ok {
		return
	}
	delete(r.links, id)
	if a.seat < 0 {
		r.spectators--
		return
	}
	if s := r.seats[a.seat]; s.client == id {
		s.client = ""
		s.expires = r.srv.now() + r.srv.cfg.ReconnectGrace
		r.dirty = true
	}
}

// onFrame handles one client frame: inputs and pings. Anything else is
// ignored. It runs on the client's read goroutine, whose hub pump recovers
// panics; the handlers below unlock with defer so a panic there cannot leave
// Room.mu held and stall the room and the server.
func (r *Room) onFrame(id string, frame []byte) {
	kind, ok := wire.KindOf(frame)
	if !ok {
		return
	}
	switch kind {
	case wire.KindInput:
		var in wire.Input
		if in.Decode(frame) != nil {
			return
		}
		r.receiveInput(id, &in)
	case wire.KindPing:
		var ping wire.Ping
		if ping.Decode(frame) != nil {
			return
		}
		r.answerPing(id, &ping)
	}
}

func (r *Room) answerPing(id string, ping *wire.Ping) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.links[id]
	if !ok {
		return
	}
	pong := wire.Pong{Seq: ping.Seq, Sent: ping.Sent}
	if r.phase == PhaseRunning {
		elapsed := r.srv.now() - r.runStart
		if elapsed > 0 {
			pong.Tick = uint64(elapsed / r.period)
			pong.Frac = uint16(min(65535, (elapsed%r.period)*65536/r.period))
		}
	}
	var buf [48]byte
	a.link.sendReliable(pong.Append(buf[:0]))
}

func (r *Room) receiveInput(id string, in *wire.Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receiveInputLocked(id, in)
}

func (r *Room) receiveInputLocked(id string, in *wire.Input) {
	a, ok := r.links[id]
	if !ok || a.seat < 0 || r.phase != PhaseRunning {
		return
	}
	s := r.seats[a.seat]
	if s.client != id {
		return
	}
	if in.Count > 0 && in.Last() <= r.tick {
		// The whole frame arrived after its newest tick ran. Count frames,
		// not ticks, so redundancy does not inflate the figure.
		s.late++
		r.stats.LateInputs++
		return
	}
	limit := r.tick + inputRingSize - 1
	for i := 0; i < in.Count; i++ {
		if t := in.First + uint64(i); t > r.tick && t <= limit {
			data := in.Inputs[i]
			if len(data) > r.mode.InputSize {
				data = data[:r.mode.InputSize]
			}
			s.ring.put(t, data)
		}
	}
}

func (r *Room) roomFrameLocked(dst []byte, now time.Duration) []byte {
	m := wire.Room{Phase: r.phase, Tick: r.tick, Results: r.results}
	if r.phaseEnd > now {
		m.Remaining = r.phaseEnd - now
	}
	m.Seats = make([]wire.SeatInfo, len(r.seats))
	for i, s := range r.seats {
		m.Seats[i] = s.info()
	}
	return m.Append(dst)
}

func (r *Room) broadcastRoomLocked(now time.Duration) {
	r.dirty = false
	frame := r.roomFrameLocked(nil, now)
	r.hub.BroadcastBinary(frame)
	for _, a := range r.links {
		if c := a.link.local(); c != nil {
			c.sendReliable(frame)
		}
	}
}

// closeLocked ends the room: every client gets code, the hub shuts down, and
// the server forgets the room and its tokens.
func (r *Room) closeLocked(code int, reason string) {
	if r.closed {
		return
	}
	r.closed = true
	r.phase, r.dirty = PhaseClosed, false
	frame := r.roomFrameLocked(nil, r.srv.now())
	r.hub.BroadcastBinary(frame)
	links := make([]link, 0, len(r.links))
	for _, a := range r.links {
		if c := a.link.local(); c != nil {
			c.sendReliable(frame)
		}
		links = append(links, a.link)
	}
	clear(r.links)
	var tokens []string
	for _, s := range r.seats {
		if s.token != "" {
			tokens = append(tokens, s.token)
			s.token = ""
		}
	}
	close(r.done)
	h := r.hub
	srv := r.srv
	srv.wg.Add(1)
	r.after = append(r.after, func() {
		srv.forgetRoom(r, tokens)
		// In-process links close at once; hub closes can block on a slow
		// socket, so they run on their own goroutine.
		remote := links[:0]
		for _, l := range links {
			if l.local() != nil {
				l.close(code, reason)
			} else {
				remote = append(remote, l)
			}
		}
		go func() {
			defer srv.wg.Done()
			for _, l := range remote {
				l.close(code, reason)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.Close(ctx)
		}()
	})
}

func botName(i int) string { return "Bot " + strconv.Itoa(i+1) }
