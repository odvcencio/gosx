package sim

import "m31labs.dev/gosx/internal/clock"

// Clock separates wall timestamps from elapsed time, including ticker deadlines.
type Clock = clock.Clock
type Instant = clock.Instant
type Ticker = clock.Ticker

func newClock() Clock { return clock.New() }
