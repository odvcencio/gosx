package main

import (
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeBuildWorkersBoundCoverageAndErrors(t *testing.T) {
	for _, limit := range []int{1, 2, 8} {
		t.Run(string(rune('0'+limit)), func(t *testing.T) {
			previous := runtime.GOMAXPROCS(limit)
			defer runtime.GOMAXPROCS(previous)
			started := make(chan int, 5)
			release := make(chan struct{})
			var active, peak atomic.Int32
			var calls [5]atomic.Int32
			fault := errors.New("compiler fixture failed")
			tasks := make([]func() error, 5)
			for i := range tasks {
				tasks[i] = func() error {
					calls[i].Add(1)
					n := active.Add(1)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
					started <- i
					<-release
					active.Add(-1)
					if i == 2 {
						return fault
					}
					return nil
				}
			}
			done := make(chan []error, 1)
			go func() { done <- runRuntimeBuildTasks(tasks) }()
			for range min(limit, 5) {
				<-started
			}
			if limit < len(tasks) {
				select {
				case extra := <-started:
					t.Errorf("variant %d started while all %d workers were occupied", extra, limit)
				case <-time.After(20 * time.Millisecond):
				}
			}
			if got := active.Load(); got != int32(min(limit, 5)) {
				t.Errorf("active compilers=%d, want %d", got, min(limit, 5))
			}
			close(release)
			results := <-done
			if got := peak.Load(); got != int32(min(limit, 5)) {
				t.Errorf("peak compilers=%d, want %d", got, min(limit, 5))
			}
			for i, err := range results {
				if calls[i].Load() != 1 {
					t.Errorf("variant %d compiled %d times", i, calls[i].Load())
				}
				if (i == 2 && !errors.Is(err, fault)) || (i != 2 && err != nil) {
					t.Errorf("variant %d error=%v", i, err)
				}
			}
		})
	}
}

func TestRuntimeBuildWorkersEmpty(t *testing.T) {
	if result := runRuntimeBuildTasks(nil); len(result) != 0 {
		t.Fatalf("empty task results=%v", result)
	}
}
