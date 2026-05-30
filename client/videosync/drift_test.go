package videosync

import (
	"math"
	"testing"
)

// driftEps is the float comparison epsilon for drift tests.
const driftEps = 1e-9

// applyTick advances a simulated playback model by one tick.
//
// The shared media clock (target) advances at 1.0x. The local player advances
// at whatever effective rate the last Decision dictated, or at 1.0x for a
// no-op. On a seek, the local position snaps to the decision's SeekTo.
//
// It returns the new currentPosition and target after dtSec of wall time.
func applyTick(d Decision, currentPosition, target, dtSec float64) (float64, float64) {
	newTarget := target + dtSec // master clock: always 1.0x
	switch d.Kind {
	case ActionSeek:
		// Snap to the seek target, then let dt of playback elapse at 1.0x
		// (rate resets to nominal after a seek).
		return d.SeekTo + dtSec, newTarget
	case ActionRate:
		return currentPosition + d.Rate*dtSec, newTarget
	default:
		// No-op / within-tolerance: keep playing at whatever rate we last had.
		// Model as nominal 1.0x (engine is signalling reset/no-change).
		rate := 1.0
		if d.ResetRate {
			rate = 1.0
		}
		return currentPosition + rate*dtSec, newTarget
	}
}

// Test 1: CONVERGENCE
// A steady -3.0s drift (behind, inside rate band 1.3<3.0<4.0) converges via
// rate changes alone — zero seeks — within a bounded number of ticks.
func TestConvergenceRateBandNoSeek(t *testing.T) {
	d := NewDriftCorrector(DefaultConfig())
	const startMs = 0.0
	d.OnPlaybackStart(startMs)

	// Begin past warmup so seeks/rate are fully enabled. Use a large dt so the
	// 6.5% rate edge can actually close the gap inside the tick budget.
	perf := startMs + DefaultConfig().WarmupMs + 1000.0
	const dtSec = 6.0

	target := 100.0
	currentPosition := target - 3.0 // behind by 3.0s

	sawRate := false
	for i := 0; i < 12; i++ {
		dec := d.Calculate(currentPosition, target, perf, true)
		if dec.Kind == ActionSeek {
			t.Fatalf("tick %d: unexpected seek for -3.0s drift (rate band)", i)
		}
		if dec.Kind == ActionRate {
			sawRate = true
		}
		currentPosition, target = applyTick(dec, currentPosition, target, dtSec)
		perf += dtSec * 1000.0
		if math.Abs(currentPosition-target) < DefaultConfig().ToleranceThreshold {
			break
		}
	}

	if !sawRate {
		t.Errorf("expected at least one ActionRate decision during convergence")
	}
	if d.SeeksTotal() != 0 || d.EmergencySeeks() != 0 {
		t.Errorf("expected zero seeks, got total=%d emergency=%d", d.SeeksTotal(), d.EmergencySeeks())
	}
	if absd := math.Abs(currentPosition - target); absd >= DefaultConfig().ToleranceThreshold {
		t.Errorf("did not converge: |drift|=%v >= tolerance %v", absd, DefaultConfig().ToleranceThreshold)
	}
}

// Test 1b: CONVERGENCE with a single seek.
// A -6.0s drift (behind, large: >4.0) triggers exactly one seek, then converges.
func TestConvergenceLargeDriftOneSeek(t *testing.T) {
	d := NewDriftCorrector(DefaultConfig())
	const startMs = 0.0
	d.OnPlaybackStart(startMs)

	perf := startMs + DefaultConfig().WarmupMs + 1000.0
	const dtSec = 6.0

	target := 100.0
	currentPosition := target - 6.0 // behind by 6.0s (large drift)

	for i := 0; i < 12; i++ {
		dec := d.Calculate(currentPosition, target, perf, true)
		currentPosition, target = applyTick(dec, currentPosition, target, dtSec)
		perf += dtSec * 1000.0
		if math.Abs(currentPosition-target) < DefaultConfig().ToleranceThreshold {
			break
		}
	}

	if d.SeeksTotal() != 1 {
		t.Errorf("expected exactly 1 seek for -6.0s drift, got %d", d.SeeksTotal())
	}
	if d.EmergencySeeks() != 0 {
		t.Errorf("expected zero emergency seeks, got %d", d.EmergencySeeks())
	}
	if absd := math.Abs(currentPosition - target); absd >= DefaultConfig().ToleranceThreshold {
		t.Errorf("did not converge after seek: |drift|=%v", absd)
	}
}

// Test 2: NO-OSCILLATION
// Drift that alternates direction each tick must never commit a rate change:
// it stays ActionNone with reason "hysteresis-pending" because no direction
// reaches HysteresisCount consecutive samples. A run of 3 consecutive same
// direction then DOES commit.
func TestNoOscillationHysteresis(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	// Alternate +0.9 / -0.9 drift each tick (within rate band: >0.75, <1.3 -> small band).
	signs := []float64{-0.9, 0.9, -0.9, 0.9, -0.9, 0.9}
	for i, s := range signs {
		cur := target + s // drift = cur - target = s
		dec := d.Calculate(cur, target, perf, true)
		if dec.Kind != ActionNone {
			t.Fatalf("tick %d: alternating drift committed a rate change (kind=%d, reason=%q); expected hysteresis-pending",
				i, dec.Kind, dec.Reason)
		}
		if dec.Reason != "hysteresis-pending" {
			t.Fatalf("tick %d: expected reason hysteresis-pending, got %q", i, dec.Reason)
		}
		perf += 1000.0
	}
	if d.RateChangesTotal() != 0 {
		t.Errorf("alternating drift should never commit a rate change, got %d", d.RateChangesTotal())
	}

	// Now 3 consecutive same-direction (behind) samples -> commit on the 3rd.
	committed := false
	for i := 0; i < 3; i++ {
		dec := d.Calculate(target-0.9, target, perf, true)
		if dec.Kind == ActionRate {
			committed = true
		}
		perf += 1000.0
	}
	if !committed {
		t.Errorf("expected a committed rate change after 3 consecutive same-direction samples")
	}
}

// Test 3: REWIND-SAFETY
// Any tick where drift > 0 (ahead) and absDrift <= emergency threshold must
// never seek; the only allowed action is a rate <= 1.0 slowdown (or none).
func TestRewindSafetyNeverSeeksAhead(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	// Ahead drifts spanning small, medium, and large bands (all <= 25).
	aheads := []float64{1.0, 1.0, 1.0, 2.0, 2.0, 2.0, 10.0, 10.0, 10.0, 20.0, 20.0, 20.0}
	for i, a := range aheads {
		cur := target + a // drift = +a (ahead)
		dec := d.Calculate(cur, target, perf, true)
		if dec.Kind == ActionSeek {
			t.Fatalf("tick %d: seek issued while ahead by %vs (must never rewind)", i, a)
		}
		if dec.Kind == ActionRate && dec.Rate > 1.0+driftEps {
			t.Fatalf("tick %d: ahead correction used rate %v > 1.0 (should slow down)", i, dec.Rate)
		}
		perf += 1000.0
	}
	if d.SeeksTotal() != 0 || d.EmergencySeeks() != 0 {
		t.Errorf("ahead-only run produced seeks: total=%d emergency=%d", d.SeeksTotal(), d.EmergencySeeks())
	}
}

// Test 4a: COOLDOWN
// After warmup, five >4s-behind spikes 1s apart yield exactly one seek; the
// rest are "seek-cooldown" no-ops until SeekCooldownMs elapses.
func TestSeekCooldown(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	seeks := 0
	for i := 0; i < 5; i++ {
		cur := target - 6.0 // behind by 6s, large drift
		dec := d.Calculate(cur, target, perf, true)
		if dec.Kind == ActionSeek {
			seeks++
		} else if i > 0 {
			if dec.Reason != "seek-cooldown" {
				t.Errorf("tick %d: expected seek-cooldown, got %q (kind=%d)", i, dec.Reason, dec.Kind)
			}
		}
		perf += 1000.0 // 1s apart, well inside 5200ms cooldown
	}
	if seeks != 1 {
		t.Errorf("expected exactly 1 seek within cooldown window, got %d", seeks)
	}
}

// Test 4b: CAP
// Repeated >4s-behind spikes spread across 60s never exceed MaxSeeksPerMinute
// non-emergency seeks.
func TestSeekCapPerMinute(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	// Start past warmup.
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	// Space spikes far enough apart to clear cooldown (>5200ms) but stay inside
	// the trailing 60s window. 6s spacing -> 10 spikes within ~54s.
	for i := 0; i < 10; i++ {
		cur := target - 6.0
		d.Calculate(cur, target, perf, true)
		perf += 6000.0
	}
	if d.SeeksTotal() > uint64(cfg.MaxSeeksPerMinute) {
		t.Errorf("seek cap exceeded: %d > %d", d.SeeksTotal(), cfg.MaxSeeksPerMinute)
	}
}

// Test 5: EMERGENCY
// Even after the per-minute seek cap is exhausted, a single -30s drift still
// emits a seek with reason containing "emergency".
func TestEmergencyBypassesCap(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	// Exhaust the cap with large (non-emergency) behind spikes spaced past cooldown.
	for i := 0; i < cfg.MaxSeeksPerMinute+2; i++ {
		d.Calculate(target-6.0, target, perf, true)
		perf += 6000.0
	}
	if d.SeeksTotal() == 0 {
		t.Fatalf("expected some non-emergency seeks before testing emergency bypass")
	}

	// Now a -30s drift: emergency must fire despite cap/cooldown.
	dec := d.Calculate(target-30.0, target, perf, true)
	if dec.Kind != ActionSeek {
		t.Fatalf("emergency drift did not seek: kind=%d reason=%q", dec.Kind, dec.Reason)
	}
	if !containsSub(dec.Reason, "emergency") {
		t.Errorf("emergency seek reason %q does not contain \"emergency\"", dec.Reason)
	}
	if d.EmergencySeeks() != 1 {
		t.Errorf("expected EmergencySeeks()==1, got %d", d.EmergencySeeks())
	}
}

// Test 6: WARMUP
// A -6s (large) drift during warmup must use a rate adjustment, never a seek.
func TestWarmupNeverSeeks(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)

	// Within warmup: perf - start <= WarmupMs.
	perf := cfg.WarmupMs - 500.0
	target := 100.0
	for i := 0; i < cfg.HysteresisCount+2; i++ {
		dec := d.Calculate(target-6.0, target, perf, true)
		if dec.Kind == ActionSeek {
			t.Fatalf("tick %d: seek issued during warmup (must only rate-adjust)", i)
		}
		perf += 100.0 // stay inside warmup
	}
	if d.SeeksTotal() != 0 {
		t.Errorf("expected zero seeks during warmup, got %d", d.SeeksTotal())
	}
}

// Test 7: SEEK-VERIFICATION
// After a seek, a tick where the actual position undershoots the expected
// position by >2.5s marks the seek failed: mode resets to None (rate
// corrections resume) and no immediate re-seek occurs that tick.
func TestSeekVerificationFailureResetsMode(t *testing.T) {
	cfg := DefaultConfig()
	d := NewDriftCorrector(cfg)
	d.OnPlaybackStart(0.0)
	perf := cfg.WarmupMs + 1000.0

	target := 100.0
	// Trigger a large-drift seek (behind 6s).
	dec := d.Calculate(target-6.0, target, perf, true)
	if dec.Kind != ActionSeek {
		t.Fatalf("setup: expected a seek, got kind=%d reason=%q", dec.Kind, dec.Reason)
	}
	if d.CurrentMode() != ModeSeekCooldown {
		t.Fatalf("after seek expected ModeSeekCooldown, got %d", d.CurrentMode())
	}
	seekTarget := dec.SeekTo

	// Advance past cooldown so verification can run.
	perf += cfg.SeekCooldownMs + 100.0

	// Build a tick where actual position massively undershoots expected.
	// expected ~= seekTarget + elapsedSec (playing). Make actual far below.
	elapsedSec := (perf - (perf - cfg.SeekCooldownMs - 100.0)) / 1000.0
	expected := seekTarget + elapsedSec
	actual := expected - 5.0 // undershoot by 5s (> 2.5s threshold)

	// New target near actual so we don't immediately re-trigger a large seek;
	// keep drift inside the rate band.
	newTarget := actual + 2.0 // behind by 2s -> rate band, not seek
	dec2 := d.Calculate(actual, newTarget, perf, true)

	if dec2.Kind == ActionSeek {
		t.Fatalf("verification-failure tick must not immediately re-seek, got seek")
	}
	if d.CurrentMode() == ModeSeekCooldown {
		t.Errorf("failed seek verification should reset mode out of seek-cooldown, mode=%d", d.CurrentMode())
	}
}

// containsSub reports whether sub occurs within s (no strings import to keep
// the test self-contained relative to package conventions).
func containsSub(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
