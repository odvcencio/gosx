package sim

import (
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

// stepClock is a manual clock; sims advance it to model tick cost.
type stepClock struct {
	mu  sync.Mutex
	now time.Duration
}

func (c *stepClock) advance(d time.Duration) { c.mu.Lock(); c.now += d; c.mu.Unlock() }
func (c *stepClock) set(d time.Duration)     { c.mu.Lock(); c.now = d; c.mu.Unlock() }
func (c *stepClock) Now() Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Instant{Monotonic: c.now}
}
func (c *stepClock) NewTicker(time.Duration) Ticker { panic("not used") }

type slowSim struct {
	sumSim
	clock *stepClock
	cost  time.Duration
	ticks int
}

func (s *slowSim) Tick(in map[string]Input) {
	s.ticks++
	s.clock.advance(s.cost)
	s.sumSim.Tick(in)
}

func TestSetSpeedSchedulesWholeTicksPerWake(t *testing.T) {
	s := &slowSim{clock: &stepClock{}}
	r := New(hub.New("speed"), s, Options{TickRate: 100, Clock: s.clock})
	r.SetSpeed(2.5)
	var got []int
	for i := 0; i < 4; i++ {
		before := s.ticks
		r.wake(0)
		got = append(got, s.ticks-before)
	}
	if want := []int{2, 3, 2, 3}; !equalInts(got, want) {
		t.Fatalf("ticks per wake %v want %v", got, want)
	}
	r.SetSpeed(-1) // ignored
	if r.Speed() != 2.5 {
		t.Fatal("negative speed accepted")
	}
}

func TestPauseSkipsTicksAndResumeDoesNotCatchUp(t *testing.T) {
	s := &slowSim{clock: &stepClock{}}
	r := New(hub.New("pause"), s, Options{TickRate: 100, Clock: s.clock, CatchUpBudget: time.Hour})
	r.wake(0)
	r.Pause()
	for i := 1; i <= 50; i++ {
		r.wake(time.Duration(i) * 10 * time.Millisecond)
	}
	if s.ticks != 1 || !r.Paused() {
		t.Fatalf("ticks=%d while paused", s.ticks)
	}
	r.Resume()
	r.wake(510 * time.Millisecond)
	if s.ticks != 2 {
		t.Fatalf("resume replayed paused time: ticks=%d", s.ticks)
	}
}

func TestCatchUpRunsMissedPeriodsWithinBudget(t *testing.T) {
	s := &slowSim{clock: &stepClock{}, cost: 5 * time.Millisecond}
	var events []TickEvent
	r := New(hub.New("catchup"), s, Options{
		TickRate: 100, Clock: s.clock, CatchUpBudget: 12 * time.Millisecond,
		Observer: tickObserverFunc(func(e TickEvent) { events = append(events, e) }),
	})
	r.wake(0)
	// Next wake arrives 6 periods late: 6 ticks are due, the budget allows 3.
	s.clock.set(60 * time.Millisecond)
	r.wake(60 * time.Millisecond)
	if s.ticks != 1+3 {
		t.Fatalf("ticks=%d", s.ticks)
	}
	last := events[len(events)-1]
	if last.Behind != 3 || last.Lag <= 0 {
		t.Fatalf("event %+v", last)
	}
	// Next on-time wake pays down the debt.
	s.clock.set(70 * time.Millisecond)
	r.wake(70 * time.Millisecond)
	if s.ticks != 4+3 || events[len(events)-1].Behind != 1 {
		t.Fatalf("ticks=%d events=%+v", s.ticks, events[len(events)-1])
	}
}

func TestDefaultsDoNotCatchUp(t *testing.T) {
	s := &slowSim{clock: &stepClock{}}
	r := New(hub.New("nocatch"), s, Options{TickRate: 100, Clock: s.clock})
	r.wake(0)
	r.wake(time.Second)
	if s.ticks != 2 {
		t.Fatalf("ticks=%d", s.ticks)
	}
}

func TestBroadcastEveryLimitsBroadcastsAndStateCalls(t *testing.T) {
	s := &slowSim{clock: &stepClock{}, cost: 30 * time.Millisecond}
	var frames []uint64
	r := New(hub.New("bevery"), s, Options{
		TickRate: 100, Clock: s.clock, BroadcastEvery: 100 * time.Millisecond,
		Broadcaster: BroadcasterFunc(func(_ *hub.Hub, f uint64, _ []byte) { frames = append(frames, f) }),
	})
	for i := 0; i < 10; i++ {
		r.tickOnce()
	}
	// Clock reads 30,60,...,300 ms after each Tick; first broadcast at once,
	// then every >=100 ms: frames 1, 5 (ticks 4 and 5 are 120 ms later), 9.
	if want := []uint64{1, 5, 9}; len(frames) != len(want) || frames[0] != 1 || frames[1] != 5 || frames[2] != 9 {
		t.Fatalf("frames %v want %v", frames, want)
	}
}

func TestInputQueueKeepsBurstAndHonorsFrameStamps(t *testing.T) {
	r := New(hub.New("queue"), &sumSim{}, Options{InputQueue: true, InputQueueDepth: 3})
	for _, b := range "abcd" {
		r.ReceiveInput("p1", Input{Data: []byte{byte(b)}})
	}
	var got string
	for i := 0; i < 4; i++ {
		for _, in := range r.DrainInputs() {
			got += string(in.Data)
		}
		r.frame.Add(1)
	}
	if got != "bcd" { // depth 3 dropped the oldest
		t.Fatalf("got %q", got)
	}

	r = New(hub.New("stamp"), &sumSim{}, Options{InputQueue: true})
	r.ReceiveInputAt("p1", 3, Input{Data: []byte("x")})
	var seen []int
	for i := 0; i < 4; i++ {
		seen = append(seen, len(r.DrainInputs()))
		r.frame.Add(1)
	}
	if !equalInts(seen, []int{0, 0, 1, 0}) { // delivered when next frame == 3
		t.Fatalf("seen %v", seen)
	}
	// Late stamp delivers on the next drain.
	r.ReceiveInputAt("p2", 1, Input{Data: []byte("late")})
	if len(r.DrainInputs()) != 1 {
		t.Fatal("late input dropped")
	}
}

func TestDefaultInputStillOverwrites(t *testing.T) {
	r := New(hub.New("overwrite"), &sumSim{}, Options{})
	r.ReceiveInput("p1", Input{Data: []byte("a")})
	r.ReceiveInput("p1", Input{Data: []byte("b")})
	if got := r.DrainInputs(); string(got["p1"].Data) != "b" || len(got) != 1 {
		t.Fatalf("%v", got)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRunningRunnerPauseResumeAndSpeed(t *testing.T) {
	r := New(hub.New("live"), &sumSim{}, Options{TickRate: 200})
	r.Start()
	defer r.Stop()
	r.Pause()
	time.Sleep(30 * time.Millisecond) // let an in-flight tick finish
	f := r.Frame()
	time.Sleep(60 * time.Millisecond)
	if r.Frame() != f {
		t.Fatal("frame advanced while paused")
	}
	r.SetSpeed(4)
	r.Resume()
	time.Sleep(100 * time.Millisecond)
	// 200 Hz x 4 = 800 ticks/s nominal; 100 ms should give well over the
	// 20 ticks speed 1 allows.
	if got := r.Frame() - f; got < 40 {
		t.Fatalf("only %d ticks at 4x", got)
	}
}
