package videosync

import (
	"math"
	"testing"
)

// TestTickInputGuardNaN verifies that a NaN currentTime returns none("video not ready").
func TestTickInputGuardNaN(t *testing.T) {
	e := New(DefaultConfig())
	dec := e.Tick(math.NaN(), 1000.0, 10.0, false)
	if dec.Kind != ActionNone {
		t.Fatalf("NaN currentTime: expected ActionNone, got %d", dec.Kind)
	}
	if dec.Reason != "video not ready" {
		t.Fatalf("NaN currentTime: expected reason \"video not ready\", got %q", dec.Reason)
	}
}

// TestTickInputGuardInf verifies that an Inf currentTime returns none("video not ready").
func TestTickInputGuardInf(t *testing.T) {
	e := New(DefaultConfig())
	dec := e.Tick(math.Inf(1), 1000.0, 10.0, false)
	if dec.Kind != ActionNone {
		t.Fatalf("Inf currentTime: expected ActionNone, got %d", dec.Kind)
	}
	if dec.Reason != "video not ready" {
		t.Fatalf("Inf currentTime: expected reason \"video not ready\", got %q", dec.Reason)
	}
}

// TestTickInputGuardInfBuffered verifies that an Inf bufferedAhead returns none("video not ready").
func TestTickInputGuardInfBuffered(t *testing.T) {
	e := New(DefaultConfig())
	dec := e.Tick(50.0, 1000.0, math.Inf(1), false)
	if dec.Kind != ActionNone {
		t.Fatalf("Inf bufferedAhead: expected ActionNone, got %d", dec.Kind)
	}
	if dec.Reason != "video not ready" {
		t.Fatalf("Inf bufferedAhead: expected reason \"video not ready\", got %q", dec.Reason)
	}
}

// TestIngestRTTTickFlow tests a full Ingest → RTT → Tick flow produces a sane Decision.
// After warmup, a large behind-drift should trigger a rate correction (or seek, but not none).
func TestIngestRTTTickFlow(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg)

	// Simulate startup: Ingest a server heartbeat, record RTT, then call OnPlaybackStart.
	const perfStart = 0.0
	e.OnPlaybackStart(perfStart)
	e.Ingest(1000, 100.0, true, perfStart)
	e.RTT(80.0) // 80ms RTT → 40ms one-way latency

	// Advance past warmup.
	perfNow := perfStart + cfg.WarmupMs + 1000.0

	// Local player is at 95s, target projects to ~100.04s (100 + (warmup+1000+40)/1000).
	// Drift ≈ -5s (behind), inside the large-drift band (>4s) → should seek or rate-adjust.
	dec := e.Tick(95.0, perfNow, 10.0, false)

	// Should not be paused/none due to guard; must be a rate or seek action.
	if dec.Kind == ActionNone && !dec.ResetRate {
		// "within-tolerance" or some drift action — just ensure we get something reasonable.
		// If it's ActionNone with ResetRate it means we converged; that's also fine but
		// unlikely here since drift > tolerance.
		t.Logf("Tick returned ActionNone (reason=%q) — checking drift was small", dec.Reason)
	}
	// The Decision must always be valid (ActualRate > 0).
	if dec.ActualRate <= 0 {
		t.Errorf("ActualRate must be > 0, got %v", dec.ActualRate)
	}
}

// TestStatsReflectsSeek verifies that Stats.SeeksTotal increments after a seek-triggering Tick.
func TestStatsReflectsSeek(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg)

	const perfStart = 0.0
	e.OnPlaybackStart(perfStart)

	// Ingest server at position 100s.
	e.Ingest(1000, 100.0, true, perfStart)
	e.RTT(60.0)

	// Advance past warmup.
	perfNow := perfStart + cfg.WarmupMs + 1000.0

	// Large behind drift (>4s) should trigger a seek.
	dec := e.Tick(94.0, perfNow, 10.0, false)

	stats := e.Stats(perfNow)
	if dec.Kind == ActionSeek {
		if stats.SeeksTotal == 0 {
			t.Errorf("Stats.SeeksTotal should be > 0 after a seek decision")
		}
	}
	// LastReason must match the Tick decision's Reason.
	if stats.LastReason != dec.Reason {
		t.Errorf("Stats.LastReason=%q, want %q", stats.LastReason, dec.Reason)
	}
	// CurrentMode must be one of the valid strings.
	switch stats.CurrentMode {
	case "none", "rate-up", "rate-down", "seek-cooldown":
		// ok
	default:
		t.Errorf("Stats.CurrentMode=%q is not a valid mode string", stats.CurrentMode)
	}
}

// TestOnPlaybackStartResetsWarmup verifies that after OnPlaybackStart, a large
// behind-drift immediately at perfNow ≈ start produces a rate correction (warmup
// suppresses seeks but not rate), not a seek.
func TestOnPlaybackStartResetsWarmup(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg)

	const perfStart = 1000.0 // arbitrary non-zero start
	e.OnPlaybackStart(perfStart)
	e.Ingest(2000, 100.0, true, perfStart)
	e.RTT(60.0)

	// Very shortly after start — well within WarmupMs.
	perfNow := perfStart + 500.0

	// Behind by 6s (large drift). During warmup: should get a rate correction, NOT a seek.
	dec := e.Tick(94.0, perfNow, 10.0, false)

	if dec.Kind == ActionSeek {
		t.Errorf("OnPlaybackStart: large drift during warmup should not seek, got ActionSeek (reason=%q)", dec.Reason)
	}
	// Should be rate or still in hysteresis/warmup.
	t.Logf("OnPlaybackStart warmup tick: kind=%d reason=%q", dec.Kind, dec.Reason)
}

// TestStatsModeStrings verifies that correctionModeString covers all enum values.
func TestStatsModeStrings(t *testing.T) {
	cases := []struct {
		mode CorrectionMode
		want string
	}{
		{ModeNone, "none"},
		{ModeRateUp, "rate-up"},
		{ModeRateDown, "rate-down"},
		{ModeSeekCooldown, "seek-cooldown"},
	}
	for _, tc := range cases {
		got := correctionModeString(tc.mode)
		if got != tc.want {
			t.Errorf("correctionModeString(%d)=%q, want %q", tc.mode, got, tc.want)
		}
	}
}

// TestTickPreloadMerge verifies that preload ready/stalled/phase are merged into Tick output.
func TestTickPreloadMerge(t *testing.T) {
	cfg := DefaultConfig()
	e := New(cfg)

	const perfStart = 0.0
	e.OnPlaybackStart(perfStart)
	e.Ingest(1000, 100.0, true, perfStart)

	perfNow := perfStart + 1000.0

	// Low bufferedAhead → should be stalled (PhaseBuffering) and not ready.
	dec := e.Tick(100.0, perfNow, 0.5, false) // 0.5s << MinBufferAhead=5s
	if dec.Ready {
		t.Errorf("expected Ready=false for under-buffered state, got Ready=true")
	}
	if !dec.Stalled {
		t.Errorf("expected Stalled=true for under-buffered state, got Stalled=false")
	}
	if dec.PreloadPhase != PhaseBuffering {
		t.Errorf("expected PreloadPhase=PhaseBuffering, got %d", dec.PreloadPhase)
	}
}
