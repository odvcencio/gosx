package videosync

import "math"

// CorrectionMode is the drift corrector's internal state-machine mode.
type CorrectionMode uint8

const (
	ModeNone         CorrectionMode = 0 // no active correction
	ModeRateUp       CorrectionMode = 1 // speeding up (behind the master)
	ModeRateDown     CorrectionMode = 2 // slowing down (ahead of the master)
	ModeSeekCooldown CorrectionMode = 3 // recently seeked; corrections suppressed
)

// seekVerifyErrorThreshold is the position error (seconds) above which a prior
// seek is judged to have failed verification. Ported from the TS DriftCorrector.
const seekVerifyErrorThreshold = 2.5

// rateClampLo / rateClampHi bound any emitted playback rate.
const (
	rateClampLo = 0.5
	rateClampHi = 2.0
)

// DriftCorrector turns a (currentPosition, targetPosition) drift into a
// corrective Decision: a no-op, a playback-rate nudge, or a seek.
//
// Sign convention: drift = currentPosition - targetPosition.
//   - drift > 0  => ahead  (slow down; never seek backward, except emergency)
//   - drift < 0  => behind (speed up; may seek to catch up)
//
// TinyGo-clean: no time.Now/time.Since, no encoding/json, no syscall/js.
// All timing arrives as caller-supplied performance.now() milliseconds.
//
// The decision order is ported from the Rust drift_corrector.rs core
// (emergency seek truly bypasses cooldown/cap/warmup), with seek-verification
// slotted in from the TS DriftCorrector (which the Rust core lacks).
type DriftCorrector struct {
	cfg Config

	// Hysteresis state machine.
	mode        CorrectionMode
	pendingMode CorrectionMode
	consecutive int

	// Timing state (all performance.now() ms, caller-supplied).
	lastSeekMs       float64
	lastRateChangeMs float64
	playbackStartMs  float64
	seekTimestamps   []float64 // rolling trailing-60000ms window
	hasPlaybackStart bool

	// Seek-verification state (ported from TS).
	seekVerificationPending bool
	lastSeekTarget          float64
	hasLastSeekTarget       bool
	lastSeekIssuedAt        float64
	lastSeekWasPlaying      bool

	// Cumulative stats counters.
	seeksTotal       uint64
	emergencySeeks   uint64
	rateChangesTotal uint64
}

// NewDriftCorrector constructs a DriftCorrector with the given config.
func NewDriftCorrector(cfg Config) *DriftCorrector {
	return &DriftCorrector{
		cfg:            cfg,
		seekTimestamps: make([]float64, 0, cfg.MaxSeeksPerMinute+1),
	}
}

// OnPlaybackStart resets the warmup epoch and seek budget for a fresh item.
// It clears mode/hysteresis and the rolling seek window, matching the TS
// onPlaybackStart (fresh epoch: new seek budget, cleared cooldowns).
func (d *DriftCorrector) OnPlaybackStart(perfNowMs float64) {
	d.playbackStartMs = perfNowMs
	d.hasPlaybackStart = true
	d.lastSeekMs = 0
	d.lastRateChangeMs = 0
	d.seekTimestamps = d.seekTimestamps[:0]
	d.resetState()
}

// Reset clears mode, hysteresis, and all seek-verification fields. It does NOT
// reset cumulative stat counters (those are lifetime totals).
func (d *DriftCorrector) Reset() {
	d.resetState()
}

// resetState clears the correction-mode + seek-verification machinery.
func (d *DriftCorrector) resetState() {
	d.mode = ModeNone
	d.pendingMode = ModeNone
	d.consecutive = 0
	d.seekVerificationPending = false
	d.hasLastSeekTarget = false
	d.lastSeekTarget = 0
	d.lastSeekIssuedAt = 0
	d.lastSeekWasPlaying = true
}

// Calculate evaluates one tick and returns the corrective Decision.
//
// currentPosition/targetPosition are seconds; perfNowMs is the caller's
// performance.now() value; isPlaying reports whether media is playing.
func (d *DriftCorrector) Calculate(currentPosition, targetPosition, perfNowMs float64, isPlaying bool) Decision {
	drift := currentPosition - targetPosition
	absDrift := math.Abs(drift)

	// Step 1: warmup flag — active while within WarmupMs of playback start.
	inWarmup := d.hasPlaybackStart && (perfNowMs-d.playbackStartMs) <= d.cfg.WarmupMs

	// Step 2: paused.
	if !isPlaying {
		return none("paused")
	}

	// Step 3: emergency seek — bypasses cooldown/cap/rewind/warmup.
	if absDrift > d.cfg.EmergencySeekThreshold {
		d.emergencySeeks++
		return d.armSeek(targetPosition, perfNowMs, isPlaying, "emergency-seek")
	}

	// Step 4: seek cooldown. If the cooldown has elapsed and we're still flagged
	// SeekCooldown, clear the mode and fall through.
	if d.mode == ModeSeekCooldown || (perfNowMs-d.lastSeekMs) < d.cfg.SeekCooldownMs {
		if d.mode == ModeSeekCooldown && (perfNowMs-d.lastSeekMs) >= d.cfg.SeekCooldownMs {
			d.mode = ModeNone
			// fall through
		} else {
			return none("seek-cooldown")
		}
	}

	// Step 5: seek verification (ported from TS). Never short-circuits.
	if d.seekVerificationPending && d.hasLastSeekTarget {
		elapsedSec := math.Max(0, perfNowMs-d.lastSeekIssuedAt) / 1000.0
		expected := d.lastSeekTarget
		if d.lastSeekWasPlaying {
			expected += elapsedSec
		}
		if math.Abs(currentPosition-expected) > seekVerifyErrorThreshold {
			// Failed seek: re-arm cooldown timer but resume rate corrections
			// (don't get stuck in seek-cooldown mode).
			d.lastSeekMs = perfNowMs
			d.mode = ModeNone
		}
		// Regardless of pass/fail, clear the verification latch.
		d.seekVerificationPending = false
		d.hasLastSeekTarget = false
		d.lastSeekTarget = 0
		d.lastSeekIssuedAt = 0
	}

	// Step 6: within tolerance — return to nominal rate.
	if absDrift < d.cfg.ToleranceThreshold {
		d.mode = ModeNone
		d.pendingMode = ModeNone
		d.consecutive = 0
		dec := none("within-tolerance")
		dec.ResetRate = true
		return dec
	}

	// Step 7: large drift.
	if absDrift > d.cfg.SeekThreshold {
		if inWarmup {
			// No non-emergency seek during warmup.
			if drift > 0 { // ahead
				return d.emitRateDirect(d.cfg.LargeDriftAheadRate, "large-drift-warmup-ahead")
			}
			return d.emitRateDirect(d.cfg.RateAdjustmentFast, "large-drift-warmup-behind")
		}
		if drift > 0 { // ahead, post-warmup: rewind asymmetry — never seek.
			return d.emitRateDirect(d.cfg.LargeDriftAheadRate, "large-drift-ahead-no-rewind")
		}
		// Behind, post-warmup: seek unless the per-minute budget is exhausted.
		d.pruneSeekWindow(perfNowMs)
		if len(d.seekTimestamps) >= d.cfg.MaxSeeksPerMinute {
			return d.emitRateDirect(d.cfg.RateAdjustmentFast, "large-drift-seek-limited")
		}
		d.seekTimestamps = append(d.seekTimestamps, perfNowMs)
		d.seeksTotal++
		return d.armSeek(targetPosition, perfNowMs, isPlaying, "large-drift-seek")
	}

	// Step 8: medium rate band — magnitude fast.
	if absDrift > d.cfg.RateThreshold {
		return d.rateWithHysteresis(drift, d.cfg.RateAdjustmentFast, perfNowMs, "medium-drift")
	}

	// Step 9: small rate band — magnitude slow.
	return d.rateWithHysteresis(drift, d.cfg.RateAdjustmentSlow, perfNowMs, "small-drift")
}

// rateWithHysteresis implements steps 8/9: a graduated rate change gated by
// hysteresis (HysteresisCount consecutive same-direction samples) and a
// rate-hold window. The magnitude argument is the "behind" rate; "ahead" uses
// the reciprocal (ported intent from the Rust core; NOT the TS 2-rate form).
func (d *DriftCorrector) rateWithHysteresis(drift, magnitude, perfNowMs float64, reason string) Decision {
	desired := ModeRateUp // behind -> speed up
	rate := magnitude
	if drift > 0 { // ahead -> slow down via reciprocal
		desired = ModeRateDown
		rate = 1.0 / magnitude
	}

	// Already committed to the desired direction: emit immediately, but honor
	// the rate-hold window so we don't thrash within a committed mode.
	if d.mode == desired {
		if (perfNowMs - d.lastRateChangeMs) < d.cfg.RateHoldMs {
			return none("rate-hold")
		}
		d.pendingMode = ModeNone
		d.consecutive = 0
		return d.commitRate(desired, rate, perfNowMs, reason)
	}

	// Direction change (or starting fresh): require HysteresisCount consecutive
	// same-direction samples before committing.
	if d.pendingMode == desired {
		d.consecutive++
	} else {
		d.pendingMode = desired
		d.consecutive = 1
	}
	if d.consecutive >= d.cfg.HysteresisCount {
		d.pendingMode = ModeNone
		d.consecutive = 0
		return d.commitRate(desired, rate, perfNowMs, reason)
	}
	return none("hysteresis-pending")
}

// commitRate sets the mode, stamps the rate-hold clock, counts the change, and
// builds an ActionRate Decision.
func (d *DriftCorrector) commitRate(mode CorrectionMode, rate, perfNowMs float64, reason string) Decision {
	d.mode = mode
	d.lastRateChangeMs = perfNowMs
	d.rateChangesTotal++
	return d.rateDecision(rate, reason)
}

// emitRateDirect emits a rate change that bypasses hysteresis (warmup /
// rewind-asymmetry / seek-limited paths). It still updates mode + counters.
func (d *DriftCorrector) emitRateDirect(rate float64, reason string) Decision {
	if rate > 1.0 {
		d.mode = ModeRateUp
	} else {
		d.mode = ModeRateDown
	}
	d.rateChangesTotal++
	return d.rateDecision(rate, reason)
}

// rateDecision builds a clamped ActionRate Decision.
func (d *DriftCorrector) rateDecision(rate float64, reason string) Decision {
	r := clampRate(rate)
	return Decision{
		Kind:       ActionRate,
		Rate:       r,
		ActualRate: r,
		Reason:     reason,
	}
}

// armSeek stamps all seek + seek-verification state and returns an ActionSeek
// Decision targeting targetPosition. Used by both emergency and large-drift
// seeks. Counter increments are the caller's responsibility.
func (d *DriftCorrector) armSeek(targetPosition, perfNowMs float64, isPlaying bool, reason string) Decision {
	seekTo := targetPosition
	if seekTo < 0 || math.IsNaN(seekTo) || math.IsInf(seekTo, 0) {
		seekTo = 0
	}
	d.lastSeekTarget = seekTo
	d.hasLastSeekTarget = true
	d.lastSeekIssuedAt = perfNowMs
	d.lastSeekWasPlaying = isPlaying
	d.seekVerificationPending = true
	d.mode = ModeSeekCooldown
	d.lastSeekMs = perfNowMs
	d.pendingMode = ModeNone
	d.consecutive = 0
	return Decision{
		Kind:       ActionSeek,
		SeekTo:     seekTo,
		ActualRate: 1.0,
		Reason:     reason,
	}
}

// pruneSeekWindow drops seek timestamps older than 60000ms before perfNowMs.
func (d *DriftCorrector) pruneSeekWindow(perfNowMs float64) {
	cutoff := perfNowMs - 60000.0
	w := d.seekTimestamps[:0]
	for _, t := range d.seekTimestamps {
		if t > cutoff {
			w = append(w, t)
		}
	}
	d.seekTimestamps = w
}

// clampRate clamps a rate into [rateClampLo, rateClampHi] and replaces any
// non-finite value with nominal 1.0.
func clampRate(r float64) float64 {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 1.0
	}
	if r < rateClampLo {
		return rateClampLo
	}
	if r > rateClampHi {
		return rateClampHi
	}
	return r
}

// SeeksTotal returns the cumulative count of non-emergency seeks issued.
func (d *DriftCorrector) SeeksTotal() uint64 { return d.seeksTotal }

// EmergencySeeks returns the cumulative count of emergency seeks issued.
func (d *DriftCorrector) EmergencySeeks() uint64 { return d.emergencySeeks }

// RateChangesTotal returns the cumulative count of committed rate changes.
func (d *DriftCorrector) RateChangesTotal() uint64 { return d.rateChangesTotal }

// CurrentMode returns the corrector's current internal mode.
func (d *DriftCorrector) CurrentMode() CorrectionMode { return d.mode }
