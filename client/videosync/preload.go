package videosync

import "math"

// PreloadManager is a pure phase calculator for the invisible-join state machine.
// It tracks buffering, sync-verification, and reveal phases without any wall-clock
// calls — all timing arrives as caller-supplied performance.now() milliseconds.
//
// TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
type PreloadManager struct {
	cfg      Config
	started  bool
	startMs  float64
	revealed bool
}

// NewPreloadManager constructs a PreloadManager with the given config.
func NewPreloadManager(cfg Config) *PreloadManager {
	return &PreloadManager{cfg: cfg}
}

// Update computes the current PreloadPhase from:
//   - bufferedAhead: seconds of video buffered past the current position (caller-computed).
//   - positionError: abs(currentTime - target) in seconds (caller-computed).
//   - perfNowMs: caller's performance.now() in milliseconds.
//
// Returns (phase, ready, stalled).
//
// Decision order:
//  1. If non-finite bufferedAhead or positionError → treat as not-ready (PhaseBuffering, stalled).
//  2. If already revealed → (PhaseRevealed, true, false).
//  3. Lazily record startMs on first call.
//  4. If bufferedAhead < MinBufferAhead AND NOT deadline exceeded → (PhaseBuffering, false, true).
//  5. If positionError > SyncVerifyThreshold AND NOT deadline exceeded → (PhaseSyncing, false, false).
//  6. Otherwise (sync verified or deadline exceeded) → set revealed=true, return (PhaseReady, true, false).
//     Subsequent calls return (PhaseRevealed, true, false).
func (p *PreloadManager) Update(bufferedAhead, positionError, perfNowMs float64) (PreloadPhase, bool, bool) {
	// Guard: non-finite inputs → not ready.
	if math.IsNaN(bufferedAhead) || math.IsInf(bufferedAhead, 0) ||
		math.IsNaN(positionError) || math.IsInf(positionError, 0) {
		return PhaseBuffering, false, true
	}

	// Already revealed.
	if p.revealed {
		return PhaseRevealed, true, false
	}

	// Lazily record start time on first Update call.
	if !p.started {
		p.startMs = perfNowMs
		p.started = true
	}

	deadlineExceeded := (perfNowMs - p.startMs) > p.cfg.MaxPreloadMs

	// Under-buffered and within deadline → stall.
	if bufferedAhead < p.cfg.MinBufferAhead && !deadlineExceeded {
		return PhaseBuffering, false, true
	}

	// Not yet in sync and within deadline → syncing.
	if positionError > p.cfg.SyncVerifyThreshold && !deadlineExceeded {
		return PhaseSyncing, false, false
	}

	// Sync verified (or deadline forced reveal).
	p.revealed = true
	return PhaseReady, true, false
}

// Reset clears the started/startMs/revealed state so the manager can be reused.
// It does NOT modify the config.
func (p *PreloadManager) Reset() {
	p.started = false
	p.startMs = 0
	p.revealed = false
}

// Revealed reports whether the manager has already transitioned to the revealed state.
func (p *PreloadManager) Revealed() bool {
	return p.revealed
}
