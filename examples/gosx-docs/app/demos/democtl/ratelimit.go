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

const (
	defaultMaxBuckets    = 1024
	defaultSweepEvery    = 256
	defaultBucketMaxIdle = 10 * time.Minute
)

// bucket holds the token count and the last refill timestamp for one key.
type bucket struct {
	tokens   float64
	lastFill time.Time
	lastSeen time.Time
}

// LimiterOption is a functional option for NewLimiter.
type LimiterOption func(*Limiter)

// WithClock injects a custom Clock into the Limiter (used in tests).
func WithClock(c Clock) LimiterOption {
	return func(l *Limiter) { l.clock = c }
}

// WithMaxBuckets caps the number of distinct caller keys retained by the
// limiter. Once the cap is reached, previously unseen keys share one overflow
// bucket instead of allocating unbounded map state or receiving fresh bursts.
func WithMaxBuckets(max int) LimiterOption {
	return func(l *Limiter) { l.maxBuckets = max }
}

// WithLazySweep controls the opportunistic idle-bucket sweep performed from
// Allow. maxIdle is the time a fully-refilled bucket must be unused before it
// can be removed; everyCalls is the number of Allow calls between sweeps.
func WithLazySweep(maxIdle time.Duration, everyCalls int) LimiterOption {
	return func(l *Limiter) {
		l.maxIdle = maxIdle
		if everyCalls > 0 {
			l.sweepEvery = uint64(everyCalls)
		} else {
			l.sweepEvery = 0
		}
	}
}

// Limiter is a bounded per-key token-bucket rate limiter. It is concurrency-safe.
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	overflow   *bucket
	clock      Clock
	rate       float64 // tokens per second
	capacity   float64 // maximum burst size
	maxBuckets int
	maxIdle    time.Duration
	sweepEvery uint64
	allowCalls uint64
}

// NewLimiter constructs a per-key token-bucket rate limiter.
// ratePerSec is the sustained refill rate; capacity is the burst size.
// Tokens are lazy-refilled on each Allow call. Caller-key state and idle
// cleanup are bounded by safe defaults that options may tune.
func NewLimiter(ratePerSec, capacity int, opts ...LimiterOption) *Limiter {
	l := &Limiter{
		buckets:    make(map[string]*bucket),
		clock:      realClock{},
		rate:       float64(ratePerSec),
		capacity:   float64(capacity),
		maxBuckets: defaultMaxBuckets,
		maxIdle:    defaultBucketMaxIdle,
		sweepEvery: defaultSweepEvery,
	}
	for _, o := range opts {
		o(l)
	}
	if l.maxBuckets <= 0 {
		l.maxBuckets = defaultMaxBuckets
	}
	if l.maxIdle <= 0 {
		l.maxIdle = defaultBucketMaxIdle
	}
	if l.sweepEvery == 0 {
		l.sweepEvery = defaultSweepEvery
	}
	return l
}

// Allow returns true if key has a token available and consumes it.
// Returns false if the bucket is empty.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	l.allowCalls++
	if l.allowCalls%l.sweepEvery == 0 {
		l.sweepLocked(now, l.maxIdle)
	}

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) < l.maxBuckets {
			// First access: create a full bucket and immediately consume one token.
			b = &bucket{tokens: l.capacity, lastFill: now, lastSeen: now}
			l.buckets[key] = b
		} else {
			// Rotating attacker-controlled keys must not grow the map or each gain
			// a fresh burst. They converge on one bounded overflow bucket.
			b = l.overflow
			if b == nil {
				b = &bucket{tokens: l.capacity, lastFill: now, lastSeen: now}
				l.overflow = b
			}
		}
	} else {
		l.refillLocked(b, now)
	}
	if b == l.overflow {
		l.refillLocked(b, now)
	}
	b.lastSeen = now

	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

// Sweep removes buckets that are both at full capacity and have been idle
// longer than maxIdle. It returns the number of buckets removed.
// Allow already performs this cleanup opportunistically; Sweep is available
// when a caller wants to force maintenance at a known lifecycle boundary.
func (l *Limiter) Sweep(maxIdle time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.sweepLocked(l.clock.Now(), maxIdle)
}

func (l *Limiter) refillLocked(b *bucket, now time.Time) {
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed * l.rate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.lastFill = now
}

func (l *Limiter) sweepLocked(now time.Time, maxIdle time.Duration) int {
	removed := 0

	for key, b := range l.buckets {
		// Compute effective token count at "now" without mutating the bucket.
		elapsed := now.Sub(b.lastFill).Seconds()
		effective := b.tokens + elapsed*l.rate
		if effective > l.capacity {
			effective = l.capacity
		}

		idle := now.Sub(b.lastSeen)
		if effective >= l.capacity && idle > maxIdle {
			delete(l.buckets, key)
			removed++
		}
	}
	return removed
}
