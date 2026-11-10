package scheduled

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockedBuffer is a bytes.Buffer safe for the scheduler's goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type panicRecorder struct {
	mu     sync.Mutex
	panics []Panic
}

func (r *panicRecorder) record(p Panic) {
	r.mu.Lock()
	r.panics = append(r.panics, p)
	r.mu.Unlock()
}

func (r *panicRecorder) list() []Panic {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Panic(nil), r.panics...)
}

// A task that panics on its first attempt has that attempt recorded as
// failed, is retried by its policy, and then succeeds. The panic reaches the
// logger and OnPanic. Before the fix the panic ended the process.
func TestScheduler_PanicFailsAttemptThenRetrySucceeds(t *testing.T) {
	t.Parallel()
	var logs lockedBuffer
	var rec panicRecorder
	s := New(Options{Logger: slog.New(slog.NewTextHandler(&logs, nil)), OnPanic: rec.record})
	var calls atomic.Int32
	if err := s.Register(Task{
		Name: "flaky", MaxAttempts: 3, Backoff: Immediate(),
		Fn: func(context.Context, TickHandle) error {
			if calls.Add(1) == 1 {
				panic("first attempt")
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	id, err := s.Enqueue("flaky", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !waitFor(2*time.Second, func() bool {
		st, _ := statusFor(s, "flaky")
		return !st.LastSuccessAt.IsZero()
	}) {
		t.Fatal("task did not succeed after the panicking attempt")
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2 (one panic, one success)", calls.Load())
	}
	ps := rec.list()
	if len(ps) != 1 || ps[0].Task != "flaky" || ps[0].Run != id || ps[0].Attempt != 1 || ps[0].Value != "first attempt" || len(ps[0].Stack) == 0 {
		t.Fatalf("OnPanic got %+v", ps)
	}
	if out := logs.String(); !strings.Contains(out, "task_panic") || !strings.Contains(out, "first attempt") {
		t.Fatalf("panic not logged:\n%s", out)
	}
	s.Stop(time.Second)
}

// A task that always panics ends with its last attempt recorded as failed
// with the panic text, and the scheduler keeps running other work.
func TestScheduler_PanicRecordedAndSchedulerKeepsRunning(t *testing.T) {
	t.Parallel()
	var rec panicRecorder
	s := New(Options{Logger: discardLogger(), OnPanic: rec.record})
	var ok atomic.Int32
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Register(Task{Name: "always", MaxAttempts: 2, Backoff: Immediate(),
		Fn: func(context.Context, TickHandle) error { panic("boom") }}))
	must(s.Register(Task{Name: "fine", Fn: func(context.Context, TickHandle) error { ok.Add(1); return nil }}))
	if _, err := s.Enqueue("always", nil); err != nil {
		t.Fatal(err)
	}
	if !waitFor(2*time.Second, func() bool { return len(rec.list()) == 2 }) {
		t.Fatalf("panics = %d, want 2 attempts", len(rec.list()))
	}
	if !waitFor(2*time.Second, func() bool {
		st, _ := statusFor(s, "always")
		return st.CurrentAttempt == 2 && st.RecentError != ""
	}) {
		st, _ := statusFor(s, "always")
		t.Fatalf("status %+v", st)
	}
	st, _ := statusFor(s, "always")
	if st.RecentError != `scheduled: task "always" panicked: boom` || !st.LastSuccessAt.IsZero() {
		t.Fatalf("status %+v, want the panic recorded as the failure", st)
	}
	if _, err := s.Enqueue("fine", nil); err != nil {
		t.Fatal(err)
	}
	if !waitFor(2*time.Second, func() bool { return ok.Load() == 1 }) {
		t.Fatal("scheduler stopped running work after a panic")
	}
	s.Stop(time.Second)
}

type panicSchedule struct{}

func (panicSchedule) NextDue(time.Time) (time.Time, bool) { panic("bad schedule") }

// A panicking Schedule ends only its own task's loop. Other tasks keep their
// schedule and Stop still drains.
func TestScheduler_SchedulePanicEndsOnlyThatLoop(t *testing.T) {
	t.Parallel()
	var rec panicRecorder
	s := New(Options{Logger: discardLogger(), OnPanic: rec.record})
	var ticks atomic.Int32
	if err := s.Register(Task{Name: "broken", Schedule: panicSchedule{}, Fn: func(context.Context, TickHandle) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := s.Register(Task{Name: "ticker", Schedule: Interval(5 * time.Millisecond),
		Fn: func(context.Context, TickHandle) error { ticks.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	if !waitFor(2*time.Second, func() bool { return ticks.Load() >= 3 && len(rec.list()) == 1 }) {
		t.Fatalf("ticks %d panics %d", ticks.Load(), len(rec.list()))
	}
	if p := rec.list()[0]; p.Task != "broken" || p.Attempt != 0 || p.Value != "bad schedule" {
		t.Fatalf("OnPanic got %+v", p)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	if err := s.StopContext(stopCtx); err != nil {
		t.Fatalf("Stop after a schedule panic: %v", err)
	}
}
