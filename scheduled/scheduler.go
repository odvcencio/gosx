package scheduled

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrStallTimeout is the cancellation cause when a run exceeds its
// ProgressTimeout without reporting progress.
var ErrStallTimeout = errors.New("scheduled: progress stall timeout")

// ErrTimeout is the cancellation cause when a run exceeds its hard Timeout.
var ErrTimeout = errors.New("scheduled: task timeout")

const defaultShutdownGrace = 30 * time.Second

// minWatchdogCadence floors the watchdog ticker so very small timeouts do not
// spin the CPU.
const minWatchdogCadence = 2 * time.Millisecond

// Options configures a Scheduler. The zero value is valid; New fills defaults.
type Options struct {
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// Logger receives structured stall/timeout events; defaults to slog.Default().
	Logger *slog.Logger
	// Store persists task status; defaults to an in-memory store.
	Store Store
	// ShutdownGrace is the default drain window for Stop when grace<=0;
	// defaults to 30s.
	ShutdownGrace time.Duration
}

// registered holds a task plus the bookkeeping the scheduler needs across runs.
type registered struct {
	task Task
}

// schedTick is the scheduler's TickHandle implementation. Unlike the foundation
// tickHandle it guards progress state with a mutex, because the watchdog
// goroutine reads lastProgressAt/lastProgress concurrently with the task
// goroutine's Progress calls.
type schedTick struct {
	run *activeRun
}

func (h *schedTick) Progress(message string) {
	now := h.run.now()
	h.run.mu.Lock()
	h.run.lastProgress = message
	h.run.lastProgressAt = now
	h.run.mu.Unlock()
}

func (h *schedTick) Attempt() int { return h.run.attempt }

func (h *schedTick) Metadata() map[string]string { return h.run.metadata }

// activeRun is the live state for a single in-flight attempt. Fields after mu
// are guarded by mu.
type activeRun struct {
	id          RunID
	taskName    string
	attempt     int
	startedAt   time.Time
	now         func() time.Time
	metadata    map[string]string
	cancelCause context.CancelCauseFunc

	mu             sync.Mutex
	lastProgressAt time.Time
	lastProgress   string
}

// snapshot returns the watchdog-relevant progress state under the lock.
func (r *activeRun) snapshot() (lastProgressAt time.Time, lastProgress string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastProgressAt, r.lastProgress
}

// cancel triggers the run's context cancellation with the given cause.
func (r *activeRun) cancel(cause error) { r.cancelCause(cause) }

// Scheduler runs registered tasks on their schedules, with per-run watchdog
// cancellation and bounded retry.
type Scheduler struct {
	opts Options

	mu      sync.Mutex
	tasks   map[string]*registered
	runs    map[RunID]*activeRun
	started bool

	wg     sync.WaitGroup // schedule loops + enqueued runs
	stopCh chan struct{}  // closed by Stop to unwind loops
}

// New constructs a Scheduler, applying defaults for any unset Options field.
func New(opts Options) *Scheduler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Store == nil {
		opts.Store = newMemStore()
	}
	if opts.ShutdownGrace <= 0 {
		opts.ShutdownGrace = defaultShutdownGrace
	}
	return &Scheduler{
		opts:   opts,
		tasks:  make(map[string]*registered),
		runs:   make(map[RunID]*activeRun),
		stopCh: make(chan struct{}),
	}
}

// Register adds a task. It errors on an empty name, a nil Fn, or a duplicate name.
func (s *Scheduler) Register(t Task) error {
	if t.Name == "" {
		return errors.New("scheduled: task name must not be empty")
	}
	if t.Fn == nil {
		return fmt.Errorf("scheduled: task %q has nil Fn", t.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[t.Name]; exists {
		return fmt.Errorf("scheduled: task %q already registered", t.Name)
	}
	s.tasks[t.Name] = &registered{task: t}
	// Seed a status row so Status reflects registered-but-not-yet-run tasks.
	if _, ok := s.opts.Store.Load(t.Name); !ok {
		_ = s.opts.Store.Save(TaskStatus{
			Name:     t.Name,
			Schedule: scheduleString(t.Schedule),
		})
	}
	return nil
}

// Start launches one schedule goroutine per registered task. It is idempotent:
// repeated calls are no-ops. Loops unwind when ctx is cancelled or Stop is called.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	tasks := make([]Task, 0, len(s.tasks))
	for _, r := range s.tasks {
		tasks = append(tasks, r.task)
	}
	s.mu.Unlock()

	for _, t := range tasks {
		t := t
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.scheduleLoop(ctx, t)
		}()
	}
}

// scheduleLoop fires task on its schedule until ctx is done, Stop is called, or
// the schedule reports "never again".
func (s *Scheduler) scheduleLoop(ctx context.Context, t Task) {
	last := s.opts.Now()
	for {
		due, ok := t.Schedule.NextDue(last)
		if !ok {
			return
		}
		wait := due.Sub(s.opts.Now())
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
		s.runOnce(ctx, t)
		last = s.opts.Now()
	}
}

// Enqueue runs a registered task once immediately, out of schedule. It returns
// the RunID of the first attempt and errors if the task is unknown.
func (s *Scheduler) Enqueue(name string, payload []byte) (RunID, error) {
	s.mu.Lock()
	r, ok := s.tasks[name]
	s.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("scheduled: task %q not registered", name)
	}
	task := r.task
	if payload != nil {
		// Attach payload via metadata copy so concurrent enqueues do not alias.
		meta := make(map[string]string, len(task.Metadata)+1)
		for k, v := range task.Metadata {
			meta[k] = v
		}
		meta["payload"] = string(payload)
		task.Metadata = meta
	}

	id := newRunID(name, s.opts.Now)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runOnceWithID(context.Background(), task, id)
	}()
	return id, nil
}

// Cancel cancels an in-flight run with cause context.Canceled. It errors if the
// run is unknown (already finished or never existed).
func (s *Scheduler) Cancel(runID RunID) error {
	s.mu.Lock()
	run, ok := s.runs[runID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("scheduled: run %q not found", runID)
	}
	run.cancel(context.Canceled)
	return nil
}

// runOnce executes task with a fresh RunID for its first attempt.
func (s *Scheduler) runOnce(ctx context.Context, task Task) {
	id := newRunID(task.Name, s.opts.Now)
	s.runOnceWithID(ctx, task, id)
}

// runOnceWithID executes task through its retry policy. firstID names the first
// attempt; subsequent attempts get fresh RunIDs. parent is the parent context
// for the run (schedule ctx or background for Enqueue).
func (s *Scheduler) runOnceWithID(parent context.Context, task Task, firstID RunID) {
	backoff := task.effectiveBackoff()
	attempt := 0
	for {
		attempt++
		id := firstID
		if attempt > 1 {
			id = newRunID(task.Name, s.opts.Now)
		}

		err := s.runAttempt(parent, task, id, attempt)

		if err == nil {
			return
		}
		if !ShouldRetry(attempt, task.MaxAttempts) {
			return
		}
		// Wait out the backoff, but bail if we are stopping or the parent dies.
		delay := backoff.NextDelay(attempt)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-s.stopCh:
				timer.Stop()
				return
			case <-parent.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		} else {
			select {
			case <-s.stopCh:
				return
			case <-parent.Done():
				return
			default:
			}
		}
	}
}

// runAttempt executes a single attempt: registers the activeRun, starts the
// watchdog, invokes the task Fn, then records status and de-registers.
func (s *Scheduler) runAttempt(parent context.Context, task Task, id RunID, attempt int) error {
	runCtx, cancelCause := context.WithCancelCause(parent)
	defer cancelCause(nil) // release context resources on return

	startedAt := s.opts.Now()
	run := &activeRun{
		id:             id,
		taskName:       task.Name,
		attempt:        attempt,
		startedAt:      startedAt,
		now:            s.opts.Now,
		metadata:       task.Metadata,
		cancelCause:    cancelCause,
		lastProgressAt: startedAt,
	}

	s.mu.Lock()
	s.runs[id] = run
	s.mu.Unlock()

	// Record run start in status.
	s.updateStatus(task, func(st *TaskStatus) {
		st.LastRunAt = startedAt
		st.CurrentAttempt = attempt
		st.CurrentProgress = ""
	})

	wdDone := s.startWatchdog(runCtx, task, run)

	tick := &schedTick{run: run}
	err := task.Fn(runCtx, tick)

	// Stop the watchdog and wait for it to exit before we drop the run.
	cancelCause(nil)
	if wdDone != nil {
		<-wdDone
	}

	s.mu.Lock()
	delete(s.runs, id)
	s.mu.Unlock()

	finishedAt := s.opts.Now()
	_, lastProgress := run.snapshot()
	s.updateStatus(task, func(st *TaskStatus) {
		st.LastRunAt = finishedAt
		st.CurrentAttempt = attempt
		st.CurrentProgress = lastProgress
		if err == nil {
			st.LastSuccessAt = finishedAt
			st.RecentError = ""
		} else {
			st.RecentError = err.Error()
		}
	})

	return err
}

// startWatchdog launches the per-run watchdog goroutine and returns a channel
// closed when it exits. It returns nil (no goroutine) when neither timeout is set.
func (s *Scheduler) startWatchdog(runCtx context.Context, task Task, run *activeRun) <-chan struct{} {
	if task.Timeout <= 0 && task.ProgressTimeout <= 0 {
		return nil
	}
	cadence := watchdogCadence(task.Timeout, task.ProgressTimeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(cadence)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			now := s.opts.Now()
			lastProgressAt, lastProgress := run.snapshot()
			switch evaluate(now, run.startedAt, lastProgressAt, task.Timeout, task.ProgressTimeout) {
			case verdictStall:
				run.cancel(ErrStallTimeout)
				s.opts.Logger.Warn("task_stall",
					"task", task.Name,
					"run", string(run.id),
					"last_progress", lastProgress,
					"stall_age", now.Sub(lastProgressAt),
				)
				return
			case verdictTimeout:
				run.cancel(ErrTimeout)
				s.opts.Logger.Warn("task_timeout",
					"task", task.Name,
					"run", string(run.id),
					"elapsed", now.Sub(run.startedAt),
				)
				return
			case verdictNone:
				// keep watching
			}
		}
	}()
	return done
}

// watchdogCadence picks a polling interval ~= min(nonzero timeouts)/4, floored.
func watchdogCadence(timeout, progressTimeout time.Duration) time.Duration {
	smallest := time.Duration(0)
	for _, d := range []time.Duration{timeout, progressTimeout} {
		if d > 0 && (smallest == 0 || d < smallest) {
			smallest = d
		}
	}
	cadence := smallest / 4
	if cadence < minWatchdogCadence {
		cadence = minWatchdogCadence
	}
	return cadence
}

// updateStatus applies mutate to the task's stored TaskStatus, refreshing the
// schedule string and next-due time, then saves it.
func (s *Scheduler) updateStatus(task Task, mutate func(*TaskStatus)) {
	st, _ := s.opts.Store.Load(task.Name)
	st.Name = task.Name
	st.Schedule = scheduleString(task.Schedule)
	mutate(&st)
	if due, ok := nextDue(task.Schedule, s.opts.Now()); ok {
		st.NextDueAt = due
	}
	_ = s.opts.Store.Save(st)
}

// liveRunSummary holds the subset of live-run state needed by Status.
type liveRunSummary struct {
	attempt      int
	progress     string
	progressAt   time.Time
	progressTOMs *int64 // ProgressTimeout in milliseconds, nil if not set
}

// Status returns a snapshot of every registered task's status, overlaying live
// run state (current attempt / progress) for tasks that are mid-run.
func (s *Scheduler) Status() []TaskStatus {
	now := s.opts.Now()

	s.mu.Lock()
	// Collect a stable view of tasks and the newest live run per task.
	names := make([]string, 0, len(s.tasks))
	scheduleStrings := make(map[string]string, len(s.tasks))
	progressTimeouts := make(map[string]*int64, len(s.tasks))
	for name, r := range s.tasks {
		names = append(names, name)
		scheduleStrings[name] = scheduleString(r.task.Schedule)
		if r.task.ProgressTimeout > 0 {
			ms := r.task.ProgressTimeout.Milliseconds()
			progressTimeouts[name] = &ms
		}
	}
	live := make(map[string]liveRunSummary)
	for _, run := range s.runs {
		progressAt, progress := run.snapshot()
		// Prefer the highest-attempt live run per task.
		if existing, ok := live[run.taskName]; !ok || run.attempt >= existing.attempt {
			live[run.taskName] = liveRunSummary{
				attempt:      run.attempt,
				progress:     progress,
				progressAt:   progressAt,
				progressTOMs: progressTimeouts[run.taskName],
			}
		}
	}
	s.mu.Unlock()

	out := make([]TaskStatus, 0, len(names))
	for _, name := range names {
		st, _ := s.opts.Store.Load(name)
		st.Name = name
		st.Schedule = scheduleStrings[name]
		// Always reflect the task's ProgressTimeout configuration.
		st.ProgressTimeoutMs = progressTimeouts[name]
		if summary, ok := live[name]; ok {
			st.CurrentAttempt = summary.attempt
			st.CurrentProgress = summary.progress
			if !summary.progressAt.IsZero() {
				ageMs := now.Sub(summary.progressAt).Milliseconds()
				st.CurrentProgressAgeMs = &ageMs
			}
		}
		out = append(out, st)
	}
	return out
}

// Stop signals every schedule loop to exit, drains in-flight runs up to grace,
// then force-cancels any stragglers and waits for them to unwind.
func (s *Scheduler) Stop(grace time.Duration) {
	if grace <= 0 {
		grace = s.opts.ShutdownGrace
	}

	// Signal loops to stop (idempotent close).
	s.mu.Lock()
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}
	s.started = false
	s.mu.Unlock()

	// Drain: wait for all tracked goroutines, bounded by grace.
	if s.waitGrace(grace) {
		return
	}

	// Grace elapsed with runs still in flight: cancel them.
	s.mu.Lock()
	for _, run := range s.runs {
		run.cancel(context.Canceled)
	}
	s.mu.Unlock()

	// Wait for the cancelled runs to finish unwinding. wg.Wait is unbounded
	// here, but cancellation guarantees the run goroutines return promptly.
	s.wg.Wait()
}

// waitGrace blocks until all tracked goroutines finish or d elapses. It reports
// whether they finished within the window.
func (s *Scheduler) waitGrace(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// scheduleString renders a Schedule for display in TaskStatus.
func scheduleString(sch Schedule) string {
	switch v := sch.(type) {
	case intervalSchedule:
		return "every " + v.d.String()
	case *onceSchedule:
		return "once"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%T", sch)
	}
}

// nextDue computes the next fire time without mutating one-shot schedule state.
func nextDue(sch Schedule, now time.Time) (time.Time, bool) {
	switch v := sch.(type) {
	case intervalSchedule:
		return now.Add(v.d), true
	case *onceSchedule:
		// A one-shot's next due is "now" until it fires; after that there is
		// no further due time. We avoid mutating it here.
		if v.fired {
			return time.Time{}, false
		}
		return now, true
	default:
		return time.Time{}, false
	}
}
