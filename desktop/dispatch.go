package desktop

import (
	"fmt"
	"sync"
)

// uiDispatcher runs functions on one owner thread. Native webview objects
// are single-threaded (WebView2 fails calls from other threads with HRESULT
// 0x802A000C), so every backend call that touches them goes through run.
//
// Calls made on the owner thread run directly. Calls from other threads are
// queued, wake is called so the owner's message loop calls drain, and the
// caller waits for the result. Before start and after stop, calls run on the
// caller's thread; the backend then has no webview and returns its usual
// "not ready" error.
type queuedCall struct {
	run  func()
	fail func(error)
}

type uiDispatcher struct {
	mu      sync.Mutex
	owner   uint32
	started bool
	closed  bool
	queue   []*queuedCall
	// wakePending is true between a successful wake and the drain that
	// takes the queue, so bursts of calls post one wake message instead of
	// one each (a full message queue would also block WM_CLOSE).
	wakePending bool

	// current returns the calling thread's ID.
	current func() uint32
	// wake asks the owner thread to call drain.
	wake func() error
}

func (d *uiDispatcher) start(owner uint32) {
	d.mu.Lock()
	d.owner = owner
	d.started = true
	d.closed = false
	d.mu.Unlock()
}

// stop runs queued calls and makes later calls run on their own thread. The
// backend calls it only after releasing its webview, so those later calls
// find no webview and never touch it off the owner thread.
func (d *uiDispatcher) stop() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.drain()
}

func (d *uiDispatcher) run(fn func() error) error {
	d.mu.Lock()
	if !d.started || d.closed || d.current() == d.owner {
		d.mu.Unlock()
		return fn()
	}
	done := make(chan error, 1)
	call := &queuedCall{run: func() { done <- fn() }, fail: func(err error) { done <- err }}
	d.queue = append(d.queue, call)
	needWake := !d.wakePending
	d.wakePending = true
	d.mu.Unlock()
	if !needWake {
		return <-done
	}
	if err := d.wake(); err != nil {
		// The owner could not be woken (for example, its message queue is
		// full). Fail every call still queued, including calls that joined
		// this wake, rather than run them off the owner thread or leave
		// them waiting. Calls the owner already took complete normally.
		err = fmt.Errorf("desktop: wake window thread: %w", err)
		d.mu.Lock()
		queue := d.queue
		d.queue = nil
		d.wakePending = false
		d.mu.Unlock()
		for _, queued := range queue {
			queued.fail(err)
		}
	}
	return <-done
}

// drain runs every queued call in order, including calls queued while it
// runs.
func (d *uiDispatcher) drain() {
	for {
		d.mu.Lock()
		queue := d.queue
		d.queue = nil
		d.wakePending = false
		d.mu.Unlock()
		if len(queue) == 0 {
			return
		}
		for _, call := range queue {
			call.run()
		}
	}
}
