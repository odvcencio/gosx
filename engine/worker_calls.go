package engine

import "fmt"

// WorkerMode says what GoSX does when a worker-thread engine cannot start.
type WorkerMode string

const (
	// WorkerPreferred runs the engine in a worker and falls back to the main
	// thread when the worker cannot start. It is the default.
	WorkerPreferred WorkerMode = ""
	// WorkerRequired never runs the engine on the main thread. When the worker
	// cannot start, the engine does not run and its status signal reports
	// StateUnsupported with the reason code.
	WorkerRequired WorkerMode = "required"
)

// WorkerModeSupported reports whether mode is understood by the bootstrap.
func WorkerModeSupported(mode WorkerMode) bool {
	return mode == WorkerPreferred || mode == WorkerRequired
}

// ValidateWorkerMode checks the worker policy against the thread. The policy
// and the forced-fallback flag only mean something for a worker-thread engine.
func ValidateWorkerMode(mode WorkerMode, forceFallback bool, thread Thread) error {
	if !WorkerModeSupported(mode) {
		return fmt.Errorf("unsupported engine worker mode: %q", mode)
	}
	if (mode != WorkerPreferred || forceFallback) && thread != ThreadWorker {
		return fmt.Errorf("engine worker mode and ForceWorkerFallback require thread %q", ThreadWorker)
	}
	return nil
}

// Worker states written to the page status signal "gosx.engine.<engine id>".
const (
	// StateBooting means the worker is loading the Go module.
	StateBooting = "booting"
	// StateRunning means the engine is mounted, in a worker or, after a
	// fallback, on the main thread. WorkerStatus.Thread says which.
	StateRunning = "running"
	// StateUnsupported means a WorkerRequired engine could not start a worker.
	StateUnsupported = "unsupported"
	// StateCrashed means the worker died after it mounted.
	StateCrashed = "crashed"
	// StateDisposed means the page ended the engine.
	StateDisposed = "disposed"
)

// WorkerStatus is the value of the page signal "gosx.engine.<engine id>" for a
// worker-thread engine. An island reads it with signal.NewShared to show or
// disable its controls.
//
// Field names are the wire keys: island records are keyed by Go field name, so
// there are no JSON tags. Thread is "worker", "native" (an engine hosted by a desktop app) or "main" (after a fallback).
// Reason is a machine-readable code such as "worker-disabled" or
// "worker-crashed", and is empty while the engine is healthy.
type WorkerStatus struct {
	State  string
	Thread string
	Reason string
}

// Call is one request from an island (or any page code) to a worker engine
// through a shared signal. The island numbers calls from 1 and increases ID by
// one per call; the worker ignores an ID it has already handled.
//
// Field names are the wire keys (no JSON tags), because island records use Go
// field names. Write the call signal once per island handler: two writes in one
// handler are batched and only the last reaches the worker.
type Call[A any] struct {
	ID   uint64
	Name string
	Args A
}

// Result answers a Call on "<channel>.result". It carries the newest reply in
// full and, in Recent, the ID and error of the last few replies so that two
// overlapping calls can both be matched. Error is empty on success; the page
// writes "timeout" or "worker-crashed" when the worker cannot answer.
//
// A result with ID >= the ID of your call means that call has finished. If the
// IDs are equal, Value and Error are its reply; if the result is newer, look
// the call up in Recent.
type Result[R any] struct {
	ID     uint64
	Name   string
	Error  string
	Value  R
	Recent []ResultRef
}

// ResultRef is the ID and error of one earlier reply, kept in Result.Recent.
type ResultRef struct {
	ID    uint64
	Error string
}
