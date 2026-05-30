package scheduled

import (
	"testing"
	"time"
)

func TestEvaluate_None(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Second)
	lastProgress := now.Add(-2 * time.Second)

	v := evaluate(now, started, lastProgress, 30*time.Second, 10*time.Second)
	if v != verdictNone {
		t.Errorf("expected verdictNone, got %v", v)
	}
}

func TestEvaluate_HardTimeout(t *testing.T) {
	now := time.Now()
	started := now.Add(-31 * time.Second) // exceeded 30s timeout
	lastProgress := now.Add(-1 * time.Second)

	v := evaluate(now, started, lastProgress, 30*time.Second, 60*time.Second)
	if v != verdictTimeout {
		t.Errorf("expected verdictTimeout, got %v", v)
	}
}

func TestEvaluate_TimeoutTakesPrecedenceOverStall(t *testing.T) {
	now := time.Now()
	started := now.Add(-31 * time.Second)      // exceeded 30s hard timeout
	lastProgress := now.Add(-20 * time.Second) // also exceeded 10s stall

	v := evaluate(now, started, lastProgress, 30*time.Second, 10*time.Second)
	if v != verdictTimeout {
		t.Errorf("expected verdictTimeout (takes precedence), got %v", v)
	}
}

func TestEvaluate_Stall(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Second)       // within 60s hard timeout
	lastProgress := now.Add(-15 * time.Second) // exceeded 10s stall

	v := evaluate(now, started, lastProgress, 60*time.Second, 10*time.Second)
	if v != verdictStall {
		t.Errorf("expected verdictStall, got %v", v)
	}
}

func TestEvaluate_DisableHardTimeout(t *testing.T) {
	now := time.Now()
	started := now.Add(-9999 * time.Second) // way past any reasonable timeout
	lastProgress := now.Add(-1 * time.Second)

	v := evaluate(now, started, lastProgress, 0, 10*time.Second) // timeout=0 disables
	if v != verdictNone {
		t.Errorf("expected verdictNone (hard timeout disabled), got %v", v)
	}
}

func TestEvaluate_DisableStall(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Second)
	lastProgress := now.Add(-9999 * time.Second) // way past any stall threshold

	v := evaluate(now, started, lastProgress, 60*time.Second, 0) // progressTimeout=0 disables
	if v != verdictNone {
		t.Errorf("expected verdictNone (stall disabled), got %v", v)
	}
}

func TestEvaluate_BothDisabled(t *testing.T) {
	now := time.Now()
	started := now.Add(-9999 * time.Second)
	lastProgress := now.Add(-9999 * time.Second)

	v := evaluate(now, started, lastProgress, 0, 0)
	if v != verdictNone {
		t.Errorf("expected verdictNone (both disabled), got %v", v)
	}
}

func TestEvaluate_ExactlyAtBoundary(t *testing.T) {
	// Exactly at threshold should trigger (>= check)
	now := time.Now()
	started := now.Add(-30 * time.Second)

	v := evaluate(now, started, now, 30*time.Second, 0)
	if v != verdictTimeout {
		t.Errorf("expected verdictTimeout at exact boundary, got %v", v)
	}
}
