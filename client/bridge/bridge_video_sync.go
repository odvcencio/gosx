//go:build !gosx_tiny_islands_only

// Video-sync bridge — per-mount Engine registry.
//
// This is the host-side bridge layer for the video drift-correction engine
// (client/videosync). It mirrors the engine-surface registry pattern: a
// map on *Bridge keyed by mount id, unknown-id safe no-ops, idempotent
// Dispose. Phase 2b (the WASM layer, client/wasm/video_sync_full.go) wraps
// these entry points and converts the hot-path Decision struct to positional
// JS numerics; no JSON marshaling happens per Tick here.
//
// The registry is lazily inited in each method (same pattern as engineSurfaces
// which is pre-inited in New()). Cold JSON (json.Unmarshal for config, json.Marshal
// for stats) is fine — encoding/json is already a bridge dependency.

package bridge

import (
	"encoding/json"

	"m31labs.dev/gosx/client/videosync"
)

// noneDecision is the zero Decision returned when Tick is called on an
// unknown or disposed id. Kind=ActionNone, ActualRate=1.0 to avoid a
// division-by-zero at the call site if the consumer divides by ActualRate.
var noneDecision = videosync.Decision{
	Kind:       videosync.ActionNone,
	ActualRate: 1.0,
}

// NewVideoSync registers a new videosync.Engine under id.
// configJSON may be empty or "" to use DefaultConfig; otherwise it must be
// a valid JSON object matching videosync.Config field names.
// Re-registering the same id silently replaces the prior engine.
func (b *Bridge) NewVideoSync(id string, configJSON string) error {
	cfg := videosync.DefaultConfig()
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return err
		}
	}
	if b.videoSyncEngines == nil {
		b.videoSyncEngines = make(map[string]*videosync.Engine)
	}
	b.videoSyncEngines[id] = videosync.New(cfg)
	return nil
}

// IngestVideoSync delivers a server heartbeat to the engine registered under id.
// Unknown id is a safe no-op.
func (b *Bridge) IngestVideoSync(id string, serverTimeMs uint64, position float32, playing bool, recvPerfMs float64) {
	if e, ok := b.videoSyncEngines[id]; ok {
		e.Ingest(serverTimeMs, position, playing, recvPerfMs)
	}
}

// RTTVideoSync records a round-trip-time sample (ms) for the engine under id.
// Unknown id is a safe no-op.
func (b *Bridge) RTTVideoSync(id string, rttMs float64) {
	if e, ok := b.videoSyncEngines[id]; ok {
		e.RTT(rttMs)
	}
}

// TickVideoSync evaluates one frame of drift correction for the engine under id.
// Unknown or disposed id returns noneDecision (Kind=ActionNone, ActualRate=1.0)
// and never panics — the rAF callback often outlives the explicit dispose path.
func (b *Bridge) TickVideoSync(id string, currentTime, perfNowMs, bufferedAhead float64, paused bool) videosync.Decision {
	if e, ok := b.videoSyncEngines[id]; ok {
		return e.Tick(currentTime, perfNowMs, bufferedAhead, paused)
	}
	return noneDecision
}

// OnPlaybackStartVideoSync resets the warmup epoch for the engine under id.
// Unknown id is a safe no-op.
func (b *Bridge) OnPlaybackStartVideoSync(id string, perfNowMs float64) {
	if e, ok := b.videoSyncEngines[id]; ok {
		e.OnPlaybackStart(perfNowMs)
	}
}

// StatsVideoSync returns a JSON-encoded videosync.Stats snapshot for the engine
// under id. Unknown id returns "{}". perfNowMs is forwarded to
// engine.Stats for staleness scoring (pass 0 if not available).
func (b *Bridge) StatsVideoSync(id string, perfNowMs float64) (string, error) {
	e, ok := b.videoSyncEngines[id]
	if !ok {
		return "{}", nil
	}
	data, err := json.Marshal(e.Stats(perfNowMs))
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

// LastReasonVideoSync returns the LastReason field from the engine's Stats.
// Unknown id returns "".
func (b *Bridge) LastReasonVideoSync(id string) string {
	if e, ok := b.videoSyncEngines[id]; ok {
		return e.Stats(0).LastReason
	}
	return ""
}

// DisposeVideoSync removes the engine registered under id. Idempotent —
// calling twice on the same id is safe.
func (b *Bridge) DisposeVideoSync(id string) {
	delete(b.videoSyncEngines, id)
}

// VideoSyncCount reports the number of live videosync engine instances.
// Exposed primarily for tests.
func (b *Bridge) VideoSyncCount() int {
	return len(b.videoSyncEngines)
}
