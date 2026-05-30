package scheduled

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// RunID uniquely identifies a single execution of a task.
type RunID string

// runCounter is used to generate unique RunIDs.
var runCounter uint64

// newRunID returns a unique RunID. The now function is used as part of the ID
// so tests can control the time component.
func newRunID(name string, now func() time.Time) RunID {
	count := atomic.AddUint64(&runCounter, 1)
	ts := now().UTC().UnixNano()
	return RunID(fmt.Sprintf("%s-%d-%d", name, ts, count))
}

// TickHandle is passed to a task's Fn so it can report progress and query
// execution metadata.
type TickHandle interface {
	// Progress records a progress message and updates the last-progress timestamp.
	Progress(message string)
	// Attempt returns the current 1-based attempt number.
	Attempt() int
	// Metadata returns the task's metadata map (read-only by convention).
	Metadata() map[string]string
}

// runRecord holds the mutable state for a single run, updated by tickHandle.
type runRecord struct {
	lastProgressAt time.Time
	lastProgress   string
	attempt        int
}

// tickHandle is the concrete TickHandle passed to task functions.
type tickHandle struct {
	run      *runRecord
	now      func() time.Time
	metadata map[string]string
}

// Progress updates the run's last-progress message and timestamp using the
// injected clock.
func (h *tickHandle) Progress(message string) {
	h.run.lastProgress = message
	h.run.lastProgressAt = h.now()
}

// Attempt returns the 1-based attempt number for this run.
func (h *tickHandle) Attempt() int {
	return h.run.attempt
}

// Metadata returns the task's metadata map.
func (h *tickHandle) Metadata() map[string]string {
	return h.metadata
}

// newTickHandle constructs a TickHandle for the given attempt, backed by a
// new runRecord and the provided clock function.
func newTickHandle(attempt int, now func() time.Time, metadata map[string]string) (*tickHandle, *runRecord) {
	rec := &runRecord{
		attempt:        attempt,
		lastProgressAt: now(),
	}
	h := &tickHandle{
		run:      rec,
		now:      now,
		metadata: metadata,
	}
	return h, rec
}

// Task describes a schedulable unit of work.
type Task struct {
	// Name is a unique identifier for the task.
	Name string
	// Schedule determines when the task fires.
	Schedule Schedule
	// Timeout is the hard deadline for a single run; 0 means no limit.
	Timeout time.Duration
	// ProgressTimeout triggers a stall verdict if no Progress call is made
	// within this duration; 0 disables stall detection.
	ProgressTimeout time.Duration
	// MaxAttempts is the maximum number of attempts before giving up; 0 means infinite.
	MaxAttempts int
	// Backoff controls retry delay; nil defaults to DefaultBackoff().
	Backoff BackoffPolicy
	// Fn is the function to execute. It receives a context and a TickHandle.
	Fn func(ctx context.Context, tick TickHandle) error
	// Metadata is arbitrary key/value data attached to the task.
	Metadata map[string]string
}

// effectiveBackoff returns the task's BackoffPolicy, defaulting to DefaultBackoff().
func (t *Task) effectiveBackoff() BackoffPolicy {
	if t.Backoff != nil {
		return t.Backoff
	}
	return DefaultBackoff()
}
