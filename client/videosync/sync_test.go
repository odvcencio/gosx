package videosync

import (
	"math"
	"testing"
)

const eps = 1e-9

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

// NewSyncEngine is the constructor under test.

// Test 1: LATENCY-MEDIAN
// Feed RTTs [20,22,200,24,26]; one-way = [10,11,100,12,13]; median = 12
func TestLatencyMedian(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	for _, rtt := range []float64{20, 22, 200, 24, 26} {
		se.RTT(rtt)
	}
	got := se.EstimatedLatencyMs()
	want := 12.0
	if !almostEqual(got, want, eps) {
		t.Errorf("EstimatedLatencyMs: got %v, want %v", got, want)
	}
}

// Test 2: RTT rejection — NaN, 0, -5, 5000 add no samples; default=50 when empty
func TestRTTRejection(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	se.RTT(math.NaN())
	se.RTT(0)
	se.RTT(-5)
	se.RTT(5000) // > MaxRTTMs(4000) → rejected
	got := se.EstimatedLatencyMs()
	if !almostEqual(got, 50.0, eps) {
		t.Errorf("EstimatedLatencyMs after rejections: got %v, want 50 (DefaultLatencyMs)", got)
	}
}

// Test 3: One-way cap — RTT(4000) is accepted (boundary <= 4000); oneWay=min(2000,1200)=1200
func TestOneWayCap(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	se.RTT(4000) // accepted; oneWay = min(2000, 1200) = 1200
	got := se.EstimatedLatencyMs()
	if !almostEqual(got, 1200.0, eps) {
		t.Errorf("EstimatedLatencyMs after RTT(4000): got %v, want 1200", got)
	}
}

// Test 4: EMPTY → EstimatedLatencyMs == DefaultLatencyMs == 50
func TestEmptyLatency(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	got := se.EstimatedLatencyMs()
	if !almostEqual(got, 50.0, eps) {
		t.Errorf("empty EstimatedLatencyMs: got %v, want 50", got)
	}
}

// Test 5: OUT-OF-ORDER
// Ingest serverTime=10000 (accepted), then serverTime=9000 (diff=1000 > 750 tol → dropped).
// Also verify serverTime=9500 (diff=500 <= 750) is accepted.
func TestOutOfOrder(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())

	// First heartbeat accepted
	se.Ingest(10000, 100.0, true, 1000.0)
	if se.OutOfOrderDropped() != 0 {
		t.Fatalf("no drops yet, got %d", se.OutOfOrderDropped())
	}

	// Out-of-order: 9000 is 1000ms before 10000 (> 750 tolerance) → dropped
	se.Ingest(9000, 200.0, false, 2000.0)
	if se.OutOfOrderDropped() != 1 {
		t.Errorf("OutOfOrderDropped: got %d, want 1", se.OutOfOrderDropped())
	}
	// Position should still reflect the first heartbeat (100), not 200
	if !almostEqual(se.LastPosition(), 100.0, eps) {
		t.Errorf("LastPosition after OOO drop: got %v, want 100", se.LastPosition())
	}
	if se.LastPlaying() != true {
		t.Errorf("LastPlaying after OOO drop: got %v, want true", se.LastPlaying())
	}

	// serverTime=9500: diff = 10000 - 9500 = 500 <= 750 → should be ACCEPTED
	// (9500 is after last accepted 10000 minus tolerance, so it passes the check)
	se.Ingest(9500, 50.0, false, 3000.0)
	if se.OutOfOrderDropped() != 1 {
		t.Errorf("OutOfOrderDropped after 9500: got %d, still want 1", se.OutOfOrderDropped())
	}
	if !almostEqual(se.LastPosition(), 50.0, eps) {
		t.Errorf("LastPosition after 9500 ingest: got %v, want 50", se.LastPosition())
	}
}

// Test 6: PROJECTION
func TestProjection(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())

	// Establish a known latency: single RTT=200 → oneWay=100
	se.RTT(200)
	L := se.EstimatedLatencyMs() // 100

	// Playing
	se.Ingest(1000, 100.0, true, 1000.0)
	// ProjectedTarget(1500): elapsedMs=500; target=100 + (500+L)/1000
	want := 100.0 + (500.0+L)/1000.0
	got := se.ProjectedTarget(1500.0)
	if !almostEqual(got, want, 1e-6) {
		t.Errorf("ProjectedTarget playing: got %v, want %v", got, want)
	}

	// Paused: position stays bare
	se.Ingest(2000, 100.0, false, 2000.0)
	got = se.ProjectedTarget(9999.0)
	if !almostEqual(got, 100.0, eps) {
		t.Errorf("ProjectedTarget paused: got %v, want 100", got)
	}
}

// Test 6b: ProjectedTarget before any heartbeat → 0
func TestProjectionNoHeartbeat(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	got := se.ProjectedTarget(1000.0)
	if got != 0 {
		t.Errorf("ProjectedTarget with no heartbeat: got %v, want 0", got)
	}
}

// Test 7: CONFIDENCE
func TestConfidence(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())

	// No heartbeat → 0
	if se.Confidence(0) != 0 {
		t.Errorf("Confidence before heartbeat: got %v, want 0", se.Confidence(0))
	}

	se.Ingest(0, 0.0, false, 0.0) // recvPerfMs = 0
	// Confidence(0) = max(0, 1 - 0/30000) = 1
	if !almostEqual(se.Confidence(0), 1.0, eps) {
		t.Errorf("Confidence(0): got %v, want 1", se.Confidence(0))
	}
	// Confidence(15000) = max(0, 1 - 15000/30000) = 0.5
	if !almostEqual(se.Confidence(15000), 0.5, eps) {
		t.Errorf("Confidence(15000): got %v, want 0.5", se.Confidence(15000))
	}
	// Confidence(30000) = max(0, 1 - 1) = 0
	if !almostEqual(se.Confidence(30000), 0.0, eps) {
		t.Errorf("Confidence(30000): got %v, want 0", se.Confidence(30000))
	}
	// Confidence(40000) = max(0, ...) = 0
	if !almostEqual(se.Confidence(40000), 0.0, eps) {
		t.Errorf("Confidence(40000): got %v, want 0", se.Confidence(40000))
	}
}

// Test 8: Non-finite position is dropped — prior state unchanged
func TestNonFinitePositionDropped(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())

	// Establish baseline
	se.Ingest(5000, 42.0, true, 500.0)
	if !almostEqual(se.LastPosition(), 42.0, eps) {
		t.Fatalf("baseline LastPosition: got %v, want 42", se.LastPosition())
	}

	// NaN position
	se.Ingest(6000, float32(math.NaN()), false, 600.0)
	if !almostEqual(se.LastPosition(), 42.0, eps) {
		t.Errorf("after NaN ingest, LastPosition: got %v, want 42", se.LastPosition())
	}
	if !se.LastPlaying() {
		t.Errorf("after NaN ingest, LastPlaying: got false, want true")
	}

	// +Inf position
	se.Ingest(7000, float32(math.Inf(1)), false, 700.0)
	if !almostEqual(se.LastPosition(), 42.0, eps) {
		t.Errorf("after +Inf ingest, LastPosition: got %v, want 42", se.LastPosition())
	}

	// -Inf position
	se.Ingest(8000, float32(math.Inf(-1)), false, 800.0)
	if !almostEqual(se.LastPosition(), 42.0, eps) {
		t.Errorf("after -Inf ingest, LastPosition: got %v, want 42", se.LastPosition())
	}
}

// Test: negative position is clamped to 0
func TestPositionClamped(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	se.Ingest(1000, -5.0, false, 0.0)
	if !almostEqual(se.LastPosition(), 0.0, eps) {
		t.Errorf("negative position clamped: got %v, want 0", se.LastPosition())
	}
}

// Test: ring buffer respects MaxLatencySamples (window size)
func TestLatencyRingWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxLatencySamples = 3
	se := NewSyncEngine(cfg)

	// Feed 5 RTTs; only last 3 should be kept
	// RTT=20 → oneWay=10
	// RTT=22 → oneWay=11
	// RTT=24 → oneWay=12
	// RTT=26 → oneWay=13
	// RTT=28 → oneWay=14
	// Window of 3: [12, 13, 14] → median = 13
	for _, rtt := range []float64{20, 22, 24, 26, 28} {
		se.RTT(rtt)
	}
	got := se.EstimatedLatencyMs()
	want := 13.0
	if !almostEqual(got, want, eps) {
		t.Errorf("EstimatedLatencyMs with window=3: got %v, want %v", got, want)
	}
}

// Test: even-count median → mean of two middle values
func TestLatencyMedianEvenCount(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	// RTTs [20, 22] → oneWay [10, 11] → median = (10+11)/2 = 10.5
	se.RTT(20)
	se.RTT(22)
	got := se.EstimatedLatencyMs()
	want := 10.5
	if !almostEqual(got, want, eps) {
		t.Errorf("EstimatedLatencyMs even: got %v, want %v", got, want)
	}
}

// Test: ProjectedTarget elapsed clamped at 0 when perfNowMs < recvPerfMs
func TestProjectionElapsedClamped(t *testing.T) {
	se := NewSyncEngine(DefaultConfig())
	se.RTT(200) // latency = 100
	se.Ingest(1000, 50.0, true, 1000.0)
	// perfNowMs=500 < recvPerfMs=1000 → elapsed clamped to 0
	// projected = 50 + (0 + 100)/1000 = 50.1
	got := se.ProjectedTarget(500.0)
	L := se.EstimatedLatencyMs()
	want := 50.0 + L/1000.0
	if !almostEqual(got, want, 1e-6) {
		t.Errorf("ProjectedTarget clamped elapsed: got %v, want %v", got, want)
	}
}
