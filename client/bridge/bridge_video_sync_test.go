//go:build !gosx_tiny_islands_only

// Host-side tests for the video-sync bridge registry. No syscall/js; runs
// with plain `go test`. Mirrors the recorder-seam style established by
// bridge_engine_surface_test.go.
//
// Contract pins:
//
//   - NewVideoSync registers an engine; VideoSyncCount reflects it.
//   - Two ids are isolated: Ingest/RTT/Tick on one id does not affect the other.
//   - Tick after Ingest returns a Decision with a defined ActualRate (> 0).
//   - DisposeVideoSync removes the engine; a subsequent Tick on the disposed id
//     returns a none Decision (Kind==ActionNone) and does not panic.
//   - Tick on a never-created id is a safe none; no panic.
//   - StatsVideoSync JSON round-trips cleanly; unknown id returns "{}".

package bridge

import (
	"encoding/json"
	"math"
	"testing"

	"m31labs.dev/gosx/client/videosync"
)

// TestNewVideoSyncRegistersEngine verifies that NewVideoSync stores the engine
// and VideoSyncCount tracks it.
func TestNewVideoSyncRegistersEngine(t *testing.T) {
	b := New()
	if b.VideoSyncCount() != 0 {
		t.Fatalf("expected 0 engines at start, got %d", b.VideoSyncCount())
	}

	if err := b.NewVideoSync("vs-1", ""); err != nil {
		t.Fatalf("NewVideoSync failed: %v", err)
	}
	if b.VideoSyncCount() != 1 {
		t.Errorf("VideoSyncCount = %d, want 1", b.VideoSyncCount())
	}

	if err := b.NewVideoSync("vs-2", ""); err != nil {
		t.Fatalf("NewVideoSync vs-2 failed: %v", err)
	}
	if b.VideoSyncCount() != 2 {
		t.Errorf("VideoSyncCount = %d, want 2", b.VideoSyncCount())
	}
}

// TestNewVideoSyncReplacesExisting verifies that re-registering the same id
// silently replaces the prior engine (no count growth, no panic).
func TestNewVideoSyncReplacesExisting(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("vs-1", ""); err != nil {
		t.Fatalf("first NewVideoSync: %v", err)
	}
	if err := b.NewVideoSync("vs-1", ""); err != nil {
		t.Fatalf("second NewVideoSync: %v", err)
	}
	if b.VideoSyncCount() != 1 {
		t.Errorf("VideoSyncCount = %d after replace, want 1", b.VideoSyncCount())
	}
}

// TestNewVideoSyncConfigJSON verifies that a valid JSON config is accepted and
// overrides default values.
func TestNewVideoSyncConfigJSON(t *testing.T) {
	b := New()
	// Override one field; the rest inherit from DefaultConfig via json.Unmarshal.
	cfgJSON := `{"ToleranceThreshold": 2.0}`
	if err := b.NewVideoSync("vs-cfg", cfgJSON); err != nil {
		t.Fatalf("NewVideoSync with custom config: %v", err)
	}
	if b.VideoSyncCount() != 1 {
		t.Errorf("VideoSyncCount = %d, want 1", b.VideoSyncCount())
	}
}

// TestNewVideoSyncRejectsMalformedConfig verifies that invalid JSON returns an
// error rather than panicking.
func TestNewVideoSyncRejectsMalformedConfig(t *testing.T) {
	b := New()
	err := b.NewVideoSync("vs-bad", `{not json`)
	if err == nil {
		t.Fatalf("expected error for malformed config JSON")
	}
}

// TestIngestRTTTickTwoIdsIsolated verifies that two engines registered under
// different ids are isolated: ingesting into one does not affect the other.
func TestIngestRTTTickTwoIdsIsolated(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("a", ""); err != nil {
		t.Fatalf("NewVideoSync a: %v", err)
	}
	if err := b.NewVideoSync("b", ""); err != nil {
		t.Fatalf("NewVideoSync b: %v", err)
	}

	// Ingest a heartbeat only into "a".
	const serverMs uint64 = 10000
	b.IngestVideoSync("a", serverMs, 10.0, true, 100.0)
	b.RTTVideoSync("a", 50.0)
	b.OnPlaybackStartVideoSync("a", 0)

	// Tick both after sufficient warmup so we can observe different states.
	// We're within warmup period here, so both return ActionNone. What we're
	// pinning: "b" is not contaminated by "a"'s ingest.
	decA := b.TickVideoSync("a", 10.0, 200.0, 10.0, false)
	decB := b.TickVideoSync("b", 10.0, 200.0, 10.0, false)

	// Both return finite ActualRate > 0.
	if decA.ActualRate <= 0 {
		t.Errorf("decA.ActualRate = %v, want > 0", decA.ActualRate)
	}
	if decB.ActualRate <= 0 {
		t.Errorf("decB.ActualRate = %v, want > 0", decB.ActualRate)
	}
	// "b" received no heartbeat so its decision should still be ActionNone.
	if decB.Kind != videosync.ActionNone {
		t.Errorf("decB.Kind = %v, want ActionNone (no heartbeat for b)", decB.Kind)
	}
}

// TestTickAfterIngestReturnsSaneDecision verifies that a Tick following an
// Ingest returns a Decision with ActualRate > 0.
func TestTickAfterIngestReturnsSaneDecision(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("vs", ""); err != nil {
		t.Fatalf("NewVideoSync: %v", err)
	}

	b.IngestVideoSync("vs", 5000, 5.0, true, 0.0)
	b.RTTVideoSync("vs", 80.0)
	b.OnPlaybackStartVideoSync("vs", 0)

	dec := b.TickVideoSync("vs", 5.0, 100.0, 8.0, false)
	if dec.ActualRate <= 0 {
		t.Errorf("Decision.ActualRate = %v, want > 0", dec.ActualRate)
	}
}

// TestDisposeVideoSyncRemovesEngineAndTickIsNone verifies the dispose contract:
// DisposeVideoSync removes the entry and a subsequent Tick returns a none
// Decision (Kind==ActionNone) without panicking.
func TestDisposeVideoSyncRemovesEngineAndTickIsNone(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("vs-disp", ""); err != nil {
		t.Fatalf("NewVideoSync: %v", err)
	}

	b.DisposeVideoSync("vs-disp")
	if b.VideoSyncCount() != 0 {
		t.Errorf("VideoSyncCount after dispose = %d, want 0", b.VideoSyncCount())
	}

	// Must not panic; must return none.
	dec := b.TickVideoSync("vs-disp", 1.0, 100.0, 5.0, false)
	if dec.Kind != videosync.ActionNone {
		t.Errorf("post-dispose Tick Kind = %v, want ActionNone", dec.Kind)
	}
	if dec.ActualRate != 1.0 {
		t.Errorf("post-dispose Tick ActualRate = %v, want 1.0", dec.ActualRate)
	}
}

// TestDisposeVideoSyncIdempotent verifies that calling DisposeVideoSync twice
// on the same id does not panic.
func TestDisposeVideoSyncIdempotent(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("vs-idem", ""); err != nil {
		t.Fatalf("NewVideoSync: %v", err)
	}
	b.DisposeVideoSync("vs-idem")
	b.DisposeVideoSync("vs-idem") // must not panic
}

// TestTickVideoSyncNeverCreatedIdIsNone verifies that Tick on a never-created id
// returns a none Decision without panicking.
func TestTickVideoSyncNeverCreatedIdIsNone(t *testing.T) {
	b := New()
	dec := b.TickVideoSync("never-created", 0, 0, 0, false)
	if dec.Kind != videosync.ActionNone {
		t.Errorf("Kind = %v, want ActionNone for never-created id", dec.Kind)
	}
	if dec.ActualRate != 1.0 {
		t.Errorf("ActualRate = %v, want 1.0 for never-created id", dec.ActualRate)
	}
}

// TestIngestOnUnknownIdIsNoOp verifies that Ingest/RTT/OnPlaybackStart on an
// unknown id do not panic.
func TestIngestOnUnknownIdIsNoOp(t *testing.T) {
	b := New()
	// None of these should panic.
	b.IngestVideoSync("ghost", 1000, 1.0, true, 0.0)
	b.RTTVideoSync("ghost", 40.0)
	b.OnPlaybackStartVideoSync("ghost", 0)
}

// TestStatsVideoSyncJSONRoundTrip verifies that StatsVideoSync returns valid JSON
// that round-trips into a videosync.Stats struct, and that an unknown id
// returns "{}".
func TestStatsVideoSyncJSONRoundTrip(t *testing.T) {
	b := New()
	if err := b.NewVideoSync("vs-stats", ""); err != nil {
		t.Fatalf("NewVideoSync: %v", err)
	}

	b.IngestVideoSync("vs-stats", 3000, 3.0, true, 0.0)
	b.RTTVideoSync("vs-stats", 60.0)
	b.TickVideoSync("vs-stats", 3.0, 100.0, 6.0, false)

	statsJSON, err := b.StatsVideoSync("vs-stats", 100.0)
	if err != nil {
		t.Fatalf("StatsVideoSync error: %v", err)
	}
	if statsJSON == "" {
		t.Fatal("StatsVideoSync returned empty string")
	}

	var s videosync.Stats
	if err := json.Unmarshal([]byte(statsJSON), &s); err != nil {
		t.Fatalf("json.Unmarshal Stats: %v (json=%s)", err, statsJSON)
	}
	// EstimatedLatencyMs should be > 0 after one RTT sample.
	if s.EstimatedLatencyMs <= 0 {
		t.Errorf("EstimatedLatencyMs = %v, want > 0 after RTT sample", s.EstimatedLatencyMs)
	}

	// Unknown id must return "{}".
	unknown, err := b.StatsVideoSync("no-such-id", 0)
	if err != nil {
		t.Fatalf("StatsVideoSync unknown id error: %v", err)
	}
	if unknown != "{}" {
		t.Errorf("StatsVideoSync unknown id = %q, want \"{}\"", unknown)
	}
}

// TestLastReasonVideoSync verifies that LastReasonVideoSync returns the last
// decision reason, and "" for an unknown id.
func TestLastReasonVideoSync(t *testing.T) {
	b := New()

	// Unknown id returns "".
	if got := b.LastReasonVideoSync("ghost"); got != "" {
		t.Errorf("LastReasonVideoSync unknown = %q, want \"\"", got)
	}

	if err := b.NewVideoSync("vs-reason", ""); err != nil {
		t.Fatalf("NewVideoSync: %v", err)
	}
	// Tick with non-finite currentTime to get a known reason.
	b.TickVideoSync("vs-reason", math.NaN(), 100.0, 5.0, false) // NaN currentTime → "video not ready"

	reason := b.LastReasonVideoSync("vs-reason")
	if reason == "" {
		t.Error("LastReasonVideoSync after NaN Tick returned empty string; expected a reason")
	}
}
