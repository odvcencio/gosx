package signal

import (
	"sync/atomic"
	"testing"
)

func TestSignalGetSet(t *testing.T) {
	s := New(0)
	if s.Get() != 0 {
		t.Errorf("expected 0, got %d", s.Get())
	}
	s.Set(42)
	if s.Get() != 42 {
		t.Errorf("expected 42, got %d", s.Get())
	}
}

func TestSignalSubscribe(t *testing.T) {
	s := New(0)
	var called int32
	unsub := s.Subscribe(func() {
		atomic.AddInt32(&called, 1)
	})

	s.Set(1)
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}

	s.Set(2)
	if atomic.LoadInt32(&called) != 2 {
		t.Errorf("expected 2 calls, got %d", called)
	}

	unsub()
	s.Set(3)
	if atomic.LoadInt32(&called) != 2 {
		t.Errorf("expected 2 calls after unsub, got %d", called)
	}
}

func TestSignalUpdate(t *testing.T) {
	s := New(10)
	s.Update(func(v int) int { return v + 5 })
	if s.Get() != 15 {
		t.Errorf("expected 15, got %d", s.Get())
	}
}

func TestSignalEqualitySuppression(t *testing.T) {
	s := NewWithEqual(42, func(a, b int) bool { return a == b })
	var called int32
	s.Subscribe(func() { atomic.AddInt32(&called, 1) })

	s.Set(42) // same value, should not notify
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("expected 0 calls for same value, got %d", called)
	}

	s.Set(43) // different value
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("expected 1 call, got %d", called)
	}
}

func TestComputedDerive(t *testing.T) {
	a := New(2)
	b := New(3)
	sum := Derive(func() int {
		return a.Get() + b.Get()
	})

	if sum.Get() != 5 {
		t.Errorf("expected 5, got %d", sum.Get())
	}

	a.Set(10)
	if sum.Get() != 13 {
		t.Errorf("expected 13, got %d", sum.Get())
	}

	b.Set(20)
	if sum.Get() != 30 {
		t.Errorf("expected 30, got %d", sum.Get())
	}

	sum.Stop()
}

func TestComputedSubscribe(t *testing.T) {
	a := New(1)
	doubled := Derive(func() int { return a.Get() * 2 })
	defer doubled.Stop()

	var called int32
	doubled.Subscribe(func() { atomic.AddInt32(&called, 1) })

	a.Set(5)
	// Computed propagates change
	if doubled.Get() != 10 {
		t.Errorf("expected 10, got %d", doubled.Get())
	}
}

func TestBatch(t *testing.T) {
	s := New(0)
	var calls int32
	s.Subscribe(func() { atomic.AddInt32(&calls, 1) })

	Batch(func() {
		s.Set(1)
		s.Set(2)
		s.Set(3)
	})

	// All notifications should fire, but after the batch
	if s.Get() != 3 {
		t.Errorf("expected 3, got %d", s.Get())
	}
}

func TestEffect(t *testing.T) {
	s := New(0)
	var lastSeen int
	e := Watch(func() {
		lastSeen = s.Get()
	})

	if lastSeen != 0 {
		t.Errorf("expected initial 0, got %d", lastSeen)
	}

	s.Set(42)
	if lastSeen != 42 {
		t.Errorf("expected 42, got %d", lastSeen)
	}

	e.Dispose()
	s.Set(100)
	if lastSeen != 42 {
		t.Errorf("expected 42 after dispose, got %d", lastSeen)
	}
}

func TestSignalString(t *testing.T) {
	s := New("hello")
	if s.Get() != "hello" {
		t.Errorf("expected hello, got %s", s.Get())
	}
	s.Set("world")
	if s.Get() != "world" {
		t.Errorf("expected world, got %s", s.Get())
	}
}
