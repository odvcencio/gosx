package signal

import (
	"sync"
	"testing"
)

func TestComputedRetiredStorageKeepsConcurrentGraphsIndependent(t *testing.T) {
	const workers = 8
	start := make(chan struct{})
	var done sync.WaitGroup
	done.Add(workers)
	for worker := range workers {
		go func() {
			defer done.Done()
			route := New(0)
			sources := make([]*Signal[int], 16)
			for i := range sources {
				sources[i] = New(worker*100 + i)
			}
			computed := Derive(func() int {
				count := []int{1, 5, 16}[route.Get()%3]
				sum := 0
				for _, source := range sources[:count] {
					sum += source.Get()
				}
				return sum
			})
			defer computed.Stop()
			<-start
			for step := 1; step <= 40; step++ {
				route.Set(step)
				sources[0].Set(worker*100 + step)
				want := worker*100 + step
				count := []int{1, 5, 16}[step%3]
				for i := 1; i < count; i++ {
					want += worker*100 + i
				}
				if got := computed.Get(); got != want {
					t.Errorf("worker %d step %d: computed = %d, want %d", worker, step, got, want)
					return
				}
			}
		}()
	}
	close(start)
	done.Wait()
}

func TestComputedStopDuringRefreshDoesNotRecycleLiveDependencies(t *testing.T) {
	source := New(1)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	computed := Derive(func() int {
		value := source.Get()
		if value == 2 {
			close(entered)
			<-release
		}
		return value
	})
	source.Set(2)
	go func() { finished <- computed.Get() }()
	<-entered
	computed.Stop()
	// Exercise the shared cache while the stopped refresh still holds a
	// dependency snapshot and a tracking window on the other goroutine.
	for range 16 {
		other := New(10)
		probe := Derive(func() int { return other.Get() * 2 })
		other.Set(11)
		if got := probe.Get(); got != 22 {
			t.Errorf("independent probe = %d, want 22", got)
		}
		probe.Stop()
	}
	close(release)
	if got := <-finished; got != 1 {
		t.Fatalf("stopped refresh published %d, want frozen value 1", got)
	}
	source.Set(3)
	if got := computed.Get(); got != 1 {
		t.Fatalf("stopped computed = %d, want 1", got)
	}
	if source.hasSubscribers() {
		t.Fatal("stopped computed retained its subscription")
	}
}

func TestComputedStopWhileRefreshingUpstreamKeepsSnapshotReadable(t *testing.T) {
	source, other := New(1), New(5)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan int, 1)
	upstream := Derive(func() int {
		value := source.Get()
		if value == 2 {
			close(entered)
			<-release
		}
		return value
	})
	defer upstream.Stop()
	computed := Derive(func() int { return upstream.Get() + other.Get() })
	// Defer eager subscriber delivery so the worker owns the paused refresh.
	Batch(func() {
		source.Set(2)
		go func() { finished <- computed.Get() }()
		<-entered
		computed.Stop()
		for range 16 {
			probeSource := New(10)
			probe := Derive(func() int { return probeSource.Get() * 2 })
			probeSource.Set(11)
			if got := probe.Get(); got != 22 {
				t.Errorf("independent probe = %d, want 22", got)
			}
			probe.Stop()
		}
		close(release)
		if got := <-finished; got != 6 {
			t.Fatalf("stopped refresh published %d, want frozen value 6", got)
		}
		if other.hasSubscribers() {
			t.Fatal("stopped computed retained the second dependency's subscription")
		}
	})
}
