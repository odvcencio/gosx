// Package signal provides reactive state primitives for GoSX island components.
//
// Signal[T] holds mutable state with change notification.
// Computed[T] derives values from signals with automatic dependency tracking.
// Effect runs side effects when dependencies change.
//
// Inspired by FluffyUI's reactive model, adapted for browser/WASM context
// with single-thread-minded semantics.
//
// # Notification rules
//
// Set and Update notify subscribers only when the value changes. New installs a
// default equality test for every comparable type; NewWithEqual overrides it.
// Types that Go cannot compare, such as slices and maps, notify on every Set.
//
// One change to a base signal produces exactly one notification per dependent
// Computed value. No lock is held while subscribers run, so a subscriber may
// read any signal or computed value, including the one that notified it.
// Derived values become stale immediately and remain readable inside Batch.
// Batch retains each raw-signal notification while derived publications settle
// against the latest state instead of replaying retired dependency callbacks.
//
// # Goroutine scope
//
// Dependency tracking belongs to the calling goroutine. A read on one goroutine
// never becomes a dependency of a Computed value built on another. While Batch
// is open, signal writes must stay on its owning goroutine; see Batch's contract
// and best-effort concurrent-write detector. Runtimes without a standard
// goroutine header, such as TinyGo, share one tracking scope.
package signal

import (
	"sync"
	"sync/atomic"
)

// Subscriber is a callback invoked when a signal's value changes.
type Subscriber func()

// Subscribable is any reactive value that supports subscriptions.
type Subscribable interface {
	subscribe(fn, invalidate Subscriber) func()
	refresh()
	revision() uint64
}

// Signal is a mutable reactive value.
type Signal[T any] struct {
	mu sync.Mutex
	// rev counts the writes that changed the value. Revision reads it with
	// one atomic load, so a caller can ask "did anything change" without
	// taking the mutex. It sits before value so that the mutex, the counter
	// and the value share the same cache line.
	rev   atomic.Uint64
	value T
	subs  []subscriberEntry
	next  int
	equal func(a, b T) bool
}

type subscriberEntry struct {
	id         int
	fn         Subscriber
	invalidate Subscriber
}

// New creates a new signal with an initial value. Comparable value types get a
// default equality test, so Set with an unchanged value does not notify.
func New[T any](initial T) *Signal[T] {
	return &Signal[T]{value: initial, equal: defaultEqual[T]()}
}

// NewShared creates a signal whose name is shared across browser islands and
// engines on the page. The GoSX compiler normalizes name to a $-prefixed name.
// In native Go, each call creates an independent signal initialized to initial;
// names never register process-global state or share values between requests.
func NewShared[T any](name string, initial T) *Signal[T] {
	return New(initial)
}

// Shared is an alias for NewShared.
func Shared[T any](name string, initial T) *Signal[T] {
	return NewShared(name, initial)
}

// NewWithEqual creates a signal with a custom equality function. Pass nil to
// notify on every Set regardless of the value.
func NewWithEqual[T any](initial T, eq func(a, b T) bool) *Signal[T] {
	return &Signal[T]{value: initial, equal: eq}
}

// Get returns the current value and records the dependency if tracking is active.
func (s *Signal[T]) Get() T {
	s.mu.Lock()
	value := s.value
	tracking := openTrackers.Load() > 0
	var revision uint64
	if tracking {
		revision = s.rev.Load()
	}
	s.mu.Unlock()
	if tracking {
		recordDependency(s, revision)
	}
	return value
}

func (s *Signal[T]) refresh()         {}
func (s *Signal[T]) revision() uint64 { return s.Revision() }

// Revision returns how many writes have changed the value.
//
// It is the uncontended fast path for a caller that polls a signal and only
// needs to know whether the value moved. Get copies T, so it must hold the
// mutex; Revision answers with one atomic load and no lock at all. Two reads
// that return the same number are separated by no write, so a value read
// between them is still current.
//
// Use it in this order, and only in this order:
//
//  1. Read Revision.
//  2. Read the value with Get.
//  3. Store the two together.
//
// A later read that finds the SAME revision may reuse the stored value. The
// order matters: a write that lands between step 1 and step 2 leaves a stored
// revision that is older than the stored value, so the next read finds a
// different number and repeats the work. Reading the revision second would
// store a number that is newer than the value, and the next read would keep a
// stale value for ever.
//
// Revision does NOT record a dependency. A Computed value that only polled the
// revision would never learn that it must recompute, so build tracking on Get.
//
// Revision counts writes, not distinct values. A Set that restores an earlier
// value still advances it, and a Set the equality test rejects does not.
func (s *Signal[T]) Revision() uint64 {
	return s.rev.Load()
}

// Set updates the value and notifies subscribers if the value changed.
func (s *Signal[T]) Set(value T) {
	s.mu.Lock()
	if s.equal != nil && s.equal(s.value, value) {
		s.mu.Unlock()
		return
	}
	s.value = value
	// Advance the counter AFTER the value and before the mutex is dropped,
	// so a reader that took the revision first can never store a number
	// that is newer than the value it read. See Revision.
	s.rev.Add(1)
	invalidators, subs := s.snapshotSubscribersLocked()
	s.mu.Unlock()

	for _, fn := range invalidators {
		fn()
	}
	batchNotify(subs)
}

// Update applies a transformation to the current value.
func (s *Signal[T]) Update(fn func(T) T) {
	s.mu.Lock()
	newVal := fn(s.value)
	if s.equal != nil && s.equal(s.value, newVal) {
		s.mu.Unlock()
		return
	}
	s.value = newVal
	s.rev.Add(1)
	invalidators, subs := s.snapshotSubscribersLocked()
	s.mu.Unlock()

	for _, fn := range invalidators {
		fn()
	}
	batchNotify(subs)
}

// snapshotSubscribersLocked copies the callback list so notification runs with
// the mutex released. The caller must hold s.mu.
func (s *Signal[T]) snapshotSubscribersLocked() ([]Subscriber, []Subscriber) {
	n := 0
	for _, e := range s.subs {
		if e.invalidate != nil {
			n++
		}
	}
	callbacks := make([]Subscriber, n+len(s.subs))
	subs := callbacks[n:]
	next := 0
	for i, e := range s.subs {
		subs[i] = e.fn
		if e.invalidate != nil {
			callbacks[next] = e.invalidate
			next++
		}
	}
	return callbacks[:n:n], subs
}

func (s *Signal[T]) invalidateSubscribers() {
	s.mu.Lock()
	var callbacks []Subscriber
	for _, e := range s.subs {
		if e.invalidate != nil {
			callbacks = append(callbacks, e.invalidate)
		}
	}
	s.mu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

// Subscribe registers a callback for value changes. Returns an unsubscribe function.
func (s *Signal[T]) Subscribe(fn Subscriber) func() {
	return s.subscribe(fn, nil)
}

func (s *Signal[T]) subscribe(fn, invalidate Subscriber) func() {
	s.mu.Lock()
	id := s.next
	s.next++
	s.subs = append(s.subs, subscriberEntry{id: id, fn: fn, invalidate: invalidate})
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		for i, e := range s.subs {
			if e.id == id {
				copy(s.subs[i:], s.subs[i+1:])
				s.subs[len(s.subs)-1] = subscriberEntry{}
				s.subs = s.subs[:len(s.subs)-1]
				break
			}
		}
		s.mu.Unlock()
	}
}

// hasSubscribers reports whether anybody listens for changes. A Computed value
// uses it to stay lazy while nobody observes it.
func (s *Signal[T]) hasSubscribers() bool {
	s.mu.Lock()
	n := len(s.subs)
	s.mu.Unlock()
	return n > 0
}

// Computed is a derived reactive value that recomputes when dependencies change.
type Computed[T any] struct {
	mu         sync.Mutex
	compute    func() T
	signal     *Signal[T]
	deps       []dependency
	dirty      bool
	refreshing bool
	stopped    bool
}

// Derive creates a computed value from a function.
// Dependencies are tracked automatically on first evaluation.
func Derive[T any](fn func() T) *Computed[T] {
	c := &Computed[T]{compute: fn}
	value, deps := trackDependencies(fn)
	c.signal = New(value)
	c.mu.Lock()
	c.subscribeToDepsLocked(deps)
	c.dirty = true
	c.mu.Unlock()
	c.refresh()
	return c
}

// Get returns the current computed value, including writes made inside Batch.
// Subscriber delivery remains deferred until that batch completes.
func (c *Computed[T]) Get() T {
	c.refresh()
	c.signal.mu.Lock()
	value := c.signal.value
	tracking := openTrackers.Load() > 0
	var revision uint64
	if tracking {
		revision = c.signal.rev.Load()
	}
	c.signal.mu.Unlock()
	if tracking {
		recordDependency(c, revision)
	}
	return value
}

func (c *Computed[T]) revision() uint64 { return c.signal.Revision() }

// Subscribe registers a callback for when the computed value changes.
func (c *Computed[T]) Subscribe(fn Subscriber) func() {
	return c.signal.Subscribe(fn)
}

func (c *Computed[T]) subscribe(fn, invalidate Subscriber) func() {
	return c.signal.subscribe(fn, invalidate)
}

// Stop disposes the computed value and unsubscribes from all dependencies.
func (c *Computed[T]) Stop() {
	c.mu.Lock()
	c.stopped = true
	deps := c.deps
	c.deps = nil
	c.mu.Unlock()
	for _, dep := range deps {
		dep.unsubscribe()
	}
}

// refresh checks upstream revisions before evaluating. A dirty descendant of
// an unchanged computed boundary can keep its cached value without running its
// function. Reads and their revisions are captured together while tracking.
func (c *Computed[T]) refresh() {
	c.mu.Lock()
	if c.stopped || !c.dirty || c.refreshing {
		c.mu.Unlock()
		return
	}
	c.refreshing = true
	c.mu.Unlock()
	completed := false
	defer func() {
		if !completed {
			c.mu.Lock()
			c.refreshing = false
			c.dirty = true
			c.mu.Unlock()
		}
	}()
	for {
		c.mu.Lock()
		if c.stopped || !c.dirty {
			c.refreshing = false
			completed = true
			c.mu.Unlock()
			return
		}
		c.dirty = false
		deps := c.deps
		c.mu.Unlock()
		changed := false
		for _, dep := range deps {
			dep.source.refresh()
			if dep.source.revision() != dep.revision {
				changed = true
			}
		}
		if !changed {
			continue
		}
		value, deps := trackDependencies(c.compute)
		c.mu.Lock()
		stopped := c.stopped
		stale := false
		if !stopped {
			c.subscribeToDepsLocked(deps)
			for _, dep := range deps {
				if dep.source.revision() != dep.revision {
					stale = true
					c.dirty = true
					break
				}
			}
		}
		c.mu.Unlock()
		if stopped {
			c.mu.Lock()
			c.refreshing = false
			completed = true
			c.mu.Unlock()
			return
		}
		if !stale {
			c.signal.Set(value)
		}
	}
}

// invalidate propagates staleness synchronously without delivering observers.
func (c *Computed[T]) invalidate() {
	c.mu.Lock()
	if c.stopped || c.dirty {
		c.mu.Unlock()
		return
	}
	c.dirty = true
	c.mu.Unlock()
	c.signal.invalidateSubscribers()
}

func (c *Computed[T]) onDepChange() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.dirty = true
	c.mu.Unlock()
	if c.signal.hasSubscribers() {
		c.refresh()
	}
}

// subscribeToDepsLocked replaces subscriptions while c.mu is held.
func (c *Computed[T]) subscribeToDepsLocked(deps []dependency) {
	old := c.deps
	c.deps = deps
	for _, dep := range old {
		dep.unsubscribe()
	}
	notify, invalidate := c.onDepChange, c.invalidate
	for i, dep := range deps {
		c.deps[i].unsubscribe = dep.source.subscribe(notify, invalidate)
	}
}

// Effect runs a side-effect function whenever its dependencies change.
type Effect struct {
	mu     sync.Mutex
	fn     func()
	unsubs []func()
	dead   bool
}

// Watch creates an effect that tracks dependencies and reruns when they change.
func Watch(fn func()) *Effect {
	e := &Effect{fn: fn}
	e.run()
	return e
}

// Dispose stops the effect and unsubscribes from all dependencies.
func (e *Effect) Dispose() {
	e.mu.Lock()
	e.dead = true
	unsubs := e.unsubs
	e.unsubs = nil
	e.mu.Unlock()

	for _, unsub := range unsubs {
		unsub()
	}
}

func (e *Effect) run() {
	e.mu.Lock()
	if e.dead {
		e.mu.Unlock()
		return
	}
	unsubs := e.unsubs
	e.unsubs = nil
	e.mu.Unlock()

	for _, unsub := range unsubs {
		unsub()
	}

	// Run and track with the mutex released.
	_, deps := trackDependencies(func() struct{} {
		e.fn()
		return struct{}{}
	})

	e.mu.Lock()
	if e.dead {
		e.mu.Unlock()
		return
	}
	for _, dep := range deps {
		e.unsubs = append(e.unsubs, dep.source.subscribe(e.run, nil))
	}
	e.mu.Unlock()
}
