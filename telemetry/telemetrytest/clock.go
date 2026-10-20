// Package telemetrytest supplies portable, deterministic telemetry helpers.
// It imports shared leaf types, never the root telemetry package.
package telemetrytest

import (
	"math"
	"sync"
	"time"

	"m31labs.dev/gosx/internal/clock"
	"m31labs.dev/gosx/internal/telemetryerr"
)

// FakeClock keeps wall and elapsed time independent. The zero clock starts at
// Unix epoch. No timer creates a goroutine. Configure wall jumps explicitly.
type FakeClock struct {
	mu      sync.Mutex
	wall    time.Time
	wallSet bool
	elapsed time.Duration
	tickers map[*fakeTicker]struct{}
}

type fakeTicker struct {
	clock        *FakeClock
	period, last time.Duration
	ch           chan time.Time
}

func NewClock(start time.Time) *FakeClock { return &FakeClock{wall: start.UTC(), wallSet: true} }

func (c *FakeClock) wallTime() time.Time {
	if !c.wallSet {
		return time.Unix(0, 0).UTC()
	}
	return c.wall
}

func (c *FakeClock) Now() clock.Instant {
	c.mu.Lock()
	defer c.mu.Unlock()
	return clock.Instant{Wall: c.wallTime(), Monotonic: c.elapsed}
}

func (c *FakeClock) NewTicker(period time.Duration) clock.Ticker {
	if period <= 0 {
		panic("telemetrytest: nonpositive ticker interval")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tickers == nil {
		c.tickers = make(map[*fakeTicker]struct{})
	}
	t := &fakeTicker{clock: c, period: period, last: c.elapsed, ch: make(chan time.Time, 1)}
	c.tickers[t] = struct{}{}
	return t
}

// Advance advances both coordinates. A ticker queues its first due deadline
// and drops later missed ticks, like a slow consumer of time.Ticker. It never
// blocks, performs a catch-up loop, or overwrites an unread timestamp.
func (c *FakeClock) Advance(delta time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if delta < 0 || delta > time.Duration(math.MaxInt64)-c.elapsed {
		return &telemetryerr.ConfigError{Field: "advance", Code: "range"}
	}
	c.wall = c.wallTime().Add(delta)
	c.wallSet = true
	c.elapsed += delta
	for t := range c.tickers {
		periods := (c.elapsed - t.last) / t.period
		if periods == 0 {
			continue
		}
		deadline := t.last + t.period
		t.last += periods * t.period
		select {
		case t.ch <- time.Unix(0, int64(deadline)).UTC():
		default:
		}
	}
	return nil
}

func (c *FakeClock) JumpWall(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wallTime().Add(delta)
	c.wallSet = true
}

func (c *FakeClock) PendingTimers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tickers)
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stamp(stamp time.Time) clock.Instant {
	now := t.clock.Now()
	elapsed := time.Duration(stamp.UnixNano())
	return clock.Instant{Wall: now.Wall.Add(elapsed - now.Monotonic), Monotonic: elapsed}
}
func (t *fakeTicker) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	delete(t.clock.tickers, t)
}
