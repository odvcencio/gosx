package videosync

import "math"

// Engine is the top-level drift-correction engine.
// It owns and wires together SyncEngine, DriftCorrector, and PreloadManager.
//
// TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
// All timing arrives as caller-supplied arguments (milliseconds as float64).
type Engine struct {
	cfg        Config
	sync       *SyncEngine
	drift      *DriftCorrector
	preload    *PreloadManager
	lastReason string // reason from the most recent Tick decision
}

// New constructs an Engine with the given config.
// It records no wall-clock time; all timing arrives as caller-supplied args.
func New(cfg Config) *Engine {
	return &Engine{
		cfg:     cfg,
		sync:    NewSyncEngine(cfg),
		drift:   NewDriftCorrector(cfg),
		preload: NewPreloadManager(cfg),
	}
}

// Ingest delegates to the SyncEngine, recording a server heartbeat.
func (e *Engine) Ingest(serverTimeMs uint64, position float32, playing bool, recvPerfMs float64) {
	e.sync.Ingest(serverTimeMs, position, playing, recvPerfMs)
}

// RTT delegates to the SyncEngine, recording a round-trip-time sample (ms).
func (e *Engine) RTT(rttMs float64) {
	e.sync.RTT(rttMs)
}

// OnPlaybackStart resets the warmup epoch for a fresh item.
// It calls drift.OnPlaybackStart and resets the preload manager.
func (e *Engine) OnPlaybackStart(perfNowMs float64) {
	e.drift.OnPlaybackStart(perfNowMs)
	e.preload.Reset()
}

// Tick evaluates one frame of drift correction.
//
// currentTime is the local player's current playback position (seconds).
// perfNowMs is the caller's performance.now() value (milliseconds).
// bufferedAhead is the seconds of video buffered past currentTime.
// paused is true when the local player is paused.
//
// Non-finite currentTime or bufferedAhead → returns none("video not ready").
func (e *Engine) Tick(currentTime, perfNowMs, bufferedAhead float64, paused bool) Decision {
	// Input guard: non-finite currentTime or bufferedAhead → not ready.
	if math.IsNaN(currentTime) || math.IsInf(currentTime, 0) ||
		math.IsNaN(bufferedAhead) || math.IsInf(bufferedAhead, 0) {
		dec := none("video not ready")
		e.lastReason = dec.Reason
		return dec
	}

	// Project server target forward to now.
	target := e.sync.ProjectedTarget(perfNowMs)

	// Drift correction decision.
	dec := e.drift.Calculate(currentTime, target, perfNowMs, !paused)

	// Preload phase (positionError is always finite here; bufferedAhead guard above).
	positionError := math.Abs(currentTime - target)
	phase, ready, stalled := e.preload.Update(bufferedAhead, positionError, perfNowMs)

	// Merge preload into decision.
	dec.Ready = ready
	dec.Stalled = stalled
	dec.PreloadPhase = phase

	e.lastReason = dec.Reason
	return dec
}

// Stats is a read-only snapshot of the engine's internal state.
type Stats struct {
	SeeksTotal, EmergencySeeks, RateChangesTotal, OutOfOrderDropped uint64
	EstimatedLatencyMs, Confidence                                  float64
	CurrentMode                                                     string // "none"|"rate-up"|"rate-down"|"seek-cooldown"
	LastReason                                                      string
}

// correctionModeString converts a CorrectionMode to its canonical string label.
func correctionModeString(m CorrectionMode) string {
	switch m {
	case ModeRateUp:
		return "rate-up"
	case ModeRateDown:
		return "rate-down"
	case ModeSeekCooldown:
		return "seek-cooldown"
	default:
		return "none"
	}
}

// Stats returns a Stats snapshot of the current engine state.
// perfNowMs is forwarded to SyncEngine.Confidence for staleness scoring.
func (e *Engine) Stats(perfNowMs float64) Stats {
	return Stats{
		SeeksTotal:         e.drift.SeeksTotal(),
		EmergencySeeks:     e.drift.EmergencySeeks(),
		RateChangesTotal:   e.drift.RateChangesTotal(),
		OutOfOrderDropped:  e.sync.OutOfOrderDropped(),
		EstimatedLatencyMs: e.sync.EstimatedLatencyMs(),
		Confidence:         e.sync.Confidence(perfNowMs),
		CurrentMode:        correctionModeString(e.drift.CurrentMode()),
		LastReason:         e.lastReason,
	}
}
