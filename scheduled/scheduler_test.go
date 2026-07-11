package scheduled

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// discardLogger returns a slog.Logger that throws away all output, so test
// runs stay quiet even when the watchdog logs stalls/timeouts.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitFor polls cond every 2ms until it returns true or the deadline elapses.
// Returns true if the condition was met.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func statusFor(s *Scheduler, name string) (TaskStatus, bool) {
	for _, st := range s.Status() {
		if st.Name == name {
			return st, true
		}
	}
	return TaskStatus{}, false
}

func TestScheduler_IntervalRunsRepeatedly(t *testing.T) {
	t.Parallel()
	var count int32
	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:     "ticker",
		Schedule: Interval(10 * time.Millisecond),
		Fn: func(ctx context.Context, tick TickHandle) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(50 * time.Millisecond)

	// Capture a scheduled due time before checking repeated execution. A
	// TaskStatus is a snapshot: with a 10ms interval, comparing its NextDueAt
	// to a later time.Now() can legitimately fail if the deadline crosses in
	// between. What the scheduler promises is that the recorded deadline
	// advances as the interval runs.
	var firstDue time.Time
	if !waitFor(2*time.Second, func() bool {
		st, ok := statusFor(s, "ticker")
		if !ok || st.NextDueAt.IsZero() {
			return false
		}
		firstDue = st.NextDueAt
		return true
	}) {
		t.Fatal("expected scheduler to record an initial NextDueAt")
	}

	if !waitFor(2*time.Second, func() bool { return atomic.LoadInt32(&count) >= 2 }) {
		t.Fatalf("expected >=2 runs, got %d", atomic.LoadInt32(&count))
	}

	var st TaskStatus
	if !waitFor(2*time.Second, func() bool {
		var ok bool
		st, ok = statusFor(s, "ticker")
		return ok && st.NextDueAt.After(firstDue)
	}) {
		t.Fatalf("NextDueAt did not advance beyond %v; latest status: %+v", firstDue, st)
	}
	if st.LastRunAt.IsZero() {
		t.Errorf("LastRunAt should be set")
	}
	if st.LastSuccessAt.IsZero() {
		t.Errorf("LastSuccessAt should be set")
	}
	if st.CurrentAttempt < 0 {
		t.Errorf("CurrentAttempt should be sane, got %d", st.CurrentAttempt)
	}
}

func TestScheduler_RetriesUpToMaxAttempts(t *testing.T) {
	t.Parallel()
	var count int32
	wantErr := errors.New("boom")
	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:        "flaky",
		Schedule:    Once(),
		MaxAttempts: 3,
		Backoff:     Fixed(5 * time.Millisecond),
		Fn: func(ctx context.Context, tick TickHandle) error {
			atomic.AddInt32(&count, 1)
			return wantErr
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(100 * time.Millisecond)

	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&count) >= 3 }) {
		t.Fatalf("expected 3 invocations, got %d", atomic.LoadInt32(&count))
	}
	// Give a margin to ensure it does NOT retry a 4th time.
	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 3 {
		t.Fatalf("expected exactly 3 invocations, got %d", got)
	}

	st, ok := statusFor(s, "flaky")
	if !ok {
		t.Fatalf("no status for flaky")
	}
	if st.RecentError == "" {
		t.Errorf("RecentError should be recorded, got empty")
	}
}

func TestScheduler_StallTimeout(t *testing.T) {
	t.Parallel()
	var stalledCause atomic.Value // error
	var healthyCancelled int32

	s := New(Options{Logger: discardLogger()})

	// Stalling task: blocks without calling Progress.
	err := s.Register(Task{
		Name:            "staller",
		Schedule:        Once(),
		ProgressTimeout: 20 * time.Millisecond,
		MaxAttempts:     1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			<-ctx.Done()
			stalledCause.Store(context.Cause(ctx))
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Register staller: %v", err)
	}

	// Healthy sibling: calls Progress every 5ms, should NOT be cancelled.
	err = s.Register(Task{
		Name:            "healthy",
		Schedule:        Once(),
		ProgressTimeout: 20 * time.Millisecond,
		MaxAttempts:     1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			deadline := time.After(80 * time.Millisecond)
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					atomic.StoreInt32(&healthyCancelled, 1)
					return ctx.Err()
				case <-ticker.C:
					tick.Progress("alive")
				case <-deadline:
					return nil
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("Register healthy: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(100 * time.Millisecond)

	// The staller should be cancelled within ~40ms.
	if !waitFor(120*time.Millisecond, func() bool { return stalledCause.Load() != nil }) {
		t.Fatalf("staller was not cancelled in time")
	}
	if cause, _ := stalledCause.Load().(error); !errors.Is(cause, ErrStallTimeout) {
		t.Fatalf("staller cause: got %v, want ErrStallTimeout", cause)
	}

	if atomic.LoadInt32(&healthyCancelled) != 0 {
		t.Errorf("healthy task should not have been cancelled")
	}

	// Stall should be recorded in status.
	if !waitFor(60*time.Millisecond, func() bool {
		st, ok := statusFor(s, "staller")
		return ok && st.RecentError != ""
	}) {
		t.Errorf("staller RecentError should be recorded")
	}
}

func TestScheduler_HardTimeout(t *testing.T) {
	t.Parallel()
	var cause atomic.Value
	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:        "slow",
		Schedule:    Once(),
		Timeout:     20 * time.Millisecond,
		MaxAttempts: 1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			<-ctx.Done()
			cause.Store(context.Cause(ctx))
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(100 * time.Millisecond)

	if !waitFor(120*time.Millisecond, func() bool { return cause.Load() != nil }) {
		t.Fatalf("slow task was not cancelled in time")
	}
	if c, _ := cause.Load().(error); !errors.Is(c, ErrTimeout) {
		t.Fatalf("slow cause: got %v, want ErrTimeout", c)
	}
}

func TestScheduler_EnqueueAndCancel(t *testing.T) {
	t.Parallel()
	var ran int32
	var cancelled int32
	s := New(Options{Logger: discardLogger()})

	err := s.Register(Task{
		Name:        "oneshot",
		Schedule:    Interval(time.Hour), // far future; only Enqueue fires it
		MaxAttempts: 1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			atomic.AddInt32(&ran, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register oneshot: %v", err)
	}

	started := make(chan struct{})
	var startedOnce sync.Once
	err = s.Register(Task{
		Name:        "longrun",
		Schedule:    Interval(time.Hour),
		MaxAttempts: 1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			atomic.StoreInt32(&cancelled, 1)
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Register longrun: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(100 * time.Millisecond)

	// Enqueue oneshot — should run once.
	if _, err := s.Enqueue("oneshot", nil); err != nil {
		t.Fatalf("Enqueue oneshot: %v", err)
	}
	if !waitFor(100*time.Millisecond, func() bool { return atomic.LoadInt32(&ran) == 1 }) {
		t.Fatalf("oneshot did not run, ran=%d", atomic.LoadInt32(&ran))
	}

	// Enqueue unknown task — should error.
	if _, err := s.Enqueue("nope", nil); err == nil {
		t.Errorf("Enqueue unknown should error")
	}

	// Enqueue longrun, then cancel it mid-run.
	id, err := s.Enqueue("longrun", nil)
	if err != nil {
		t.Fatalf("Enqueue longrun: %v", err)
	}
	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("longrun did not start")
	}
	if err := s.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !waitFor(100*time.Millisecond, func() bool { return atomic.LoadInt32(&cancelled) == 1 }) {
		t.Fatalf("longrun was not cancelled")
	}

	// Cancel unknown run — should error.
	if err := s.Cancel(RunID("does-not-exist")); err == nil {
		t.Errorf("Cancel unknown should error")
	}
}

func TestScheduler_OnceEnqueueStatusNoRace(t *testing.T) {
	t.Parallel()
	var ran int32
	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:        "once-race",
		Schedule:    Once(),
		MaxAttempts: 1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			atomic.AddInt32(&ran, 1)
			tick.Progress("x")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer s.Stop(100 * time.Millisecond)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine A: Enqueue the same once-scheduled task a few times. Each
	// Enqueue run touches the schedule via updateStatus, racing the loop's
	// NextDue on the shared *onceSchedule.fired field.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if _, err := s.Enqueue("once-race", nil); err != nil {
				t.Errorf("Enqueue: %v", err)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine B: hammer Status() in a tight loop for ~50ms.
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(50 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = s.Status()
		}
		close(stop)
	}()

	<-stop
	wg.Wait()

	// Semantic guard: the one-shot must actually have run at least once.
	// (A status computation must not be able to flip fired=true and skip the run.)
	if !waitFor(200*time.Millisecond, func() bool { return atomic.LoadInt32(&ran) >= 1 }) {
		t.Fatalf("once task never ran (semantic bug: status mutated the schedule), ran=%d", atomic.LoadInt32(&ran))
	}
}

func TestScheduler_StopDrains(t *testing.T) {
	t.Parallel()
	before := runtime.NumGoroutine()

	var cancelled int32
	started := make(chan struct{})
	var startedOnce sync.Once

	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:        "blocker",
		Schedule:    Once(),
		MaxAttempts: 1,
		Fn: func(ctx context.Context, tick TickHandle) error {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done() // only stops when cancelled after grace
			atomic.StoreInt32(&cancelled, 1)
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("blocker did not start")
	}

	done := make(chan struct{})
	stopStart := time.Now()
	go func() {
		s.Stop(50 * time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Stop did not return")
	}
	elapsed := time.Since(stopStart)
	// Stop should have waited at least the grace period (the task never
	// returns until cancelled), then cancelled it.
	if elapsed < 40*time.Millisecond {
		t.Errorf("Stop returned too quickly (%v); expected to wait out grace", elapsed)
	}
	if atomic.LoadInt32(&cancelled) != 1 {
		t.Errorf("blocker should have been cancelled after grace")
	}

	// No leaked goroutines: allow scheduler goroutines to unwind.
	if !waitFor(500*time.Millisecond, func() bool {
		return runtime.NumGoroutine() <= before+2
	}) {
		t.Errorf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
	}
}

func TestScheduler_RegisterErrors(t *testing.T) {
	t.Parallel()
	s := New(Options{Logger: discardLogger()})
	noop := func(ctx context.Context, tick TickHandle) error { return nil }

	if err := s.Register(Task{Name: "", Schedule: Once(), Fn: noop}); err == nil {
		t.Errorf("empty name should error")
	}
	if err := s.Register(Task{Name: "x", Schedule: Once(), Fn: nil}); err == nil {
		t.Errorf("nil Fn should error")
	}
	if err := s.Register(Task{Name: "dup", Schedule: Once(), Fn: noop}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := s.Register(Task{Name: "dup", Schedule: Once(), Fn: noop}); err == nil {
		t.Errorf("duplicate name should error")
	}
}

func TestScheduler_StartIdempotent(t *testing.T) {
	t.Parallel()
	var count int32
	s := New(Options{Logger: discardLogger()})
	err := s.Register(Task{
		Name:     "t",
		Schedule: Interval(10 * time.Millisecond),
		Fn: func(ctx context.Context, tick TickHandle) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Start(ctx) // second Start must not double-launch loops
	s.Start(ctx)
	defer s.Stop(50 * time.Millisecond)

	// Over ~45ms at 10ms interval, a single loop yields ~4 runs. If Start
	// launched 3 loops we'd see ~12. Assert it stays in the single-loop range.
	time.Sleep(55 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got > 8 {
		t.Errorf("Start not idempotent: got %d runs (expected single-loop cadence)", got)
	}
}
