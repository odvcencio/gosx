package hubclient

import "time"

// Backoff computes the delay before a reconnect attempt. The zero value is
// ready to use and matches DefaultBackoff.
//
// Delay is deterministic given attempt: Backoff carries no state and no
// randomness of its own, which keeps reconnect timing testable. Callers that
// want jitter can wrap Delay's result.
type Backoff struct {
	// Base is the delay before the first retry (attempt 1). Zero selects 250ms.
	Base time.Duration
	// Max caps the delay. Zero selects 5s.
	Max time.Duration
	// Factor multiplies the delay for each attempt beyond the first. Zero or
	// less than 1 selects 2.
	Factor float64
}

// DefaultBackoff returns the package default backoff schedule: 250ms, 500ms,
// 1s, 2s, 4s, 5s, 5s, ...
func DefaultBackoff() Backoff {
	return Backoff{Base: 250 * time.Millisecond, Max: 5 * time.Second, Factor: 2}
}

func (b Backoff) normalized() Backoff {
	if b.Base <= 0 {
		b.Base = 250 * time.Millisecond
	}
	if b.Max <= 0 {
		b.Max = 5 * time.Second
	}
	if b.Factor < 1 {
		b.Factor = 2
	}
	if b.Max < b.Base {
		b.Max = b.Base
	}
	return b
}

// Delay returns the wait before retry number attempt. attempt 1 is the first
// retry after an initial connection failure. attempt 0 or less always
// returns 0.
func (b Backoff) Delay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	b = b.normalized()
	delay := float64(b.Base)
	for i := 1; i < attempt; i++ {
		delay *= b.Factor
		if delay >= float64(b.Max) {
			return b.Max
		}
	}
	if delay > float64(b.Max) {
		return b.Max
	}
	return time.Duration(delay)
}
