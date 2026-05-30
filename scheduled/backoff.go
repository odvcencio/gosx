package scheduled

import (
	"math"
	"time"
)

// BackoffPolicy computes the delay before the next retry attempt.
// attempt is 1-based (attempt=1 means "first retry after the first failure").
type BackoffPolicy interface {
	NextDelay(attempt int) time.Duration
}

// immediatePolicy always returns zero delay.
type immediatePolicy struct{}

func (immediatePolicy) NextDelay(_ int) time.Duration { return 0 }

// Immediate returns a BackoffPolicy with no delay between retries.
func Immediate() BackoffPolicy { return immediatePolicy{} }

// fixedPolicy returns a constant delay.
type fixedPolicy struct{ d time.Duration }

func (f fixedPolicy) NextDelay(_ int) time.Duration { return f.d }

// Fixed returns a BackoffPolicy with a constant delay d between retries.
func Fixed(d time.Duration) BackoffPolicy { return fixedPolicy{d: d} }

// exponentialPolicy implements base * factor^(attempt-1), capped at max.
type exponentialPolicy struct {
	base   time.Duration
	factor float64
	max    time.Duration
}

func (e exponentialPolicy) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(e.base) * math.Pow(e.factor, float64(attempt-1))
	if e.max > 0 && time.Duration(delay) > e.max {
		return e.max
	}
	return time.Duration(delay)
}

// Exponential returns a BackoffPolicy that multiplies base by factor^(attempt-1),
// capped at max. If max <= 0 there is no cap.
func Exponential(base time.Duration, factor float64, max time.Duration) BackoffPolicy {
	return exponentialPolicy{base: base, factor: factor, max: max}
}

// DefaultBackoff returns Exponential(1s, 4, 64s), which yields 1s, 4s, 16s, 64s, 64s, …
func DefaultBackoff() BackoffPolicy {
	return Exponential(time.Second, 4, 64*time.Second)
}

// ShouldRetry reports whether another attempt should be made.
// maxAttempts <= 0 means unlimited retries.
// attempt is the number of attempts already made (1-based attempt just completed).
func ShouldRetry(attempt, maxAttempts int) bool {
	if maxAttempts <= 0 {
		return true
	}
	return attempt < maxAttempts
}
