package room

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// sumSim is a minimal deterministic simulation: per seat, the sum of every
// input byte it was given. Snapshot reuses one buffer, so a step allocates
// nothing on the simulation's side.
type sumSim struct {
	tick uint64
	sums []uint64
	buf  []byte
}

func newSumSim(seats int) *sumSim { return &sumSim{sums: make([]uint64, seats)} }

func (s *sumSim) Tick(inputs map[string]sim.Input) {
	s.tick++
	for i := range s.sums {
		if in, ok := inputs[mode.PlayerID(i)]; ok {
			for _, b := range in.Data {
				s.sums[i] += uint64(b)
			}
		}
	}
}

func (s *sumSim) Snapshot() []byte {
	s.buf = binary.LittleEndian.AppendUint64(s.buf[:0], s.tick)
	for _, v := range s.sums {
		s.buf = binary.LittleEndian.AppendUint64(s.buf, v)
	}
	return s.buf
}

func (s *sumSim) Restore(b []byte) { _ = s.RestoreSnapshot(b) }
func (s *sumSim) RestoreSnapshot(b []byte) error {
	if len(b) != 8+8*len(s.sums) {
		return errors.New("size")
	}
	s.tick = binary.LittleEndian.Uint64(b)
	for i := range s.sums {
		s.sums[i] = binary.LittleEndian.Uint64(b[8+8*i:])
	}
	return nil
}
func (s *sumSim) State() []byte { return s.Snapshot() }

func sumMode(name string, seats int) mode.Mode {
	return mode.Mode{
		Name: name, Seats: seats, TickRate: 20, InputSize: 4, Countdown: time.Second,
		NewSim: func(uint64) sim.Simulation { return newSumSim(seats) },
		NewBot: func(seat int, _ string) mode.Bot {
			return mode.BotFunc(func(_ mode.BotContext, dst []byte) []byte { return append(dst, 1) })
		},
	}
}

type vclock struct{ t time.Duration }

func (c *vclock) now() time.Duration { return c.t }

func manualServer(t *testing.T, cfg Config) (*Server, *vclock) {
	t.Helper()
	clock := &vclock{}
	cfg.Manual, cfg.Now = true, clock.now
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if len(cfg.Modes) == 0 {
		cfg.Modes = []mode.Mode{sumMode("sum", 3)}
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, clock
}

// advance moves the clock in tick-sized steps so rooms run every tick.
func advance(s *Server, c *vclock, d time.Duration) {
	const step = 10 * time.Millisecond
	for end := c.t + d; c.t < end; {
		c.t = min(c.t+step, end)
		s.Advance()
	}
}

// peer decodes everything a LocalConn receives.
type peer struct {
	conn      *LocalConn
	welcome   wire.Welcome
	room      wire.Room
	snap      wire.Snapshot
	state     []byte
	keyframes int
	deltas    int
	closed    bool
	key       []byte
}

func (p *peer) poll(t *testing.T) {
	t.Helper()
	st := p.conn.Poll(func(f []byte) {
		kind, _ := wire.KindOf(f)
		switch kind {
		case wire.KindWelcome:
			if err := p.welcome.Decode(f); err != nil {
				t.Fatal(err)
			}
		case wire.KindRoom:
			if err := p.room.Decode(f); err != nil {
				t.Fatal(err)
			}
		case wire.KindKeyframe, wire.KindDelta:
			if err := p.snap.Decode(f); err != nil {
				t.Fatal(err)
			}
			if p.snap.Keyframe {
				p.keyframes++
				p.key = append(p.key[:0], p.snap.Body...)
				p.state = append(p.state[:0], p.snap.Body...)
			} else {
				p.deltas++
				var err error
				if p.state, err = wire.ApplyDelta(p.state, p.key, p.snap.Body, 0); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
	p.closed = st == wire.ConnClosed
}

func connect(t *testing.T, s *Server, req JoinRequest) *peer {
	t.Helper()
	c, err := s.ConnectLocal(req)
	if err != nil {
		t.Fatalf("ConnectLocal(%+v): %v", req, err)
	}
	p := &peer{conn: c}
	p.poll(t)
	return p
}

func TestSoloQuickMatchStartsWithBots(t *testing.T) {
	s, clock := manualServer(t, Config{})
	p := connect(t, s, JoinRequest{Name: "solo"})
	if p.welcome.Seat != 0 || p.welcome.Seats != 3 || p.welcome.Token == "" || len(p.welcome.Code) != codeLength {
		t.Fatalf("welcome %+v", p.welcome)
	}
	advance(s, clock, 10*time.Millisecond)
	p.poll(t)
	if p.room.Phase != PhaseCountdown {
		t.Fatalf("phase %v, want countdown right after a solo join", p.room.Phase)
	}
	advance(s, clock, time.Second)
	p.poll(t)
	if p.room.Phase != PhaseRunning {
		t.Fatalf("phase %v after the countdown, want running", p.room.Phase)
	}
	if p.room.Seats[0].Kind != SeatHuman || p.room.Seats[1].Kind != SeatBot || p.room.Seats[2].Kind != SeatBot {
		t.Fatalf("seats %+v, want one person and two bots", p.room.Seats)
	}
	advance(s, clock, time.Second)
	p.poll(t)
	r, _ := s.Room(p.welcome.Code)
	if got := r.Tick(); got < 19 || got > 21 {
		t.Fatalf("tick %d after one second at 20 Hz", got)
	}
	if p.keyframes == 0 {
		t.Fatal("no keyframe received")
	}
	// The decoded state matches the server's simulation.
	var server []byte
	r.With(func(sm sim.Simulation, _ uint64) { server = append(server, sm.Snapshot()...) })
	if !bytes.Equal(p.state, server) {
		t.Fatalf("client state %x != server %x", p.state, server)
	}
	if p.snap.Seats[1].Flags&wire.FlagBot == 0 {
		t.Fatal("bot seat not flagged as bot-played")
	}
}

func TestQuickMatchGroupsPeopleAndCodesJoin(t *testing.T) {
	s, clock := manualServer(t, Config{})
	a := connect(t, s, JoinRequest{})
	advance(s, clock, 100*time.Millisecond)
	b := connect(t, s, JoinRequest{})
	if b.welcome.Code != a.welcome.Code || b.welcome.Seat != 1 {
		t.Fatalf("quick match split people: %s seat %d vs %s", b.welcome.Code, b.welcome.Seat, a.welcome.Code)
	}
	// A code typed with look-alike letters and lower case still joins.
	typed := strings.ToLower(a.welcome.Code)
	typed = strings.NewReplacer("0", "o", "1", "l").Replace(typed)
	c := connect(t, s, JoinRequest{Code: typed[:2] + "-" + typed[2:]})
	if c.welcome.Code != a.welcome.Code || c.welcome.Seat != 2 {
		t.Fatalf("code join landed in %s seat %d", c.welcome.Code, c.welcome.Seat)
	}
	// Full: a fourth person becomes a spectator.
	d := connect(t, s, JoinRequest{Code: a.welcome.Code})
	if d.welcome.Seat != -1 {
		t.Fatalf("fourth join got seat %d, want spectator", d.welcome.Seat)
	}
	// A private room is never quick-matched into.
	priv := connect(t, s, JoinRequest{Private: true})
	if priv.welcome.Code == a.welcome.Code {
		t.Fatal("private join reused a public room")
	}
	e := connect(t, s, JoinRequest{})
	if e.welcome.Code == priv.welcome.Code {
		t.Fatal("quick match entered a private room")
	}
	if _, err := s.ConnectLocal(JoinRequest{Code: "ZZZZZ"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown code: %v", err)
	}
	if _, err := s.ConnectLocal(JoinRequest{Mode: "nope"}); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("unknown mode: %v", err)
	}
}

func TestHotJoinTakesOverBotSeat(t *testing.T) {
	s, clock := manualServer(t, Config{})
	a := connect(t, s, JoinRequest{})
	advance(s, clock, 1100*time.Millisecond)
	a.poll(t)
	if a.room.Phase != PhaseRunning {
		t.Fatal("not running")
	}
	b := connect(t, s, JoinRequest{})
	if b.welcome.Code != a.welcome.Code || b.welcome.Seat != 1 {
		t.Fatalf("hot join got %s seat %d", b.welcome.Code, b.welcome.Seat)
	}
	advance(s, clock, 50*time.Millisecond)
	a.poll(t)
	if a.room.Seats[1].Kind != SeatHuman || !a.room.Seats[1].Connected {
		t.Fatalf("seat 1 %+v, want a connected person", a.room.Seats[1])
	}
}

func TestReconnectResumesSeatWithinGrace(t *testing.T) {
	s, clock := manualServer(t, Config{ReconnectGrace: 5 * time.Second})
	a := connect(t, s, JoinRequest{})
	advance(s, clock, 1100*time.Millisecond)
	token, code := a.welcome.Token, a.welcome.Code
	a.conn.Close(1001, "tab closed")
	advance(s, clock, 2*time.Second)

	r, _ := s.Room(code)
	info := r.Info()
	if info.SeatList[0].Kind != "human" || info.SeatList[0].Connected {
		t.Fatalf("seat 0 during grace: %+v", info.SeatList[0])
	}
	// The bot covers the absent person: seat 0's input is bot-made.
	b := connect(t, s, JoinRequest{Resume: token})
	if b.welcome.Seat != 0 || b.welcome.Code != code || b.welcome.Token != token {
		t.Fatalf("resume landed on %s seat %d", b.welcome.Code, b.welcome.Seat)
	}
	advance(s, clock, 100*time.Millisecond)
	b.poll(t)
	if !b.room.Seats[0].Connected {
		t.Fatal("resumed seat not connected")
	}

	// Past the grace, the seat goes to a bot and the token stops working.
	b.conn.Close(1001, "gone")
	advance(s, clock, 6*time.Second)
	info = r.Info()
	if info.SeatList[0].Kind != "bot" {
		t.Fatalf("seat 0 after grace is %s, want bot", info.SeatList[0].Kind)
	}
	// A stale token falls back to a normal join, which takes the bot's seat.
	c := connect(t, s, JoinRequest{Resume: token})
	if c.welcome.Token == token {
		t.Fatal("expired token was honoured")
	}
	if c.welcome.Code != code {
		t.Fatalf("stale resume quick-matched into %s, want the running room %s", c.welcome.Code, code)
	}
}

func TestResumeReplacesOldConnection(t *testing.T) {
	s, clock := manualServer(t, Config{})
	a := connect(t, s, JoinRequest{})
	b := connect(t, s, JoinRequest{Resume: a.welcome.Token})
	advance(s, clock, 10*time.Millisecond)
	a.poll(t)
	if !a.closed {
		t.Fatal("old connection still open after its seat was resumed")
	}
	if code, _ := a.conn.CloseStatus(); code != wire.CloseReplaced {
		t.Fatalf("close code %d, want CloseReplaced", code)
	}
	if b.welcome.Seat != a.welcome.Seat {
		t.Fatal("resume changed seat")
	}
}

func TestInputsApplyAtTheirTick(t *testing.T) {
	s, clock := manualServer(t, Config{InputDelay: 3})
	p := connect(t, s, JoinRequest{})
	advance(s, clock, 1100*time.Millisecond) // countdown done; tick 0 keyframe sent
	p.poll(t)
	if p.room.Phase != PhaseRunning {
		t.Fatalf("phase %v", p.room.Phase)
	}
	r, _ := s.Room(p.welcome.Code)
	now := r.Tick()
	// Five ticks of 10, then five of 0. After the last one the server repeats
	// the held 0, so the sum stays at 50.
	var in wire.Input
	in.First, in.Count = now+1, 10
	for i := range 10 {
		in.Inputs[i] = []byte{0}
		if i < 5 {
			in.Inputs[i] = []byte{10}
		}
	}
	if err := p.conn.Send(in.Append(nil)); err != nil {
		t.Fatal(err)
	}
	// Late inputs are counted and ignored.
	late := wire.Input{First: 1, Count: 1}
	late.Inputs[0] = []byte{200}
	advance(s, clock, 60*time.Millisecond)
	_ = p.conn.Send(late.Append(nil))
	advance(s, clock, 700*time.Millisecond)
	p.poll(t)
	var sums []uint64
	r.With(func(sm sim.Simulation, _ uint64) { sums = append(sums, sm.(*sumSim).sums...) })
	if sums[0] != 50 {
		t.Fatalf("seat 0 sum %d, want 50 from five inputs of 10", sums[0])
	}
	st := r.Stats()
	if st.LateInputs != 1 {
		t.Fatalf("late inputs %d, want 1", st.LateInputs)
	}
	if st.MissedInputs == 0 {
		t.Fatal("ticks without input were not counted as missed")
	}
	if p.welcome.InputDelay != 3 {
		t.Fatalf("welcome input delay %d", p.welcome.InputDelay)
	}
}

func TestLeadReportsBufferedInput(t *testing.T) {
	s, clock := manualServer(t, Config{})
	p := connect(t, s, JoinRequest{})
	advance(s, clock, 1010*time.Millisecond) // the countdown ends; tick 0 has not run a step
	r, _ := s.Room(p.welcome.Code)
	if r.Phase() != PhaseRunning || r.Tick() != 0 {
		t.Fatalf("phase %v tick %d", r.Phase(), r.Tick())
	}
	now := r.Tick()
	var in wire.Input
	in.First, in.Count = now+1, 8
	for i := range 8 {
		in.Inputs[i] = []byte{1}
	}
	_ = p.conn.Send(in.Append(nil))
	advance(s, clock, 50*time.Millisecond) // one tick at 20 Hz
	p.poll(t)
	if got := p.snap.Seats[0].Lead; got != 7 {
		t.Fatalf("lead %d after one of eight buffered ticks ran, want 7", got)
	}
}

func TestMatchEndsWithResultsThenCloses(t *testing.T) {
	m := sumMode("timed", 2)
	m.Duration = 500 * time.Millisecond
	m.Results = func(sim.Simulation) []byte { return []byte("gg") }
	s, clock := manualServer(t, Config{Modes: []mode.Mode{m}, ResultsHold: time.Second})
	p := connect(t, s, JoinRequest{})
	advance(s, clock, 1600*time.Millisecond)
	p.poll(t)
	if p.room.Phase != PhaseResults || string(p.room.Results) != "gg" {
		t.Fatalf("phase %v results %q", p.room.Phase, p.room.Results)
	}
	r, _ := s.Room(p.welcome.Code)
	if r.Tick() != 10 {
		t.Fatalf("match ran %d ticks, want 10 (0.5 s at 20 Hz)", r.Tick())
	}
	advance(s, clock, 1100*time.Millisecond)
	p.poll(t)
	if !p.closed {
		t.Fatal("connection open after the results hold")
	}
	if code, _ := p.conn.CloseStatus(); code != wire.CloseRoomOver {
		t.Fatalf("close code %d", code)
	}
	if _, ok := s.Room(p.welcome.Code); ok {
		t.Fatal("server still lists the closed room")
	}
	<-r.Done()
}

func TestIdleRoomCloses(t *testing.T) {
	s, clock := manualServer(t, Config{IdleTimeout: 2 * time.Second, ReconnectGrace: time.Second})
	p := connect(t, s, JoinRequest{})
	advance(s, clock, 1100*time.Millisecond)
	p.conn.Close(1000, "")
	advance(s, clock, 2100*time.Millisecond)
	if _, ok := s.Room(p.welcome.Code); ok {
		t.Fatal("room with nobody connected outlived IdleTimeout")
	}
}

func TestCapacityLimits(t *testing.T) {
	s, _ := manualServer(t, Config{MaxRooms: 1, MaxSpectators: -1})
	a := connect(t, s, JoinRequest{})
	if _, err := s.ConnectLocal(JoinRequest{Private: true}); !errors.Is(err, ErrServerFull) {
		t.Fatalf("second room: %v, want ErrServerFull", err)
	}
	for range 2 {
		connect(t, s, JoinRequest{Code: a.welcome.Code})
	}
	if _, err := s.ConnectLocal(JoinRequest{Code: a.welcome.Code}); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("join a full room without spectator places: %v", err)
	}
}

func TestBadConfig(t *testing.T) {
	if _, err := NewServer(Config{}); err == nil {
		t.Fatal("no modes accepted")
	}
	m := sumMode("a", 2)
	if _, err := NewServer(Config{Modes: []mode.Mode{m, m}}); err == nil {
		t.Fatal("duplicate modes accepted")
	}
	if _, err := NewServer(Config{Modes: []mode.Mode{m}, AllowedOrigins: []string{"not an origin"}}); err == nil {
		t.Fatal("bad origin accepted")
	}
}

// TestStepAllocations bounds the server's per-tick work with a simulation
// that allocates nothing: a tick without a snapshot allocates nothing, and a
// tick with a delta snapshot allocates only its frame.
func TestStepAllocations(t *testing.T) {
	for _, tc := range []struct {
		name          string
		snapshotEvery int
		max           float64
	}{
		{"no snapshot", 1000, 0},
		{"delta snapshot", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, clock := manualServer(t, Config{SnapshotEvery: tc.snapshotEvery, KeyframeEvery: 1 << 20})
			p := connect(t, s, JoinRequest{})
			advance(s, clock, 1100*time.Millisecond)
			r, _ := s.Room(p.welcome.Code)
			var in wire.Input
			in.First, in.Count = r.Tick()+1, 1
			in.Inputs[0] = []byte{3}
			frame := in.Append(nil)
			r.mu.Lock()
			defer r.mu.Unlock()
			for range 5 {
				r.stepLocked()
			}
			n := testing.AllocsPerRun(200, func() {
				r.receiveInputLocked(p.conn.clientID, &in)
				r.stepLocked()
				in.First = r.tick + 1
				_ = frame
			})
			if n > tc.max {
				t.Fatalf("%v allocations per tick, want <= %v", n, tc.max)
			}
		})
	}
}

func TestInputDecodeAllocations(t *testing.T) {
	s, clock := manualServer(t, Config{})
	p := connect(t, s, JoinRequest{})
	advance(s, clock, 1100*time.Millisecond)
	r, _ := s.Room(p.welcome.Code)
	var in wire.Input
	in.First, in.Count = r.Tick()+1, 8
	for i := range 8 {
		in.Inputs[i] = []byte{1, 2}
	}
	frame := in.Append(nil)
	if n := testing.AllocsPerRun(200, func() { r.onFrame(p.conn.clientID, frame) }); n != 0 {
		t.Fatalf("input frame handling allocates %v times", n)
	}
}

// --- real WebSocket tests ---

func httpServer(t *testing.T, cfg Config) (*Server, *httptest.Server, string) {
	t.Helper()
	if len(cfg.Modes) == 0 {
		cfg.Modes = []mode.Mode{sumMode("sum", 2)}
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/play/", http.StripPrefix("/play", s))
	hs := httptest.NewServer(mux)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Close(ctx)
		hs.Close()
	})
	return s, hs, "ws" + strings.TrimPrefix(hs.URL, "http") + "/play/ws"
}

func readWelcome(t *testing.T, c *websocket.Conn) wire.Welcome {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		typ, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		if k, _ := wire.KindOf(data); k == wire.KindWelcome {
			var w wire.Welcome
			if err := w.Decode(data); err != nil {
				t.Fatal(err)
			}
			return w
		}
	}
}

func TestOriginAllowlistGatesWebSocket(t *testing.T) {
	_, _, url := httpServer(t, Config{AllowedOrigins: []string{"https://portal.example"}, RequireOrigin: true})
	_, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"https://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown origin: err %v status %v, want 403", err, resp)
	}
	if _, resp, err = websocket.DefaultDialer.Dial(url, nil); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing origin with RequireOrigin: %v", err)
	}
	c, _, err := websocket.DefaultDialer.Dial(url+"?name=judge", http.Header{"Origin": {"https://portal.example"}})
	if err != nil {
		t.Fatalf("allowed origin: %v", err)
	}
	defer c.Close()
	if w := readWelcome(t, c); w.Seat != 0 || w.Mode != "sum" {
		t.Fatalf("welcome %+v", w)
	}
}

func TestRefusedJoinCarriesCloseCode(t *testing.T) {
	_, _, url := httpServer(t, Config{})
	c, _, err := websocket.DefaultDialer.Dial(url+"?code=ZZZZZ", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = c.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != wire.CloseNotFound {
		t.Fatalf("read error %v, want close %d", err, wire.CloseNotFound)
	}
}

func TestConnectRateLimit(t *testing.T) {
	_, _, url := httpServer(t, Config{ConnectsPerMinute: 2})
	for i := range 2 {
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		c.Close()
	}
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third connect: %v, want 429", err)
	}
}

func TestWebSocketReconnectResumesSeat(t *testing.T) {
	_, _, url := httpServer(t, Config{})
	a, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := readWelcome(t, a)
	a.Close() // abrupt drop, no close frame
	time.Sleep(50 * time.Millisecond)
	b, _, err := websocket.DefaultDialer.Dial(url+"?resume="+w.Token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if w2 := readWelcome(t, b); w2.Seat != w.Seat || w2.Code != w.Code {
		t.Fatalf("resumed into %s seat %d, want %s seat %d", w2.Code, w2.Seat, w.Code, w.Seat)
	}
}

func TestLobbyListsPublicRoomsWithCORS(t *testing.T) {
	s, hs, _ := httpServer(t, Config{AllowedOrigins: []string{"https://portal.example"}})
	if _, err := s.ConnectLocal(JoinRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConnectLocal(JoinRequest{Private: true}); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, hs.URL+"/play/rooms", nil)
	req.Header.Set("Origin", "https://portal.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Rooms []Info }
	if err := jsonDecode(resp, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rooms) != 1 || !body.Rooms[0].Public {
		t.Fatalf("lobby %+v, want the one public room", body.Rooms)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://portal.example" {
		t.Fatalf("CORS header %q", got)
	}
	req.Header.Set("Origin", "https://evil.example")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown origin lobby status %d", resp2.StatusCode)
	}
}

func TestClientKeyUsesProxyAppendedAddress(t *testing.T) {
	s, _ := manualServer(t, Config{TrustForwardedFor: true})
	r := httptest.NewRequest(http.MethodGet, "http://rooms.test/ws", nil)
	r.RemoteAddr = "10.42.0.7:51234"                    // the proxy
	r.Header.Add("X-Forwarded-For", "6.6.6.6, 1.2.3.4") // client forged 6.6.6.6
	r.Header.Add("X-Forwarded-For", "203.0.113.9")      // appended by the proxy
	if got := s.clientKey(r); got != "203.0.113.9" {
		t.Fatalf("key %q, want the proxy-appended address", got)
	}
	r.Header.Del("X-Forwarded-For")
	if got := s.clientKey(r); got != "10.42.0.7" {
		t.Fatalf("key without the header %q, want the peer", got)
	}
	plain, _ := manualServer(t, Config{})
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := plain.clientKey(r); got != "10.42.0.7" {
		t.Fatalf("untrusted header used: %q", got)
	}
}
