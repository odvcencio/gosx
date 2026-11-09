package netsync_test

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game/netsync"
	"m31labs.dev/gosx/room"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/room/wire"
	"m31labs.dev/gosx/sim"
)

// clientRecorderMode wraps m so the client's first prediction of every tick
// is recorded.
func clientRecorderMode(m mode.Mode, out **recorder) mode.Mode {
	base := m.NewSim
	m.NewSim = func(seed uint64) sim.Simulation {
		r := &recorder{Simulation: base(seed), states: map[uint64][]byte{}, first: true}
		*out = r
		return r
	}
	return m
}

// TestPredictionMatchesServerUnderLatencyJitterLoss runs one person and an
// idle bot over a link with 60 ms up, 40 ms down, 30 ms jitter and 10% loss.
// Every tick the client predicts after warm-up must equal the server's state
// for that tick: inputs reach the server in time (redundancy covers the loss),
// and the idle bot's held input is predicted exactly.
func TestPredictionMatchesServerUnderLatencyJitterLoss(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 2, props: 6, bot: mode.IdleBot,
		net: netsync.SimNet{Seed: 1, Up: 60 * time.Millisecond, Down: 40 * time.Millisecond, Jitter: 30 * time.Millisecond, Loss: 0.10}})
	var predicted *recorder
	c := h.client(clientRecorderMode(m, &predicted), scripted, nil)
	h.run(8*time.Second, []*netsync.Client{c}, nil)

	st := c.Stats()
	if c.Status() != netsync.StatusOnline || c.Phase() != wire.PhaseRunning {
		t.Fatalf("status %v phase %v", c.Status(), c.Phase())
	}
	if st.AuthTick < 200 {
		t.Fatalf("only %d authoritative ticks in 8 s at 30 Hz", st.AuthTick)
	}
	r := roomOf(t, h, c)
	if rs := r.Stats(); rs.LateInputs > 3 {
		t.Fatalf("%d late input frames; the clock did not keep inputs ahead (room stats %+v)", rs.LateInputs, rs)
	}
	warm := uint64(60) // two seconds
	checked, wrong := 0, 0
	for tick := warm; tick <= st.AuthTick; tick++ {
		want, ok := h.serverState(tick)
		got, ok2 := predicted.get(tick)
		if !ok || !ok2 {
			continue
		}
		checked++
		if !sameState(got, want) {
			wrong++
		}
	}
	if checked < 150 {
		t.Fatalf("compared only %d ticks", checked)
	}
	if wrong != 0 {
		t.Fatalf("%d of %d predicted ticks differ from the server (stats %+v)", wrong, checked, st)
	}
	if st.Reconciles == 0 || st.Replayed == 0 {
		t.Fatalf("no reconciliation happened: %+v", st)
	}
	t.Logf("rtt %v jitter %v ahead %d lead %d target %.2f reconciles %d replayed %d resyncs %d in %dB out %dB",
		st.RTT, st.Jitter, st.Ahead, st.Lead, st.TargetLead, st.Reconciles, st.Replayed, st.Resyncs, st.BytesIn, st.BytesOut)
}

// TestReconciledStateEqualsServer checks every snapshot the client restores
// against the server's state for that tick, with chasing bots (so remote
// predictions are often wrong) and a lossy, reordering link. It also checks
// that the client's own body is predicted right most of the time.
func TestReconciledStateEqualsServer(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 3, props: 4,
		net: netsync.SimNet{Seed: 2, Up: 50 * time.Millisecond, Down: 50 * time.Millisecond, Jitter: 40 * time.Millisecond, Loss: 0.05}})
	var predicted *recorder
	restored, mismatched := 0, 0
	c := h.client(clientRecorderMode(m, &predicted), scripted, func(cfg *netsync.Config) {
		cfg.OnSnapshot = func(tick uint64, s sim.Simulation) {
			want, ok := h.serverState(tick)
			if tick == 0 || !ok {
				return
			}
			restored++
			if !sameState(bodiesOf(s).World.Encode(nil), want) {
				mismatched++
			}
		}
	})
	h.run(6*time.Second, []*netsync.Client{c}, nil)
	if restored < 100 || mismatched != 0 {
		t.Fatalf("%d of %d restored snapshots differ from the server", mismatched, restored)
	}
	// The client's own body: first prediction versus server.
	seat := c.Seat()
	checked, right := 0, 0
	for tick := uint64(60); tick <= c.Stats().AuthTick; tick++ {
		want, ok := h.serverState(tick)
		got, ok2 := predicted.get(tick)
		if !ok || !ok2 {
			continue
		}
		checked++
		w, g := decodeWorld(t, want, 3, 4), decodeWorld(t, got, 3, 4)
		if w.Bodies[seat] == g.Bodies[seat] {
			right++
			continue
		}
		// A wrong prediction of the own body must come from contact with a
		// body whose input was predicted: within reach of the local body in
		// the server's or the predicted world.
		if near(w, seat) > 6*roomtest.Unit && near(g, seat) > 6*roomtest.Unit {
			t.Errorf("tick %d: own body mispredicted with no body nearby (server %+v predicted %+v)", tick, w.Bodies[seat], g.Bodies[seat])
		}
	}
	if checked < 100 {
		t.Fatalf("compared only %d ticks", checked)
	}
	t.Logf("own body right in %d of %d ticks; %+v", right, checked, c.Stats())
}

// near returns the distance (largest axis) from body i to the nearest other.
func near(w *roomtest.World, i int) int32 {
	best := int32(1 << 30)
	for j, b := range w.Bodies {
		if j == i {
			continue
		}
		d := max(abs32(b.X-w.Bodies[i].X), abs32(b.Y-w.Bodies[i].Y))
		best = min(best, d)
	}
	return best
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func decodeWorld(t *testing.T, state []byte, seats, props int) *roomtest.World {
	t.Helper()
	w := &roomtest.World{Seats: seats, Bodies: make([]roomtest.Body, seats+props)}
	if err := w.Decode(state); err != nil {
		t.Fatal(err)
	}
	return w
}

// TestInputLeadConvergesWithAsymmetricLatency: 150 ms up and 20 ms down. A
// round-trip estimate alone would put the client 65 ms too early on the way
// up; the server-reported lead must pull the input buffer to the target.
func TestInputLeadConvergesWithAsymmetricLatency(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 1,
		net: netsync.SimNet{Seed: 3, Up: 150 * time.Millisecond, Down: 20 * time.Millisecond, Jitter: 10 * time.Millisecond}})
	c := h.client(m, scripted, nil)
	var leads []int
	var lateAtWarm uint64
	h.run(10*time.Second, []*netsync.Client{c}, func() {
		if h.now > 5*time.Second && c.Phase() == wire.PhaseRunning {
			if lateAtWarm == 0 {
				lateAtWarm = roomOf(t, h, c).Stats().LateInputs + 1
			}
			leads = append(leads, c.Stats().Lead)
		}
	})
	sum, low := 0, 1<<30
	for _, l := range leads {
		sum += l
		low = min(low, l)
	}
	avg := float64(sum) / float64(len(leads))
	st := c.Stats()
	if avg < 1 || avg > 5 {
		t.Fatalf("average server-side lead %.2f ticks over the last 5 s, want 1 to 5 (target %.2f)", avg, st.TargetLead)
	}
	if late := roomOf(t, h, c).Stats().LateInputs + 1 - lateAtWarm; late != 0 {
		t.Fatalf("%d late input frames after convergence", late)
	}
	t.Logf("lead avg %.2f min %d, target %.2f, rtt %v", avg, low, st.TargetLead, st.RTT)
}

// TestReconnectResumesSeat cuts the link mid-match. The client keeps
// predicting, notices the stall, redials with its token, and gets its seat
// back; meanwhile a bot covered the seat on the server.
func TestReconnectResumesSeat(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 2, net: netsync.SimNet{Seed: 4, Up: 30 * time.Millisecond, Down: 30 * time.Millisecond}})
	var statuses []netsync.Status
	c := h.client(m, scripted, func(cfg *netsync.Config) {
		cfg.StallTimeout = time.Second
		cfg.OnStatus = func(s netsync.Status, _ string) { statuses = append(statuses, s) }
	})
	h.run(3*time.Second, []*netsync.Client{c}, nil)
	seat, code, token := c.Seat(), c.Room().Code, c.Token()
	tickBefore := c.Tick()
	h.conns[len(h.conns)-1].Cut(true)
	h.refuse = 2 // the first two redials fail too
	h.run(1500*time.Millisecond, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusReconnecting {
		t.Fatalf("status %v after the link died, want reconnecting", c.Status())
	}
	if c.Tick() <= tickBefore+30 {
		t.Fatalf("prediction stalled during the outage: tick %d -> %d", tickBefore, c.Tick())
	}
	r := roomOf(t, h, c)
	h.run(3*time.Second, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusOnline || c.Seat() != seat || c.Room().Code != code || c.Token() != token {
		t.Fatalf("after reconnect: status %v seat %d code %s (want %d %s)", c.Status(), c.Seat(), c.Room().Code, seat, code)
	}
	info := r.Info()
	if !info.SeatList[seat].Connected || info.SeatList[seat].Kind != "human" {
		t.Fatalf("server seat %+v after resume", info.SeatList[seat])
	}
	if st := c.Stats(); st.Dials < 4 || st.Ahead < 0 || st.Ahead > 20 {
		t.Fatalf("stats after resume: %+v", st)
	}
	want := []netsync.Status{netsync.StatusConnecting, netsync.StatusOnline, netsync.StatusReconnecting, netsync.StatusOnline}
	if len(statuses) != len(want) {
		t.Fatalf("status changes %v, want %v", statuses, want)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("status changes %v, want %v", statuses, want)
		}
	}
}

// TestFallbackWhenServerUnreachable: no server answers, so after the connect
// timeout the client plays a local match against bots.
func TestFallbackWhenServerUnreachable(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 3})
	h.down = true
	c := h.client(m, func(_ uint64, dst []byte) []byte { return append(dst, roomtest.Right) }, func(cfg *netsync.Config) {
		cfg.ConnectTimeout = 2 * time.Second
	})
	h.run(1900*time.Millisecond, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusConnecting {
		t.Fatalf("status %v before the timeout", c.Status())
	}
	h.run(2*time.Second, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusOffline || !c.Room().Offline {
		t.Fatalf("status %v, want offline", c.Status())
	}
	if c.Tick() < 50 || c.Tick() > 70 {
		t.Fatalf("offline match ran %d ticks in ~1.9 s at 30 Hz", c.Tick())
	}
	g := roomtest.Of(c.Sim())
	if g.World.Bodies[0].X <= 0 && g.World.Bodies[0].VX <= 0 {
		t.Fatalf("local input did not move the local body: %+v", g.World.Bodies[0])
	}
	seats := c.Room().Seats
	if seats[0].Kind != wire.SeatHuman || seats[1].Kind != wire.SeatBot || seats[2].Kind != wire.SeatBot {
		t.Fatalf("offline seats %+v", seats)
	}
}

// TestFallbackKeepsWorldWhenRoomCloses: the server closes the room with a
// code that rules out reconnecting. The client continues the same world
// offline, from the same tick, in the same seat.
func TestFallbackKeepsWorldWhenRoomCloses(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 2, net: netsync.SimNet{Seed: 5, Up: 20 * time.Millisecond, Down: 20 * time.Millisecond}})
	c := h.client(m, scripted, nil)
	h.run(3*time.Second, []*netsync.Client{c}, nil)
	seat, tick := c.Seat(), c.Tick()
	roomOf(t, h, c).Close(wire.CloseIdle, "maintenance")
	h.run(500*time.Millisecond, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusOffline {
		t.Fatalf("status %v (%s), want offline", c.Status(), c.Reason())
	}
	if c.Seat() != seat || c.Tick() < tick+5 || c.Tick() > tick+30 {
		t.Fatalf("offline continuation: seat %d->%d tick %d->%d", seat, c.Seat(), tick, c.Tick())
	}
}

// TestMatchOverEndsClient: a timed match ends, results arrive, and the client
// closes instead of falling back.
func TestMatchOverEndsClient(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 2, duration: 2 * time.Second, cfg: room.Config{ResultsHold: time.Second}})
	c := h.client(m, scripted, nil)
	h.run(3*time.Second, []*netsync.Client{c}, nil)
	if c.Phase() != wire.PhaseResults || len(c.Room().Results) != 8 {
		t.Fatalf("phase %v results %v", c.Phase(), c.Room().Results)
	}
	h.run(1500*time.Millisecond, []*netsync.Client{c}, nil)
	if c.Status() != netsync.StatusClosed || c.Reason() != "match over" {
		t.Fatalf("status %v (%s), want closed after the match", c.Status(), c.Reason())
	}
}

func TestNoURLPlaysOffline(t *testing.T) {
	_, m := newHarness(t, harnessOptions{seats: 2, duration: time.Second})
	now := time.Duration(0)
	c, err := netsync.New(netsync.Config{Mode: m, Now: func() time.Duration { return now }, CatchUpBudget: -1, Seed: 9})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 90; i++ {
		now += 16 * time.Millisecond
		c.Frame(16 * time.Millisecond)
	}
	if c.Status() != netsync.StatusOffline || c.Phase() != wire.PhaseResults || len(c.Room().Results) != 8 {
		t.Fatalf("status %v phase %v results %v", c.Status(), c.Phase(), c.Room().Results)
	}
	if c.Tick() != 30 {
		t.Fatalf("a 1 s offline match ran %d ticks, want 30", c.Tick())
	}
}

// TestTwoClientsSeeEachOther runs two people in one room over different
// links; both converge on the server's state.
func TestTwoClientsSeeEachOther(t *testing.T) {
	h, m := newHarness(t, harnessOptions{seats: 2, net: netsync.SimNet{Seed: 6, Up: 40 * time.Millisecond, Down: 40 * time.Millisecond, Jitter: 20 * time.Millisecond, Loss: 0.02}})
	a := h.client(m, scripted, nil)
	b := h.client(m, func(t uint64, dst []byte) []byte { return append(dst, roomtest.Left|roomtest.Up) }, nil)
	h.run(5*time.Second, []*netsync.Client{a, b}, nil)
	if a.Room().Code != b.Room().Code || a.Seat() == b.Seat() {
		t.Fatalf("quick match split the pair: %s/%d and %s/%d", a.Room().Code, a.Seat(), b.Room().Code, b.Seat())
	}
	info := roomOf(t, h, a).Info()
	if info.Humans != 2 || info.Bots != 0 {
		t.Fatalf("room %+v, want two people", info)
	}
	for _, c := range []*netsync.Client{a, b} {
		if st := c.Stats(); st.Status != netsync.StatusOnline || st.Keyframes == 0 || st.Deltas == 0 {
			t.Fatalf("client stats %+v", st)
		}
	}
}
