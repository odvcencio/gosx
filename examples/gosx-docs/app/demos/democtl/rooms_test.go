package democtl

import (
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// roomClock is a controllable clock for deterministic room registry tests.
// Duplicated from the fakeClock pattern in ratelimit_test.go under a distinct
// name so the two test files stay independent.
type roomClock struct {
	mu  sync.Mutex
	now time.Time
}

func newRoomClock(t time.Time) *roomClock {
	return &roomClock{now: t}
}

func (c *roomClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *roomClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TestRegistryJoinCreatesRoom verifies that joining an empty registry creates a
// room with Presence == 1 and timestamps equal to clock's Now.
func TestRegistryJoinCreatesRoom(t *testing.T) {
	clk := newRoomClock(t0)
	r := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	room, err := r.Join("alpha")
	if err != nil {
		t.Fatalf("Join: unexpected error: %v", err)
	}
	if room.ID != "alpha" {
		t.Errorf("ID: got %q, want %q", room.ID, "alpha")
	}
	if room.Presence != 1 {
		t.Errorf("Presence: got %d, want 1", room.Presence)
	}
	if !room.CreatedAt.Equal(t0) {
		t.Errorf("CreatedAt: got %v, want %v", room.CreatedAt, t0)
	}
	if !room.LastActive.Equal(t0) {
		t.Errorf("LastActive: got %v, want %v", room.LastActive, t0)
	}
	if r.Len() != 1 {
		t.Errorf("Len: got %d, want 1", r.Len())
	}
}

// TestRegistryJoinReusesExistingRoom verifies that a second Join call on the
// same ID reuses the room, increments Presence, and updates LastActive without
// changing CreatedAt.
func TestRegistryJoinReusesExistingRoom(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	r1, err := reg.Join("alpha")
	if err != nil {
		t.Fatalf("first Join: %v", err)
	}
	createdAt := r1.CreatedAt

	clk.advance(5 * time.Second)
	t1 := clk.Now()

	r2, err := reg.Join("alpha")
	if err != nil {
		t.Fatalf("second Join: %v", err)
	}

	if r2.ID != "alpha" {
		t.Errorf("ID: got %q, want %q", r2.ID, "alpha")
	}
	if r2.Presence != 2 {
		t.Errorf("Presence: got %d, want 2", r2.Presence)
	}
	if !r2.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt changed: got %v, want %v", r2.CreatedAt, createdAt)
	}
	if !r2.LastActive.Equal(t1) {
		t.Errorf("LastActive: got %v, want %v", r2.LastActive, t1)
	}
	if reg.Len() != 1 {
		t.Errorf("Len: got %d, want 1", reg.Len())
	}
}

// TestRegistryJoinFullReturnsError verifies that joining a full registry with a
// new ID returns ErrRegistryFull, but existing rooms can still grow presence.
func TestRegistryJoinFullReturnsError(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(2, 10*time.Second, WithRegistryClock(clk))

	if _, err := reg.Join("a"); err != nil {
		t.Fatalf("Join a: %v", err)
	}
	if _, err := reg.Join("b"); err != nil {
		t.Fatalf("Join b: %v", err)
	}

	_, err := reg.Join("c")
	if !errors.Is(err, ErrRegistryFull) {
		t.Errorf("Join c: got %v, want ErrRegistryFull", err)
	}
	if reg.Len() != 2 {
		t.Errorf("Len after full: got %d, want 2", reg.Len())
	}

	// Existing room "a" must still accept joins.
	ra, err := reg.Join("a")
	if err != nil {
		t.Errorf("Join a again: unexpected error: %v", err)
	}
	if ra.Presence != 2 {
		t.Errorf("Presence of a: got %d, want 2", ra.Presence)
	}
}

// TestRegistryLeaveDecrements verifies that Leave decrements Presence, does not
// go below zero, and is a no-op for unknown rooms.
func TestRegistryLeaveDecrements(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck
	reg.Join("alpha") //nolint:errcheck
	// Presence == 2

	reg.Leave("alpha")
	var presence int
	reg.WithRoom("alpha", func(r *Room) error { presence = r.Presence; return nil }) //nolint:errcheck
	if presence != 1 {
		t.Errorf("after first Leave: Presence = %d, want 1", presence)
	}

	reg.Leave("alpha")
	reg.WithRoom("alpha", func(r *Room) error { presence = r.Presence; return nil }) //nolint:errcheck
	if presence != 0 {
		t.Errorf("after second Leave: Presence = %d, want 0", presence)
	}

	// Third Leave must not go negative.
	reg.Leave("alpha")
	reg.WithRoom("alpha", func(r *Room) error { presence = r.Presence; return nil }) //nolint:errcheck
	if presence != 0 {
		t.Errorf("after third Leave: Presence = %d, want 0 (no negative)", presence)
	}

	// Leave on unknown room must not panic.
	reg.Leave("bogus")
}

// TestRegistrySweepRemovesIdleEmptyRooms verifies that Sweep evicts empty rooms
// whose LastActive is older than idleTTL.
func TestRegistrySweepRemovesIdleEmptyRooms(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 1*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck
	reg.Leave("alpha")
	// Presence == 0, LastActive == t0.

	clk.advance(2 * time.Second)

	removed := reg.Sweep()
	if removed != 1 {
		t.Errorf("Sweep: got %d, want 1", removed)
	}
	if reg.Len() != 0 {
		t.Errorf("Len after Sweep: got %d, want 0", reg.Len())
	}
}

// TestRegistrySweepKeepsActiveRooms verifies that rooms with Presence > 0 are
// never swept regardless of age.
func TestRegistrySweepKeepsActiveRooms(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 1*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck
	// Presence == 1, not left.

	clk.advance(10 * time.Second)

	removed := reg.Sweep()
	if removed != 0 {
		t.Errorf("Sweep: got %d, want 0 (active room must not be swept)", removed)
	}
	if reg.Len() != 1 {
		t.Errorf("Len: got %d, want 1", reg.Len())
	}
}

// TestRegistrySweepKeepsYoungIdleRooms verifies that an empty room is not swept
// before its idleTTL expires.
func TestRegistrySweepKeepsYoungIdleRooms(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 1*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck
	reg.Leave("alpha")
	// Presence == 0, LastActive == t0.

	clk.advance(500 * time.Millisecond) // only 0.5s < 1s TTL

	removed := reg.Sweep()
	if removed != 0 {
		t.Errorf("Sweep: got %d, want 0 (room still within TTL)", removed)
	}
	if reg.Len() != 1 {
		t.Errorf("Len: got %d, want 1", reg.Len())
	}
}

// TestRegistryWithRoomMutatesData verifies that WithRoom executes fn under the
// lock and mutations to Room.Data are visible on subsequent calls.
func TestRegistryWithRoomMutatesData(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck

	err := reg.WithRoom("alpha", func(r *Room) error {
		r.Data = map[string]int{"ticks": 42}
		return nil
	})
	if err != nil {
		t.Fatalf("WithRoom (set): %v", err)
	}

	var got map[string]int
	err = reg.WithRoom("alpha", func(r *Room) error {
		m, ok := r.Data.(map[string]int)
		if !ok {
			t.Errorf("Data: unexpected type %T", r.Data)
			return nil
		}
		got = m
		return nil
	})
	if err != nil {
		t.Fatalf("WithRoom (read): %v", err)
	}
	if got["ticks"] != 42 {
		t.Errorf("Data[ticks]: got %d, want 42", got["ticks"])
	}
}

// TestRegistryWithRoomNotFound verifies that WithRoom returns ErrRoomNotFound
// when the room does not exist, and that fn is never called.
func TestRegistryWithRoomNotFound(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	calls := 0
	err := reg.WithRoom("ghost", func(r *Room) error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("WithRoom: got %v, want ErrRoomNotFound", err)
	}
	if calls != 0 {
		t.Errorf("fn call count: got %d, want 0", calls)
	}
}

// TestRegistryWithRoomPropagatesError verifies that WithRoom returns the exact
// error returned by fn.
func TestRegistryWithRoomPropagatesError(t *testing.T) {
	clk := newRoomClock(t0)
	reg := NewRegistry(10, 10*time.Second, WithRegistryClock(clk))

	reg.Join("alpha") //nolint:errcheck

	sentinel := errors.New("sentinel error")
	err := reg.WithRoom("alpha", func(r *Room) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("WithRoom: got %v, want sentinel error", err)
	}
}

// TestNewRegistryPanicsOnBadArgs verifies that NewRegistry panics for
// non-positive capacity or idleTTL.
func TestNewRegistryPanicsOnBadArgs(t *testing.T) {
	t.Run("zero capacity", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for capacity=0")
			}
		}()
		NewRegistry(0, time.Second)
	})

	t.Run("zero idleTTL", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for idleTTL=0")
			}
		}()
		NewRegistry(1, 0)
	})
}

// TestRegistryConcurrent validates thread safety under the race detector.
// No exact state assertions are made beyond Len() <= pool size and no panics.
func TestRegistryConcurrent(t *testing.T) {
	const (
		goroutines = 100
		poolSize   = 5
		iters      = 10
	)

	reg := NewRegistry(poolSize, 10*time.Second)

	ids := []string{"room0", "room1", "room2", "room3", "room4"}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				id := ids[rand.Intn(poolSize)]
				if _, err := reg.Join(id); err != nil && !errors.Is(err, ErrRegistryFull) {
					// Only ErrRegistryFull is a valid non-nil return here.
					panic("unexpected Join error: " + err.Error())
				}
				reg.Leave(id)
			}
		}()
	}
	wg.Wait()

	if reg.Len() > poolSize {
		t.Errorf("Len: got %d, want <= %d", reg.Len(), poolSize)
	}
}
