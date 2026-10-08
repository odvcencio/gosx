package telemetrytest

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/telemetryerr"
)

func TestFakeClockIndependentCoordinatesAndDeadlines(t *testing.T) {
	var c FakeClock
	if got := c.Now(); got.Wall != time.Unix(0, 0).UTC() || got.Monotonic != 0 {
		t.Fatal(got)
	}
	ticker := c.NewTicker(10 * time.Millisecond)
	if c.PendingTimers() != 1 {
		t.Fatal("missing timer")
	}
	if err := c.Advance(35 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	c.JumpWall(-24 * time.Hour)
	stamp := ticker.Stamp(<-ticker.C())
	if stamp.Monotonic != 10*time.Millisecond || stamp.Wall != time.Unix(0, 0).UTC().Add(-24*time.Hour+10*time.Millisecond) {
		t.Fatal(stamp)
	}
	if c.Now().Monotonic != 35*time.Millisecond {
		t.Fatal("wall jump advanced elapsed time")
	}
	_ = c.Advance(5 * time.Millisecond)
	if ticker.Stamp(<-ticker.C()).Monotonic != 40*time.Millisecond {
		t.Fatal("missed periods were replayed")
	}
	_ = c.Advance(100 * time.Millisecond)
	_ = c.Advance(100 * time.Millisecond)
	if ticker.Stamp(<-ticker.C()).Monotonic != 50*time.Millisecond {
		t.Fatal("unread timestamp overwritten")
	}
	ticker.Stop()
	ticker.Stop()
	if c.PendingTimers() != 0 {
		t.Fatal("timer retained after stop")
	}
}

func TestFakeClockRangesAndConcurrentReaders(t *testing.T) {
	if NewClock(time.Time{}).Now().Wall != (time.Time{}) {
		t.Fatal("explicit zero timestamp changed")
	}
	c := NewClock(time.Unix(1234, 0))
	if err := c.Advance(-1); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				ticker := c.NewTicker(time.Millisecond)
				_ = c.Now()
				c.JumpWall(time.Second)
				_ = c.Advance(time.Millisecond)
				ticker.Stop()
			}
		})
	}
	wg.Wait()
	if c.PendingTimers() != 0 {
		t.Fatal("retained stopped tickers")
	}
	if err := c.Advance(time.Duration(math.MaxInt64) - c.Now().Monotonic); err != nil {
		t.Fatal(err)
	}
	if err := c.Advance(1); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal("elapsed overflow accepted")
	}
}

func TestFakeClockWarmAllocationBudget(t *testing.T) {
	c := NewClock(time.Unix(0, 0))
	ticker := c.NewTicker(time.Millisecond)
	defer ticker.Stop()
	allocs := testing.AllocsPerRun(1000, func() {
		_ = c.Advance(time.Millisecond)
		_ = ticker.Stamp(<-ticker.C())
		_ = c.Now()
	})
	if allocs != 0 {
		t.Fatalf("warm fake clock allocated %v", allocs)
	}
}

func BenchmarkFakeClockAdvanceAndStamp(b *testing.B) {
	c := NewClock(time.Unix(0, 0))
	ticker := c.NewTicker(time.Millisecond)
	defer ticker.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = c.Advance(time.Millisecond)
		_ = ticker.Stamp(<-ticker.C())
		_ = c.Now()
	}
}

func TestFakeTickerMisuseMatchesRealTicker(t *testing.T) {
	for _, period := range []time.Duration{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("nonpositive period accepted")
				}
			}()
			NewClock(time.Unix(0, 0)).NewTicker(period)
		}()
	}
}
