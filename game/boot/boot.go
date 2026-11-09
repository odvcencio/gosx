// Package boot is the loading and readiness gate of a game.
//
// A Gate tracks named tasks (asset batches, model preparation, the first
// network pose) and reports one progress number between 0 and 1. It also
// watches for stalls. When no task makes progress for StallTimeout, the Gate
// recovers in this order:
//
//  1. Soft retry: call the stalled tasks' Retry hooks, up to Config.Retries times.
//  2. One automatic page reload, guarded by a Marker so a reload loop is
//     impossible. If the marker cannot be read or written, no reload happens.
//  3. Failed: the error screen shows and the player can retry by hand. The
//     Gate keeps listening, so a task that finishes late still ends in Ready.
//
// The Gate uses an injected clock: the caller passes the elapsed time to
// Update, so tests run without sleeping. The stall timer stops while the page
// is hidden (SetHidden), because browsers throttle loading in background tabs.
//
// Package bootpage (server side) emits the WASM preload links and a small
// JavaScript watchdog that works even when the WASM module never starts.
package boot

import (
	"errors"
	"sync"
	"time"
)

// Phase is the gate's overall state.
type Phase uint8

const (
	// PhaseLoading means tasks are still running.
	PhaseLoading Phase = iota
	// PhaseRecovering means a reload is scheduled.
	PhaseRecovering
	// PhaseFailed means recovery is exhausted. Late completion still reaches Ready.
	PhaseFailed
	// PhaseReady means every task finished.
	PhaseReady
)

func (p Phase) String() string {
	switch p {
	case PhaseLoading:
		return "loading"
	case PhaseRecovering:
		return "recovering"
	case PhaseFailed:
		return "failed"
	case PhaseReady:
		return "ready"
	}
	return "unknown"
}

// Marker remembers across a page reload that the automatic reload was used.
// storage.Session() (browser sessionStorage) satisfies it.
type Marker interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Delete(key string) error
}

// Defaults for Config.
const (
	DefaultStallTimeout = 12 * time.Second
	DefaultReloadDelay  = 250 * time.Millisecond
	reloadedValue       = "retried"
)

// Config tunes a Gate. The zero value is usable: 12 s stall timeout, one soft
// retry, and no automatic reload (set Reload and Marker to enable it).
type Config struct {
	// StallTimeout is how long all tasks may sit without progress.
	StallTimeout time.Duration
	// Retries is the number of soft retries before the reload step. A negative
	// value means none; zero means one.
	Retries int
	// Reload reloads the page. If nil, step 2 is skipped.
	Reload func()
	// Marker and MarkerKey guard the single automatic reload. Without a Marker
	// the gate never reloads on its own.
	Marker    Marker
	MarkerKey string
	// ReloadDelay lets the "recovering" screen show before the reload.
	// Zero selects DefaultReloadDelay.
	ReloadDelay time.Duration
	// OnChange is called after the phase, progress or task list changes. It
	// runs outside the gate's lock and may call gate methods.
	OnChange func(Snapshot)
}

// Task is one unit of loading work.
type Task struct {
	Name string
	// Weight is the task's share of the total progress. Zero means 1.
	Weight float64
	// Progress, if set, is polled on each Update and returns completed and
	// total units. The task is done when total > 0 and done >= total.
	// Without it, drive the task with Set, Complete and Fail.
	Progress func() (done, total float64)
	// Retry restarts the task after a stall or failure. Optional.
	Retry func()
}

// TaskStatus is a task's state in a Snapshot.
type TaskStatus struct {
	Name     string
	Progress float64 // 0..1
	Done     bool
	Err      error
}

// Snapshot is the state for rendering a loading screen.
type Snapshot struct {
	Phase    Phase
	Progress float64 // 0..1, never decreases
	Tasks    []TaskStatus
	Err      error // first task error, if any
	// Attempt is 1 when this page load follows the automatic reload, else 0.
	Attempt int
	// SoftRetries counts soft retries used so far.
	SoftRetries int
}

type taskState struct {
	Task
	done, total float64
	err         error
	complete    bool
}

func (t *taskState) fraction() float64 {
	if t.complete {
		return 1
	}
	if t.total <= 0 {
		return 0
	}
	f := t.done / t.total
	if f > 1 {
		return 1
	}
	if f < 0 {
		return 0
	}
	return f
}

// Gate is the readiness gate. It is safe for concurrent use, so loaders may
// report from goroutines.
type Gate struct {
	mu          sync.Mutex
	cfg         Config
	tasks       []*taskState
	phase       Phase
	best        float64
	clock       time.Duration
	lastMove    time.Duration // clock value of the last progress change
	hidden      bool
	soft        int
	attempt     int
	reloadAt    time.Duration
	reloadArmed bool
	lastEmit    Snapshot
	emitted     bool
}

// New creates a Gate. If cfg.Marker records an earlier automatic reload,
// Snapshot.Attempt is 1 and the reload step is spent.
func New(cfg Config, tasks ...Task) *Gate {
	if cfg.StallTimeout <= 0 {
		cfg.StallTimeout = DefaultStallTimeout
	}
	if cfg.ReloadDelay <= 0 {
		cfg.ReloadDelay = DefaultReloadDelay
	}
	if cfg.Retries == 0 {
		cfg.Retries = 1
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	}
	g := &Gate{cfg: cfg}
	if cfg.Marker != nil && cfg.MarkerKey != "" {
		if v, ok := cfg.Marker.Get(cfg.MarkerKey); ok && v == reloadedValue {
			g.attempt = 1
		}
	}
	for _, t := range tasks {
		g.addLocked(t)
	}
	return g
}

func (g *Gate) addLocked(t Task) *taskState {
	if t.Weight <= 0 {
		t.Weight = 1
	}
	st := &taskState{Task: t}
	g.tasks = append(g.tasks, st)
	return st
}

func (g *Gate) find(name string) *taskState {
	for _, t := range g.tasks {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Add registers a task. Adding after Ready reopens the gate.
func (g *Gate) Add(t Task) {
	g.mu.Lock()
	g.addLocked(t)
	if g.phase == PhaseReady {
		g.phase = PhaseLoading
		g.lastMove = g.clock
	}
	snap, fire := g.snapshotLocked()
	g.mu.Unlock()
	g.emit(snap, fire)
}

// Set reports done of total units for a task that has no Progress poll.
// Unknown names are added.
func (g *Gate) Set(name string, done, total float64) {
	g.mu.Lock()
	t := g.find(name)
	if t == nil {
		t = g.addLocked(Task{Name: name})
	}
	if t.done != done || t.total != total {
		t.done, t.total = done, total
		t.err = nil
		g.lastMove = g.clock
	}
	snap, fire := g.snapshotLocked()
	g.mu.Unlock()
	g.emit(snap, fire)
}

// Complete marks a task finished.
func (g *Gate) Complete(name string) {
	g.mu.Lock()
	t := g.find(name)
	if t == nil {
		t = g.addLocked(Task{Name: name})
	}
	if !t.complete {
		t.complete, t.err = true, nil
		g.lastMove = g.clock
	}
	snap, fire := g.snapshotLocked()
	g.mu.Unlock()
	g.emit(snap, fire)
}

// Fail records an error for a task. The gate starts recovery on the next Update.
func (g *Gate) Fail(name string, err error) {
	if err == nil {
		err = errors.New("boot: task failed")
	}
	g.mu.Lock()
	t := g.find(name)
	if t == nil {
		t = g.addLocked(Task{Name: name})
	}
	t.err = err
	snap, fire := g.snapshotLocked()
	g.mu.Unlock()
	g.emit(snap, fire)
}

// SetHidden pauses the stall timer while the page is hidden.
func (g *Gate) SetHidden(hidden bool) {
	g.mu.Lock()
	if g.hidden && !hidden {
		g.lastMove = g.clock // do not count the time spent hidden
	}
	g.hidden = hidden
	g.mu.Unlock()
}

// Update advances the clock by dt, polls tasks, and runs recovery. Call it
// once per frame, or from a timer while the game loop is not running yet.
func (g *Gate) Update(dt time.Duration) {
	if dt < 0 {
		dt = 0
	}
	var reload func()
	var retries []func()
	g.mu.Lock()
	g.clock += dt
	for _, t := range g.tasks {
		if t.Progress == nil || t.complete {
			continue
		}
		done, total := t.Progress()
		if done != t.done || total != t.total {
			t.done, t.total = done, total
			g.lastMove = g.clock
		}
	}
	if g.phase != PhaseReady && g.allDoneLocked() {
		g.phase = PhaseReady
		if g.cfg.Marker != nil && g.cfg.MarkerKey != "" {
			_ = g.cfg.Marker.Delete(g.cfg.MarkerKey)
		}
		g.reloadArmed = false
	} else if g.phase != PhaseReady {
		reload, retries = g.recoverLocked()
	}
	snap, fire := g.snapshotLocked()
	g.mu.Unlock()
	for _, r := range retries {
		r()
	}
	g.emit(snap, fire)
	if reload != nil {
		reload()
	}
}

func (g *Gate) allDoneLocked() bool {
	if len(g.tasks) == 0 {
		return false
	}
	for _, t := range g.tasks {
		if !t.complete && !(t.total > 0 && t.done >= t.total && t.err == nil) {
			return false
		}
	}
	return true
}

func (g *Gate) failedLocked() bool {
	for _, t := range g.tasks {
		if t.err != nil {
			return true
		}
	}
	return false
}

// recoverLocked decides the next recovery step. It returns a reload to call
// and Retry hooks to run, both outside the lock.
func (g *Gate) recoverLocked() (reload func(), retries []func()) {
	if g.phase == PhaseRecovering {
		if g.reloadArmed && g.clock >= g.reloadAt && g.cfg.Reload != nil {
			g.reloadArmed = false
			return g.cfg.Reload, nil
		}
		return nil, nil
	}
	if g.phase == PhaseFailed || g.hidden {
		return nil, nil
	}
	stalled := g.clock-g.lastMove >= g.cfg.StallTimeout
	if !stalled && !g.failedLocked() {
		return nil, nil
	}
	if g.soft < g.cfg.Retries {
		g.soft++
		g.lastMove = g.clock
		for _, t := range g.tasks {
			if t.complete || (t.total > 0 && t.done >= t.total) {
				continue
			}
			t.err = nil
			if t.Retry != nil {
				retries = append(retries, t.Retry)
			}
		}
		return nil, retries
	}
	if g.canReloadLocked() {
		if err := g.cfg.Marker.Set(g.cfg.MarkerKey, reloadedValue); err == nil {
			g.phase = PhaseRecovering
			g.reloadArmed = true
			g.reloadAt = g.clock + g.cfg.ReloadDelay
			return nil, nil
		}
	}
	g.phase = PhaseFailed
	return nil, nil
}

func (g *Gate) canReloadLocked() bool {
	if g.cfg.Reload == nil || g.cfg.Marker == nil || g.cfg.MarkerKey == "" {
		return false
	}
	v, _ := g.cfg.Marker.Get(g.cfg.MarkerKey)
	return v != reloadedValue
}

// Retry is the error screen's retry button. It clears the marker and reloads
// the page, which gives the next load a fresh automatic reload.
func (g *Gate) Retry() {
	g.mu.Lock()
	reload := g.cfg.Reload
	if g.cfg.Marker != nil && g.cfg.MarkerKey != "" {
		_ = g.cfg.Marker.Delete(g.cfg.MarkerKey)
	}
	g.mu.Unlock()
	if reload != nil {
		reload()
	}
}

// Snapshot returns the current state.
func (g *Gate) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, _ := g.snapshotLocked()
	return s
}

// Phase returns the current phase.
func (g *Gate) Phase() Phase {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.phase
}

// Ready reports whether every task finished.
func (g *Gate) Ready() bool { return g.Phase() == PhaseReady }

// Progress returns the weighted progress in [0,1]. It never goes down, even
// when a task is added or restarted, so a progress bar does not jump back.
func (g *Gate) Progress() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.progressLocked()
}

func (g *Gate) progressLocked() float64 {
	var sum, weight float64
	for _, t := range g.tasks {
		sum += t.fraction() * t.Weight
		weight += t.Weight
	}
	p := 0.0
	if weight > 0 {
		p = sum / weight
	}
	if g.phase == PhaseReady {
		p = 1
	}
	if p > g.best {
		g.best = p
	}
	return g.best
}

func (g *Gate) snapshotLocked() (Snapshot, bool) {
	s := Snapshot{Phase: g.phase, Progress: g.progressLocked(), Attempt: g.attempt, SoftRetries: g.soft}
	for _, t := range g.tasks {
		s.Tasks = append(s.Tasks, TaskStatus{Name: t.Name, Progress: t.fraction(), Done: t.complete || (t.total > 0 && t.done >= t.total), Err: t.err})
		if s.Err == nil && t.err != nil {
			s.Err = t.err
		}
	}
	fire := g.cfg.OnChange != nil && (!g.emitted || changed(g.lastEmit, s))
	if fire {
		g.lastEmit, g.emitted = s, true
	}
	return s, fire
}

func changed(a, b Snapshot) bool {
	if a.Phase != b.Phase || a.Progress != b.Progress || a.SoftRetries != b.SoftRetries || len(a.Tasks) != len(b.Tasks) || (a.Err == nil) != (b.Err == nil) {
		return true
	}
	for i := range a.Tasks {
		if a.Tasks[i].Done != b.Tasks[i].Done || (a.Tasks[i].Err == nil) != (b.Tasks[i].Err == nil) {
			return true
		}
	}
	return false
}

func (g *Gate) emit(s Snapshot, fire bool) {
	if fire && g.cfg.OnChange != nil {
		g.cfg.OnChange(s)
	}
}

// CountTask makes a Task from a counter, such as "models prepared" over
// "models wanted". Pass the loader's own readiness function.
func CountTask(name string, weight float64, count func() (done, total int)) Task {
	return Task{Name: name, Weight: weight, Progress: func() (float64, float64) {
		d, t := count()
		return float64(d), float64(t)
	}}
}
