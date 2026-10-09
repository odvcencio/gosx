package telemetry

import "m31labs.dev/gosx/sim"

type simObserver struct{ loop *Loop }

func (o simObserver) ObserveTick(e sim.TickEvent) {
	_ = o.loop.Observe(e.Duration, TickInfo{Lag: e.Lag, StateBytes: e.StateBytes, Inputs: e.Inputs})
}

// SimObserver binds the runner's whole-tick event to this handle's loop meter.
// Disabled, inert, foreign or closed meters return nil, preserving the runner's
// path without timing reads. The application still owns the runner and meter.
func (t *Telemetry) SimObserver(l *Loop) sim.Observer {
	if t == nil || !t.Enabled() || l == nil || l.kind == nil || l.kind.owner != t {
		return nil
	}
	if l.slot == nil {
		if l.closed.Load() || t.loops.closed.Load() {
			return nil
		}
	} else {
		l.slot.mu.Lock()
		active := l.slot.kind == l.kind && l.slot.epoch == l.epoch
		l.slot.mu.Unlock()
		if !active {
			return nil
		}
	}
	return simObserver{loop: l}
}
