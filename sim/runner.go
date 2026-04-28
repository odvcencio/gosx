package sim

import "time"

// Start begins the fixed-rate tick loop in a background goroutine.
func (r *Runner) Start() {
	if r == nil || !r.running.CompareAndSwap(false, true) {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.tickLoop()
	}()
}

// Stop signals the tick loop to end.
func (r *Runner) Stop() {
	if r == nil || !r.running.Swap(false) {
		return
	}
	r.wg.Wait()
}

// Frame returns the current frame number.
func (r *Runner) Frame() uint64 {
	return r.frame.Load()
}

// tickLoop runs the simulation at a fixed rate until stopped.
func (r *Runner) tickLoop() {
	ticker := time.NewTicker(time.Second / time.Duration(r.tickRate))
	defer ticker.Stop()

	for r.running.Load() {
		<-ticker.C
		if !r.running.Load() {
			break
		}

		r.tickOnce()
	}
}

func (r *Runner) tickOnce() {
	inputs := r.DrainInputs()
	replayInputs := cloneInputs(inputs)
	r.sim.Tick(inputs)

	frame := r.frame.Add(1)
	r.snapshots.Push(frame, r.sim.Snapshot())
	r.recorder.Record(frame, replayInputs)

	state := r.sim.State()
	r.hub.Broadcast("sim:tick", r.tickPayload(frame, state))
}
