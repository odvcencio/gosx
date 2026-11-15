package netsync_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/game/netsync"
	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// recorder wraps a simulation and keeps its snapshot after every tick, keyed
// by the tick the world reports.
type recorder struct {
	sim.Simulation
	mu     sync.Mutex
	states map[uint64][]byte
	first  bool // keep only the first state per tick (a client's first prediction)
}

func (r *recorder) Tick(inputs map[string]sim.Input) {
	r.Simulation.Tick(inputs)
	g := roomtest.Of(r.Simulation)
	t := g.World.Tick
	r.mu.Lock()
	if _, seen := r.states[t]; !seen || !r.first {
		r.states[t] = g.World.Encode(nil)
	}
	r.mu.Unlock()
}

func (r *recorder) Unwrap() sim.Simulation { return r.Simulation }

func (r *recorder) RestoreSnapshot(b []byte) error {
	return r.Simulation.(sim.CheckedRestorer).RestoreSnapshot(b)
}

func (r *recorder) get(t uint64) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[t]
	return s, ok
}

// bodiesOf unwraps a recorder.
func bodiesOf(s sim.Simulation) *roomtest.Game {
	if r, ok := s.(*recorder); ok {
		return roomtest.Of(r.Simulation)
	}
	return roomtest.Of(s)
}

type harness struct {
	t      *testing.T
	now    time.Duration
	srv    *room.Server
	net    netsync.SimNet
	conns  []*netsync.SimConn
	refuse int // dial attempts to refuse with 1006 before connecting
	down   bool
	server *recorder
	mu     sync.Mutex
}

type harnessOptions struct {
	seats    int
	props    int
	bot      mode.Bot
	duration time.Duration
	tickRate int
	cfg      room.Config
	net      netsync.SimNet
}

// newHarness starts a manual room server running roomtest bodies, with every
// server simulation recorded.
func newHarness(t *testing.T, o harnessOptions) (*harness, mode.Mode) {
	t.Helper()
	if o.seats == 0 {
		o.seats = 2
	}
	h := &harness{t: t, net: o.net}
	base := roomtest.Mode(roomtest.Options{Seats: o.seats, Props: o.props, Countdown: 500 * time.Millisecond, Duration: o.duration, TickRate: o.tickRate})
	if o.bot != nil {
		bot := o.bot
		base.NewBot = func(int, string) mode.Bot { return bot }
	}
	serverMode := base
	serverMode.NewSim = func(seed uint64) sim.Simulation {
		r := &recorder{Simulation: base.NewSim(seed), states: map[uint64][]byte{}}
		h.mu.Lock()
		h.server = r
		h.mu.Unlock()
		return r
	}
	serverMode.Results = func(s sim.Simulation) []byte { return base.Results(s.(*recorder).Simulation) }
	cfg := o.cfg
	cfg.Modes = []mode.Mode{serverMode}
	cfg.Manual, cfg.Now = true, func() time.Duration { return h.now }
	cfg.Logf = func(string, ...any) {}
	seed := o.net.Seed
	cfg.NewSeed = func() uint64 { seed++; return seed * 0x9E3779B97F4A7C15 }
	srv, err := room.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.srv = srv
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	return h, base
}

func (h *harness) clock() time.Duration { return h.now }

func (h *harness) dial(raw string) (netsync.Conn, error) {
	if h.down {
		return netsync.FailedConn(1006, "unreachable"), nil
	}
	if h.refuse > 0 {
		h.refuse--
		return netsync.FailedConn(1006, "refused"), nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	lc, err := h.srv.ConnectLocal(room.ParseJoin(u.Query()))
	if err != nil {
		var re *room.Error
		if errors.As(err, &re) {
			return netsync.FailedConn(re.Code, re.Reason), nil
		}
		return nil, err
	}
	net := h.net
	net.Seed += uint64(len(h.conns)) * 7919
	sc := netsync.NewSimConn(lc, net, h.clock)
	h.conns = append(h.conns, sc)
	return sc, nil
}

func (h *harness) client(m mode.Mode, input func(uint64, []byte) []byte, tweak func(*netsync.Config)) *netsync.Client {
	h.t.Helper()
	cfg := netsync.Config{
		URL: "wss://rooms.test/play/ws", Mode: m, Input: input, Dial: h.dial, Now: h.clock,
		CatchUpBudget: -1, // virtual time: never stop a frame's ticks early
	}
	if tweak != nil {
		tweak(&cfg)
	}
	c, err := netsync.New(cfg)
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

// run advances virtual time in render frames, driving the server and the
// clients. Frame lengths vary between 14 and 19 ms, like a real display loop
// under load.
func (h *harness) run(d time.Duration, clients []*netsync.Client, each func()) {
	end := h.now + d
	for i := 0; h.now < end; i++ {
		frame := time.Duration(14+i*7%6) * time.Millisecond
		h.now += frame
		h.srv.Advance()
		for _, c := range clients {
			c.Frame(frame)
		}
		if each != nil {
			each()
		}
	}
}

func (h *harness) serverState(t uint64) ([]byte, bool) {
	h.mu.Lock()
	r := h.server
	h.mu.Unlock()
	if r == nil {
		return nil, false
	}
	return r.get(t)
}

// scripted is a held-input pattern that changes every few ticks.
func scripted(t uint64, dst []byte) []byte {
	pattern := [...]byte{roomtest.Right, roomtest.Right | roomtest.Down, roomtest.Down, roomtest.Left | roomtest.Boost, 0, roomtest.Up}
	return append(dst, pattern[(t/7)%uint64(len(pattern))])
}

func roomOf(t *testing.T, h *harness, c *netsync.Client) *room.Room {
	t.Helper()
	r, ok := h.srv.Room(c.Room().Code)
	if !ok {
		t.Fatalf("server has no room %q", c.Room().Code)
	}
	return r
}

func sameState(a, b []byte) bool { return bytes.Equal(a, b) }

var _ = wire.PhaseRunning
