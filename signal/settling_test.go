package signal

import (
	"testing"
	"time"
)

func TestBatchReadsCurrentDerivedChainBeforeNotifications(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "set", true: "update"}[update], func(t *testing.T) {
			base := New(1)
			doubled := Derive(func() int { return 2 * base.Get() })
			defer doubled.Stop()
			result := Derive(func() int { return doubled.Get() + 1 })
			defer result.Stop()
			var seen []int
			result.Subscribe(func() { seen = append(seen, result.Get()) })
			Batch(func() {
				if update {
					base.Update(func(v int) int { return v + 1 })
				} else {
					base.Set(2)
				}
				if got := result.Get(); got != 5 {
					t.Errorf("in-batch derived chain = %d, want 5", got)
				}
				if len(seen) != 0 {
					t.Errorf("subscriber ran before the batch completed: %v", seen)
				}
			})
			if len(seen) != 1 || seen[0] != 5 {
				t.Fatalf("notifications = %v, want [5]", seen)
			}
		})
	}
}

func TestSourceObserverReadsCurrentDerivedRegardlessOfSubscriptionOrder(t *testing.T) {
	base := New(1)
	var doubled *Computed[int]
	var seen int
	base.Subscribe(func() { seen = base.Get() + doubled.Get() })
	doubled = Derive(func() int { return 2 * base.Get() })
	defer doubled.Stop()
	base.Set(2)
	if seen != 6 {
		t.Fatalf("observer saw source plus stale derived = %d, want 6", seen)
	}
}

func TestDerivedDiamondPublishesOnceForIncomparableValues(t *testing.T) {
	base := New(1)
	var result *Computed[[]int]
	var fromSource int
	base.Subscribe(func() { fromSource = result.Get()[0] })
	left := Derive(func() int { return base.Get() * 2 })
	defer left.Stop()
	right := Derive(func() int { return base.Get() * 3 })
	defer right.Stop()
	result = Derive(func() []int { return []int{left.Get() + right.Get()} })
	defer result.Stop()
	var seen []int
	result.Subscribe(func() { seen = append(seen, result.Get()[0]) })
	base.Set(2)
	if fromSource != 10 || len(seen) != 1 || seen[0] != 10 {
		t.Fatalf("diamond did not settle once: source observer=%d, computed notifications=%v", fromSource, seen)
	}
}

func TestBatchExplicitDerivedReadsDoNotRepublishRetiredNotifications(t *testing.T) {
	base := New(1)
	first := Derive(func() []int { return []int{base.Get() * 2} })
	defer first.Stop()
	last := Derive(func() []int { return []int{first.Get()[0] + 1} })
	defer last.Stop()
	var seen []int
	last.Subscribe(func() { seen = append(seen, last.Get()[0]) })
	Batch(func() {
		base.Set(2)
		if got := last.Get()[0]; got != 5 {
			t.Errorf("derived read=%d, want 5", got)
		}
		if len(seen) != 0 {
			t.Errorf("notifications ran inside batch: %v", seen)
		}
	})
	if len(seen) != 1 || seen[0] != 5 {
		t.Fatalf("retired dependency callback republished a computed slice: %v", seen)
	}
}

func TestDependentSubscribedDuringRefreshReceivesPublication(t *testing.T) {
	base := New(1)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	parent := Derive(func() int {
		value := base.Get()
		if value == 2 {
			close(started)
			<-release
		}
		return 2 * value
	})
	defer parent.Stop()
	parent.Subscribe(func() {})
	go func() {
		defer close(finished)
		base.Set(2)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("parent did not begin refreshing")
	}
	child := Derive(func() int { return parent.Get() + 1 })
	defer child.Stop()
	var seen []int
	child.Subscribe(func() { seen = append(seen, child.Get()) })
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("parent publication did not finish")
	}
	if got := child.Get(); got != 5 || len(seen) != 1 || seen[0] != 5 {
		t.Fatalf("new dependent missed the publication: value=%d notifications=%v", got, seen)
	}
}

func TestComputedRefreshCanRecoverFromComputationPanic(t *testing.T) {
	base := New(1)
	panicOnce := true
	result := Derive(func() int {
		value := base.Get()
		if value == 2 && panicOnce {
			panicOnce = false
			panic("compute failed")
		}
		return 2 * value
	})
	defer result.Stop()
	var seen []int
	result.Subscribe(func() { seen = append(seen, result.Get()) })
	var caught any
	func() {
		defer func() { caught = recover() }()
		base.Set(2)
	}()
	if caught != "compute failed" {
		t.Fatalf("computation panic = %v", caught)
	}
	base.Set(3)
	if got := result.Get(); got != 6 || len(seen) != 1 || seen[0] != 6 {
		t.Fatalf("failed computation poisoned later updates: value=%d notifications=%v", got, seen)
	}
}

func TestDerivedSettlingKeepsRawPulseNotifications(t *testing.T) {
	base := NewWithEqual(1, nil)
	derived := Derive(func() []int { return []int{base.Get()} })
	defer derived.Stop()
	rawCalls, derivedCalls := 0, 0
	base.Subscribe(func() { rawCalls++ })
	derived.Subscribe(func() { derivedCalls++ })
	Batch(func() {
		base.Set(1)
		base.Set(1)
		base.Set(1)
		if rawCalls != 0 || derivedCalls != 0 {
			t.Fatal("observers ran inside the batch")
		}
	})
	if rawCalls != 3 || derivedCalls != 1 {
		t.Fatalf("raw pulses=%d, settled derived publications=%d, want 3 and 1", rawCalls, derivedCalls)
	}
}

func TestUnchangedDerivedBoundaryKeepsDependentsCached(t *testing.T) {
	base := New(1)
	bucket := Derive(func() int { return base.Get() / 10 })
	defer bucket.Stop()
	computations := 0
	leaf := Derive(func() int {
		computations++
		return 2 * bucket.Get()
	})
	defer leaf.Stop()
	leaf.Subscribe(func() {})
	base.Set(2)
	if got := leaf.Get(); got != 0 || computations != 1 {
		t.Fatalf("unchanged boundary recomputed its dependent: value=%d computations=%d", got, computations)
	}
	Batch(func() {
		base.Set(3)
		if got := leaf.Get(); got != 0 || computations != 1 {
			t.Errorf("in-batch equality boundary was lost: value=%d computations=%d", got, computations)
		}
	})
}

func TestDerivedObserverReadsItsOwnDependencyWrite(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "batch"}[batch], func(t *testing.T) {
			base := New(1)
			computations := 0
			result := Derive(func() int {
				computations++
				return 2 * base.Get()
			})
			defer result.Stop()
			changed, seen, notifications := false, 0, 0
			result.Subscribe(func() {
				notifications++
				if changed {
					return
				}
				changed = true
				writeAndRead := func() {
					base.Set(3)
					seen = result.Get()
				}
				if batch {
					Batch(writeAndRead)
				} else {
					writeAndRead()
				}
			})
			base.Set(2)
			if seen != 6 || result.Get() != 6 || computations != 3 || notifications != 2 {
				t.Fatalf("reentrant publication: read=%d value=%d computations=%d notifications=%d", seen, result.Get(), computations, notifications)
			}
		})
	}
}

func TestComputedEqualityPanicDoesNotPoisonRefresh(t *testing.T) {
	type payload struct{ value any }
	base := New(1)
	result := Derive(func() payload {
		value := base.Get()
		if value < 3 {
			return payload{[]int{value}}
		}
		return payload{value}
	})
	defer result.Stop()
	result.Subscribe(func() {})
	var caught any
	func() {
		defer func() { caught = recover() }()
		base.Set(2)
	}()
	if caught == nil {
		t.Fatal("expected equality of dynamically incomparable values to panic")
	}
	base.Set(3)
	if got := result.Get().value; got != 3 {
		t.Fatalf("equality panic poisoned the next refresh: got %v, want 3", got)
	}
}
