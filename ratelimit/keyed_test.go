package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyedWindowCapacityAndIsolation(t *testing.T) {
	now := time.Unix(100, 0)
	l := NewKeyed(2, func() time.Time { return now })
	if !l.Allow("a", 2, time.Minute) || !l.Allow("a", 2, time.Minute) || l.Allow("a", 2, time.Minute) {
		t.Fatal("limit")
	}
	if !l.Allow("b", 1, time.Minute) || l.Allow("c", 1, time.Minute) {
		t.Fatal("capacity")
	}
	now = now.Add(time.Minute)
	if !l.Allow("c", 1, time.Minute) || !l.Allow("a", 2, time.Minute) {
		t.Fatal("expired entries not reclaimed")
	}
	if l.Allow("bad", 0, time.Minute) || l.Allow("bad", 1, 0) {
		t.Fatal("invalid policy")
	}
}
func TestKeyedConcurrentAdmission(t *testing.T) {
	l := NewKeyed(1, time.Now)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if l.Allow("a", 17, time.Hour) {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 17 {
		t.Fatal(accepted.Load())
	}
}
