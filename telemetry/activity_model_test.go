//go:build !js || !wasm

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/telemetry/metric"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

// Each read captures its coordinates before waiting. Tests can finish a newer
// operation first without sleeps, scheduler assumptions, or production hooks.
type activityClockRead struct {
	now    Instant
	resume chan bool
}

type activityModelClock struct {
	Clock
	reads chan *activityClockRead
	abort chan struct{}
	once  sync.Once
}

func newActivityModelClock(clock Clock) *activityModelClock {
	return &activityModelClock{Clock: clock, reads: make(chan *activityClockRead, 4), abort: make(chan struct{})}
}

func (c *activityModelClock) Now() Instant {
	r := &activityClockRead{now: c.Clock.Now(), resume: make(chan bool, 1)}
	select {
	case c.reads <- r:
	case <-c.abort:
		return r.now
	}
	select {
	case fail := <-r.resume:
		if fail {
			panic("test clock failure")
		}
	case <-c.abort:
	}
	return r.now
}

func (c *activityModelClock) unblock() { c.once.Do(func() { close(c.abort) }) }

func TestActivitySetPreservesConcurrentTouch(t *testing.T) {
	tel, clock := lifecycleOwner(t)
	tel.opts.Activities.IdleTimeout = time.Minute
	k := emptyActivityKind(t, tel)
	a, err := k.begin(ActivityStart[NoFields]{})
	if err != nil {
		t.Fatal(err)
	}
	c := newActivityModelClock(clock)
	tel.opts.Clock = c
	defer c.unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	read := func() *activityClockRead {
		t.Helper()
		select {
		case r := <-c.reads:
			return r
		case <-ctx.Done():
			t.Fatal("operation did not read the clock")
			return nil
		}
	}
	set := make(chan error, 1)
	go func() { set <- a.Set(NoFields{}) }()
	older := read()
	if err := clock.Advance(50 * time.Second); err != nil {
		t.Fatal(err)
	}
	touched := make(chan struct{})
	go func() { a.Touch(); close(touched) }()
	newer := read()
	newer.resume <- false
	select {
	case <-touched:
	case <-ctx.Done():
		t.Fatal("Touch did not finish")
	}
	older.resume <- false
	select {
	case err := <-set:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Set did not finish")
	}
	a.entity.mu.Lock()
	heartbeat := a.entity.lastTouch
	a.entity.mu.Unlock()
	if heartbeat != newer.now.Monotonic {
		t.Errorf("Set erased newer Touch: heartbeat=%v want=%v", heartbeat, newer.now.Monotonic)
	}
	if err := clock.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	tel.maintainActivities(clock.Now())
	a.entity.mu.Lock()
	final := a.entity.final
	a.entity.mu.Unlock()
	if final {
		t.Fatal("maintenance interrupted an activity touched inside the idle window")
	}
}

type activityModelReceipt struct {
	receipt Receipt
	done    chan struct{}
	ready   bool
}

// The reference owns one activity at a time. A final remains reserved until
// collection, while shutdown may discard an unfinished projection without
// inventing a final. Revision and heartbeat are independent coordinates.
type activityReference struct {
	retained, final, closed, dirty bool
	revision                       uint64
	touch, maintenance             time.Duration
	reason                         string
	receipt                        Receipt
	freshReceipt, receiptReady     bool
	receipts                       map[*receiptState]*activityModelReceipt
	queued, committed              uint64
}

func (m *activityReference) finish(reason string) {
	m.final, m.dirty, m.reason = true, false, reason
	m.revision++
	m.queued++
	m.freshReceipt, m.receiptReady = true, false
}

func (m *activityReference) collect() {
	if m.retained && m.final {
		m.retained = false
		m.committed++
		m.receiptReady = true
		if r := m.receipts[m.receipt.state]; r != nil {
			r.ready = true
		}
	}
}

type activityModelResult struct {
	activity *Activity[NoFields, NoFields, NoFields]
	receipt  Receipt
	err      error
}

type activityModelCommand struct {
	name     string
	worker   int
	activity *Activity[NoFields, NoFields, NoFields]
	revision uint64
	now      Instant
	read     *activityClockRead
	done     chan activityModelResult
	result   *activityModelResult
}

func (m *activityReference) apply(c *activityModelCommand) error {
	switch c.name {
	case "begin":
		if m.closed {
			return ErrClosed
		}
		if m.retained {
			return ErrCapacity
		}
		m.retained, m.final, m.dirty = true, false, true
		m.revision, m.touch, m.reason, m.receipt = 1, c.now.Monotonic, "", Receipt{}
	case "set":
		if m.closed || m.final {
			return ErrClosed
		}
		if c.revision != m.revision {
			return ErrConflict
		}
		m.revision++
		m.dirty = true
		m.touch = max(m.touch, c.now.Monotonic)
	case "touch":
		if !m.closed && !m.final {
			m.touch = max(m.touch, c.now.Monotonic)
		}
	case "checkpoint":
		if m.final || !m.dirty {
			break
		}
		if m.closed {
			return ErrClosed
		}
		m.revision++
		m.dirty = false
		m.freshReceipt, m.receiptReady = true, true
	case "end":
		if m.final {
			if m.reason != "complete" {
				return ErrConflict
			}
			break // Identical End shares the one accepted final receipt.
		}
		if m.closed {
			return ErrClosed
		}
		if c.revision != m.revision {
			return ErrConflict
		}
		m.finish("complete")
	case "maintenance":
		if m.closed || c.now.Monotonic-m.maintenance < time.Second {
			break
		}
		m.maintenance = c.now.Monotonic
		if m.retained && !m.final {
			if c.now.Monotonic-m.touch >= time.Minute {
				m.finish("idle")
			} else if m.dirty {
				m.revision++
				m.dirty = false
				m.freshReceipt, m.receiptReady = true, true
			}
		}
		m.collect()
	case "collect":
		m.collect()
	default:
		panic("unknown model operation")
	}
	return nil
}

type activityModelRunner struct {
	t        *testing.T
	tel      *Telemetry
	kind     *ActivityKind[NoFields, NoFields, NoFields]
	loop     *Loop
	clock    *telemetrytest.FakeClock
	gated    *activityModelClock
	model    activityReference
	activity *Activity[NoFields, NoFields, NoFields]
	handles  []*Activity[NoFields, NoFields, NoFields]
	commands [3]chan *activityModelCommand
	workers  sync.WaitGroup
	ctx      context.Context
	bytes    int64
	memory   float64
	misc     int64
	usage    metric.Usage
	count    int
	staleSet int
}

func newActivityModelRunner(t *testing.T) *activityModelRunner {
	tel, clock := lifecycleOwner(t)
	tel.opts.Activities.MaxOpen = 1
	tel.opts.Activities.IdleTimeout = time.Minute
	tel.opts.Activities.CheckpointInterval = time.Second
	k := emptyActivityKind(t, tel)
	h := &activityModelRunner{t: t, tel: tel, kind: k, clock: clock, gated: newActivityModelClock(clock), bytes: tel.activities.bytes.Load()}
	loopKind, err := tel.NewLoopKind("simulation", LoopOptions{Budget: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	h.loop, err = loopKind.Instance()
	if err != nil {
		t.Fatal(err)
	}
	tel.updateCoreUsage()
	h.memory = hubSample(t, tel, "gosx_telemetry_memory_bytes").Gauge
	h.misc, h.usage = tel.miscBytes.Load(), tel.registry.Usage()
	h.model.receipts = make(map[*receiptState]*activityModelReceipt)
	tel.opts.Clock = h.gated
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	h.ctx = ctx
	for i := range h.commands {
		h.commands[i] = make(chan *activityModelCommand)
		h.workers.Add(1)
		go func(commands <-chan *activityModelCommand) {
			defer h.workers.Done()
			for c := range commands {
				var r activityModelResult
				switch c.name {
				case "begin":
					r.activity, r.err = k.begin(ActivityStart[NoFields]{Loop: h.loop})
				case "set":
					r.err = c.activity.Set(NoFields{})
				case "touch":
					c.activity.Touch()
				case "checkpoint":
					r.receipt, r.err = c.activity.Checkpoint()
				case "end":
					r.receipt, r.err = c.activity.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
				case "maintenance":
					tel.maintainActivities(c.now)
				case "collect":
					tel.collectActivityReceipts()
				}
				c.done <- r
			}
		}(h.commands[i])
	}
	t.Cleanup(func() {
		h.gated.unblock()
		for _, commands := range h.commands {
			close(commands)
		}
		h.workers.Wait()
		if tel.done != nil {
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = tel.Close(closeCtx)
		}
		cancel()
	})
	return h
}

func (h *activityModelRunner) start(name string, worker int) *activityModelCommand {
	h.t.Helper()
	c := &activityModelCommand{name: name, worker: worker, activity: h.activity, revision: h.model.revision, now: h.clock.Now(), done: make(chan activityModelResult, 1)}
	h.commands[worker] <- c
	select {
	case c.read = <-h.gated.reads:
		c.now = c.read.now
	case r := <-c.done:
		c.result = &r
	case <-h.ctx.Done():
		h.t.Fatal("operation did not reach a clock gate or complete", name)
	}
	return c
}

func (h *activityModelRunner) finish(c *activityModelCommand) {
	h.t.Helper()
	if c.result == nil {
		c.read.resume <- false
		select {
		case r := <-c.done:
			c.result = &r
		case <-h.ctx.Done():
			h.t.Fatal("operation did not complete", c.name)
		}
	}
	if c.name == "set" && c.result.err == nil && c.now.Monotonic < h.model.touch {
		h.staleSet++
	}
	want := h.model.apply(c)
	if !errors.Is(c.result.err, want) {
		h.t.Fatalf("operation %d %s: error=%v want=%v", h.count, c.name, c.result.err, want)
	}
	if c.name == "begin" && want == nil {
		h.activity = c.result.activity
		h.handles = append(h.handles, h.activity)
	}
	h.count++
	h.check(c.name)
	if (c.name == "end" || c.name == "checkpoint") && want == nil && c.result.receipt.state != h.model.receipt.state {
		h.t.Fatalf("%s returned a different receipt from the admitted revision", c.name)
	}
}

func (h *activityModelRunner) check(operation string) {
	h.t.Helper()
	m, s := &h.model, h.tel.activities
	if h.activity != nil {
		e := h.activity.entity
		e.mu.Lock()
		heartbeat, revision, final, dirty, receipt := e.lastTouch, e.record.Envelope().Revision, e.final, e.dirty, e.receipt
		v, _ := e.record.Activity()
		e.mu.Unlock()
		if heartbeat != m.touch || revision != m.revision || final != m.final || dirty != m.dirty || v.Reason != m.reason {
			h.t.Fatalf("operation %d %s: heartbeat=%v revision=%d final=%v dirty=%v reason=%q; model=%+v", h.count, operation, heartbeat, revision, final, dirty, v.Reason, *m)
		}
		if m.freshReceipt {
			if receipt.state == nil || m.receipts[receipt.state] != nil {
				h.t.Fatal("new admitted revision reused a receipt")
			}
			m.receipt = receipt
			m.receipts[receipt.state] = &activityModelReceipt{receipt: receipt, done: receipt.state.done, ready: m.receiptReady}
			m.freshReceipt = false
		}
		if receipt.state != m.receipt.state {
			h.t.Fatal("receipt identity changed without an admitted revision")
		}
	}
	reserved, open := 0, int64(0)
	if m.retained {
		reserved = 1
		if !m.final {
			open = 1
		}
	}
	s.mu.Lock()
	live, attached, actualOpen := len(s.live), len(s.attached), h.kind.core.open
	s.mu.Unlock()
	if live != reserved || attached != reserved || actualOpen != open || s.bytes.Load() != h.bytes+int64(reserved)*activitySlotBytes {
		h.t.Fatalf("%s: live=%d attached=%d open=%d bytes=%d; want=%d, %d, %d, %d", operation, live, attached, actualOpen, s.bytes.Load(), reserved, reserved, open, h.bytes+int64(reserved)*activitySlotBytes)
	}
	for _, r := range m.receipts {
		r.receipt.state.mu.Lock()
		completed, err, done := r.receipt.state.completed, r.receipt.state.err, r.receipt.state.done
		r.receipt.state.mu.Unlock()
		if completed != r.ready || r.receipt.ready() != r.ready || err != nil || done != r.done {
			h.t.Fatal("accepted receipt lost or changed its single completion", completed, r.ready, err)
		}
	}
	if got := hubSample(h.t, h.tel, "gosx_activity_records_total", "kind", "match", "state", "final", "result", "queued").Counter; got != m.queued {
		h.t.Fatal("final accepted more than once", got, m.queued)
	}
	if got := hubSample(h.t, h.tel, "gosx_activity_records_total", "kind", "match", "state", "final", "result", "committed").Counter; got != m.committed {
		h.t.Fatal("final acknowledged more than once or left pending", got, m.committed)
	}
	if got := hubSample(h.t, h.tel, "gosx_activities_open", "kind", "match").Gauge; got != float64(open) {
		h.t.Fatal("open gauge diverged from the model", got, open)
	}
	if h.tel.miscBytes.Load() != h.misc || h.tel.registry.Usage() != h.usage {
		h.t.Fatal("operation changed fixed memory or metric reservations")
	}
}

func (h *activityModelRunner) shutdown(mode int, acceptedFinal bool) {
	h.t.Helper()
	// Alternate an uncollected final and an unfinished activity at worker exit.
	h.finish(h.start("collect", 0))
	if h.model.final {
		h.finish(h.start("begin", 0))
	}
	if acceptedFinal && mode != 1 {
		h.finish(h.start("end", 0))
	}
	pending := []*activityModelCommand{h.start("set", 0), h.start("touch", 1)}
	h.tel.start = h.clock.Now()
	h.tel.ticker = h.clock.NewTicker(time.Second)
	h.tel.ticks, h.tel.done = h.tel.ticker.C(), make(chan struct{})
	go h.tel.run()
	var wantError error
	switch mode {
	case 0:
		h.tel.signal(context.Background())
	case 1:
		if err := h.clock.Advance(time.Second); err != nil {
			h.t.Fatal(err)
		}
		wantError = ErrInvalidOptions
	case 2:
		expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if err := h.tel.Close(expired); !errors.Is(err, context.DeadlineExceeded) {
			h.t.Fatal("expired caller did not retain its deadline", err)
		}
		wantError = context.DeadlineExceeded
	}
	var workerRead *activityClockRead
	select {
	case workerRead = <-h.gated.reads:
	case <-h.ctx.Done():
		h.t.Fatal("worker did not reach the clock gate")
	}
	if mode == 1 && acceptedFinal {
		// The accepted final cannot take the wake path: the worker is already
		// inside the clock callback that will fail. Cleanup must acknowledge it.
		h.finish(h.start("end", 2))
	}
	if mode != 1 {
		// The wake path collects accepted finals and stops admission before Now.
		h.model.closed = true
		h.model.collect()
		for _, c := range pending {
			h.finish(c)
		}
	}
	workerRead.resume <- mode == 1
	select {
	case <-h.tel.done:
	case <-h.ctx.Done():
		h.t.Fatal("worker did not finish")
	}
	h.model.closed = true
	if mode == 0 && !h.model.final {
		h.model.finish("server_shutdown")
	}
	h.model.collect()
	h.model.retained = false // Clock/deadline exits discard unfinished state.
	h.count++
	h.check("worker exit")
	if mode == 1 {
		for _, c := range pending {
			h.finish(c)
		}
	}
	if err := h.tel.Close(context.Background()); !errors.Is(err, wantError) {
		h.t.Fatal("worker completion changed the original close error", err, wantError)
	}
	if h.tel.activities.transactions.Load() != 0 || h.tel.activities.pool.mask.Load() != 0 || h.clock.PendingTimers() != 0 || len(h.tel.wake) != 0 || h.tel.Enabled() || !h.tel.activities.stopping.Load() {
		h.t.Fatal("worker exit retained operation leases, timers, notifications, or admission")
	}
	if got := hubSample(h.t, h.tel, "gosx_telemetry_memory_bytes").Gauge; got != h.memory {
		h.t.Fatal("worker exit did not restore accounted memory", got, h.memory)
	}
	for _, a := range h.handles {
		a.entity.mu.Lock()
		before, heartbeat := a.entity.record.Envelope(), a.entity.lastTouch
		final, view, receipt := a.entity.final, a.entity.record, a.entity.receipt
		a.entity.mu.Unlock()
		unchanged := func(operation string) {
			h.t.Helper()
			a.entity.mu.Lock()
			changed := a.entity.record.Envelope() != before || a.entity.lastTouch != heartbeat
			a.entity.mu.Unlock()
			if changed {
				h.t.Fatal("ended or closed handle accepted an update", operation)
			}
			h.count++
			h.check(operation)
		}
		if err := a.Set(NoFields{}); !errors.Is(err, ErrClosed) {
			h.t.Fatal("ended or closed handle accepted Set", err)
		}
		unchanged("closed Set")
		// Admission is already stopped. Unblock only the test clock so rejected
		// Touch and Checkpoint calls can still inspect their frozen receipts.
		h.gated.unblock()
		a.Touch()
		unchanged("closed Touch")
		_, _ = a.Checkpoint()
		unchanged("closed Checkpoint")
		ended, err := a.End(ActivityEnd[NoFields]{Outcome: "won", Reason: "complete"})
		v, _ := view.Activity()
		if final && v.Reason == "complete" {
			if err != nil || ended.state != receipt.state {
				h.t.Fatal("repeated End changed the accepted final", err)
			}
		} else if final {
			if !errors.Is(err, ErrConflict) {
				h.t.Fatal("End changed an interrupted final", err)
			}
		} else if !errors.Is(err, ErrClosed) {
			h.t.Fatal("closed unfinished handle accepted End", err)
		}
		unchanged("closed End")
	}
	for _, r := range h.model.receipts {
		if !r.ready {
			h.t.Fatal("accepted receipt still pending after worker exit")
		}
		for range 2 {
			if err := r.receipt.Wait(h.ctx); err != nil {
				h.t.Fatal("accepted receipt lost its completion", err)
			}
		}
	}
}

func TestActivityLifecycleModel(t *testing.T) {
	const seed = int64(0x5632026)
	random := rand.New(rand.NewSource(seed))
	total := 0
	staleSets := 0
	for round := 0; round < 24; round++ {
		t.Run(fmt.Sprintf("%02d", round), func(t *testing.T) {
			h := newActivityModelRunner(t)
			h.finish(h.start("begin", 0))
			var pending []*activityModelCommand
			for h.count < 192 {
				if err := h.clock.Advance(time.Duration(random.Intn(10)) * time.Second); err != nil {
					t.Fatal(err)
				}
				h.check("advance")
				if len(pending) != 0 && (len(pending) == 3 || h.count+len(pending) == 192 || random.Intn(100) < 40) {
					i := random.Intn(len(pending))
					h.finish(pending[i])
					pending = append(pending[:i], pending[i+1:]...)
					continue
				}
				worker := random.Intn(3)
				for busy := true; busy; {
					busy = false
					for _, c := range pending {
						if c.worker == worker {
							worker = (worker + 1) % 3
							busy = true
							break
						}
					}
				}
				name := []string{"set", "touch", "checkpoint", "end", "maintenance", "collect"}[random.Intn(6)]
				if !h.model.retained && len(pending) == 0 && random.Intn(3) == 0 {
					name = "begin"
				}
				c := h.start(name, worker)
				// Change the modeled identity only between generations; overlaps
				// within a generation remain gated and finish in random order.
				if c.result != nil || name == "begin" {
					h.finish(c)
				} else {
					pending = append(pending, c)
				}
			}
			if len(pending) != 0 {
				t.Fatal("operation budget left unfinished commands")
			}
			h.shutdown(round%3, round%2 == 0)
			total += h.count
			staleSets += h.staleSet
		})
	}
	if staleSets == 0 {
		t.Fatal("seed did not exercise Set committing behind a newer heartbeat")
	}
	t.Logf("seed=%#x operations=%d workers=3 rounds=24 stale_set_commits=%d", seed, total, staleSets)
}
