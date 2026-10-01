package desktop

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// goroutineID returns the current goroutine's ID from its stack header. The
// fake owner uses it as a thread ID, so each goroutine has its own identity.
func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	id, _ := strconv.ParseUint(fields[1], 10, 64)
	return id
}

// fakeOwner simulates a window thread: one goroutine that drains the
// dispatcher whenever it is woken.
type fakeOwner struct {
	d      *uiDispatcher
	wakeCh chan struct{}
	owner  atomic.Uint64
}

func (o *fakeOwner) onOwner() bool { return goroutineID() == o.owner.Load() }

func newFakeOwner() *fakeOwner {
	o := &fakeOwner{wakeCh: make(chan struct{}, 64)}
	o.d = &uiDispatcher{
		current: func() uint32 {
			if o.onOwner() {
				return 1
			}
			return 2
		},
		wake: func() error { o.wakeCh <- struct{}{}; return nil },
	}
	ready := make(chan struct{})
	go func() {
		o.owner.Store(goroutineID())
		close(ready)
		for range o.wakeCh {
			o.d.drain()
		}
	}()
	<-ready
	o.d.start(1)
	return o
}

func TestUIDispatcherRunsOffThreadCallsOnOwner(t *testing.T) {
	o := newFakeOwner()
	defer close(o.wakeCh)
	var ranOnUI bool
	err := o.d.run(func() error {
		ranOnUI = o.onOwner()
		return errors.New("from owner")
	})
	if err == nil || err.Error() != "from owner" {
		t.Fatalf("run error = %v", err)
	}
	if !ranOnUI {
		t.Fatal("call did not run on the owner thread")
	}
}

func TestUIDispatcherRunsOwnerCallsDirectly(t *testing.T) {
	woke := false
	d := &uiDispatcher{current: func() uint32 { return 7 }, wake: func() error { woke = true; return nil }}
	d.start(7)
	calls := 0
	if err := d.run(func() error { calls++; return nil }); err != nil || calls != 1 || woke {
		t.Fatalf("owner call: err=%v calls=%d woke=%v", err, calls, woke)
	}
}

func TestUIDispatcherBeforeStartAndAfterStopRunsOnCaller(t *testing.T) {
	d := &uiDispatcher{current: func() uint32 { return 2 }, wake: func() error { t.Fatal("wake called"); return nil }}
	calls := 0
	_ = d.run(func() error { calls++; return nil })
	d.start(1)
	d.stop()
	_ = d.run(func() error { calls++; return nil })
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestUIDispatcherStopRunsQueuedCalls(t *testing.T) {
	d := &uiDispatcher{current: func() uint32 { return 2 }, wake: func() error { return nil }}
	d.start(1)
	result := make(chan error, 1)
	go func() { result <- d.run(func() error { return errors.New("ran") }) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.mu.Lock()
		queued := len(d.queue)
		d.mu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("call was not queued")
		}
		time.Sleep(time.Millisecond)
	}
	d.stop()
	select {
	case err := <-result:
		if err == nil || err.Error() != "ran" {
			t.Fatalf("queued call result = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued call still waiting after stop")
	}
}

func TestUIDispatcherWakeFailureReturnsErrorWithoutRunning(t *testing.T) {
	d := &uiDispatcher{current: func() uint32 { return 2 }, wake: func() error { return errors.New("queue full") }}
	d.start(1)
	calls := 0
	err := d.run(func() error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatalf("wake failure: err=%v calls=%d; want an error and no off-thread call", err, calls)
	}
	d.mu.Lock()
	queued := len(d.queue)
	d.mu.Unlock()
	if queued != 0 {
		t.Fatalf("failed call left %d queued entries", queued)
	}
}

func TestUIDispatcherConcurrentCallersAllRunOnOwner(t *testing.T) {
	o := newFakeOwner()
	defer close(o.wakeCh)
	var mu sync.Mutex
	var seen []int
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = o.d.run(func() error {
				if !o.onOwner() {
					t.Error("call ran off the owner thread")
				}
				mu.Lock()
				seen = append(seen, i)
				mu.Unlock()
				return nil
			})
		}(i)
	}
	wg.Wait()
	if len(seen) != 50 {
		t.Fatalf("ran %d calls, want 50", len(seen))
	}
}

func TestUIDispatcherCoalescesWakes(t *testing.T) {
	var wakes atomic.Int32
	d := &uiDispatcher{current: func() uint32 { return 2 }, wake: func() error { wakes.Add(1); return nil }}
	d.start(1)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = d.run(func() error { return nil }) }()
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.mu.Lock()
		queued := len(d.queue)
		d.mu.Unlock()
		if queued == 20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls queued", queued)
		}
		time.Sleep(time.Millisecond)
	}
	if got := wakes.Load(); got != 1 {
		t.Fatalf("20 queued calls posted %d wakes, want 1", got)
	}
	d.drain()
	wg.Wait()
	// After a drain, the next call must wake the owner again.
	result := make(chan error, 1)
	go func() { result <- d.run(func() error { return nil }) }()
	for wakes.Load() != 2 {
		if time.Now().After(deadline.Add(2 * time.Second)) {
			t.Fatalf("call after drain posted no wake (wakes=%d)", wakes.Load())
		}
		time.Sleep(time.Millisecond)
	}
	d.drain()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestUIDispatcherWakeFailureFailsJoinedCalls(t *testing.T) {
	release := make(chan struct{})
	var wakes atomic.Int32
	d := &uiDispatcher{current: func() uint32 { return 2 }, wake: func() error {
		wakes.Add(1)
		<-release // hold the first wake open while others join it
		return errors.New("queue full")
	}}
	d.start(1)
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { results <- d.run(func() error { t.Error("call ran despite failed wake"); return nil }) }()
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.mu.Lock()
		queued := len(d.queue)
		d.mu.Unlock()
		if queued == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls queued", queued)
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for i := 0; i < 3; i++ {
		select {
		case err := <-results:
			if err == nil {
				t.Fatal("joined call returned nil after a failed wake")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a joined call is still blocked after the wake failed")
		}
	}
	if got := wakes.Load(); got != 1 {
		t.Fatalf("wakes = %d, want 1", got)
	}
}
