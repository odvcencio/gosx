// Pure-JS port of the Go video drift-correction engine (client/videosync).
//
// This is the brain-ABSENT fallback: when the runtime WASM bridge is not
// loaded, synced video pages still get the SAME proven drift correction by
// running this in-factory port. It is PARITY-LOCKED to the Go engine — the
// committed golden vector (client/videosync/testdata/parity_basic.json)
// replays through both and they must agree within 1e-3.
//
// Mirror of:
//   client/videosync/config.go   — constants + Decision/ActionKind/PreloadPhase
//   client/videosync/sync.go     — SyncEngine (latency median + projection)
//   client/videosync/drift.go    — DriftCorrector (decision order)
//   client/videosync/preload.go  — PreloadManager (phase machine)
//   client/videosync/engine.go   — Engine.Tick (merge + input guard)
//
// HARD RULE: no Date.now()/performance.now()/setInterval/setTimeout anywhere
// in the decision logic. All time is injected via method arguments.
(function() {
  "use strict";

  // ActionKind (config.go).
  // ActionNone = 0, ActionRate = 1, ActionSeek = 2.

  // PreloadPhase (config.go).
  var PhaseIdle = 0;
  var PhaseConnecting = 1;
  var PhaseBuffering = 2;
  var PhaseSyncing = 3;
  var PhaseReady = 4;
  var PhaseRevealed = 5;

  // CorrectionMode (drift.go).
  var ModeNone = 0;
  var ModeRateUp = 1;
  var ModeRateDown = 2;
  var ModeSeekCooldown = 3;

  // seekVerifyErrorThreshold (drift.go).
  var SEEK_VERIFY_ERROR_THRESHOLD = 2.5;

  // rateClampLo / rateClampHi (drift.go).
  var RATE_CLAMP_LO = 0.5;
  var RATE_CLAMP_HI = 2.0;

  // DefaultConfig (config.go).
  function defaultConfig() {
    return {
      ToleranceThreshold: 0.75,
      RateThreshold: 1.3,
      SeekThreshold: 4.0,
      EmergencySeekThreshold: 25.0,
      RateAdjustmentSlow: 1.035,
      RateAdjustmentFast: 1.065,
      LargeDriftAheadRate: 0.88,
      HysteresisCount: 3,
      SeekCooldownMs: 5200,
      RateHoldMs: 2400,
      MaxSeeksPerMinute: 4,
      WarmupMs: 6500,
      MaxLatencySamples: 10,
      DefaultLatencyMs: 50,
      MaxRTTMs: 4000,
      MaxOneWayLatencyMs: 1200,
      MaxOutOfOrderMs: 750,
      ConfidenceDecayMs: 30000,
      MinBufferAhead: 5,
      SyncVerifyThreshold: 0.5,
      MaxPreloadMs: 15000,
      FadeInDurationMs: 300,
    };
  }

  function isFiniteNum(value) {
    return typeof value === "number" && isFinite(value);
  }

  // Merge a caller-supplied tuning object onto the proven defaults. Only finite
  // numeric overrides are honored; everything else falls through to default.
  function buildConfig(overrides) {
    var cfg = defaultConfig();
    if (overrides && typeof overrides === "object") {
      for (var key in cfg) {
        if (Object.prototype.hasOwnProperty.call(overrides, key) && isFiniteNum(overrides[key])) {
          cfg[key] = overrides[key];
        }
      }
    }
    return cfg;
  }

  // clampRate (drift.go) — clamp into [lo, hi]; non-finite => nominal 1.0.
  function clampRate(r) {
    if (!isFiniteNum(r)) {
      return 1.0;
    }
    if (r < RATE_CLAMP_LO) {
      return RATE_CLAMP_LO;
    }
    if (r > RATE_CLAMP_HI) {
      return RATE_CLAMP_HI;
    }
    return r;
  }

  // A Decision mirrors videosync.Decision. Reason is debug-only.
  function makeDecision() {
    return {
      kind: 0,
      rate: 0,
      seekTo: 0,
      resetRate: false,
      ready: false,
      stalled: false,
      actualRate: 1,
      preloadPhase: PhaseIdle,
      reason: "",
    };
  }

  function none(reason) {
    var d = makeDecision();
    d.kind = 0;
    d.actualRate = 1;
    d.reason = reason;
    return d;
  }

  // ----------------------------------------------------------------------------
  // SyncEngine (sync.go)
  // ----------------------------------------------------------------------------
  function createSyncEngine(cfg) {
    var latencySamples = []; // ring of most-recent one-way latency values (ms)
    var hasHeartbeat = false;
    var position = 0;
    var playing = false;
    var lastServerTimeMs = 0;
    var recvPerfMs = 0;
    var outOfOrderDropped = 0;

    // Ingest (sync.go). Drops non-finite positions and out-of-order packets.
    function ingest(serverTimeMs, pos, isPlaying, recvNow) {
      var p64 = pos;
      if (!isFiniteNum(p64)) {
        return;
      }
      // Out-of-order rejection: drop if more than MaxOutOfOrderMs behind the
      // last accepted packet.
      if (hasHeartbeat) {
        if (serverTimeMs + cfg.MaxOutOfOrderMs < lastServerTimeMs) {
          outOfOrderDropped += 1;
          return;
        }
      }
      if (p64 < 0) {
        p64 = 0;
      }
      position = p64;
      playing = !!isPlaying;
      lastServerTimeMs = serverTimeMs;
      recvPerfMs = recvNow;
      hasHeartbeat = true;
    }

    // RTT (sync.go). Reject non-finite / <=0 / >MaxRTTMs.
    function rtt(rttMs) {
      if (!isFiniteNum(rttMs)) {
        return;
      }
      if (rttMs <= 0 || rttMs > cfg.MaxRTTMs) {
        return;
      }
      var oneWay = Math.min(rttMs / 2, cfg.MaxOneWayLatencyMs);
      var max = cfg.MaxLatencySamples;
      if (latencySamples.length < max) {
        latencySamples.push(oneWay);
      } else {
        // Shift oldest out and append the new value at the end.
        for (var i = 0; i < max - 1; i += 1) {
          latencySamples[i] = latencySamples[i + 1];
        }
        latencySamples[max - 1] = oneWay;
      }
    }

    // EstimatedLatencyMs (sync.go) — median; mean of two middles for even n.
    function estimatedLatencyMs() {
      var n = latencySamples.length;
      if (n === 0) {
        return cfg.DefaultLatencyMs;
      }
      var scratch = latencySamples.slice();
      scratch.sort(function(a, b) { return a - b; });
      var mid = Math.floor(n / 2);
      if (n % 2 === 1) {
        return scratch[mid];
      }
      return (scratch[mid - 1] + scratch[mid]) / 2.0;
    }

    // ProjectedTarget (sync.go). Paused => bare position; no heartbeat => 0.
    function projectedTarget(perfNowMs) {
      if (!hasHeartbeat) {
        return 0;
      }
      if (!playing) {
        return position;
      }
      var elapsedMs = Math.max(0, perfNowMs - recvPerfMs);
      return position + (elapsedMs + estimatedLatencyMs()) / 1000.0;
    }

    return {
      ingest: ingest,
      rtt: rtt,
      estimatedLatencyMs: estimatedLatencyMs,
      projectedTarget: projectedTarget,
    };
  }

  // ----------------------------------------------------------------------------
  // DriftCorrector (drift.go)
  // ----------------------------------------------------------------------------
  function createDriftCorrector(cfg) {
    var mode = ModeNone;
    var pendingMode = ModeNone;
    var consecutive = 0;

    var lastSeekMs = 0;
    var lastRateChangeMs = 0;
    var playbackStartMs = 0;
    var seekTimestamps = [];
    var hasPlaybackStart = false;

    var seekVerificationPending = false;
    var lastSeekTarget = 0;
    var hasLastSeekTarget = false;
    var lastSeekIssuedAt = 0;
    var lastSeekWasPlaying = true;

    function resetState() {
      mode = ModeNone;
      pendingMode = ModeNone;
      consecutive = 0;
      seekVerificationPending = false;
      hasLastSeekTarget = false;
      lastSeekTarget = 0;
      lastSeekIssuedAt = 0;
      lastSeekWasPlaying = true;
    }

    // OnPlaybackStart (drift.go) — fresh warmup epoch + seek budget.
    function onPlaybackStart(perfNowMs) {
      playbackStartMs = perfNowMs;
      hasPlaybackStart = true;
      lastSeekMs = 0;
      lastRateChangeMs = 0;
      seekTimestamps = [];
      resetState();
    }

    // pruneSeekWindow (drift.go) — drop timestamps older than 60000ms.
    function pruneSeekWindow(perfNowMs) {
      var cutoff = perfNowMs - 60000.0;
      var w = [];
      for (var i = 0; i < seekTimestamps.length; i += 1) {
        if (seekTimestamps[i] > cutoff) {
          w.push(seekTimestamps[i]);
        }
      }
      seekTimestamps = w;
    }

    // rateDecision (drift.go) — clamped ActionRate.
    function rateDecision(rate, reason) {
      var r = clampRate(rate);
      var d = makeDecision();
      d.kind = 1; // ActionRate
      d.rate = r;
      d.actualRate = r;
      d.reason = reason;
      return d;
    }

    // commitRate (drift.go).
    function commitRate(m, rate, perfNowMs, reason) {
      mode = m;
      lastRateChangeMs = perfNowMs;
      return rateDecision(rate, reason);
    }

    // emitRateDirect (drift.go) — bypasses hysteresis.
    function emitRateDirect(rate, reason) {
      if (rate > 1.0) {
        mode = ModeRateUp;
      } else {
        mode = ModeRateDown;
      }
      return rateDecision(rate, reason);
    }

    // armSeek (drift.go) — stamp seek + verification state; ActionSeek.
    function armSeek(targetPosition, perfNowMs, isPlaying, reason) {
      var seekTo = targetPosition;
      if (seekTo < 0 || !isFiniteNum(seekTo)) {
        seekTo = 0;
      }
      lastSeekTarget = seekTo;
      hasLastSeekTarget = true;
      lastSeekIssuedAt = perfNowMs;
      lastSeekWasPlaying = isPlaying;
      seekVerificationPending = true;
      mode = ModeSeekCooldown;
      lastSeekMs = perfNowMs;
      pendingMode = ModeNone;
      consecutive = 0;
      var d = makeDecision();
      d.kind = 2; // ActionSeek
      d.seekTo = seekTo;
      d.actualRate = 1.0;
      d.reason = reason;
      return d;
    }

    // rateWithHysteresis (drift.go) — steps 8/9. Ahead uses reciprocal 1/rate.
    function rateWithHysteresis(drift, magnitude, perfNowMs, reason) {
      var desired = ModeRateUp; // behind -> speed up
      var rate = magnitude;
      if (drift > 0) { // ahead -> slow down via reciprocal
        desired = ModeRateDown;
        rate = 1.0 / magnitude;
      }

      // Already committed to the desired direction: emit immediately, but honor
      // the rate-hold window.
      if (mode === desired) {
        if ((perfNowMs - lastRateChangeMs) < cfg.RateHoldMs) {
          return none("rate-hold");
        }
        pendingMode = ModeNone;
        consecutive = 0;
        return commitRate(desired, rate, perfNowMs, reason);
      }

      // Direction change / fresh: require HysteresisCount consecutive samples.
      if (pendingMode === desired) {
        consecutive += 1;
      } else {
        pendingMode = desired;
        consecutive = 1;
      }
      if (consecutive >= cfg.HysteresisCount) {
        pendingMode = ModeNone;
        consecutive = 0;
        return commitRate(desired, rate, perfNowMs, reason);
      }
      return none("hysteresis-pending");
    }

    // Calculate (drift.go) — one tick, branch-for-branch with the Go core.
    function calculate(currentPosition, targetPosition, perfNowMs, isPlaying) {
      var drift = currentPosition - targetPosition;
      var absDrift = Math.abs(drift);

      // Step 1: warmup flag.
      var inWarmup = hasPlaybackStart && (perfNowMs - playbackStartMs) <= cfg.WarmupMs;

      // Step 2: paused.
      if (!isPlaying) {
        return none("paused");
      }

      // Step 3: emergency seek — bypasses cooldown/cap/rewind/warmup.
      if (absDrift > cfg.EmergencySeekThreshold) {
        return armSeek(targetPosition, perfNowMs, isPlaying, "emergency-seek");
      }

      // Step 4: seek cooldown.
      if (mode === ModeSeekCooldown || (perfNowMs - lastSeekMs) < cfg.SeekCooldownMs) {
        if (mode === ModeSeekCooldown && (perfNowMs - lastSeekMs) >= cfg.SeekCooldownMs) {
          mode = ModeNone;
          // fall through
        } else {
          return none("seek-cooldown");
        }
      }

      // Step 5: seek verification — never short-circuits.
      if (seekVerificationPending && hasLastSeekTarget) {
        var elapsedSec = Math.max(0, perfNowMs - lastSeekIssuedAt) / 1000.0;
        var expected = lastSeekTarget;
        if (lastSeekWasPlaying) {
          expected += elapsedSec;
        }
        if (Math.abs(currentPosition - expected) > SEEK_VERIFY_ERROR_THRESHOLD) {
          // Failed seek: re-arm cooldown but resume rate corrections.
          lastSeekMs = perfNowMs;
          mode = ModeNone;
        }
        seekVerificationPending = false;
        hasLastSeekTarget = false;
        lastSeekTarget = 0;
        lastSeekIssuedAt = 0;
      }

      // Step 6: within tolerance — return to nominal rate.
      if (absDrift < cfg.ToleranceThreshold) {
        mode = ModeNone;
        pendingMode = ModeNone;
        consecutive = 0;
        var dec = none("within-tolerance");
        dec.resetRate = true;
        return dec;
      }

      // Step 7: large drift.
      if (absDrift > cfg.SeekThreshold) {
        if (inWarmup) {
          if (drift > 0) { // ahead
            return emitRateDirect(cfg.LargeDriftAheadRate, "large-drift-warmup-ahead");
          }
          return emitRateDirect(cfg.RateAdjustmentFast, "large-drift-warmup-behind");
        }
        if (drift > 0) { // ahead, post-warmup: never seek (rewind asymmetry)
          return emitRateDirect(cfg.LargeDriftAheadRate, "large-drift-ahead-no-rewind");
        }
        // Behind, post-warmup: seek unless per-minute budget exhausted.
        pruneSeekWindow(perfNowMs);
        if (seekTimestamps.length >= cfg.MaxSeeksPerMinute) {
          return emitRateDirect(cfg.RateAdjustmentFast, "large-drift-seek-limited");
        }
        seekTimestamps.push(perfNowMs);
        return armSeek(targetPosition, perfNowMs, isPlaying, "large-drift-seek");
      }

      // Step 8: medium rate band — magnitude fast.
      if (absDrift > cfg.RateThreshold) {
        return rateWithHysteresis(drift, cfg.RateAdjustmentFast, perfNowMs, "medium-drift");
      }

      // Step 9: small rate band — magnitude slow.
      return rateWithHysteresis(drift, cfg.RateAdjustmentSlow, perfNowMs, "small-drift");
    }

    return {
      onPlaybackStart: onPlaybackStart,
      calculate: calculate,
      reset: resetState,
    };
  }

  // ----------------------------------------------------------------------------
  // PreloadManager (preload.go)
  // ----------------------------------------------------------------------------
  function createPreloadManager(cfg) {
    var started = false;
    var startMs = 0;
    var revealed = false;

    // Update (preload.go) — returns [phase, ready, stalled].
    function update(bufferedAhead, positionError, perfNowMs) {
      if (!isFiniteNum(bufferedAhead) || !isFiniteNum(positionError)) {
        return [PhaseBuffering, false, true];
      }
      if (revealed) {
        return [PhaseRevealed, true, false];
      }
      if (!started) {
        startMs = perfNowMs;
        started = true;
      }
      var deadlineExceeded = (perfNowMs - startMs) > cfg.MaxPreloadMs;
      if (bufferedAhead < cfg.MinBufferAhead && !deadlineExceeded) {
        return [PhaseBuffering, false, true];
      }
      if (positionError > cfg.SyncVerifyThreshold && !deadlineExceeded) {
        return [PhaseSyncing, false, false];
      }
      revealed = true;
      return [PhaseReady, true, false];
    }

    function reset() {
      started = false;
      startMs = 0;
      revealed = false;
    }

    return { update: update, reset: reset };
  }

  // ----------------------------------------------------------------------------
  // Engine (engine.go) — wires Sync + Drift + Preload.
  // ----------------------------------------------------------------------------
  function createEngine(config) {
    var cfg = buildConfig(config);
    var sync = createSyncEngine(cfg);
    var drift = createDriftCorrector(cfg);
    var preload = createPreloadManager(cfg);

    function ingest(serverTimeMs, position, playing, recvPerfMs) {
      sync.ingest(serverTimeMs, position, playing, recvPerfMs);
    }

    function rtt(rttMs) {
      sync.rtt(rttMs);
    }

    function onPlaybackStart(perfNowMs) {
      drift.onPlaybackStart(perfNowMs);
      preload.reset();
    }

    // Tick (engine.go) — merge drift decision + preload + input guard.
    function tick(currentTime, perfNowMs, bufferedAhead, paused) {
      if (!isFiniteNum(currentTime) || !isFiniteNum(bufferedAhead)) {
        return none("video not ready");
      }
      var target = sync.projectedTarget(perfNowMs);
      var dec = drift.calculate(currentTime, target, perfNowMs, !paused);
      var positionError = Math.abs(currentTime - target);
      var pr = preload.update(bufferedAhead, positionError, perfNowMs);
      dec.preloadPhase = pr[0];
      dec.ready = pr[1];
      dec.stalled = pr[2];
      return dec;
    }

    return {
      ingest: ingest,
      rtt: rtt,
      onPlaybackStart: onPlaybackStart,
      tick: tick,
    };
  }

  // Factory entry point used by the video factory's brain-absent path.
  if (typeof window !== "undefined") {
    window.__gosx_video_sync_js_create = function(config) {
      return createEngine(config);
    };
  }
})();
