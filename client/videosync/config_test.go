package videosync

import "testing"

func TestNew_NonNil(t *testing.T) {
	e := New(DefaultConfig())
	if e == nil {
		t.Fatal("New(DefaultConfig()) returned nil")
	}
}

func TestDefaultConfig_KeyValues(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ToleranceThreshold != 0.75 {
		t.Errorf("ToleranceThreshold: got %v, want 0.75", cfg.ToleranceThreshold)
	}
	if cfg.RateThreshold != 1.3 {
		t.Errorf("RateThreshold: got %v, want 1.3", cfg.RateThreshold)
	}
	if cfg.SeekThreshold != 4.0 {
		t.Errorf("SeekThreshold: got %v, want 4.0", cfg.SeekThreshold)
	}
	if cfg.EmergencySeekThreshold != 25.0 {
		t.Errorf("EmergencySeekThreshold: got %v, want 25.0", cfg.EmergencySeekThreshold)
	}
	if cfg.RateAdjustmentSlow != 1.035 {
		t.Errorf("RateAdjustmentSlow: got %v, want 1.035", cfg.RateAdjustmentSlow)
	}
	if cfg.RateAdjustmentFast != 1.065 {
		t.Errorf("RateAdjustmentFast: got %v, want 1.065", cfg.RateAdjustmentFast)
	}
	if cfg.LargeDriftAheadRate != 0.88 {
		t.Errorf("LargeDriftAheadRate: got %v, want 0.88", cfg.LargeDriftAheadRate)
	}
	if cfg.HysteresisCount != 3 {
		t.Errorf("HysteresisCount: got %v, want 3", cfg.HysteresisCount)
	}
	if cfg.SeekCooldownMs != 5200 {
		t.Errorf("SeekCooldownMs: got %v, want 5200", cfg.SeekCooldownMs)
	}
	if cfg.RateHoldMs != 2400 {
		t.Errorf("RateHoldMs: got %v, want 2400", cfg.RateHoldMs)
	}
	if cfg.MaxSeeksPerMinute != 4 {
		t.Errorf("MaxSeeksPerMinute: got %v, want 4", cfg.MaxSeeksPerMinute)
	}
	if cfg.WarmupMs != 6500 {
		t.Errorf("WarmupMs: got %v, want 6500", cfg.WarmupMs)
	}
	if cfg.MaxLatencySamples != 10 {
		t.Errorf("MaxLatencySamples: got %v, want 10", cfg.MaxLatencySamples)
	}
	if cfg.DefaultLatencyMs != 50 {
		t.Errorf("DefaultLatencyMs: got %v, want 50", cfg.DefaultLatencyMs)
	}
	if cfg.MaxRTTMs != 4000 {
		t.Errorf("MaxRTTMs: got %v, want 4000", cfg.MaxRTTMs)
	}
	if cfg.MaxOneWayLatencyMs != 1200 {
		t.Errorf("MaxOneWayLatencyMs: got %v, want 1200", cfg.MaxOneWayLatencyMs)
	}
	if cfg.MaxOutOfOrderMs != 750 {
		t.Errorf("MaxOutOfOrderMs: got %v, want 750", cfg.MaxOutOfOrderMs)
	}
	if cfg.ConfidenceDecayMs != 30000 {
		t.Errorf("ConfidenceDecayMs: got %v, want 30000", cfg.ConfidenceDecayMs)
	}
	if cfg.MinBufferAhead != 5 {
		t.Errorf("MinBufferAhead: got %v, want 5", cfg.MinBufferAhead)
	}
	if cfg.SyncVerifyThreshold != 0.5 {
		t.Errorf("SyncVerifyThreshold: got %v, want 0.5", cfg.SyncVerifyThreshold)
	}
	if cfg.MaxPreloadMs != 15000 {
		t.Errorf("MaxPreloadMs: got %v, want 15000", cfg.MaxPreloadMs)
	}
	if cfg.FadeInDurationMs != 300 {
		t.Errorf("FadeInDurationMs: got %v, want 300", cfg.FadeInDurationMs)
	}
}

func TestNone(t *testing.T) {
	d := none("x")
	if d.Kind != ActionNone {
		t.Errorf("none: Kind got %v, want ActionNone(%v)", d.Kind, ActionNone)
	}
	if d.ActualRate != 1.0 {
		t.Errorf("none: ActualRate got %v, want 1.0", d.ActualRate)
	}
	if d.Reason != "x" {
		t.Errorf("none: Reason got %q, want %q", d.Reason, "x")
	}
}

func TestActionKind_Zero(t *testing.T) {
	if ActionNone != 0 {
		t.Errorf("ActionNone should be 0, got %v", ActionNone)
	}
}

func TestPreloadPhase_Zero(t *testing.T) {
	if PhaseIdle != 0 {
		t.Errorf("PhaseIdle should be 0, got %v", PhaseIdle)
	}
}

func TestActionKind_Values(t *testing.T) {
	if ActionRate != 1 {
		t.Errorf("ActionRate should be 1, got %v", ActionRate)
	}
	if ActionSeek != 2 {
		t.Errorf("ActionSeek should be 2, got %v", ActionSeek)
	}
}

func TestPreloadPhase_Values(t *testing.T) {
	phases := []PreloadPhase{PhaseIdle, PhaseConnecting, PhaseBuffering, PhaseSyncing, PhaseReady, PhaseRevealed}
	for i, p := range phases {
		if int(p) != i {
			t.Errorf("phase %d: got %v, want %d", i, p, i)
		}
	}
}

func TestEngine_StoresCfg(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SeekThreshold = 99.0
	e := New(cfg)
	if e.cfg.SeekThreshold != 99.0 {
		t.Errorf("Engine.cfg.SeekThreshold: got %v, want 99.0", e.cfg.SeekThreshold)
	}
}
