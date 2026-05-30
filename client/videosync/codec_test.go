package videosync

import (
	"math"
	"testing"
)

// Test 1: Heartbeat round-trip
func TestHeartbeatRoundTrip(t *testing.T) {
	h := Heartbeat{
		ServerTimeMs: 1717000000123,
		Position:     42.5,
		Playing:      true,
		ViewerCount:  7,
	}
	b := EncodeHeartbeat(h)
	if len(b) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(b))
	}
	if b[0] != 0x01 {
		t.Fatalf("expected b[0]==0x01, got 0x%02x", b[0])
	}
	got, ok := DecodeHeartbeat(b)
	if !ok {
		t.Fatal("DecodeHeartbeat returned ok=false")
	}
	if got.ServerTimeMs != h.ServerTimeMs {
		t.Errorf("ServerTimeMs: got %d, want %d", got.ServerTimeMs, h.ServerTimeMs)
	}
	if got.Playing != h.Playing {
		t.Errorf("Playing: got %v, want %v", got.Playing, h.Playing)
	}
	if got.ViewerCount != h.ViewerCount {
		t.Errorf("ViewerCount: got %d, want %d", got.ViewerCount, h.ViewerCount)
	}
	if math.Abs(float64(got.Position-h.Position)) > 1e-3 {
		t.Errorf("Position: got %v, want %v (delta > 1e-3)", got.Position, h.Position)
	}
}

// Test 2: Heartbeat exact bytes for a simple input
// ServerTimeMs=1, Position=0, Playing=false, ViewerCount=0
// Expected: [0x01, 0,0,0,0,0,0,0,1, 0,0,0,0, 0, 0,0]
func TestHeartbeatExactBytes(t *testing.T) {
	h := Heartbeat{ServerTimeMs: 1, Position: 0, Playing: false, ViewerCount: 0}
	b := EncodeHeartbeat(h)
	want := []byte{
		0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // u64 big-endian = 1
		0x00, 0x00, 0x00, 0x00, // f32 big-endian = 0.0
		0x00,       // playing = false
		0x00, 0x00, // ViewerCount = 0
	}
	if len(b) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(b), len(want))
	}
	for i, v := range want {
		if b[i] != v {
			t.Errorf("byte[%d]: got 0x%02x, want 0x%02x", i, b[i], v)
		}
	}
}

// Test 3: Ping/Pong round-trip
func TestPingPongRoundTrip(t *testing.T) {
	const ts uint64 = 1717000000456

	ping := EncodePing(ts)
	if len(ping) != 9 {
		t.Fatalf("EncodePing: expected 9 bytes, got %d", len(ping))
	}
	if ping[0] != 0x05 {
		t.Fatalf("EncodePing: expected b[0]==0x05, got 0x%02x", ping[0])
	}

	pong := EncodePong(ts)
	if len(pong) != 9 {
		t.Fatalf("EncodePong: expected 9 bytes, got %d", len(pong))
	}
	if pong[0] != 0x04 {
		t.Fatalf("EncodePong: expected b[0]==0x04, got 0x%02x", pong[0])
	}

	echoedMs, ok := DecodePong(pong)
	if !ok {
		t.Fatal("DecodePong returned ok=false")
	}
	if echoedMs != ts {
		t.Errorf("DecodePong: got %d, want %d", echoedMs, ts)
	}
}

// Test 4: DriftReport exact bytes for -1.25
func TestDriftReportExactBytes(t *testing.T) {
	b := EncodeDriftReport(-1.25)
	if len(b) != 5 {
		t.Fatalf("expected 5 bytes, got %d", len(b))
	}
	if b[0] != 0x03 {
		t.Fatalf("expected b[0]==0x03, got 0x%02x", b[0])
	}
	// Float32bits(-1.25) = 0xBFA00000
	bits := math.Float32bits(-1.25)
	want := [4]byte{
		byte(bits >> 24),
		byte(bits >> 16),
		byte(bits >> 8),
		byte(bits),
	}
	for i, v := range want {
		if b[i+1] != v {
			t.Errorf("drift byte[%d]: got 0x%02x, want 0x%02x", i+1, b[i+1], v)
		}
	}
}

// Test 5: Safety — no panics on nil/short/wrong-kind input
func TestSafety(t *testing.T) {
	_, ok := DecodeHeartbeat(nil)
	if ok {
		t.Error("DecodeHeartbeat(nil): expected ok=false")
	}

	// wrong kind: starts with 0x05 (Ping), not 0x01
	wrongKind := make([]byte, 16)
	wrongKind[0] = 0x05
	_, ok = DecodeHeartbeat(wrongKind)
	if ok {
		t.Error("DecodeHeartbeat(wrong kind): expected ok=false")
	}

	// too short for DecodePong
	_, ok = DecodePong([]byte{0x04})
	if ok {
		t.Error("DecodePong(too short): expected ok=false")
	}

	// FrameKind(nil) == 0
	if k := FrameKind(nil); k != 0 {
		t.Errorf("FrameKind(nil): got %d, want 0", k)
	}
}
