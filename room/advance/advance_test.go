package advance

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/store"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

var epoch = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

type env struct {
	st  *store.Store
	reg *group.Registry
	r   *Runner
	clk *clock
}

func newEnv(t *testing.T, dir string, clk *clock) *env {
	t.Helper()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	reg := group.New(st, group.Options{Now: clk.Now})
	return &env{st: st, reg: reg, clk: clk, r: New(st, reg, Options{Now: clk.Now})}
}

// counterHandler advances a per-group counter stored in the group state. ready
// is read at each scan.
func counterHandler(ready *atomic.Bool, calls *atomic.Int32, st *store.Store) Handler {
	return Handler{
		Ready: func(context.Context, View) (bool, error) { return ready.Load(), nil },
		Advance: func(_ context.Context, a Advance) (Result, error) {
			calls.Add(1)
			return Result{Apply: func(b *store.Batch) {
				b.Put("test/round/"+a.Group, []byte(strconv.FormatUint(a.Round, 10)))
			}}, nil
		},
	}
}

func TestDeadlineAdvancesAndKeepsCadence(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "League", nil)
	var ready atomic.Bool
	var calls atomic.Int32
	must(t, e.r.Handle("round", counterHandler(&ready, &calls, e.st)))
	_, err := e.r.Schedule(Job{Group: "lg", Name: "weekly", Kind: "round", Interval: 24 * time.Hour, Deadline: epoch.Add(time.Hour)})
	must(t, err)

	if n, _ := e.r.RunDue(context.Background()); n != 0 {
		t.Fatalf("advanced before deadline: %d", n)
	}
	clk.Advance(90 * time.Minute) // 1h30 in; deadline was at 1h
	if n, _ := e.r.RunDue(context.Background()); n != 1 {
		t.Fatalf("advances = %d", n)
	}
	j, _ := e.r.Job("lg", "weekly")
	if j.Round != 1 || !j.Deadline.Equal(epoch.Add(25*time.Hour)) {
		t.Fatalf("job = %+v", j)
	}
	if v, _ := e.st.Get("test/round/lg"); string(v) != "1" {
		t.Fatalf("applied state %q", v)
	}
	if n, _ := e.r.RunDue(context.Background()); n != 0 {
		t.Fatal("ran twice for one deadline")
	}
	ev := e.reg.Events("lg", 0, 0)
	if len(ev) != 1 {
		t.Fatalf("events = %d", len(ev))
	}
	var rec struct {
		Job    string
		Round  uint64
		Reason Reason
	}
	json.Unmarshal(ev[0].Data, &rec)
	if rec.Job != "weekly" || rec.Round != 1 || rec.Reason != ReasonDeadline {
		t.Fatalf("event = %s", ev[0].Data)
	}
}

func TestConditionAdvancesBeforeDeadline(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "League", nil)
	var ready atomic.Bool
	var calls atomic.Int32
	e.r.Handle("round", counterHandler(&ready, &calls, e.st))
	e.r.Schedule(Job{Group: "lg", Name: "weekly", Kind: "round", Interval: 24 * time.Hour, Deadline: epoch.Add(24 * time.Hour)})
	e.r.RunDue(context.Background())
	if calls.Load() != 0 {
		t.Fatal("advanced while not ready")
	}
	ready.Store(true)
	clk.Advance(time.Hour)
	if n, _ := e.r.RunDue(context.Background()); n != 1 {
		t.Fatalf("advances = %d", n)
	}
	j, _ := e.r.Job("lg", "weekly")
	// An early advance restarts the clock from the moment it happened.
	if !j.Deadline.Equal(epoch.Add(25 * time.Hour)) {
		t.Fatalf("deadline = %v", j.Deadline)
	}
	if ev := e.reg.Events("lg", 0, 0); len(ev) != 1 || !contains(string(ev[0].Data), `"condition"`) {
		t.Fatalf("events = %+v", ev)
	}
}

func TestMissedDeadlinesAdvanceOnceAfterDowntime(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "League", nil)
	var ready atomic.Bool
	var calls atomic.Int32
	e.r.Handle("round", counterHandler(&ready, &calls, e.st))
	e.r.Schedule(Job{Group: "lg", Name: "w", Kind: "round", Interval: time.Hour, Deadline: epoch.Add(time.Hour)})
	clk.Advance(10*time.Hour + 10*time.Minute) // the server was down for ten deadlines
	e.r.RunDue(context.Background())
	e.r.RunDue(context.Background())
	j, _ := e.r.Job("lg", "w")
	if calls.Load() != 1 || j.Round != 1 {
		t.Fatalf("calls=%d round=%d, want one catch-up advance", calls.Load(), j.Round)
	}
	if !j.Deadline.Equal(clk.Now().Add(time.Hour)) {
		t.Fatalf("deadline = %v, want one interval from now", j.Deadline)
	}
}

func TestFailingHandlerRetriesWithSameKey(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "L", nil)
	var keys []string
	fail := true
	e.r.Handle("k", Handler{Advance: func(_ context.Context, a Advance) (Result, error) {
		keys = append(keys, a.Key)
		if fail {
			return Result{}, errors.New("boom")
		}
		return Result{}, nil
	}})
	e.r.Schedule(Job{Group: "lg", Name: "j", Kind: "k", Interval: time.Hour, Deadline: epoch})
	e.r.RunDue(context.Background())
	j, _ := e.r.Job("lg", "j")
	if j.Attempts != 1 || j.LastError != "boom" || j.Pending == nil || j.Round != 0 {
		t.Fatalf("after failure: %+v", j)
	}
	e.r.RunDue(context.Background()) // inside backoff: no call
	if len(keys) != 1 {
		t.Fatalf("called during backoff: %v", keys)
	}
	clk.Advance(2 * time.Second)
	fail = false
	e.r.RunDue(context.Background())
	j, _ = e.r.Job("lg", "j")
	if j.Round != 1 || j.Pending != nil || j.LastError != "" || len(keys) != 2 || keys[0] != keys[1] || keys[0] != "lg/j/1" {
		t.Fatalf("job %+v keys %v", j, keys)
	}
}

// The process dies after the handler ran but before its outcome committed.
// The next process re-runs the same round, and the handler's change lands once.
func TestCrashBetweenIntentAndCommit(t *testing.T) {
	dir, clk := t.TempDir(), &clock{t: epoch}
	e := newEnv(t, dir, clk)
	e.reg.Create("lg", "L", nil)
	var applied atomic.Int32
	var keys []string
	mk := func(e *env, crash bool) {
		e.r.Handle("k", Handler{Advance: func(_ context.Context, a Advance) (Result, error) {
			keys = append(keys, a.Key)
			if crash {
				e.st.Close() // the commit that follows will fail, like a kill
			}
			return Result{Apply: func(b *store.Batch) {
				applied.Add(1)
				cur, _ := e.st.Get("score")
				n, _ := strconv.Atoi(string(cur))
				b.Put("score", []byte(strconv.Itoa(n+10)))
			}}, nil
		}})
	}
	mk(e, true)
	e.r.Schedule(Job{Group: "lg", Name: "j", Kind: "k", Interval: time.Hour, Deadline: epoch})
	if _, err := e.r.RunDue(context.Background()); err == nil {
		t.Fatal("expected a store error from the simulated crash")
	}

	e2 := newEnv(t, dir, clk)
	defer e2.st.Close()
	j, _ := e2.r.Job("lg", "j")
	if j.Pending == nil || j.Pending.Round != 1 || j.Round != 0 {
		t.Fatalf("intent not persisted: %+v", j)
	}
	mk(e2, false)
	n, err := e2.r.RunDue(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("recovery run: n=%d err=%v", n, err)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("keys %v: retry must reuse the key", keys)
	}
	if v, _ := e2.st.Get("score"); string(v) != "10" {
		t.Fatalf("score = %q: effect applied %d times", v, applied.Load())
	}
	e2.r.RunDue(context.Background())
	if v, _ := e2.st.Get("score"); string(v) != "10" {
		t.Fatalf("score changed on third scan: %q", v)
	}
}

func TestOneShotAndDoneFlags(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "L", nil)
	var calls atomic.Int32
	e.r.Handle("k", Handler{Advance: func(context.Context, Advance) (Result, error) { calls.Add(1); return Result{}, nil }})
	e.r.Handle("fin", Handler{Advance: func(context.Context, Advance) (Result, error) {
		return Result{Done: true, NextDeadline: epoch.Add(time.Hour)}, nil
	}})
	e.r.Schedule(Job{Group: "lg", Name: "once", Kind: "k", Deadline: epoch})
	e.r.Schedule(Job{Group: "lg", Name: "fin", Kind: "fin", Interval: time.Hour, Deadline: epoch})
	e.r.RunDue(context.Background())
	clk.Advance(5 * time.Hour)
	e.r.RunDue(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("one-shot ran %d times", calls.Load())
	}
	for _, n := range []string{"once", "fin"} {
		if j, _ := e.r.Job("lg", n); !j.Done {
			t.Fatalf("%s not done: %+v", n, j)
		}
	}
}

func TestScheduleValidationAndCancel(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "L", nil)
	e.r.Handle("k", Handler{Advance: func(context.Context, Advance) (Result, error) { return Result{}, nil }})
	if _, err := e.r.Schedule(Job{Group: "lg", Name: "a", Kind: "nope", Deadline: epoch}); !errors.Is(err, ErrNoHandler) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.r.Schedule(Job{Group: "lg", Name: "a/b", Kind: "k", Deadline: epoch}); !errors.Is(err, ErrBadJob) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.r.Schedule(Job{Group: "ghost", Name: "a", Kind: "k", Deadline: epoch}); !errors.Is(err, group.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	e.r.Schedule(Job{Group: "lg", Name: "a", Kind: "k", Deadline: epoch})
	if _, err := e.r.Schedule(Job{Group: "lg", Name: "a", Kind: "k", Deadline: epoch}); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v", err)
	}
	must(t, e.r.Cancel("lg", "a"))
	if err := e.r.Cancel("lg", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeletedGroupDropsItsJobs(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "L", nil)
	e.r.Handle("k", Handler{Advance: func(context.Context, Advance) (Result, error) { return Result{}, nil }})
	e.r.Schedule(Job{Group: "lg", Name: "a", Kind: "k", Interval: time.Hour, Deadline: epoch.Add(time.Hour)})
	e.reg.Delete("lg")
	e.r.RunDue(context.Background())
	if len(e.r.Jobs("")) != 0 {
		t.Fatal("job outlived its group")
	}
}

// The real scheduler wakes at the deadline without any manual RunDue.
func TestStartRunsOnSchedulerAndStops(t *testing.T) {
	st, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := group.New(st, group.Options{})
	reg.Create("lg", "L", nil)
	r := New(st, reg, Options{Poll: 50 * time.Millisecond, MinGap: 10 * time.Millisecond})
	var calls atomic.Int32
	r.Handle("k", Handler{Advance: func(context.Context, Advance) (Result, error) { calls.Add(1); return Result{}, nil }})
	if _, err := r.Schedule(Job{Group: "lg", Name: "a", Kind: "k", Interval: time.Hour, Deadline: time.Now().Add(150 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	must(t, r.Start(ctx))
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer scancel()
	must(t, r.Stop(sctx))
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("ran again after stop or before next deadline: %d", calls.Load())
	}
}

func TestRunDueRace(t *testing.T) {
	clk := &clock{t: epoch}
	e := newEnv(t, t.TempDir(), clk)
	defer e.st.Close()
	e.reg.Create("lg", "L", nil)
	var calls atomic.Int32
	e.r.Handle("k", Handler{Advance: func(context.Context, Advance) (Result, error) { calls.Add(1); return Result{}, nil }})
	e.r.Schedule(Job{Group: "lg", Name: "a", Kind: "k", Interval: time.Hour, Deadline: epoch})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.r.RunDue(context.Background()) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times for one deadline", calls.Load())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
