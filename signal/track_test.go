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
