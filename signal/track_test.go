package signal

import (
	"fmt"
	"sync"
	"testing"
)

func TestTrackingRetainsIndependentOrderedDependencies(t *testing.T) {
	for _, count := range []int{0, 1, 7, 8, 9, 64, 65, 129} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			sources := make([]*Signal[int], count)
			for i := range sources {
				sources[i] = New(i)
			}
			_, deps := trackDependencies(func() int {
				for _, s := range sources {
					s.Get()
					s.Set(s.Get() + 1)
					s.Get()
				}
				for i := len(sources) - 1; i >= 0; i-- {
					sources[i].Get()
				}
				return 0
			})
			// Later windows may reuse scratch, but must not overwrite the
			// caller's dependency identities or the first revision it read.
			for range 3 {
				trackDependencies(func() int { return New(100).Get() })
			}
			if len(deps) != count {
				t.Fatalf("got %d dependencies, want %d", len(deps), count)
			}
			for i, dep := range deps {
				if dep.source != sources[i] || dep.revision != 0 {
					t.Fatalf("dependency %d lost its identity or first revision: %+v", i, dep)
				}
			}
		})
	}
}

func TestTrackingNestedPanicRestoresOuterWindow(t *testing.T) {
	first, last, abandoned := New(1), New(2), New(3)
	_, deps := trackDependencies(func() int {
		first.Get()
		func() {
			defer func() {
				if got := recover(); got != "abandoned" {
					t.Fatalf("panic = %v", got)
				}
			}()
			trackDependencies(func() int {
				abandoned.Get()
				panic("abandoned")
			})
		}()
		return last.Get()
	})
	if len(deps) != 2 || deps[0].source != first || deps[1].source != last {
		t.Fatalf("nested panic changed the outer dependencies: %+v", deps)
	}
	_, next := trackDependencies(func() int { return abandoned.Get() })
	if len(next) != 1 || next[0].source != abandoned {
		t.Fatalf("abandoned window leaked into the next window: %+v", next)
	}
	if openTrackers.Load() != 0 {
		t.Fatal("tracking remained active after the windows closed")
	}
}

func TestTrackingOverlappingWindowsRemainIndependent(t *testing.T) {
	const workers = 8
	var ready, done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	release := make(chan struct{})
	for range workers {
		go func() {
			defer done.Done()
			source := New(1)
			_, deps := trackDependencies(func() int {
				ready.Done()
				<-release
				return source.Get()
			})
			if len(deps) != 1 || deps[0].source != source {
				t.Errorf("window captured another goroutine's dependencies: %+v", deps)
			}
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()
}

// Tracking scratch outlives computations. Its cache must not keep disposed
// signals, callbacks, or an unbounded number of nested windows alive.
func TestTrackingScratchDoesNotRetainDependencyGraphs(t *testing.T) {
	var nested func(int)
	nested = func(depth int) {
		trackDependencies(func() int {
			count := 64
			if depth%2 != 0 {
				count = 129
			}
			for i := 0; i < count; i++ {
				New(i).Get()
			}
			if depth > 0 {
				nested(depth - 1)
			}
			return 0
		})
	}
	nested(16)
	// Leave retired buffers of both supported and oversized graphs behind.
	for _, count := range []int{129, 64} {
		source := New(0)
		sources := make([]*Signal[int], count)
		for i := range sources {
			sources[i] = New(i)
		}
		computed := Derive(func() int {
			sum := source.Get()
			for _, dependency := range sources {
				sum += dependency.Get()
			}
			return sum
		})
		source.Set(1)
		computed.Get()
		computed.Stop()
	}
	trackerMu.Lock()
	defer trackerMu.Unlock()
	count := 0
	for tracker := spareTrackers; tracker != nil; tracker = tracker.parent {
		count++
		if count > 8 {
			t.Fatal("scratch cache retained too many windows or a cycle")
		}
		if len(tracker.deps) != 0 || cap(tracker.deps) > 128 || len(tracker.seen) != 0 {
			t.Fatal("scratch retained dependency identities or an oversized buffer")
		}
		for _, dep := range tracker.deps[:cap(tracker.deps)] {
			if dep.source != nil || dep.unsubscribe != nil {
				t.Fatal("scratch backing array retained a dependency graph")
			}
		}
	}
	if count != spareCount {
		t.Fatalf("scratch count = %d, advertised %d", count, spareCount)
	}
}

func TestSharedTrackingScopeCanCloseWindowsOutOfOrder(t *testing.T) {
	var deps []dependency
	outer := pushTracker(0)
	middle := pushTracker(0)
	inner := pushTracker(0)
	popTracker(outer, &deps)
	// Reusing the closed window elsewhere must not change the still-live
	// shared scope or leave its parent pointing into the scratch cache.
	reused := pushTracker(0)
	trackerMu.Lock()
	valid := currentTracker(0) == reused && reused.parent == inner && inner.parent == middle && middle.parent == nil
	trackerMu.Unlock()
	popTracker(reused, &deps)
	popTracker(middle, &deps)
	trackerMu.Lock()
	valid = valid && currentTracker(0) == inner && inner.parent == nil
	trackerMu.Unlock()
	popTracker(inner, &deps)
	if !valid {
		t.Fatal("closing the outer window lost or aliased the live inner window")
	}
	trackerMu.Lock()
	empty := currentTracker(0) == nil
	trackerMu.Unlock()
	if !empty || openTrackers.Load() != 0 {
		t.Fatal("closing shared windows left tracking active")
	}
}

func BenchmarkTrackDependencies(b *testing.B) {
	for _, count := range []int{1, 5, 16, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			sources := make([]*Signal[int], count)
			for i := range sources {
				sources[i] = New(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_, deps := trackDependencies(func() int {
					for _, s := range sources {
						s.Get()
					}
					return 0
				})
				if len(deps) != count {
					b.Fatal("lost dependencies")
				}
			}
		})
	}
}
