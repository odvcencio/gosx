package democtl

import (
	"sync"
	"time"
)

// Clock abstracts time.Now so tests can inject a fake clock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// bucket holds the token count and the last refill timestamp for one key.
type bucket struct {
	tokens   float64
	lastFill time.Time
}

// LimiterOption is a functional option for NewLimiter.
type LimiterOption func(*Limiter)

// WithClock injects a custom Clock into the Limiter (used in tests).
func WithClock(c Clock) LimiterOption {
	return func(l *Limiter) { l.clock = c }
}

// Limiter is a per-key token-bucket rate limiter. It is concurrency-safe.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	clock    Clock
	rate     float64 // tokens per second
	capacity float64 // maximum burst size
}

// NewLimiter constructs a per-key token-bucket rate limiter.
// ratePerSec is the sustained refill rate; capacity is the burst size.
// Tokens are lazy-refilled on each Allow call.
func NewLimiter(ratePerSec, capacity int, opts ...LimiterOption) *Limiter {
	l := &Limiter{
		buckets:  make(map[string]*bucket),
		clock:    realClock{},
		rate:     float64(ratePerSec),
		capacity: float64(capacity),
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Allow returns true if key has a token available and consumes it.
// Returns false if the bucket is empty.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()

	b, ok := l.buckets[key]
	if !ok {
		// First access: create a full bucket and immediately consume one token.
		b = &bucket{tokens: l.capacity, lastFill: now}
		l.buckets[key] = b
	} else {
		// Lazy refill: add tokens proportional to elapsed time since last fill.
		elapsed := now.Sub(b.lastFill).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.rate
			if b.tokens > l.capacity {
				b.tokens = l.capacity
			}
			b.lastFill = now
		}
	}

	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

// Sweep removes buckets that are both at full capacity and have been idle
// longer than maxIdle. It returns the number of buckets removed.
// Callers should invoke Sweep periodically from a background goroutine.
func (l *Limiter) Sweep(maxIdle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	removed := 0

	for key, b := range l.buckets {
		// Compute effective token count at "now" without mutating the bucket.
		elapsed := now.Sub(b.lastFill).Seconds()
		effective := b.tokens + elapsed*l.rate
		if effective > l.capacity {
			effective = l.capacity
		}

		idle := now.Sub(b.lastFill)
		if effective >= l.capacity && idle > maxIdle {
			delete(l.buckets, key)
			removed++
		}
	}
	return removed
}
