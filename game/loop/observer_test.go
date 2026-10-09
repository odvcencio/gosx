package loop

import (
	"errors"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/game"
)

type frameObserverFunc func(FrameEvent)

func (f frameObserverFunc) ObserveFrame(e FrameEvent) { f(e) }

func TestFrameObserverWholeWorkVisibilityAndUnlockedCallback(t *testing.T) {
	now := time.Unix(0, 0)
	rt := game.New(game.Config{Systems: []game.System{game.Func("measure", game.PhaseUpdate, func(*game.Context) error {
		now = now.Add(2 * time.Millisecond)
		return nil
	})}})
	source := NewManualFrameSource()
	d := New(rt, source)
	d.now = func() time.Time { return now }
	var events []FrameEvent
	if err := d.SetObserver(frameObserverFunc(func(e FrameEvent) {
		if !d.Running() {
			t.Error("callback not admitted")
		}
		if !errors.Is(d.SetObserver(nil), ErrObserverStarted) {
			t.Error("observer replaced after Start")
		}
		if !d.mu.TryLock() {
			t.Error("state lock held")
		} else {
			d.mu.Unlock()
		}
		if !d.frameMu.TryLock() {
			t.Error("frame lock held")
		} else {
			d.frameMu.Unlock()
		}
		events = append(events, e)
		if len(events) == 4 {
			d.Stop()
		}
	})); err != nil {
		t.Fatal(err)
	}
	d.Start(func(float64, game.Frame) { now = now.Add(3 * time.Millisecond) })
	source.Fire(100)
	source.Fire(116)
	source.SetHidden(true)
	source.Fire(200)
	source.Fire(300)
	source.SetHidden(false)
	source.Fire(400)
	source.Fire(390) // regressed host timestamp is clamped
	if len(events) != 4 {
		t.Fatal(events)
	}
	for i, e := range events {
		interval := time.Duration(0)
		if i == 1 {
			interval = 16 * time.Millisecond
		}
		if e != (FrameEvent{Duration: 5 * time.Millisecond, Interval: interval}) {
			t.Fatal(i, e)
		}
	}
	if source.Pending() || d.Running() {
		t.Fatal("observer Stop scheduled another frame")
	}
	if err := d.SetObserver(nil); !errors.Is(err, ErrObserverStarted) {
		t.Fatal(err)
	}
}

func TestNilFrameObserverAvoidsTiming(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	source := NewManualFrameSource()
	d := New(rt, source)
	d.now = func() time.Time { t.Fatal("nil observer read clock"); return time.Time{} }
	if err := d.SetObserver(frameObserverFunc(func(FrameEvent) { t.Fatal("replaced observer called") })); err != nil {
		t.Fatal(err)
	}
	if err := d.SetObserver(nil); err != nil {
		t.Fatal(err)
	}
	d.Start(nil)
	source.Fire(0)
	source.Fire(10)
	d.Stop()
	if err := (*Driver)(nil).SetObserver(nil); err == nil {
		t.Fatal("nil driver accepted configuration")
	}
}

type gatedFrameSource struct {
	*ManualFrameSource
	entered, release chan struct{}
}

func (s *gatedFrameSource) RequestFrame(cb func(float64)) int {
	handle := s.ManualFrameSource.RequestFrame(cb)
	close(s.entered)
	<-s.release
	return handle
}

func TestStopCancelsRequestAdmittedBeforeHandlePublished(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	s := &gatedFrameSource{ManualFrameSource: NewManualFrameSource(), entered: make(chan struct{}), release: make(chan struct{})}
	d := New(rt, s)
	done := make(chan struct{})
	go func() { d.Start(nil); close(done) }()
	<-s.entered
	d.Stop()
	close(s.release)
	<-done
	if s.Pending() || d.Running() {
		t.Fatal("request survived Stop")
	}
}

func TestDriverObserverStartStopAndFramesConcurrent(t *testing.T) {
	rt, _ := newCountingRuntime(t, 10*time.Millisecond)
	s := NewManualFrameSource()
	d := New(rt, s)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				_ = d.SetObserver(frameObserverFunc(func(FrameEvent) { _ = d.Running() }))
				d.Start(nil)
				s.Fire(float64(i))
				_ = d.Running()
				d.Stop()
			}
		}()
	}
	wg.Wait()
	d.Stop()
	if s.Pending() {
		t.Fatal("request survived concurrent Stop")
	}
}

func BenchmarkFrameDriver(b *testing.B) {
	for _, observed := range []bool{false, true} {
		name := "nil"
		if observed {
			name = "observer"
		}
		b.Run(name, func(b *testing.B) {
			s := NewManualFrameSource()
			d := New(game.New(game.Config{}), s)
			if observed {
				_ = d.SetObserver(frameObserverFunc(func(FrameEvent) {}))
			}
			d.Start(nil)
			defer d.Stop()
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				s.Fire(float64(i) * 16)
			}
		})
	}
}

func TestFrameObserverAddsNoWarmAllocations(t *testing.T) {
	allocs := func(observed bool) float64 {
		s := NewManualFrameSource()
		d := New(game.New(game.Config{}), s)
		if observed {
			_ = d.SetObserver(frameObserverFunc(func(FrameEvent) {}))
		}
		d.Start(nil)
		defer d.Stop()
		i := 0
		return testing.AllocsPerRun(1000, func() { i++; s.Fire(float64(i) * 16) })
	}
	baseline, observed := allocs(false), allocs(true)
	if observed != baseline {
		t.Fatal("added frame allocations", baseline, observed)
	}
}
