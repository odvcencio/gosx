//go:build gosx_tiny_islands_only

// Video-sync bridge stubs — islands-only build.
//
// The islands-only build elides engine/surface and the scene-engine; videosync
// itself is pure Go and is included in every build (the Bridge struct field
// videoSyncEngines lives in bridge.go). These stubs expose the same method
// surface as bridge_video_sync.go so any caller compiled against the
// islands-only build gets a consistent API: NewVideoSync returns an error and
// every other method is a silent no-op or zero-value return, matching the
// engine-surface and canvas-board islands stubs.

package bridge

import (
	"fmt"

	"m31labs.dev/gosx/client/videosync"
)

// NewVideoSync returns an error in the islands-only build — video sync is
// not available in builds that strip the engine dependencies.
func (b *Bridge) NewVideoSync(id string, configJSON string) error {
	return fmt.Errorf("video sync unavailable in islands-only build")
}

// IngestVideoSync is a silent no-op in the islands-only build.
func (b *Bridge) IngestVideoSync(id string, serverTimeMs uint64, position float32, playing bool, recvPerfMs float64) {
}

// RTTVideoSync is a silent no-op in the islands-only build.
func (b *Bridge) RTTVideoSync(id string, rttMs float64) {}

// TickVideoSync returns a none Decision (Kind=ActionNone, ActualRate=1.0) in
// the islands-only build.
func (b *Bridge) TickVideoSync(id string, currentTime, perfNowMs, bufferedAhead float64, paused bool) videosync.Decision {
	return videosync.Decision{Kind: videosync.ActionNone, ActualRate: 1.0}
}

// OnPlaybackStartVideoSync is a silent no-op in the islands-only build.
func (b *Bridge) OnPlaybackStartVideoSync(id string, perfNowMs float64) {}

// StatsVideoSync returns "{}" in the islands-only build.
func (b *Bridge) StatsVideoSync(id string, perfNowMs float64) (string, error) {
	return "{}", nil
}

// LastReasonVideoSync returns "" in the islands-only build.
func (b *Bridge) LastReasonVideoSync(id string) string { return "" }

// DisposeVideoSync is a silent no-op in the islands-only build.
func (b *Bridge) DisposeVideoSync(id string) {}

// VideoSyncCount always returns 0 in the islands-only build.
func (b *Bridge) VideoSyncCount() int { return 0 }
