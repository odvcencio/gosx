package videosync

import "math"

// SyncEngine tracks network latency, projects server position forward in time,
// detects out-of-order heartbeats, and computes a staleness confidence score.
//
// TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
// All timing is caller-supplied (milliseconds as float64).
type SyncEngine struct {
	cfg Config

	// Latency ring: holds the most-recent cfg.MaxLatencySamples one-way latency
	// values (ms). Dynamically sized; oldest entry evicted when full.
	latencySamples []float64

	// Heartbeat state — set on each accepted Ingest call.
	hasHeartbeat     bool
	position         float64 // seconds, clamped >= 0
	playing          bool
	lastServerTimeMs uint64
	recvPerfMs       float64

	// Cumulative count of out-of-order drops.
	outOfOrderDropped uint64
}

// NewSyncEngine constructs a SyncEngine with the given config.
func NewSyncEngine(cfg Config) *SyncEngine {
	return &SyncEngine{
		cfg:            cfg,
		latencySamples: make([]float64, 0, cfg.MaxLatencySamples),
	}
}

// Ingest processes a server heartbeat.
//
// Non-finite positions and out-of-order packets are silently dropped.
// serverTimeMs is the server's media-time stamp in milliseconds.
// position is the server's current playback position in seconds.
// playing indicates whether the server is currently playing.
// recvPerfMs is the caller's performance.now() value at receive time (ms).
func (se *SyncEngine) Ingest(serverTimeMs uint64, position float32, playing bool, recvPerfMs float64) {
	// Drop non-finite positions.
	p64 := float64(position)
	if math.IsNaN(p64) || math.IsInf(p64, 0) {
		return
	}

	// Out-of-order rejection: drop if this packet is more than MaxOutOfOrderMs
	// behind the last accepted packet.
	if se.hasHeartbeat {
		// float64 arithmetic to avoid overflow on subtraction.
		if float64(serverTimeMs)+se.cfg.MaxOutOfOrderMs < float64(se.lastServerTimeMs) {
			se.outOfOrderDropped++
			return
		}
	}

	// Accept the heartbeat.
	if p64 < 0 {
		p64 = 0
	}
	se.position = p64
	se.playing = playing
	se.lastServerTimeMs = serverTimeMs
	se.recvPerfMs = recvPerfMs
	se.hasHeartbeat = true
}

// RTT records a round-trip-time sample (ms) and converts it to a one-way
// latency estimate stored in the latency ring.
//
// Rejected (no sample added) if rttMs is not finite, <= 0, or > MaxRTTMs.
func (se *SyncEngine) RTT(rttMs float64) {
	if math.IsNaN(rttMs) || math.IsInf(rttMs, 0) {
		return
	}
	if rttMs <= 0 || rttMs > se.cfg.MaxRTTMs {
		return
	}
	oneWay := math.Min(rttMs/2, se.cfg.MaxOneWayLatencyMs)
	max := se.cfg.MaxLatencySamples
	if len(se.latencySamples) < max {
		se.latencySamples = append(se.latencySamples, oneWay)
	} else {
		// Shift oldest out (index 0) and append new value at the end.
		copy(se.latencySamples, se.latencySamples[1:])
		se.latencySamples[max-1] = oneWay
	}
}

// EstimatedLatencyMs returns the median of the current latency samples.
//
// Even-length ring: mean of the two middle values.
// No samples: returns cfg.DefaultLatencyMs (never 0).
func (se *SyncEngine) EstimatedLatencyMs() float64 {
	n := len(se.latencySamples)
	if n == 0 {
		return se.cfg.DefaultLatencyMs
	}
	// Copy into a scratch slice and sort without mutating the ring.
	scratch := make([]float64, n)
	copy(scratch, se.latencySamples)
	sortFloat64s(scratch)

	mid := n / 2
	if n%2 == 1 {
		return scratch[mid]
	}
	return (scratch[mid-1] + scratch[mid]) / 2.0
}

// ProjectedTarget returns the estimated current server playback position
// in seconds, accounting for network latency and elapsed time since receive.
//
// If no heartbeat has been accepted yet, returns 0.
// If paused, returns the last known position (bare, no projection).
func (se *SyncEngine) ProjectedTarget(perfNowMs float64) float64 {
	if !se.hasHeartbeat {
		return 0
	}
	if !se.playing {
		return se.position
	}
	elapsedMs := math.Max(0, perfNowMs-se.recvPerfMs)
	return se.position + (elapsedMs+se.EstimatedLatencyMs())/1000.0
}

// Confidence returns a staleness score in [0, 1] that decays linearly from 1
// (just received) to 0 (cfg.ConfidenceDecayMs ms after last receive).
//
// Returns 0 if no heartbeat has been accepted yet.
func (se *SyncEngine) Confidence(perfNowMs float64) float64 {
	if !se.hasHeartbeat {
		return 0
	}
	return math.Max(0, 1-(perfNowMs-se.recvPerfMs)/se.cfg.ConfidenceDecayMs)
}

// LastPosition returns the most-recently accepted server position (seconds).
func (se *SyncEngine) LastPosition() float64 { return se.position }

// LastPlaying returns whether the most-recently accepted heartbeat was playing.
func (se *SyncEngine) LastPlaying() bool { return se.playing }

// OutOfOrderDropped returns the cumulative count of dropped out-of-order packets.
func (se *SyncEngine) OutOfOrderDropped() uint64 { return se.outOfOrderDropped }

// sortFloat64s sorts a slice of float64 in ascending order (insertion sort —
// samples are small, typically ≤ 10 elements).
func sortFloat64s(s []float64) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
