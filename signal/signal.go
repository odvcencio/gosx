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
//
// # Goroutine scope
//
// Dependency tracking and Batch are scoped to the calling goroutine. A read on
// one goroutine never becomes a dependency of a Computed value built on
// another, and one goroutine's Batch never delays another goroutine's
// notifications. Runtimes without a standard goroutine header, such as TinyGo,
// share one scope; that matches their single-threaded model.
package signal

import (
	"sync"
	"sync/atomic"
)

// Subscriber is a callback invoked when a signal's value changes.
type Subscriber func()

// Subscribable is any reactive value that supports subscriptions.
type Subscribable interface {
	subscribe(fn Subscriber) func()
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
	id int
	fn Subscriber
}

// New creates a new signal with an initial value. Comparable value types get a
// default equality test, so Set with an unchanged value does not notify.
func New[T any](initial T) *Signal[T] {
	return &Signal[T]{value: initial, equal: defaultEqual[T]()}
}

// NewWithEqual creates a signal with a custom equality function. Pass nil to
// notify on every Set regardless of the value.
func NewWithEqual[T any](initial T, eq func(a, b T) bool) *Signal[T] {
	return &Signal[T]{value: initial, equal: eq}
}

// Get returns the current value and records the dependency if tracking is active.
func (s *Signal[T]) Get() T {
	s.mu.Lock()
	v := s.value
	s.mu.Unlock()
	recordDependency(s)
	return v
}

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
	subs := s.snapshotSubscribersLocked()
	s.mu.Unlock()

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
	subs := s.snapshotSubscribersLocked()
	s.mu.Unlock()

	batchNotify(subs)
}

// snapshotSubscribersLocked copies the callback list so notification runs with
// the mutex released. The caller must hold s.mu.
func (s *Signal[T]) snapshotSubscribersLocked() []Subscriber {
	if len(s.subs) == 0 {
		return nil
	}
	subs := make([]Subscriber, len(s.subs))
	for i, e := range s.subs {
		subs[i] = e.fn
	}
	return subs
}

// Subscribe registers a callback for value changes. Returns an unsubscribe function.
func (s *Signal[T]) Subscribe(fn Subscriber) func() {
	s.mu.Lock()
	id := s.next
	s.next++
	s.subs = append(s.subs, subscriberEntry{id: id, fn: fn})
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		for i, e := range s.subs {
			if e.id == id {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
	}
}

func (s *Signal[T]) subscribe(fn Subscriber) func() {
	return s.Subscribe(fn)
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
	unsubs     []func()
	dirty      bool
	refreshing bool
	stopped    bool
}

// Derive creates a computed value from a function.
// Dependencies are tracked automatically on first evaluation.
func Derive[T any](fn func() T) *Computed[T] {
	c := &Computed[T]{compute: fn}
	// Evaluate immediately to track deps and get the initial value.
	val, deps := trackDependencies(fn)
	c.signal = New(val)
	c.mu.Lock()
	c.subscribeToDepsLocked(deps)
	c.mu.Unlock()
	return c
}

// Get returns the current computed value. It recomputes first when a dependency
// changed while nobody observed the value.
func (c *Computed[T]) Get() T {
	c.refresh()
	return c.signal.Get()
}

// Subscribe registers a callback for when the computed value changes.
func (c *Computed[T]) Subscribe(fn Subscriber) func() {
	return c.signal.Subscribe(fn)
}

func (c *Computed[T]) subscribe(fn Subscriber) func() {
	return c.Subscribe(fn)
}

// Stop disposes the computed value and unsubscribes from all dependencies.
func (c *Computed[T]) Stop() {
	c.mu.Lock()
	c.stopped = true
	unsubs := c.unsubs
	c.unsubs = nil
	c.mu.Unlock()

	for _, unsub := range unsubs {
		unsub()
	}
}

// refresh recomputes the value when it is stale and publishes the result once.
// It never holds c.mu while it runs user code or notifies subscribers, so a
// subscriber may call Get without deadlocking.
func (c *Computed[T]) refresh() {
	c.mu.Lock()
	if c.stopped || !c.dirty {
		c.mu.Unlock()
		return
	}
	if c.refreshing {
		// Another goroutine already recomputes. It repeats the pass because
		// dirty stays set, so this caller must not compute in parallel.
		c.mu.Unlock()
		return
	}
	c.refreshing = true
	c.mu.Unlock()

	for {
		c.mu.Lock()
		if c.stopped || !c.dirty {
			c.refreshing = false
			c.mu.Unlock()
			return
		}
		c.dirty = false
		c.mu.Unlock()

		val, deps := trackDependencies(c.compute)

		c.mu.Lock()
		stopped := c.stopped
		if !stopped {
			c.subscribeToDepsLocked(deps)
		}
		c.mu.Unlock()

		if stopped {
			c.mu.Lock()
			c.refreshing = false
			c.mu.Unlock()
			return
		}
		// Publish outside c.mu. Exactly one Set runs per dependency change.
		c.signal.Set(val)
	}
}

// onDepChange marks the value stale and republishes it once. It stays lazy while
// nobody subscribes: the next Get then does the work.
func (c *Computed[T]) onDepChange() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.dirty = true
	c.mu.Unlock()

	if !c.signal.hasSubscribers() {
		return
	}
	c.refresh()
}

// subscribeToDepsLocked replaces the dependency subscriptions. The caller must
// hold c.mu.
func (c *Computed[T]) subscribeToDepsLocked(deps []Subscribable) {
	old := c.unsubs
	c.unsubs = nil
	for _, unsub := range old {
		unsub()
	}
	for _, dep := range deps {
		c.unsubs = append(c.unsubs, dep.subscribe(c.onDepChange))
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
		e.unsubs = append(e.unsubs, dep.subscribe(e.run))
	}
	e.mu.Unlock()
}
