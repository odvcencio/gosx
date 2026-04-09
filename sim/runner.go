package sim

import "time"

// Start begins the fixed-rate tick loop in a background goroutine.
func (r *Runner) Start() {
	r.running.Store(true)
	go r.tickLoop()
}

// Stop signals the tick loop to end.
func (r *Runner) Stop() {
	r.running.Store(false)
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

		inputs := r.DrainInputs()
		r.sim.Tick(inputs)

		frame := r.frame.Add(1)
		r.snapshots.Push(frame, r.sim.Snapshot())

		state := r.sim.State()
		r.hub.Broadcast("sim:tick", map[string]any{
			"frame": frame,
			"state": state,
		})
	}
}
