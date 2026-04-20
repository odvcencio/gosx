package bundle

import (
	"sort"
	"sync"
)

// frameSampleCapacity is the size of the rolling frame-time window. 120
// samples at 60 fps = 2 seconds of history, enough for a stable p95/p99
// estimate without pinning GB of memory.
const frameSampleCapacity = 120

// droppedFrameThresholdSec is the frame time above which a tick is counted
// as a dropped frame. 1/30 s matches "below 30 fps" — the industry-standard
// threshold for user-visible stutter.
const droppedFrameThresholdSec = 1.0 / 30.0

// FrameStats is a snapshot of per-frame timing + device-health metrics
// exposed by a Renderer. Stats are rolling — a single dropped frame
// contributes to DroppedCount permanently, but percentiles only reflect
// the last frameSampleCapacity frames. Safe to pass by value.
type FrameStats struct {
	// FrameCount is a monotonic counter of Frame() calls that successfully
	// recorded at least one pass.
	FrameCount uint64

	// LastFrameMS is the wall-clock duration of the most recent frame, in
	// milliseconds. Zero on the first frame.
	LastFrameMS float64

	// AvgFrameMS is the arithmetic mean of the rolling window.
	AvgFrameMS float64

	// P50FrameMS is the median frame time over the rolling window.
	P50FrameMS float64

	// P95FrameMS is the 95th-percentile frame time over the rolling window.
	P95FrameMS float64

	// DroppedCount is the total number of frames whose duration exceeded
	// the 30-fps threshold since Renderer construction. Not rolling.
	DroppedCount uint64

	// DeviceLost is true if the underlying GPU device has reported a loss;
	// subsequent frames no-op until the Renderer is destroyed + recreated.
	DeviceLost bool

	// DeviceLostReason and DeviceLostMessage carry the platform's
	// explanation of a loss, when available.
	DeviceLostReason  string
	DeviceLostMessage string
}

// frameStatsRecorder owns the rolling ring buffer + counters behind
// Renderer.Stats. All operations are goroutine-safe; the device-lost
// callback runs on an arbitrary goroutine.
type frameStatsRecorder struct {
	mu sync.Mutex

	samples     [frameSampleCapacity]float64 // seconds per frame
	sampleCount int                          // < frameSampleCapacity until full
	next        int                          // ring index for the next write

	totalFrames  uint64
	droppedCount uint64
	lastFrameSec float64

	deviceLost       bool
	deviceLostReason string
	deviceLostMsg    string
}

// record appends one frame time to the ring and updates counters. Called
// once per Frame call after submission completes.
func (s *frameStatsRecorder) record(durationSec float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples[s.next] = durationSec
	s.next = (s.next + 1) % frameSampleCapacity
	if s.sampleCount < frameSampleCapacity {
		s.sampleCount++
	}
	s.totalFrames++
	s.lastFrameSec = durationSec
	if durationSec > droppedFrameThresholdSec {
		s.droppedCount++
	}
}

// markLost transitions the recorder into the device-lost state. Idempotent.
func (s *frameStatsRecorder) markLost(reason, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deviceLost {
		return
	}
	s.deviceLost = true
	s.deviceLostReason = reason
	s.deviceLostMsg = message
}

// snapshot returns a consistent FrameStats view. Percentiles are computed
// on a sorted copy of the active samples — cheap for 120 entries, avoids
// lock contention for sort-in-place approaches.
func (s *frameStatsRecorder) snapshot() FrameStats {
	s.mu.Lock()
	active := s.sampleCount
	samples := make([]float64, active)
	copy(samples, s.samples[:active])
	out := FrameStats{
		FrameCount:        s.totalFrames,
		LastFrameMS:       s.lastFrameSec * 1000,
		DroppedCount:      s.droppedCount,
		DeviceLost:        s.deviceLost,
		DeviceLostReason:  s.deviceLostReason,
		DeviceLostMessage: s.deviceLostMsg,
	}
	s.mu.Unlock()

	if active == 0 {
		return out
	}
	var sum float64
	for _, v := range samples {
		sum += v
	}
	out.AvgFrameMS = (sum / float64(active)) * 1000

	sort.Float64s(samples)
	p := func(pct float64) float64 {
		idx := int(pct * float64(active-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= active {
			idx = active - 1
		}
		return samples[idx] * 1000
	}
	out.P50FrameMS = p(0.50)
	out.P95FrameMS = p(0.95)
	return out
}

// isLost reports whether the device-lost flag has been set. Fast path
// consumed by Frame() to short-circuit on a dead device.
func (s *frameStatsRecorder) isLost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceLost
}
