package democtl

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock is a controllable clock for deterministic tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock {
	return &fakeClock{now: t}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// TestLimiterAllowsBurstUpToCapacity verifies that a bucket holds exactly
// capacity tokens and refuses the (capacity+1)th call.
func TestLimiterAllowsBurstUpToCapacity(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(10, 5, WithClock(clk))

	for i := 0; i < 5; i++ {
		if !l.Allow("ip1") {
			t.Fatalf("call %d: expected Allow to return true (burst available)", i+1)
		}
	}
	if l.Allow("ip1") {
		t.Fatal("6th call: expected Allow to return false (burst exhausted)")
	}
}

// TestLimiterRefillsOverTime verifies lazy token refill based on elapsed time.
func TestLimiterRefillsOverTime(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(10, 5, WithClock(clk))

	// Exhaust the 5-token burst.
	for i := 0; i < 5; i++ {
		l.Allow("ip1")
	}

	// Advance 200ms → 10 tok/sec * 0.2s = 2 tokens refilled; 1 consumed → 1 left.
	clk.advance(200 * time.Millisecond)
	if !l.Allow("ip1") {
		t.Fatal("after 200ms: expected Allow true (≥1 token refilled)")
	}

	// Advance another 300ms → 10 * 0.3 = 3 more tokens; 3 available, 4 more should succeed.
	// Wait — total elapsed since last fill at 200ms mark is another 300ms = 3 tokens.
	// We consumed 1 at 200ms, so: 2-1=1 + 3 = 4 tokens remain; 4 calls should succeed.
	clk.advance(300 * time.Millisecond)
	for i := 0; i < 4; i++ {
		if !l.Allow("ip1") {
			t.Fatalf("call %d after 500ms total: expected Allow true", i+1)
		}
	}
	// Now bucket is empty again.
	if l.Allow("ip1") {
		t.Fatal("expected Allow false after consuming all refilled tokens")
	}
}

// TestLimiterPerKeyIsolation ensures buckets are independent per key.
func TestLimiterPerKeyIsolation(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(10, 5, WithClock(clk))

	// Exhaust ip1.
	for i := 0; i < 5; i++ {
		l.Allow("ip1")
	}
	if l.Allow("ip1") {
		t.Fatal("ip1 should be exhausted")
	}

	// ip2 should still have its full capacity.
	for i := 0; i < 5; i++ {
		if !l.Allow("ip2") {
			t.Fatalf("ip2 call %d: expected Allow true (independent bucket)", i+1)
		}
	}
}

// TestLimiterSweepRemovesIdleFullBuckets verifies that Sweep removes buckets
// that are both full and idle beyond maxIdle.
func TestLimiterSweepRemovesIdleFullBuckets(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(10, 5, WithClock(clk))

	// Consume one token — bucket now has 4.
	l.Allow("ip1")

	// Advance 10 seconds — enough to refill to capacity (10 tok/sec * 10s = 100, clamped to 5).
	clk.advance(10 * time.Second)

	// Sweep with 1s maxIdle — bucket is full and idle >1s, should be removed.
	removed := l.Sweep(1 * time.Second)
	if removed != 1 {
		t.Fatalf("Sweep: expected 1 removal, got %d", removed)
	}

	// A subsequent Allow("ip1") should create a fresh full bucket and return true.
	if !l.Allow("ip1") {
		t.Fatal("after Sweep, Allow on fresh bucket should return true")
	}
}

// TestLimiterSweepKeepsActiveBuckets verifies that a recently-used bucket is
// not evicted by Sweep.
func TestLimiterSweepKeepsActiveBuckets(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(10, 5, WithClock(clk))

	// Allow creates the bucket at "now".
	l.Allow("ip1")

	// Sweep immediately — bucket was just used (lastFill == now, not idle), should stay.
	removed := l.Sweep(1 * time.Second)
	if removed != 0 {
		t.Fatalf("Sweep: expected 0 removals (bucket active), got %d", removed)
	}

	// Next Allow should succeed without a fresh-bucket reset.
	if !l.Allow("ip1") {
		t.Fatal("bucket should still be present and have tokens remaining")
	}
}

// TestLimiterConcurrentSafe verifies no data races or panics under high
// concurrency. Exact allow counts are not asserted (timing-dependent).
func TestLimiterConcurrentSafe(t *testing.T) {
	l := NewLimiter(100, 10) // real clock; no fake needed for concurrency test.

	const goroutines = 500
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("ip%d", i%5)
			l.Allow(key)
		}()
	}
	wg.Wait()
}
