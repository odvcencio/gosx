package sim

import "sync"

// ReplayFrame records the inputs for a single simulation tick.
type ReplayFrame struct {
	Frame  uint64
	Inputs map[string]Input
}

// ReplayLog is the complete input log for a simulation run.
type ReplayLog struct {
	Frames []ReplayFrame
}

// replayRecorder captures per-frame inputs for deterministic replay.
type replayRecorder struct {
	mu     sync.Mutex
	frames []ReplayFrame
}

// newReplayRecorder creates an empty recorder.
func newReplayRecorder() *replayRecorder {
	return &replayRecorder{}
}

// Record stores a deep copy of the inputs for the given frame.
func (r *replayRecorder) Record(frame uint64, inputs map[string]Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	return ReplayLog{Frames: frames}
}
