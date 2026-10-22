package wasm

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type fakeSink struct {
	mu      sync.Mutex
	batches []map[string]json.RawMessage
	applied []func()
	isHid   bool
	vis     func()
}

func (s *fakeSink) send(b []byte, applied func()) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	s.mu.Lock()
	s.batches = append(s.batches, m)
	s.applied = append(s.applied, applied)
	s.mu.Unlock()
	return nil
}
func (s *fakeSink) hidden() bool                      { s.mu.Lock(); defer s.mu.Unlock(); return s.isHid }
func (s *fakeSink) onVisibility(fn func()) HandleFunc { s.vis = fn; return func() {} }
func (s *fakeSink) release()                          {}
func (s *fakeSink) count() int                        { s.mu.Lock(); defer s.mu.Unlock(); return len(s.batches) }
func (s *fakeSink) ack(i int)                         { s.mu.Lock(); f := s.applied[i]; s.mu.Unlock(); f() }

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestViewSendsNewestValueAfterIntervalAndAck(t *testing.T) {
	s := &fakeSink{}
	v := newView(s, ViewOptions{MinInterval: 30 * time.Millisecond})
	defer v.Dispose()
	_ = v.Set(map[string]any{"$a": 1})
	if s.count() != 1 {
		t.Fatalf("first value should go at once, batches = %d", s.count())
	}
	// Written while a batch is unacknowledged: only the newest is kept.
	for i := 2; i <= 50; i++ {
		_ = v.Set(map[string]any{"$a": i})
	}
	time.Sleep(60 * time.Millisecond)
	if s.count() != 1 {
		t.Fatalf("no credit yet, batches = %d", s.count())
	}
	s.ack(0)
	waitFor(t, "second batch", func() bool { return s.count() == 2 })
	if got := string(s.batches[1]["$a"]); got != "50" {
		t.Fatalf("second batch carried %s, want the newest value 50", got)
	}
}

func TestViewSendsOnlyChangedSignals(t *testing.T) {
	s := &fakeSink{}
	v := newView(s, ViewOptions{MinInterval: time.Millisecond})
	defer v.Dispose()
	_ = v.Set(map[string]any{"$a": 1, "$b": "x"})
	s.ack(0)
	_ = v.Set(map[string]any{"$a": 1, "$b": "y"})
	waitFor(t, "second batch", func() bool { return s.count() == 2 })
	if _, has := s.batches[1]["$a"]; has || string(s.batches[1]["$b"]) != `"y"` {
		t.Fatalf("batch = %v, want only $b", s.batches[1])
	}
	s.ack(1)
	_ = v.Set(map[string]any{"$a": 1, "$b": "y"})
	time.Sleep(20 * time.Millisecond)
	if s.count() != 2 {
		t.Fatalf("an unchanged state must not send, batches = %d", s.count())
	}
}

func TestViewPausesWhileHiddenAndResumesWithNewestState(t *testing.T) {
	s := &fakeSink{isHid: true}
	v := newView(s, ViewOptions{MinInterval: time.Millisecond})
	defer v.Dispose()
	_ = v.Set(map[string]any{"$a": 1})
	_ = v.Set(map[string]any{"$a": 2})
	time.Sleep(15 * time.Millisecond)
	if s.count() != 0 {
		t.Fatal("a hidden view must not send")
	}
	s.mu.Lock()
	s.isHid = false
	s.mu.Unlock()
	s.vis()
	waitFor(t, "resume", func() bool { return s.count() == 1 })
	if string(s.batches[0]["$a"]) != "2" {
		t.Fatalf("resumed with %s, want 2", s.batches[0]["$a"])
	}
}

func TestViewAckTimeoutFreesCredit(t *testing.T) {
	s := &fakeSink{}
	v := newView(s, ViewOptions{MinInterval: time.Millisecond, AckTimeout: 20 * time.Millisecond})
	defer v.Dispose()
	_ = v.Set(map[string]any{"$a": 1})
	_ = v.Set(map[string]any{"$a": 2})
	waitFor(t, "second batch after ack timeout", func() bool { return s.count() == 2 })
}

func TestViewSetReportsUnencodableValueAndKeepsOthers(t *testing.T) {
	s := &fakeSink{}
	v := newView(s, ViewOptions{})
	defer v.Dispose()
	err := v.Set(map[string]any{"$ok": 1, "$bad": make(chan int)})
	if err == nil {
		t.Fatal("want an encode error")
	}
	if s.count() != 1 || string(s.batches[0]["$ok"]) != "1" {
		t.Fatalf("batches = %v", s.batches)
	}
}
