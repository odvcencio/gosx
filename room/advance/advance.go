// Package advance runs scheduled, per-group jobs for long-running online
// leagues: "advance the round when every human match is finished, or when the
// deadline passes". It builds on GoSX's scheduled package and the room/store
// durable store, and holds no game rules; the game supplies a Handler.
//
// # Exactly-once effects
//
// Each advance has a round number. The runner records the intent (round,
// reason) in the store before it calls the handler, and records the outcome in
// one atomic batch together with whatever state changes the handler asks for
// (Result.Apply). If the process dies between the two, the next process finds
// the intent and calls the handler again with the same round and Key. The
// handler's own changes are only ever committed once, because they ride in the
// outcome batch and the batch only commits if the job's round is still the
// one the intent expected. A handler with side effects outside the store (a
// push message, an HTTP call) must dedupe on Advance.Key.
//
// # Single writer
//
// The runner assumes it is the only process writing the store (the store's
// directory lock enforces this). Running two replicas against one store is not
// supported.
package advance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/store"
	"m31labs.dev/gosx/scheduled"
)

// Reason says what triggered an advance.
type Reason string

// Advance reasons.
const (
	ReasonDeadline  Reason = "deadline"
	ReasonCondition Reason = "condition"
)

// Errors returned by the runner.
var (
	ErrNoHandler = errors.New("advance: no handler registered for kind")
	ErrExists    = errors.New("advance: job already exists")
	ErrNotFound  = errors.New("advance: job not found")
	ErrBadJob    = errors.New("advance: invalid job")
)

const (
	keyJob    = "adv/job/"
	taskName  = "room-advance"
	maxJobErr = 300
)

// Job is a persisted per-group schedule.
type Job struct {
	Group string `json:"group"`
	Name  string `json:"name"`
	// Kind selects the registered Handler.
	Kind string `json:"kind"`
	// Interval is the time between deadlines. Zero with no Result.NextDeadline
	// makes the job one-shot.
	Interval time.Duration `json:"interval,omitempty"`
	// Deadline is the next time the job advances regardless of conditions.
	Deadline time.Time         `json:"deadline"`
	Params   map[string]string `json:"params,omitempty"`

	// Round is the number of completed advances.
	Round   uint64  `json:"round"`
	Done    bool    `json:"done,omitempty"`
	Pending *Intent `json:"pending,omitempty"`
	// Attempts, LastError, and RetryAt describe a failing handler.
	Attempts  int       `json:"attempts,omitempty"`
	LastError string    `json:"lastError,omitempty"`
	RetryAt   time.Time `json:"retryAt,omitempty"`
}

// Intent is a started advance that has not committed.
type Intent struct {
	Round   uint64    `json:"round"`
	Reason  Reason    `json:"reason"`
	Started time.Time `json:"started"`
}

// View is what a Handler's Ready function sees.
type View struct {
	Group    string
	Job      string
	Round    uint64
	Deadline time.Time
	Params   map[string]string
	Now      time.Time
}

// Advance is one request to the handler.
type Advance struct {
	Group  string
	Job    string
	Round  uint64 // the round being completed; stable across retries
	Reason Reason
	// Key identifies this advance across retries and restarts:
	// "<group>/<job>/<round>".
	Key      string
	Deadline time.Time
	Params   map[string]string
	Now      time.Time
}

// Result is a handler's outcome.
type Result struct {
	// Apply adds the handler's state changes to the batch that commits the
	// advance. May be nil.
	Apply func(b *store.Batch)
	// NextDeadline overrides the computed next deadline.
	NextDeadline time.Time
	// Done ends the job.
	Done bool
	// Event is appended to the group's event log with the advance record.
	Event []byte
}

// Handler is the game-supplied half of a job.
type Handler struct {
	// Ready reports whether the condition holds before the deadline. Nil means
	// the job advances on its deadline only.
	Ready func(ctx context.Context, v View) (bool, error)
	// Advance computes one advance. It must not commit state itself; return it
	// in Result.Apply. It may run more than once for the same Key.
	Advance func(ctx context.Context, a Advance) (Result, error)
}

// Options configures a Runner.
type Options struct {
	// Now returns the current time. Default time.Now.
	Now func() time.Time
	// Poll is the longest wait between scans; it bounds how late a Ready
	// condition is noticed. Default 15 s.
	Poll time.Duration
	// MinGap is the shortest wait between scans. Default 250 ms.
	MinGap time.Duration
	// AdvanceTimeout limits one handler call. Default 30 s.
	AdvanceTimeout time.Duration
	// MaxBackoff limits the retry delay of a failing handler. Default 5 min.
	MaxBackoff time.Duration
	// Scheduler runs the scan task. Default: a private scheduled.Scheduler.
	Scheduler *scheduled.Scheduler
	// Logf receives diagnostics. Default: discard.
	Logf func(format string, args ...any)
}

// Runner owns the job table and its schedule.
type Runner struct {
	st     *store.Store
	groups *group.Registry
	opts   Options
	sched  *scheduled.Scheduler
	ownSch bool

	mu       sync.Mutex // guards handlers and serializes job runs
	handlers map[string]Handler
	started  bool
}

// New returns a runner over st. groups is optional; when set, jobs of deleted
// groups are removed and advances append to the group's event log.
func New(st *store.Store, groups *group.Registry, opts Options) *Runner {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Poll <= 0 {
		opts.Poll = 15 * time.Second
	}
	if opts.MinGap <= 0 {
		opts.MinGap = 250 * time.Millisecond
	}
	if opts.AdvanceTimeout <= 0 {
		opts.AdvanceTimeout = 30 * time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Minute
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	return &Runner{st: st, groups: groups, opts: opts, sched: opts.Scheduler, handlers: map[string]Handler{}}
}

// Handle registers the handler for a job kind.
func (r *Runner) Handle(kind string, h Handler) error {
	if kind == "" || h.Advance == nil {
		return ErrBadJob
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.handlers[kind]; dup {
		return fmt.Errorf("advance: handler %q already registered", kind)
	}
	r.handlers[kind] = h
	return nil
}

func jobKey(group, name string) string { return keyJob + group + "/" + name }

// Schedule creates a job. Deadline must be set; Kind must have a handler.
func (r *Runner) Schedule(j Job) (Job, error) {
	if j.Group == "" || j.Name == "" || j.Kind == "" || j.Deadline.IsZero() || j.Interval < 0 ||
		containsSlash(j.Group) || containsSlash(j.Name) {
		return Job{}, ErrBadJob
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handlers[j.Kind]; !ok {
		return Job{}, ErrNoHandler
	}
	if r.groups != nil {
		if _, err := r.groups.Get(j.Group); err != nil {
			return Job{}, err
		}
	}
	if _, ok := r.st.Get(jobKey(j.Group, j.Name)); ok {
		return Job{}, ErrExists
	}
	j.Round, j.Done, j.Pending, j.Attempts, j.LastError, j.RetryAt = 0, false, nil, 0, "", time.Time{}
	j.Deadline = j.Deadline.UTC()
	if err := r.save(j); err != nil {
		return Job{}, err
	}
	r.pokeLocked()
	return j, nil
}

func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

func (r *Runner) save(j Job) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return r.st.Put(jobKey(j.Group, j.Name), b)
}

// Job returns one job.
func (r *Runner) Job(group, name string) (Job, error) {
	b, ok := r.st.Get(jobKey(group, name))
	if !ok {
		return Job{}, ErrNotFound
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return Job{}, err
	}
	return j, nil
}

// Jobs returns a group's jobs sorted by name; an empty group returns all jobs.
func (r *Runner) Jobs(group string) []Job {
	prefix := keyJob
	if group != "" {
		prefix += group + "/"
	}
	var out []Job
	r.st.Scan(prefix, func(_ string, v []byte) bool {
		var j Job
		if json.Unmarshal(v, &j) == nil {
			out = append(out, j)
		}
		return true
	})
	sort.Slice(out, func(i, k int) bool {
		if out[i].Group != out[k].Group {
			return out[i].Group < out[k].Group
		}
		return out[i].Name < out[k].Name
	})
	return out
}

// Cancel removes a job.
func (r *Runner) Cancel(group, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Get(jobKey(group, name)); !ok {
		return ErrNotFound
	}
	return r.st.Delete(jobKey(group, name))
}

// RunDue scans every job once and advances those that are due or ready. It
// returns the number of advances committed. A failing handler is recorded on
// its job and retried with backoff; RunDue itself only returns store errors.
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, j := range r.Jobs("") {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		ok, err := r.runJob(ctx, j)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// runJob advances one job if due. Caller holds r.mu.
func (r *Runner) runJob(ctx context.Context, j Job) (bool, error) {
	if j.Done {
		return false, nil
	}
	now := r.opts.Now().UTC()
	if r.groups != nil {
		if _, err := r.groups.Get(j.Group); errors.Is(err, group.ErrNotFound) {
			return false, r.st.Delete(jobKey(j.Group, j.Name))
		}
	}
	h, ok := r.handlers[j.Kind]
	if !ok {
		return false, nil // another build may own this kind; keep the job
	}
	if !j.RetryAt.IsZero() && now.Before(j.RetryAt) {
		return false, nil
	}
	if j.Pending == nil {
		reason := Reason("")
		switch {
		case !now.Before(j.Deadline):
			reason = ReasonDeadline
		case h.Ready != nil:
			ready, err := h.Ready(ctx, View{Group: j.Group, Job: j.Name, Round: j.Round, Deadline: j.Deadline, Params: j.Params, Now: now})
			if err != nil {
				r.opts.Logf("advance: %s/%s ready: %v", j.Group, j.Name, err)
				return false, nil
			}
			if ready {
				reason = ReasonCondition
			}
		}
		if reason == "" {
			return false, nil
		}
		j.Pending = &Intent{Round: j.Round + 1, Reason: reason, Started: now}
		if err := r.save(j); err != nil {
			return false, err
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, r.opts.AdvanceTimeout)
	res, err := h.Advance(callCtx, Advance{
		Group: j.Group, Job: j.Name, Round: j.Pending.Round, Reason: j.Pending.Reason,
		Key:      fmt.Sprintf("%s/%s/%d", j.Group, j.Name, j.Pending.Round),
		Deadline: j.Deadline, Params: j.Params, Now: now,
	})
	cancel()
	if err != nil {
		j.Attempts++
		j.LastError = truncate(err.Error(), maxJobErr)
		j.RetryAt = now.Add(backoff(j.Attempts, r.opts.MaxBackoff))
		r.opts.Logf("advance: %s/%s round %d attempt %d: %v", j.Group, j.Name, j.Pending.Round, j.Attempts, err)
		return false, r.save(j)
	}

	intent := *j.Pending
	next := res.NextDeadline
	if next.IsZero() && j.Interval > 0 {
		if intent.Reason == ReasonDeadline {
			next = j.Deadline.Add(j.Interval)
		}
		if next.IsZero() || !next.After(now) {
			next = now.Add(j.Interval)
		}
	}
	j.Round, j.Pending = intent.Round, nil
	j.Attempts, j.LastError, j.RetryAt = 0, "", time.Time{}
	if next.IsZero() || res.Done {
		j.Done = true
	} else {
		j.Deadline = next.UTC()
	}
	jb, err := json.Marshal(j)
	if err != nil {
		return false, err
	}
	b := &store.Batch{}
	if res.Apply != nil {
		res.Apply(b)
	}
	b.Put(jobKey(j.Group, j.Name), jb)
	if r.groups != nil {
		ev, _ := json.Marshal(struct {
			Job    string          `json:"job"`
			Round  uint64          `json:"round"`
			Reason Reason          `json:"reason"`
			At     time.Time       `json:"at"`
			Detail json.RawMessage `json:"detail,omitempty"`
		}{j.Name, intent.Round, intent.Reason, now, rawOrNil(res.Event)})
		b.Append(group.EventsKey(j.Group), ev)
	}
	if err := r.st.Commit(b); err != nil {
		return false, err
	}
	return true, nil
}

func rawOrNil(b []byte) json.RawMessage {
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return b
}

func backoff(attempt int, max time.Duration) time.Duration {
	d := time.Second
	for i := 1; i < attempt && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// wake is the scheduled.Schedule: it wakes at the earliest deadline or retry
// time, but never later than Poll and never sooner than MinGap.
type wake struct{ r *Runner }

func (w wake) NextDue(after time.Time) (time.Time, bool) {
	due := after.Add(w.r.opts.Poll)
	for _, j := range w.r.Jobs("") {
		if j.Done {
			continue
		}
		t := j.Deadline
		if !j.RetryAt.IsZero() && j.RetryAt.After(t) {
			t = j.RetryAt
		}
		if t.Before(due) {
			due = t
		}
	}
	if floor := after.Add(w.r.opts.MinGap); due.Before(floor) {
		due = floor
	}
	return due, true
}

func (wake) String() string { return "advance wake" }

// Start registers the scan task with the scheduler, starts it, and runs one
// scan at once so deadlines missed during downtime are caught up. Stop it with
// Stop.
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	r.started = true
	if r.sched == nil {
		r.sched = scheduled.New(scheduled.Options{Now: r.opts.Now})
		r.ownSch = true
	}
	r.mu.Unlock()
	err := r.sched.Register(scheduled.Task{
		Name:        taskName,
		Schedule:    wake{r},
		Timeout:     5 * time.Minute,
		MaxAttempts: 1,
		Fn: func(ctx context.Context, _ scheduled.TickHandle) error {
			_, err := r.RunDue(ctx)
			return err
		},
	})
	if err != nil {
		return err
	}
	if r.ownSch {
		r.sched.Start(ctx)
	}
	_, err = r.sched.Enqueue(taskName, nil)
	return err
}

// Poke asks for a scan soon, for example after a match finishes. It does
// nothing before Start.
func (r *Runner) Poke() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pokeLocked()
}

func (r *Runner) pokeLocked() {
	if r.started && r.sched != nil {
		_, _ = r.sched.Enqueue(taskName, nil)
	}
}

// Stop stops the private scheduler and waits for a running scan until ctx ends.
// With a shared scheduler the owner stops it.
func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	s, own := r.sched, r.ownSch
	r.mu.Unlock()
	if s != nil && own {
		return s.StopContext(ctx)
	}
	return nil
}
