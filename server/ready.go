package server

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// ReadyCheck validates whether the app is ready to serve traffic.
type ReadyCheck interface {
	CheckReady(context.Context) error
}

// ReadyCheckFunc adapts a function into a readiness check.
type ReadyCheckFunc func(context.Context) error

// CheckReady runs the readiness check.
func (fn ReadyCheckFunc) CheckReady(ctx context.Context) error {
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

type namedReadyCheck struct {
	name         string
	snapshotName string
	check        ReadyCheck
	panicked     atomic.Bool
}

var errReadyCheckPanicked = errors.New("readiness_callback_panic")

func (entry *namedReadyCheck) evaluate(ctx context.Context) (err error) {
	if entry.panicked.Load() {
		return errReadyCheckPanicked
	}
	defer func() {
		if recover() != nil {
			if entry.panicked.CompareAndSwap(false, true) {
				log.Print("[gosx] readiness_callback_panic")
			}
			err = errReadyCheckPanicked
		}
	}()
	return entry.check.CheckReady(ctx)
}

const maxReadinessSnapshotChecks = 64

// ReadySnapshotCheck is a cached readiness result without request or error data.
// Name is copied at registration and contains at most 128 UTF-8 bytes. Invalid
// names are represented by "other".
type ReadySnapshotCheck struct {
	Name string
	OK   bool
}

// ReadinessSnapshot describes the last completed readiness probe. Known is false
// until a probe runs. Checks contains at most 64 entries; Complete is false when
// further checks were omitted. OK includes the result of every evaluated check,
// including those omitted from Checks. EvaluatedAt is the probe completion in UTC.
type ReadinessSnapshot struct {
	Known, OK   bool
	Complete    bool
	EvaluatedAt time.Time
	Checks      []ReadySnapshotCheck
}

type readinessState struct {
	mu          sync.RWMutex
	known, ok   bool
	complete    bool
	evaluatedAt time.Time
	count       int
	checks      [maxReadinessSnapshotChecks]ReadySnapshotCheck
}

// LastReadiness returns a copied, bounded snapshot without running checks or
// creating a scheduler. The caller owns the returned Checks slice.
func (a *App) LastReadiness() ReadinessSnapshot {
	if a == nil {
		return ReadinessSnapshot{}
	}
	state := &a.readiness
	state.mu.RLock()
	defer state.mu.RUnlock()
	snapshot := ReadinessSnapshot{
		Known: state.known, OK: state.ok, Complete: state.complete,
		EvaluatedAt: state.evaluatedAt,
	}
	if state.count != 0 {
		snapshot.Checks = append([]ReadySnapshotCheck(nil), state.checks[:state.count]...)
	}
	return snapshot
}

func (a *App) cacheReadiness(ok bool, checks []ReadinessCheckResult) {
	state := &a.readiness
	state.mu.Lock()
	defer state.mu.Unlock()
	clear(state.checks[:])
	state.known, state.ok = true, ok
	state.complete = len(checks) <= maxReadinessSnapshotChecks
	state.evaluatedAt = time.Now().UTC()
	state.count = min(len(checks), maxReadinessSnapshotChecks)
	// Results follow the registered non-nil checks in order. Their snapshot names
	// are already bounded copies; do not retain names from the public report.
	i := 0
	for _, entry := range a.readyChecks {
		if i == state.count {
			break
		}
		if entry.check != nil {
			state.checks[i] = ReadySnapshotCheck{Name: entry.snapshotName, OK: checks[i].OK}
			i++
		}
	}
}

func readinessSnapshotName(name string) string {
	if len(name) > 128 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "other"
	}
	return strings.Clone(name)
}

// ReadinessCheckResult reports the result of one named readiness check.
type ReadinessCheckResult struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ReadinessReport is the JSON shape served by `/readyz`.
type ReadinessReport struct {
	RequestID string                 `json:"requestID,omitempty"`
	OK        bool                   `json:"ok"`
	Checks    []ReadinessCheckResult `json:"checks,omitempty"`
}

func normalizeReadyCheckName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	return name
}
