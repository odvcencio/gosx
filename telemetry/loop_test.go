package telemetry

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/metric"
	"m31labs.dev/gosx/telemetry/schema"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

// The meter itself is portable; native attachment is tested separately.
func portableLoop(tb testing.TB) *Loop {
	tb.Helper()
	r, err := metric.NewRegistry(metric.RegistryOptions{})
	if err != nil {
		tb.Fatal(err)
	}
	k := &LoopKind{opts: LoopOptions{Budget: time.Millisecond}}
	x := &loopSlot{kind: k, epoch: 1}
	for _, item := range []struct {
		name string
		dst  **metric.Counter
	}{{"ticks", &k.metrics.ticks}, {"overruns", &k.metrics.overruns}} {
		v, err := r.NewCounter(metric.CounterOptions{Name: item.name})
		if err != nil {
			tb.Fatal(err)
		}
		*item.dst, _ = v.Bind()
	}
	for _, item := range []struct {
		name string
		dst  **metric.Histogram
	}{{"duration", &k.metrics.duration}, {"lag", &k.metrics.lag}, {"state_bytes", &k.metrics.stateBytes}} {
		v, err := r.NewHistogram(metric.HistogramOptions{Name: item.name, Bounds: loopBounds})
		if err != nil {
			tb.Fatal(err)
		}
		*item.dst, _ = v.Bind()
	}
	c := telemetrytest.NewClock(time.Unix(100, 0))
	k.owner = &Telemetry{opts: Options{Clock: c}, registry: r, loops: &loopState{}}
	return &Loop{kind: k, slot: x, epoch: 1}
}

func TestLoopHealthReference(t *testing.T) {
	l := portableLoop(t)
	if h := l.Health(); h.Available || h.Samples != 0 {
		t.Fatal(h)
	}
	values := []time.Duration{0, time.Nanosecond, 100000, 100001, 51200000, 51200001, time.Second}
	rng := rand.New(rand.NewSource(0))
	for range 1000 {
		values = append(values, time.Duration(rng.Int63n(100000000)))
	}
	var overruns, overflow uint64
	for _, d := range values {
		if err := l.Observe(d, TickInfo{StateBytes: 128, Inputs: 2}); err != nil {
			t.Fatal(err)
		}
		if d > l.kind.opts.Budget {
			overruns++
		}
		if d > 51200000 {
			overflow++
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	reference := func(rank int) float64 {
		d := values[rank-1]
		if d > 51200000 {
			return float64(values[len(values)-1]) / 1e6
		}
		n := int64(d)
		if n == 0 {
			n = 1
		}
		return float64((n+99999)/100000) / 10
	}
	n := len(values)
	h := l.Health()
	if !h.Available || h.Samples != uint64(n) || h.P50MS != reference((n+1)/2) || h.P99MS != reference(n-n/100) || h.MaxMS != 1000 || h.Overruns != overruns || h.OverflowSamples != overflow || h.BudgetMS != 1 {
		t.Fatal(h)
	}
}

func TestLoopExactBinEdgesAndIntegerRanks(t *testing.T) {
	for _, tc := range []struct {
		d   time.Duration
		bin int
	}{{0, 0}, {1, 0}, {100000, 0}, {100001, 1}, {51200000, 511}, {51200001, 512}, {math.MaxInt64, 512}} {
		l := portableLoop(t)
		if err := l.Observe(tc.d, TickInfo{}); err != nil {
			t.Fatal(err)
		}
		if l.slot.data.bins[tc.bin] != 1 {
			t.Fatal(tc, l.slot.data)
		}
	}
	l := portableLoop(t)
	n := uint64(math.MaxUint64)
	l.slot.data = loopData{samples: n, max: time.Second}
	l.slot.data.bins[0] = n / 2
	l.slot.data.bins[1] = n/2 - n/100
	l.slot.data.bins[512] = n - l.slot.data.bins[0] - l.slot.data.bins[1]
	h := l.Health()
	if h.P50MS != .2 || h.P99MS != 1000 {
		t.Fatal(h)
	}
	before := l.slot.data
	if err := l.Observe(1, TickInfo{}); !errors.Is(err, ErrCapacity) || l.slot.data != before {
		t.Fatal(err)
	}
}

func TestLoopRejectsWholeObservation(t *testing.T) {
	l := portableLoop(t)
	for _, tc := range []struct {
		d    time.Duration
		info TickInfo
	}{{-1, TickInfo{}}, {1, TickInfo{StateBytes: -1}}, {1, TickInfo{Inputs: -1}}, {1, TickInfo{Lag: -1}}} {
		if err := l.Observe(tc.d, tc.info); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	if l.Health().Samples != 0 {
		t.Fatal(l.Health())
	}
	for _, l := range []*Loop{nil, {}} {
		if err := l.Observe(1, TickInfo{}); err != nil {
			t.Fatal(err)
		}
		if err := l.End(l.Begin(), TickInfo{}); err != nil {
			t.Fatal(err)
		}
		if l.Health().Available {
			t.Fatal("inert health")
		}
		l.Close()
	}
}

func TestLoopTokenScopeOnceAndWallJump(t *testing.T) {
	l, other := portableLoop(t), portableLoop(t)
	c := l.kind.owner.opts.Clock.(*telemetrytest.FakeClock)
	tok := l.Begin()
	if err := other.End(tok, TickInfo{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := l.End(tok, TickInfo{Inputs: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	c.JumpWall(-24 * time.Hour)
	if err := c.Advance(150 * time.Microsecond); err != nil {
		t.Fatal(err)
	}
	if err := l.End(tok, TickInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := l.End(tok, TickInfo{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if h := l.Health(); h.Samples != 1 || h.P50MS != .2 || h.MaxMS != .15 {
		t.Fatal(h)
	}
	stale := l.Begin()
	latest := l.Begin()
	if err := l.End(stale, TickInfo{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := l.End(latest, TickInfo{}); err != nil {
		t.Fatal(err)
	}
}

func TestLoopHealthConcurrentReaders(t *testing.T) {
	l := portableLoop(t)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 10000 {
				h := l.Health()
				if h.Overruns > h.Samples || h.OverflowSamples > h.Samples || (h.Available && h.P50MS > h.P99MS) {
					t.Error(h)
					return
				}
			}
		})
	}
	for range 10000 {
		if err := l.Observe(2*time.Millisecond, TickInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if l.Health().Samples != 10000 {
		t.Fatal(l.Health())
	}
}

func TestLoopObserveWarmAllocationBudget(t *testing.T) {
	l := portableLoop(t)
	if n := testing.AllocsPerRun(1000, func() {
		if err := l.Observe(time.Millisecond, TickInfo{StateBytes: 128, Inputs: 4}); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal(n)
	}
}

type loopClockProbe struct {
	Clock
	calls int
	fail  bool
	now   func() Instant
}

func (c *loopClockProbe) Now() Instant {
	c.calls++
	if c.fail {
		panic("private-clock-canary")
	}
	if c.now != nil {
		return c.now()
	}
	return c.Clock.Now()
}

func TestLoopEndClockFailuresConsumeToken(t *testing.T) {
	for _, failure := range []string{"panic", "backward"} {
		t.Run(failure, func(t *testing.T) {
			l := portableLoop(t)
			clock := l.kind.owner.opts.Clock.(*telemetrytest.FakeClock)
			if err := clock.Advance(time.Millisecond); err != nil {
				t.Fatal(err)
			}
			c := &loopClockProbe{Clock: clock}
			l.kind.owner.opts.Clock = c
			tok := l.Begin()
			c.now = func() Instant {
				if failure == "panic" {
					panic("clock-test-canary")
				}
				now := clock.Now()
				now.Monotonic -= time.Millisecond
				return now
			}
			if err := l.End(tok, TickInfo{}); !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("failed clock: %v", err)
			}
			c.now = nil
			if err := clock.Advance(2 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			if err := l.End(tok, TickInfo{}); !errors.Is(err, ErrConflict) {
				t.Errorf("retry after clock recovery: %v, want ErrConflict", err)
			}
			if h := l.Health(); h.Samples != 0 || h.Overruns != 0 || h.MaxMS != 0 || h.P50MS != 0 || h.P99MS != 0 {
				t.Errorf("failed tick affected health: %+v", h)
			}
			if err := l.kind.owner.registry.WithSnapshot(t.Context(), func(snapshot metric.Snapshot) error {
				for _, family := range snapshot.Families {
					for _, series := range family.Series {
						if series.Counter != 0 || (series.Histogram != nil && series.Histogram.Count != 0) {
							t.Errorf("failed tick affected %s: %+v", family.Name, series)
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoopEndClockFailuresPreserveNewerTokens(t *testing.T) {
	for _, failure := range []string{"panic", "backward"} {
		for _, replacement := range []string{"new_tick", "reused_slot"} {
			t.Run(failure+"/"+replacement, func(t *testing.T) {
				l := portableLoop(t)
				clock := l.kind.owner.opts.Clock.(*telemetrytest.FakeClock)
				if err := clock.Advance(time.Millisecond); err != nil {
					t.Fatal(err)
				}
				c := &loopClockProbe{Clock: clock}
				l.kind.owner.opts.Clock = c
				tok := l.Begin()
				newer := l
				var latest TickToken
				c.now = func() Instant {
					// Replace the token while End's clock callback runs outside the lock.
					c.now = nil
					if replacement == "reused_slot" {
						x := l.slot
						x.mu.Lock()
						x.epoch++
						x.next, x.pending, x.data = 0, 0, loopData{}
						newer = &Loop{kind: l.kind, slot: x, epoch: x.epoch}
						x.mu.Unlock()
					}
					latest = newer.Begin()
					if failure == "panic" {
						panic("clock-test-canary")
					}
					now := clock.Now()
					now.Monotonic -= time.Millisecond
					return now
				}
				if err := l.End(tok, TickInfo{}); !errors.Is(err, ErrInvalidOptions) {
					t.Fatalf("failed clock: %v", err)
				}
				if err := l.End(tok, TickInfo{}); !errors.Is(err, ErrConflict) {
					t.Fatalf("old token: %v, want ErrConflict", err)
				}
				if err := clock.Advance(100 * time.Microsecond); err != nil {
					t.Fatal(err)
				}
				if err := newer.End(latest, TickInfo{}); err != nil {
					t.Fatalf("newer token was consumed: %v", err)
				}
				if h := newer.Health(); h.Samples != 1 || h.MaxMS != .1 || h.Overruns != 0 {
					t.Fatal(h)
				}
			})
		}
	}
}

func TestLoopTokenChecksPrecedeClockCallbacks(t *testing.T) {
	l := portableLoop(t)
	c := &loopClockProbe{Clock: l.kind.owner.opts.Clock}
	l.kind.owner.opts.Clock = c
	tok := l.Begin()
	if err := l.End(tok, TickInfo{}); err != nil {
		t.Fatal(err)
	}
	before := c.calls
	c.fail = true
	if err := l.End(tok, TickInfo{Inputs: -1}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if c.calls != before {
		t.Fatal("spent token read clock")
	}
	fault := l.Begin()
	if err := l.End(fault, TickInfo{}); !errors.Is(err, ErrInvalidOptions) || err.Error() != "telemetry: invalid clock (callback_panic)" {
		t.Fatal(err)
	}
	if err := l.End(fault, TickInfo{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if l.Health().Samples != 1 {
		t.Fatal(l.Health())
	}
}

var _ schema.TickHealth = (*Loop)(nil).Health()
