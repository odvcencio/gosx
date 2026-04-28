package democtl

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrRegistryFull is returned by Join when the registry is at capacity and the
// requested room does not yet exist.
var ErrRegistryFull = errors.New("democtl: room registry full")

// ErrRoomNotFound is returned by WithRoom when the named room does not exist.
var ErrRoomNotFound = errors.New("democtl: room not found")

// ErrRegistryNotConfigured is returned when a nil registry is used.
var ErrRegistryNotConfigured = errors.New("democtl: room registry not configured")

// ErrInvalidRegistryConfig is returned by NewRegistryChecked for invalid
// capacity or idleTTL values.
var ErrInvalidRegistryConfig = errors.New("democtl: invalid room registry config")

// Room represents an ephemeral in-memory room. Callers may store demo-specific
// state by reading/writing Room.Data, which is an untyped slot guarded by the
// registry's mutex when accessed through Registry.WithRoom.
type Room struct {
	ID         string
	CreatedAt  time.Time
	LastActive time.Time
	Presence   int
	Data       any // caller state slot — opaque to democtl
}

// RegistryOption configures a Registry at construction time.
type RegistryOption func(*Registry)

// WithRegistryClock overrides the registry's clock (for tests).
func WithRegistryClock(c Clock) RegistryOption {
	return func(r *Registry) { r.clock = c }
}

// Registry is a concurrency-safe in-memory room registry.
type Registry struct {
	mu      sync.Mutex
	rooms   map[string]*Room
	cap     int
	idleTTL time.Duration
	clock   Clock
}

// NewRegistry constructs a Registry with the given capacity (max live rooms)
// and idleTTL (how long an empty room may sit before Sweep removes it).
// Returns nil if capacity <= 0 or idleTTL <= 0. Use NewRegistryChecked when
// callers need the validation error.
func NewRegistry(capacity int, idleTTL time.Duration, opts ...RegistryOption) *Registry {
	registry, _ := NewRegistryChecked(capacity, idleTTL, opts...)
	return registry
}

// NewRegistryChecked constructs a Registry and reports invalid configuration.
func NewRegistryChecked(capacity int, idleTTL time.Duration, opts ...RegistryOption) (*Registry, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("%w: capacity must be > 0", ErrInvalidRegistryConfig)
	}
	if idleTTL <= 0 {
		return nil, fmt.Errorf("%w: idleTTL must be > 0", ErrInvalidRegistryConfig)
	}
	r := &Registry{
		rooms:   make(map[string]*Room),
		cap:     capacity,
		idleTTL: idleTTL,
		clock:   realClock{},
	}
	for _, o := range opts {
		o(r)
	}
	return r, nil
}

// Join atomically looks up or creates a room with the given id, increments its
// Presence, and updates its LastActive. Returns ErrRegistryFull if a new room
// would exceed capacity (existing rooms are always allowed to grow presence).
func (r *Registry) Join(id string) (*Room, error) {
	if r == nil {
		return nil, ErrRegistryNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock.Now()

	room, ok := r.rooms[id]
	if !ok {
		if len(r.rooms) >= r.cap {
			return nil, ErrRegistryFull
		}
		room = &Room{
			ID:         id,
			CreatedAt:  now,
			LastActive: now,
		}
		r.rooms[id] = room
	}

	room.Presence++
	room.LastActive = now
	return room, nil
}

// Leave decrements the room's Presence and bumps LastActive. No-op if the room
// doesn't exist. Presence does not go below zero.
func (r *Registry) Leave(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	room, ok := r.rooms[id]
	if !ok {
		return
	}
	if room.Presence > 0 {
		room.Presence--
	}
	room.LastActive = r.clock.Now()
}

// WithRoom runs fn while holding the registry lock with the named room passed
// in. fn may return an error, which WithRoom returns unchanged. Returns
// ErrRoomNotFound if the room doesn't exist.
func (r *Registry) WithRoom(id string, fn func(*Room) error) error {
	if r == nil {
		return ErrRegistryNotConfigured
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	room, ok := r.rooms[id]
	if !ok {
		return ErrRoomNotFound
	}
	return fn(room)
}

// Sweep removes empty rooms (Presence == 0) whose LastActive is older than
// idleTTL. Returns the count removed.
func (r *Registry) Sweep() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock.Now()
	removed := 0

	for id, room := range r.rooms {
		if room.Presence == 0 && now.Sub(room.LastActive) > r.idleTTL {
			delete(r.rooms, id)
			removed++
		}
	}
	return removed
}

// Len returns the current number of rooms in the registry.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rooms)
}
