package scheduled

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStopContextRetainsIgnoringRun(t *testing.T) {
	s := New(Options{Logger: discardLogger()})
	entered, release, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	if err := s.Register(Task{Name: "blocked", MaxAttempts: 1, Fn: func(ctx context.Context, _ TickHandle) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release // deliberately ignores cancellation while unwinding
		return ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue("blocked", nil); err != nil {
		t.Fatal(err)
	}
	<-entered
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s.StopContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopContext: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("caller waited for an ignoring task")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("task was not cancelled")
	}
	for i := 0; i < 100; i++ {
		if err := s.StopContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("repeat StopContext: %v", err)
		}
	}
	if got := runtime.NumGoroutine(); got > before+2 {
		t.Fatalf("repeated stops created waiters: before=%d after=%d", before, got)
	}
	if _, err := s.Enqueue("blocked", nil); !errors.Is(err, ErrStopped) {
		t.Fatalf("enqueue after stop: %v", err)
	}
	s.Start(context.Background())
	select {
	case <-s.stopDone:
		t.Fatal("unfinished task lost ownership")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	finish, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	if err := s.StopContext(finish); err != nil {
		t.Fatalf("final stop: %v", err)
	}
}

func TestStopContextGracefulConcurrentAdmission(t *testing.T) {
	for iteration := 0; iteration < 20; iteration++ {
		s := New(Options{Logger: discardLogger()})
		var accepted, completed atomic.Int64
		if err := s.Register(Task{Name: "work", Schedule: Interval(time.Hour), Fn: func(context.Context, TickHandle) error {
			completed.Add(1)
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				s.Start(context.Background())
				for n := 0; n < 10; n++ {
					if _, err := s.Enqueue("work", nil); err == nil {
						accepted.Add(1)
					} else if !errors.Is(err, ErrStopped) {
						t.Errorf("enqueue: %v", err)
					}
				}
			}()
		}
		close(start)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := s.StopContext(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
		wg.Wait()
		if accepted.Load() != completed.Load() {
			t.Fatalf("accepted=%d completed=%d", accepted.Load(), completed.Load())
		}
		if err := s.Register(Task{Name: "late", Fn: func(context.Context, TickHandle) error { return nil }}); !errors.Is(err, ErrStopped) {
			t.Fatalf("register after stop: %v", err)
		}
	}
}

func TestStopContextCancelsAcceptedRunBeforePublication(t *testing.T) {
	// Enqueue reserves work before computing its ID. Stop must retain that
	// reservation, and cancellation must reach the run after publication.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	s := New(Options{Logger: discardLogger(), Now: func() time.Time {
		once.Do(func() { close(entered); <-release })
		return time.Now()
	}})
	observed := make(chan error, 1)
	if err := s.Register(Task{Name: "accepted", MaxAttempts: 1, Fn: func(ctx context.Context, _ TickHandle) error {
		observed <- ctx.Err()
		return ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	enqueued := make(chan error, 1)
	go func() { _, err := s.Enqueue("accepted", nil); enqueued <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.StopContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-s.stopDone:
		t.Fatal("accepted work was released before publication")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-enqueued; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-observed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("accepted task context: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("accepted run never finished")
	}
	if err := s.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStopCompatibilityWrapperIsBounded(t *testing.T) {
	s := New(Options{Logger: discardLogger()})
	entered, release := make(chan struct{}), make(chan struct{})
	if err := s.Register(Task{Name: "ignore", Fn: func(context.Context, TickHandle) error {
		close(entered)
		<-release
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue("ignore", nil); err != nil {
		t.Fatal(err)
	}
	<-entered
	start := time.Now()
	s.Stop(10 * time.Millisecond)
	close(release)
	if time.Since(start) > time.Second {
		t.Fatal("legacy wrapper waited without a bound")
	}
	if err := s.StopContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
