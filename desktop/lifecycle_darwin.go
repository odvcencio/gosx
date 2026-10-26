//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"runtime"
)

// AppKit and WebKit are main-thread frameworks: NSApplication, NSWindow and
// WKWebView may only be created and driven from the process's first thread. Go
// starts main on that thread but is free to move it. Calling LockOSThread from
// an init function pins the main goroutine to the thread it started on for the
// life of the program ("the main thread", runtime.LockOSThread's documented
// recipe for GUI toolkits), so App.Run called from main runs there, pumps the
// AppKit event loop itself, and every other goroutine reaches the window through
// the uiDispatcher (ui_thread_darwin.go).
//
// The lock has a cost for programs that import this package but never open a
// window: main's wakeups after a block hand off to its thread, a few
// microseconds each. It is the price of owning the main thread without cgo.
func init() {
	runtime.LockOSThread()
}

// errNotMainThread reports that App.Run was called off the main thread.
func errNotMainThread() error {
	return fmt.Errorf("%w: the darwin desktop backend must run on the process's main thread; call App.Run from the main goroutine, "+
		"and do not call runtime.UnlockOSThread from an init function or before App.Run", ErrUnsupported)
}

// Process-directed signals and threads the Go runtime does not own.
//
// A signal sent to the process (SIGCHLD when the sidecar exits, SIGTERM from a
// terminal, a profiling tick) may be delivered to any thread that does not block
// it. AppKit starts threads of its own (com.apple.NSEventThread), WebKit starts
// IPC and networking threads, and the open panel's XPC service starts more; none
// of them has a Go g. Go's handler, run on such a thread, finds no g and, in a
// binary built without cgo, prints "fatal: bad g in signal handler" and exits
// (badsignal). The kernel normally offers a process-directed signal to the
// oldest thread first, which is the Go main thread, and Go's handlers run with
// every signal masked, so a burst can find each Go thread inside its handler and
// fall through to a later, foreign thread. Under Rosetta, where the handlers are
// slower, a burst of a few hundred SIGCHLD does it within seconds.
//
// The fix is the standard one for threads a runtime does not own: a thread
// inherits its creator's signal mask, so Run blocks the asynchronous signals on
// the main thread before AppKit starts any thread, every thread that AppKit and
// WebKit create from here on starts with them blocked, and process-directed
// signals can only reach threads the Go runtime made. Go's own threads set their
// masks when they start and take the signals as before. The main thread stops
// taking asynchronous signals itself while Run runs, which costs it only Go's
// asynchronous preemption signal (cooperative preemption still works) and
// profiling signals, and Run restores its mask on return.

// blockedAsyncSignals is every signal that can be delivered to any thread of the
// process, except the synchronous faults, which must still reach their handler to
// be reported. SIGKILL and SIGSTOP cannot be blocked. It is the set Tymbal's Core
// Audio backend blocks on the render thread.
const blockedAsyncSignals = ^uint32(0) &^ (1<<(4-1) | // SIGILL
	1<<(5-1) | // SIGTRAP
	1<<(6-1) | // SIGABRT
	1<<(8-1) | // SIGFPE
	1<<(9-1) | // SIGKILL
	1<<(10-1) | // SIGBUS
	1<<(11-1) | // SIGSEGV
	1<<(12-1) | // SIGSYS
	1<<(17-1)) // SIGSTOP

const (
	sigBlock   = 1
	sigSetmask = 3
)

// blockAsyncSignalsOnThread blocks the asynchronous signals on the calling
// thread, which must be locked to its OS thread, and returns a function that
// restores the mask it had.
func blockAsyncSignalsOnThread() (restore func()) {
	set, old := blockedAsyncSignals, uint32(0)
	if pthreadSigmask(sigBlock, &set, &old) != 0 {
		return func() {}
	}
	return func() { pthreadSigmask(sigSetmask, &old, nil) }
}
