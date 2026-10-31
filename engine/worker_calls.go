package engine

import (
	"encoding/json"
	"fmt"
)

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
// one per call; the engine ignores an ID it has already handled.
//
// Field names are the wire keys (no JSON tags), because island records use Go
// field names.
//
// The call signal holds either one Call (the newest call) or a JSON array of
// Calls, a mailbox of the calls still waiting for a reply. A signal that holds a
// single Call keeps only the latest value, so two writes in one island handler
// (a batch) deliver the second call alone; the engine then lists the first in
// Result.Recent with the error "overwritten". A mailbox keeps both: append each
// call to the list, with an ID above every ID in it, and the engine runs every
// call whose ID is above the last one it handled, in ID order. Calls that have
// a reply leave the list: the page host removes them for a worker or native
// engine, and the engine itself does on the main thread.
type Call[A any] struct {
	ID   uint64
	Name string
	Args A
}

// Result answers a Call on "<channel>.result". It carries the newest reply in
// full and, in Recent, the last few replies, so that overlapping calls can all
// be matched. Error is empty on success; the page writes "timeout" or
// "worker-crashed" when the worker cannot answer.
//
// A result with ID >= the ID of your call means that call has finished. If the
// IDs are equal, Value and Error are its reply; if the result is newer, look
// the call up in Recent, which holds its error and, when it fits the engine's
// byte budget (CallOptions.RecentBytes), its value.
//
// ID is not a number to count from. A reply that comes late, after the page
// wrote "timeout" for a newer call, puts ID back to an earlier call. Seen is the
// number to count from: the highest call ID that the engine has accepted or the
// page has watched, written by both of them and never lower than a Seen the
// channel has already published. Number the next call above Seen and above every
// ID in your mailbox. The engine and the page both ignore an ID at or below the
// highest they have seen, so a call that reuses one never runs and never times
// out.
type Result[R any] struct {
	ID     uint64
	Name   string
	Error  string
	Value  R
	Recent []ResultRef
	Seen   uint64
}

// ResultRef is one earlier reply, kept in Result.Recent: its ID, its error and,
// while the engine's byte budget allows, the JSON of its value. A reply whose
// value did not fit has no Value key; a client treats such a reply as lost,
// unless it has an Error. The wire key stays the field name; omitempty only
// leaves the key out.
//
// Kept is true when Value is there. An island decodes Recent into typed records,
// where a key that is missing decodes as the zero value, so a lost reply and a
// successful reply whose value is zero look the same in Value. Kept tells them
// apart: Error is empty and Kept is false for a lost reply, and Kept is true for
// a reply that arrived. The engine writes Kept in every entry. The page, which
// writes entries for the errors it makes ("timeout", "worker-crashed"), leaves
// it out; those entries have an Error, so none of them is lost.
type ResultRef struct {
	ID    uint64
	Error string
	Kept  bool
	Value json.RawMessage `json:",omitempty"`
}
