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
	// Capture the replay baseline here so it stays out of tick timing.
	r.simMu.Lock()
	r.beginReplayLocked()
	r.simMu.Unlock()
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

// tickLoop wakes once per tick period. Each wake adds Speed ticks to the
// debt, plus the dropped periods when CatchUpBudget is set, then runs the whole
// ticks within the time budget. With defaults this is one tick per wake and
// dropped periods only count as lag.
func (r *Runner) tickLoop(stop <-chan struct{}) {
	ticker := r.clock.NewTicker(r.period)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case received := <-ticker.C():
			if !r.running.Load() {
				return
			}
			var scheduled time.Duration
			if r.observer != nil || r.catchUp > 0 {
				scheduled = ticker.Stamp(received).Monotonic
			}
			r.wake(scheduled)
		}
	}
}

const owedEpsilon = 1e-9

// wake runs the ticks due at one loop wake. scheduled is the ticker deadline
// and is only read when an observer or catch-up is configured.
func (r *Runner) wake(scheduled time.Duration) {
	periods := 1.0
	if r.catchUp > 0 {
		if r.haveWake {
			if missed := int((scheduled-r.lastWake+r.period/2)/r.period) - 1; missed > 0 {
				periods += float64(missed)
			}
		}
		r.lastWake, r.haveWake = scheduled, true
	}
	if r.paused.Load() {
		r.owed = 0
		return
	}
	r.owed += periods * r.Speed()
	if r.catchUp > 0 {
		if limit := float64(r.tickRate); r.owed > limit {
			r.owed = limit
		}
	}
	var begin time.Duration
	if r.catchUp > 0 {
		begin = r.clock.Now().Monotonic
	}
	for ran := 0; r.owed >= 1-owedEpsilon; ran++ {
		if ran > 0 && r.catchUp > 0 && r.clock.Now().Monotonic-begin >= r.catchUp {
			return
		}
		if r.paused.Load() {
			r.owed = 0
			return
		}
		r.owed--
		if r.owed < 0 {
			r.owed = 0
		}
		behind := int(r.owed + owedEpsilon)
		if r.observer == nil {
			r.tickOnce()
		} else {
			r.behind = behind
			r.observeTick(scheduled)
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
		StateBytes: stateBytes, Inputs: inputs, Behind: r.behind,
	})
}

func (r *Runner) tickOnce() (uint64, int, int) {
	inputs := r.DrainInputs()
	inputCount := len(inputs)
	var replayInputs map[string]Input
	if r.recorder != nil {
		replayInputs = cloneInputs(inputs)
	}
	r.simMu.Lock()
	r.beginReplayLocked()
	r.sim.Tick(inputs)

	frame := r.frame.Add(1)
	if r.snapshots != nil && frame%r.snapEvery == 0 {
		r.snapshots.Push(frame, r.sim.Snapshot())
	}
	if r.recorder != nil {
		r.recorder.Record(frame, replayInputs)
	}

	broadcast := true
	if r.bcastEvery > 0 {
		now := r.clock.Now().Monotonic
		if r.haveBcast && now-r.lastBcastAt < r.bcastEvery {
			broadcast = false
		} else {
			r.lastBcastAt, r.haveBcast = now, true
		}
	}
	if !broadcast {
		r.simMu.Unlock()
		return frame, 0, inputCount
	}
	state := r.sim.State()
	r.simMu.Unlock()
	if r.bcast != nil {
		r.bcast.Broadcast(r.hub, frame, state)
	} else {
		r.hub.Broadcast("sim:tick", r.tickPayload(frame, state))
	}
	return frame, len(state), inputCount
}

// beginReplayLocked records the replay baseline once. Caller holds simMu.
func (r *Runner) beginReplayLocked() {
	if r.recorder != nil && !r.recorder.started() {
		r.recorder.begin(r.frame.Load(), r.sim.Snapshot())
	}
}
