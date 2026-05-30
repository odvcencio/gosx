package videosync

import (
	"math"
	"testing"
)

func defaultPreloadCfg() Config {
	return DefaultConfig()
}

// Test 1: bufferedAhead < MinBufferAhead (5), positionError=0, first call
// → PhaseBuffering, ready=false, stalled=true
func TestPreloadManager_UnderBuffer(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())
	phase, ready, stalled := p.Update(3, 0, 0)
	if phase != PhaseBuffering {
		t.Errorf("want PhaseBuffering (%d), got %d", PhaseBuffering, phase)
	}
	if ready {
		t.Error("want ready=false, got true")
	}
	if !stalled {
		t.Error("want stalled=true, got false")
	}
}

// Test 2: bufferedAhead >= 5, positionError > 0.5 → PhaseSyncing, ready=false
func TestPreloadManager_Syncing(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())
	phase, ready, stalled := p.Update(6, 1.0, 100)
	if phase != PhaseSyncing {
		t.Errorf("want PhaseSyncing (%d), got %d", PhaseSyncing, phase)
	}
	if ready {
		t.Error("want ready=false, got true")
	}
	_ = stalled // not asserted in spec for this case
}

// Test 3: bufferedAhead >= 5, positionError <= 0.5 → PhaseReady, ready=true
// followed by another Update → PhaseRevealed, ready=true
func TestPreloadManager_ReadyThenRevealed(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())

	phase, ready, stalled := p.Update(6, 0.2, 100)
	if phase != PhaseReady {
		t.Errorf("want PhaseReady (%d), got %d", PhaseReady, phase)
	}
	if !ready {
		t.Error("want ready=true, got false")
	}
	if stalled {
		t.Error("want stalled=false, got true")
	}
	if !p.Revealed() {
		t.Error("want Revealed()=true after ready transition")
	}

	// Subsequent Update → PhaseRevealed
	phase2, ready2, stalled2 := p.Update(6, 0.2, 200)
	if phase2 != PhaseRevealed {
		t.Errorf("want PhaseRevealed (%d), got %d", PhaseRevealed, phase2)
	}
	if !ready2 {
		t.Error("want ready=true on PhaseRevealed, got false")
	}
	if stalled2 {
		t.Error("want stalled=false on PhaseRevealed, got true")
	}
}

// Test 4: deadline exceeded → ready=true regardless of buffer/sync state
// First Update at perfNow=0 (under-buffered → sets startMs=0).
// Later Update at perfNow=15001 with bufferedAhead=0 and positionError=99
// → ready=true (reveal-anyway).
func TestPreloadManager_DeadlineExceeded(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())

	// First call: under-buffered, records startMs=0
	phase0, ready0, _ := p.Update(0, 99, 0)
	if phase0 != PhaseBuffering {
		t.Errorf("first call: want PhaseBuffering, got %d", phase0)
	}
	if ready0 {
		t.Error("first call: want ready=false")
	}

	// Second call: past deadline
	phase1, ready1, stalled1 := p.Update(0, 99, 15001)
	if !ready1 {
		t.Errorf("past deadline: want ready=true, got false (phase=%d)", phase1)
	}
	if stalled1 {
		t.Error("past deadline: want stalled=false, got true")
	}
}

// Test 5: non-finite bufferedAhead → PhaseBuffering, stalled=true
func TestPreloadManager_NonFiniteBufferedAhead(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		p.Reset()
		phase, ready, stalled := p.Update(bad, 0, 0)
		if phase != PhaseBuffering {
			t.Errorf("non-finite bufferedAhead=%v: want PhaseBuffering, got %d", bad, phase)
		}
		if ready {
			t.Errorf("non-finite bufferedAhead=%v: want ready=false, got true", bad)
		}
		if !stalled {
			t.Errorf("non-finite bufferedAhead=%v: want stalled=true, got false", bad)
		}
	}
}

// Test Reset clears state
func TestPreloadManager_Reset(t *testing.T) {
	p := NewPreloadManager(defaultPreloadCfg())
	// Transition to revealed
	p.Update(6, 0.1, 100)
	if !p.Revealed() {
		t.Fatal("should be revealed before Reset")
	}
	p.Reset()
	if p.Revealed() {
		t.Error("want Revealed()=false after Reset")
	}
	// After reset, should start fresh
	phase, ready, stalled := p.Update(3, 0, 0)
	if phase != PhaseBuffering || ready || !stalled {
		t.Errorf("after Reset: want PhaseBuffering/false/true, got %d/%v/%v", phase, ready, stalled)
	}
}
