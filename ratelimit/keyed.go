// Package ratelimit provides bounded admission shared by HTTP and realtime
// handlers. Callers choose keys and policy; clocks are injectable for replay.
package ratelimit

import (
	"sync"
	"time"
)

type window struct {
	expires time.Time
	count   int
}
type Keyed struct {
	mu       sync.Mutex
	entries  map[string]window
	capacity int
	now      func() time.Time
}

// NewKeyed bounds the number of live keys. When full it rejects new keys;
// active limits are never evicted to admit an attacker-controlled identity.
func NewKeyed(capacity int, now func() time.Time) *Keyed {
	if now == nil {
		now = time.Now
	}
	return &Keyed{entries: make(map[string]window), capacity: max(0, capacity), now: now}
}

// Allow consumes one request from a fixed window. Rejections do not extend it.
// Calls are concurrent-safe. A key should have one consistent limit/window.
func (l *Keyed) Allow(key string, limit int, duration time.Duration) bool {
	if limit <= 0 || duration <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	rate := l.entries[key]
	if !now.Before(rate.expires) {
		for k, v := range l.entries {
			if !now.Before(v.expires) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= l.capacity {
			return false
		}
		rate = window{expires: now.Add(duration)}
	}
	if rate.count >= limit {
		return false
	}
	rate.count++
	l.entries[key] = rate
	return true
}
