package telemetry

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/sim"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

func TestSimObserverInertClosedForeignAndWholeValidation(t *testing.T) {
	for _, tel := range []*Telemetry{nil, {}} {
		for _, l := range []*Loop{nil, {}} {
			if tel.SimObserver(l) != nil {
				t.Fatal("inert adapter")
			}
		}
	}
	l := portableLoop(t)
	tel := l.kind.owner
	tel.active.Store(true)
	if tel.SimObserver(&Loop{}) != nil || (&Telemetry{}).SimObserver(l) != nil {
		t.Fatal("foreign/inert adapter")
	}
	o := tel.SimObserver(l)
	if o == nil {
		t.Fatal("live adapter missing")
	}
	for _, e := range []sim.TickEvent{{Duration: -1}, {Lag: -1}, {StateBytes: -1}, {Inputs: -1}} {
		o.ObserveTick(e)
	}
	if l.Health().Samples != 0 {
		t.Fatal(l.Health())
	}
	o.ObserveTick(sim.TickEvent{Duration: 2 * time.Millisecond, Lag: time.Millisecond, StateBytes: 64, Inputs: 2})
	if h := l.Health(); h.Samples != 1 || h.MaxMS != 2 || h.Overruns != 1 {
		t.Fatal(h)
	}
	if allocs := testing.AllocsPerRun(1000, func() { o.ObserveTick(sim.TickEvent{Duration: 1}) }); allocs != 0 {
		t.Fatal(allocs)
	}
	l.slot.mu.Lock()
	l.slot.kind = nil // source cleanup / reused slot, with no native application owner
	l.slot.mu.Unlock()
	if tel.SimObserver(l) != nil {
		t.Fatal("closed adapter")
	}
	o.ObserveTick(sim.TickEvent{Duration: time.Second})
	if l.slot.data.samples != 1002 {
		t.Fatal("closed callback changed bins", l.slot.data.samples)
	}
	tel.active.Store(false)
	if tel.SimObserver(l) != nil {
		t.Fatal("disabled adapter")
	}
}

func TestGameNewRunnerInheritsSimulationObserver(t *testing.T) {
	c := telemetrytest.NewClock(time.Unix(0, 0))
	runtime := game.New(game.Config{FixedStep: time.Millisecond})
	events := make(chan sim.TickEvent, 1)
	r := game.NewRunner(hub.New("test"), runtime, sim.Options{TickRate: 10, Clock: c, Observer: simObserverFunc(func(e sim.TickEvent) { events <- e })})
	r.Start()
	t.Cleanup(r.Stop)
	deadline := time.Now().Add(time.Second)
	for c.PendingTimers() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.PendingTimers() != 1 {
		t.Fatal("runner did not own exactly one ticker")
	}
	_ = c.Advance(100 * time.Millisecond)
	select {
	case e := <-events:
		if e.Frame != 1 || e.Lag != 0 || e.StateBytes == 0 {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("game runner did not inherit observer")
	}
	r.Stop()
	if c.PendingTimers() != 0 {
		t.Fatal("ticker retained")
	}
}

type simObserverFunc func(sim.TickEvent)

func (f simObserverFunc) ObserveTick(e sim.TickEvent) { f(e) }

func BenchmarkSimObserver(b *testing.B) {
	l := portableLoop(b)
	l.kind.owner.active.Store(true)
	o := l.kind.owner.SimObserver(l)
	e := sim.TickEvent{Duration: time.Millisecond, Lag: time.Millisecond, StateBytes: 1024, Inputs: 2}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		o.ObserveTick(e)
	}
}
