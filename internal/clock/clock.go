// Package clock separates timestamp and elapsed-time coordinates.
package clock

import "time"

type Instant struct {
	Wall      time.Time
	Monotonic time.Duration
}

type Clock interface {
	Now() Instant
	NewTicker(time.Duration) Ticker
}

type Ticker interface {
	C() <-chan time.Time
	Stamp(time.Time) Instant
	Stop()
}

type realClock struct{ origin time.Time }
type realTicker struct {
	clock  *realClock
	ticker *time.Ticker
}

// New retains one monotonic origin. Wall coordinates are UTC timestamps;
// durations use only Monotonic, including after a system-clock adjustment.
func New() Clock { return &realClock{origin: time.Now()} }

func (c *realClock) Now() Instant {
	now := time.Now()
	return Instant{Wall: now.UTC(), Monotonic: now.Sub(c.origin)}
}

func (c *realClock) NewTicker(period time.Duration) Ticker {
	return &realTicker{clock: c, ticker: time.NewTicker(period)}
}

func (t *realTicker) C() <-chan time.Time { return t.ticker.C }
func (t *realTicker) Stamp(stamp time.Time) Instant {
	return Instant{Wall: stamp.UTC(), Monotonic: stamp.Sub(t.clock.origin)}
}
func (t *realTicker) Stop() { t.ticker.Stop() }
