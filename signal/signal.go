// Package signal provides reactive state primitives for GoSX island components.
//
// Signal[T] holds mutable state with change notification.
// Computed[T] derives values from signals with automatic dependency tracking.
// Effect runs side effects when dependencies change.
//
// Inspired by FluffyUI's reactive model, adapted for browser/WASM context
// with single-thread-minded semantics.
package signal

import "sync"

// Subscriber is a callback invoked when a signal's value changes.
type Subscriber func()

// Subscribable is any reactive value that supports subscriptions.
type Subscribable interface {
	subscribe(fn Subscriber) func()
}

// Signal is a mutable reactive value.
type Signal[T any] struct {
	mu    sync.Mutex
	value T
	subs  []subscriberEntry
	next  int
	equal func(a, b T) bool
}

type subscriberEntry struct {
	id int
	fn Subscriber
}

// New creates a new signal with an initial value.
func New[T any](initial T) *Signal[T] {
	return &Signal[T]{value: initial}
}

// NewWithEqual creates a signal with a custom equality function.
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

// Set updates the value and notifies subscribers if the value changed.
func (s *Signal[T]) Set(value T) {
	s.mu.Lock()
	if s.equal != nil && s.equal(s.value, value) {
		s.mu.Unlock()
		return
	}
	s.value = value
	subs := make([]Subscriber, 0, len(s.subs))
	for _, e := range s.subs {
		subs = append(subs, e.fn)
	}
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
	subs := make([]Subscriber, 0, len(s.subs))
	for _, e := range s.subs {
		subs = append(subs, e.fn)
	}
	s.mu.Unlock()

	batchNotify(subs)
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

// Computed is a derived reactive value that recomputes when dependencies change.
type Computed[T any] struct {
	mu      sync.Mutex
	compute func() T
	signal  *Signal[T]
	unsubs  []func()
	dirty   bool
}

// Derive creates a computed value from a function.
// Dependencies are tracked automatically on first evaluation.
func Derive[T any](fn func() T) *Computed[T] {
	c := &Computed[T]{
		compute: fn,
		dirty:   true,
	}
	// Evaluate immediately to track deps and get initial value
	val, deps := trackDependencies(fn)
	c.signal = New(val)
	c.dirty = false
	c.subscribeToDeps(deps)
	return c
}

// Get returns the current computed value.
func (c *Computed[T]) Get() T {
	c.mu.Lock()
	if c.dirty {
		c.recompute()
	}
	c.mu.Unlock()
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
	for _, unsub := range c.unsubs {
		unsub()
	}
	c.unsubs = nil
	c.mu.Unlock()
}

func (c *Computed[T]) recompute() {
	// Unsubscribe old deps
	for _, unsub := range c.unsubs {
		unsub()
	}
	c.unsubs = nil

	// Re-track dependencies
	val, deps := trackDependencies(c.compute)
	c.signal.Set(val)
	c.dirty = false
	c.subscribeToDeps(deps)
}

func (c *Computed[T]) subscribeToDeps(deps []Subscribable) {
	for _, dep := range deps {
		unsub := dep.subscribe(func() {
			c.mu.Lock()
			c.dirty = true
			c.mu.Unlock()
			// Propagate change notification
			c.signal.Set(c.signal.Get())
		})
		c.unsubs = append(c.unsubs, unsub)
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
	for _, unsub := range e.unsubs {
		unsub()
	}
	e.unsubs = nil
	e.mu.Unlock()
}

func (e *Effect) run() {
	e.mu.Lock()
	if e.dead {
		e.mu.Unlock()
		return
	}
	// Unsubscribe old deps
	for _, unsub := range e.unsubs {
		unsub()
	}
	e.unsubs = nil
	e.mu.Unlock()

	// Run and track
	_, deps := trackDependencies(func() struct{} {
		e.fn()
		return struct{}{}
	})

	e.mu.Lock()
	for _, dep := range deps {
		unsub := dep.subscribe(func() {
			e.run()
		})
		e.unsubs = append(e.unsubs, unsub)
	}
	e.mu.Unlock()
}
