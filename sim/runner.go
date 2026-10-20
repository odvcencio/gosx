package sim

import "time"

// Start begins the fixed-rate tick loop in a background goroutine. It is a
// no-op while an earlier run is active or finishing.
func (r *Runner) Start() {
	if r == nil {
		return
	}
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if r.done != nil {
		return
	}
	r.stop, r.done = make(chan struct{}), make(chan struct{})
	r.running.Store(true)
	stop := r.stop
	go func() {
		defer func() {
			r.runMu.Lock()
			defer r.runMu.Unlock()
			r.running.Store(false)
			close(r.done)
			r.stop, r.done = nil, nil
		}()
		r.tickLoop(stop)
	}()
}

// Stop interrupts the ticker wait, then joins the current tick and observer.
// Call it from the owner, rather than from that run's synchronous callbacks.
func (r *Runner) Stop() {
	if r == nil {
		return
	}
	r.runMu.Lock()
	done := r.done
	if r.running.Swap(false) {
		close(r.stop)
	}
	r.runMu.Unlock()
	if done != nil {
		<-done
	}
}

// Frame returns the current frame number.
func (r *Runner) Frame() uint64 {
	return r.frame.Load()
}

// tickLoop consumes the existing ticker's scheduled coordinate. Dropped
// periods contribute lag; they never trigger a catch-up loop.
func (r *Runner) tickLoop(stop <-chan struct{}) {
	ticker := r.clock.NewTicker(time.Second / time.Duration(r.tickRate))
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case received := <-ticker.C():
			if !r.running.Load() {
				return
			}
			if r.observer == nil {
				r.tickOnce()
			} else {
				r.observeTick(ticker.Stamp(received).Monotonic)
			}
		}
	}
}

func (r *Runner) observeTick(scheduled time.Duration) {
	start := r.clock.Now().Monotonic
	lag := start - scheduled
	if lag < 0 {
		lag = 0
	}
	frame, stateBytes, inputs := r.tickOnce()
	r.observer.ObserveTick(TickEvent{
		Frame: frame, Duration: r.clock.Now().Monotonic - start, Lag: lag,
		StateBytes: stateBytes, Inputs: inputs,
	})
}

func (r *Runner) tickOnce() (uint64, int, int) {
	inputs := r.DrainInputs()
	inputCount := len(inputs)
	var replayInputs map[string]Input
	if r.recorder != nil {
		replayInputs = cloneInputs(inputs)
	}
	r.sim.Tick(inputs)

	frame := r.frame.Add(1)
	r.snapshots.Push(frame, r.sim.Snapshot())
	if r.recorder != nil {
		r.recorder.Record(frame, replayInputs)
	}

	state := r.sim.State()
	r.hub.Broadcast("sim:tick", r.tickPayload(frame, state))
	return frame, len(state), inputCount
}
