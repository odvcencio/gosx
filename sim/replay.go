package sim

import "sync"

// ReplayFrame records the inputs for a single simulation tick.
type ReplayFrame struct {
	Frame  uint64
	Inputs map[string]Input
}

// ReplayLog is the complete input log for a simulation run.
//
// To replay, call Simulation.Restore(Initial), seed any RNG with Seed, then
// call Tick with each frame's Inputs in order.
type ReplayLog struct {
	// Initial is the Simulation snapshot taken before Frames[0] ran.
	Initial []byte
	// InitialFrame is the runner frame number of Initial (0 for a fresh run).
	InitialFrame uint64
	// Seed is Options.Seed, for simulations that derive their RNG from it.
	Seed uint64
	// Truncated reports that recording stopped at Options.ReplayMaxFrames.
	Truncated bool
	Frames    []ReplayFrame
}

// replayRecorder captures per-frame inputs for deterministic replay.
type replayRecorder struct {
	mu           sync.Mutex
	frames       []ReplayFrame
	initial      []byte
	initialFrame uint64
	hasInitial   bool
	seed         uint64
	max          int
	truncated    bool
}

// begin records the starting snapshot and discards any earlier frames.
func (r *replayRecorder) begin(frame uint64, snapshot []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames, r.truncated = nil, false
	r.initial = append([]byte(nil), snapshot...)
	r.initialFrame, r.hasInitial = frame, true
}

func (r *replayRecorder) started() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasInitial
}

// newReplayRecorder creates an empty recorder.
func newReplayRecorder() *replayRecorder {
	return &replayRecorder{}
}

// Record stores a deep copy of the inputs for the given frame.
func (r *replayRecorder) Record(frame uint64, inputs map[string]Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.max > 0 && len(r.frames) >= r.max {
		r.truncated = true
		return
	}
	r.frames = append(r.frames, ReplayFrame{Frame: frame, Inputs: cloneInputs(inputs)})
}

// Finish returns the complete replay log.
func (r *replayRecorder) Finish() ReplayLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	frames := make([]ReplayFrame, len(r.frames))
	for i, frame := range r.frames {
		frames[i] = ReplayFrame{Frame: frame.Frame, Inputs: cloneInputs(frame.Inputs)}
	}
	return ReplayLog{
		Initial:      append([]byte(nil), r.initial...),
		InitialFrame: r.initialFrame,
		Seed:         r.seed,
		Truncated:    r.truncated,
		Frames:       frames,
	}
}
