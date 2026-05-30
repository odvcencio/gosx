// Package videosync implements a video drift-correction engine.
// It is TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
// All timing arrives as caller-supplied arguments (milliseconds as float64).
package videosync

// ActionKind classifies what corrective action the engine recommends.
type ActionKind uint8

const (
	ActionNone ActionKind = 0 // no action needed
	ActionRate ActionKind = 1 // adjust playbackRate
	ActionSeek ActionKind = 2 // seek to target position
)

// PreloadPhase tracks the preload/reveal state machine.
type PreloadPhase uint8

const (
	PhaseIdle       PreloadPhase = 0
	PhaseConnecting PreloadPhase = 1
	PhaseBuffering  PreloadPhase = 2
	PhaseSyncing    PreloadPhase = 3
	PhaseReady      PreloadPhase = 4
	PhaseRevealed   PreloadPhase = 5
)

// Decision is the per-tick output of the engine.
// The hot path marshals this positionally (later task); Reason is debug-only.
type Decision struct {
	Kind         ActionKind
	Rate         float64 // playbackRate to set (when Kind==ActionRate)
	SeekTo       float64 // target position seconds (when Kind==ActionSeek)
	ResetRate    bool    // signal: return to 1.0x
	Ready        bool
	Stalled      bool
	ActualRate   float64
	PreloadPhase PreloadPhase
	Reason       string // debug-only; never crosses the hot path in production
}

// Config holds every tunable for the drift-correction engine.
// Construct via DefaultConfig() to get the canonical proven defaults.
type Config struct {
	ToleranceThreshold     float64 // 0.75 s  — drift below this: no action
	RateThreshold          float64 // 1.3 s   — drift in rate-adjustment band
	SeekThreshold          float64 // 4.0 s   — drift large enough to seek
	EmergencySeekThreshold float64 // 25.0 s  — drift so large to emergency-seek
	RateAdjustmentSlow     float64 // 1.035   — behind small; ahead uses 1/1.035
	RateAdjustmentFast     float64 // 1.065   — behind medium; ahead uses 1/1.065
	LargeDriftAheadRate    float64 // 0.88    — ahead + large-drift fixed rate
	HysteresisCount        int     // 3       — ticks before acting on new state
	SeekCooldownMs         float64 // 5200 ms — minimum gap between seeks
	RateHoldMs             float64 // 2400 ms — minimum time holding a rate
	MaxSeeksPerMinute      int     // 4       — trailing-60000ms seek budget
	WarmupMs               float64 // 6500 ms — engine ignores drift during warmup
	MaxLatencySamples      int     // 10      — latency estimator ring size
	DefaultLatencyMs       float64 // 50 ms   — assumed latency before samples
	MaxRTTMs               float64 // 4000 ms — RTT filter ceiling
	MaxOneWayLatencyMs     float64 // 1200 ms — one-way latency filter ceiling
	MaxOutOfOrderMs        float64 // 750 ms  — out-of-order packet tolerance
	ConfidenceDecayMs      float64 // 30000 ms — latency confidence half-life
	MinBufferAhead         float64 // 5 s     — minimum ahead-buffer for preload
	SyncVerifyThreshold    float64 // 0.5 s   — threshold to declare sync verified
	MaxPreloadMs           float64 // 15000 ms — max time in preload state
	FadeInDurationMs       float64 // 300 ms  — JS-actuation fade hint
}

// DefaultConfig returns the canonical proven defaults for Config.
func DefaultConfig() Config {
	return Config{
		ToleranceThreshold:     0.75,
		RateThreshold:          1.3,
		SeekThreshold:          4.0,
		EmergencySeekThreshold: 25.0,
		RateAdjustmentSlow:     1.035,
		RateAdjustmentFast:     1.065,
		LargeDriftAheadRate:    0.88,
		HysteresisCount:        3,
		SeekCooldownMs:         5200,
		RateHoldMs:             2400,
		MaxSeeksPerMinute:      4,
		WarmupMs:               6500,
		MaxLatencySamples:      10,
		DefaultLatencyMs:       50,
		MaxRTTMs:               4000,
		MaxOneWayLatencyMs:     1200,
		MaxOutOfOrderMs:        750,
		ConfidenceDecayMs:      30000,
		MinBufferAhead:         5,
		SyncVerifyThreshold:    0.5,
		MaxPreloadMs:           15000,
		FadeInDurationMs:       300,
	}
}
