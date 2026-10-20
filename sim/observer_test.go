package sim

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

type tickObserverFunc func(TickEvent)

func (f tickObserverFunc) ObserveTick(e TickEvent) { f(e) }

type spyClock struct {
	*telemetrytest.FakeClock
	reads, stamps atomic.Int64
	ready         chan struct{}
	firstRead     chan struct{}
}
type spyTicker struct {
	Ticker
	owner *spyClock
}

func (c *spyClock) Now() Instant {
	now := c.FakeClock.Now()
	if c.reads.Add(1) == 1 && c.firstRead != nil {
		close(c.firstRead)
	}
	return now
}
func (c *spyClock) NewTicker(d time.Duration) Ticker {
	t := &spyTicker{Ticker: c.FakeClock.NewTicker(d), owner: c}
	close(c.ready)
	return t
}
func (t *spyTicker) Stamp(received time.Time) Instant {
	t.owner.stamps.Add(1)
	return t.Ticker.Stamp(received)
}
func observerClock() *spyClock {
	return &spyClock{FakeClock: telemetrytest.NewClock(time.Unix(0, 0)), ready: make(chan struct{})}
}
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatal("runner did not finish expected work")
		var v T
		return v
	}
}

type timedSimulation struct {
	clock             *telemetrytest.FakeClock
	snapshots, states int
}

func (s *timedSimulation) Tick(inputs map[string]Input) {
	_ = s.clock.Advance(2 * time.Millisecond)
	// Observation counts the original drain, not a simulation's mutated map.
	for k := range inputs {
		delete(inputs, k)
	}
}
func (s *timedSimulation) Snapshot() []byte {
	s.snapshots++
	_ = s.clock.Advance(3 * time.Millisecond)
	return []byte("snapshot")
}
func (*timedSimulation) Restore([]byte) {}
func (s *timedSimulation) State() []byte {
	s.states++
	_ = s.clock.Advance(4 * time.Millisecond)
	return []byte(`{"hp":1}`)
}

type timedBroadcast struct {
	hub.NoopObserver
	clock *telemetrytest.FakeClock
}

func (o timedBroadcast) Broadcast(*hub.Hub, int, int) { _ = o.clock.Advance(5 * time.Millisecond) }

func TestObserverWholeTickScheduledLagAndWallJump(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-replay", true: "replay"}[replay], func(t *testing.T) {
			c := observerClock()
			h := hub.New("test")
			h.Latch("sim:tick")
			if _, err := h.UseObserver(timedBroadcast{clock: c.FakeClock}); err != nil {
				t.Fatal(err)
			}
			s := &timedSimulation{clock: c.FakeClock}
			events := make(chan TickEvent, 2)
			var r *Runner
			r = New(h, s, Options{TickRate: 10, Clock: c, DisableReplay: !replay, Observer: tickObserverFunc(func(e TickEvent) {
				if r.Frame() != e.Frame || s.snapshots != int(e.Frame) || s.states != int(e.Frame) {
					t.Error("duplicate or incomplete state work")
				}
				if replay && len(r.Replay().Frames) != int(e.Frame) {
					t.Error("replay work excluded")
				}
				r.ReceiveInput("next", Input{}) // callbacks run outside the input lock
				events <- e
			})})
			r.ReceiveInput("a", Input{Data: []byte("one")})
			r.ReceiveInput("b", Input{Data: []byte("two")})
			r.Start()
			t.Cleanup(r.Stop)
			await(t, c.ready)
			c.JumpWall(24 * time.Hour)
			_ = c.Advance(350 * time.Millisecond) // queued deadline 100 ms; no catch-up
			first := await(t, events)
			if first != (TickEvent{Frame: 1, Duration: 14 * time.Millisecond, Lag: 250 * time.Millisecond, StateBytes: 8, Inputs: 2}) {
				t.Fatal(first)
			}
			c.JumpWall(-48 * time.Hour)
			_ = c.Advance(286 * time.Millisecond) // now 650 ms; next queued deadline 400 ms
			second := await(t, events)
			if second != (TickEvent{Frame: 2, Duration: 14 * time.Millisecond, Lag: 250 * time.Millisecond, StateBytes: 8, Inputs: 1}) {
				t.Fatal(second)
			}
			r.Stop() // no fake-clock advance is needed to interrupt the ticker wait
			if r.Frame() != 2 || c.reads.Load() != 4 || c.stamps.Load() != 2 || c.PendingTimers() != 0 {
				t.Fatal(r.Frame(), c.reads.Load(), c.stamps.Load(), c.PendingTimers())
			}
		})
	}
}

func TestNilObserverAvoidsClockReadsAndStopWakesWait(t *testing.T) {
	c := observerClock()
	r := New(hub.New("test"), &mockSim{}, Options{Clock: c, TickRate: 10})
	r.Start()
	await(t, c.ready)
	_ = c.Advance(100 * time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for r.Frame() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r.Stop()
	if r.Frame() != 1 || c.reads.Load() != 0 || c.stamps.Load() != 0 || c.PendingTimers() != 0 {
		t.Fatal(r.Frame(), c.reads.Load(), c.stamps.Load(), c.PendingTimers())
	}
}

func TestObserverDurationIncludesBlockedInputDrain(t *testing.T) {
	c := observerClock()
	c.firstRead = make(chan struct{})
	events := make(chan TickEvent, 1)
	r := New(hub.New("test"), &mockSim{}, Options{Clock: c, TickRate: 10, Observer: tickObserverFunc(func(e TickEvent) { events <- e })})
	r.mu.Lock()
	locked := true
	defer func() {
		if locked {
			r.mu.Unlock()
		}
		r.Stop()
	}()
	r.Start()
	await(t, c.ready)
	_ = c.Advance(100 * time.Millisecond)
	await(t, c.firstRead)
	_ = c.Advance(7 * time.Millisecond)
	r.mu.Unlock()
	locked = false
	e := await(t, events)
	if e.Duration != 7*time.Millisecond || e.Lag != 0 {
		t.Fatal(e)
	}
}

func TestObserverClampsEarlyScheduledLag(t *testing.T) {
	c := observerClock()
	var event TickEvent
	r := New(hub.New("test"), &mockSim{}, Options{Clock: c, Observer: tickObserverFunc(func(e TickEvent) { event = e })})
	r.observeTick(time.Second)
	if event.Lag != 0 || event.Duration != 0 || event.Frame != 1 {
		t.Fatal(event)
	}
}

func TestConcurrentStopJoinsObserver(t *testing.T) {
	c := observerClock()
	entered, release := make(chan struct{}), make(chan struct{})
	r := New(hub.New("test"), &mockSim{}, Options{Clock: c, TickRate: 10, Observer: tickObserverFunc(func(TickEvent) { close(entered); <-release })})
	r.Start()
	await(t, c.ready)
	_ = c.Advance(100 * time.Millisecond)
	await(t, entered)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); r.Stop() }()
	}
	stopped := make(chan struct{})
	go func() { wg.Wait(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("Stop did not join observer")
	case <-time.After(10 * time.Millisecond):
	}
	r.Start() // cannot create a second loop while the observer is finishing
	close(release)
	await(t, stopped)
	if c.PendingTimers() != 0 {
		t.Fatal(c.PendingTimers())
	}
}

func BenchmarkRunnerTick(b *testing.B) {
	for _, observed := range []bool{false, true} {
		name := "nil"
		if observed {
			name = "observer"
		}
		b.Run(name, func(b *testing.B) {
			h := hub.New("bench")
			h.Latch("sim:tick") // exercise the existing encode/enqueue path
			r := New(h, &mockSim{}, Options{DisableReplay: true})
			r.observer = tickObserverFunc(func(TickEvent) {})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if observed {
					r.observeTick(0)
				} else {
					r.tickOnce()
				}
			}
		})
	}
}

func TestTickObserverAddsNoWarmAllocations(t *testing.T) {
	newRunner := func() *Runner {
		h := hub.New("test")
		h.Latch("sim:tick")
		return New(h, &mockSim{}, Options{DisableReplay: true, Observer: tickObserverFunc(func(TickEvent) {})})
	}
	baseline, observed := newRunner(), newRunner()
	nilAllocs := testing.AllocsPerRun(1000, func() { baseline.tickOnce() })
	observedAllocs := testing.AllocsPerRun(1000, func() { observed.observeTick(0) })
	if observedAllocs != nilAllocs {
		t.Fatal("added tick allocations", nilAllocs, observedAllocs)
	}
}
