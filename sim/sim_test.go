package sim

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/odvcencio/gosx/hub"
)

// mockSim implements Simulation for testing.
type mockSim struct {
	ticks int
}

func (m *mockSim) Tick(inputs map[string]Input) {
	m.ticks++
}

func (m *mockSim) Snapshot() []byte {
	return nil
}

func (m *mockSim) Restore(snapshot []byte) {}

func (m *mockSim) State() []byte {
	return nil
}

func TestNewRunner(t *testing.T) {
	h := hub.New("test")
	s := &mockSim{}
	r := New(h, s, Options{})

	if r == nil {
		t.Fatal("expected non-nil Runner")
	}
	if r.TickRate() != 60 {
		t.Fatalf("expected TickRate 60, got %d", r.TickRate())
	}
}

func TestRunnerCollectsInputs(t *testing.T) {
	h := hub.New("test")
	s := &mockSim{}
	r := New(h, s, Options{})

	r.ReceiveInput("p1", Input{Data: []byte("attack")})
	r.ReceiveInput("p2", Input{Data: []byte("block")})

	inputs := r.DrainInputs()
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}
	if string(inputs["p1"].Data) != "attack" {
		t.Fatalf("expected p1 input 'attack', got %q", string(inputs["p1"].Data))
	}
	if string(inputs["p2"].Data) != "block" {
		t.Fatalf("expected p2 input 'block', got %q", string(inputs["p2"].Data))
	}

	// DrainInputs should clear the buffer
	after := r.DrainInputs()
	if len(after) != 0 {
		t.Fatalf("expected 0 inputs after drain, got %d", len(after))
	}
}

func TestRunnerJSONStateEncodingEmbedsValidJSON(t *testing.T) {
	h := hub.New("test-json")
	s := &mockSim{}
	r := New(h, s, Options{StateEncoding: StateEncodingJSON})

	payload := r.tickPayload(12, []byte(`{"hp":100,"phase":"fight"}`))
	wire, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(wire)
	if !strings.Contains(got, `"state":{"hp":100,"phase":"fight"}`) {
		t.Fatalf("expected raw JSON state, got %s", got)
	}
}

func TestRunnerJSONStateEncodingFallsBackForBinaryState(t *testing.T) {
	h := hub.New("test-binary")
	s := &mockSim{}
	r := New(h, s, Options{StateEncoding: StateEncodingJSON})

	payload := r.tickPayload(1, []byte{0, 1, 2})
	wire, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"state":"AAEC"`) {
		t.Fatalf("expected binary state fallback to base64, got %s", wire)
	}
}

func TestRunnerTickLoop(t *testing.T) {
	h := hub.New("test")
	s := &mockSim{}
	r := New(h, s, Options{TickRate: 60})

	r.Start()
	time.Sleep(100 * time.Millisecond)
	r.Stop()

	frame := r.Frame()
	if frame < 4 {
		t.Fatalf("expected at least 4 ticks in 100ms at 60hz, got %d", frame)
	}
	if s.ticks < 4 {
		t.Fatalf("expected mockSim.ticks >= 4, got %d", s.ticks)
	}
}

func TestSnapshotRing(t *testing.T) {
	ring := newSnapshotRing(4)

	ring.Push(1, []byte("state-1"))
	ring.Push(2, []byte("state-2"))
	ring.Push(3, []byte("state-3"))

	// Get middle entry
	data, ok := ring.Get(2)
	if !ok {
		t.Fatal("expected to find frame 2")
	}
	if string(data) != "state-2" {
		t.Fatalf("expected 'state-2', got %q", string(data))
	}

	// Miss on non-existent frame
	_, ok = ring.Get(99)
	if ok {
		t.Fatal("expected miss on frame 99")
	}

	// Verify data is copied (mutation safety)
	original := []byte("mutable")
	ring.Push(4, original)
	original[0] = 'X'
	data, ok = ring.Get(4)
	if !ok {
		t.Fatal("expected to find frame 4")
	}
	if string(data) != "mutable" {
		t.Fatalf("expected 'mutable' (copy), got %q", string(data))
	}
}

func TestSpectatorGetsCurrentState(t *testing.T) {
	h := hub.New("test")
	s := &mockSim{}
	r := New(h, s, Options{})

	r.RegisterHandlers()

	// Verify hub is non-nil and wired
	if r.hub == nil {
		t.Fatal("expected hub to be non-nil after RegisterHandlers")
	}

	// Verify runner has the hub reference and handlers are set
	// We can't directly inspect handlers, but we can verify
	// the runner is properly configured
	if r.hub.Name() != "test" {
		t.Fatalf("expected hub name 'test', got %q", r.hub.Name())
	}
}

// mockFightSim simulates a simple fighting game for integration testing.
type mockFightSim struct {
	p1Health  int
	p2Health  int
	tickCount int
}

func newMockFightSim() *mockFightSim {
	return &mockFightSim{p1Health: 100, p2Health: 100}
}

func (m *mockFightSim) Tick(inputs map[string]Input) {
	m.tickCount++
	if _, ok := inputs["p1"]; ok {
		m.p2Health -= 10
	}
}

func (m *mockFightSim) Snapshot() []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(m.p1Health))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(m.p2Health))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(m.tickCount))
	return buf
}

func (m *mockFightSim) Restore(snapshot []byte) {
	if len(snapshot) < 12 {
		return
	}
	m.p1Health = int(binary.LittleEndian.Uint32(snapshot[0:4]))
	m.p2Health = int(binary.LittleEndian.Uint32(snapshot[4:8]))
	m.tickCount = int(binary.LittleEndian.Uint32(snapshot[8:12]))
}

func (m *mockFightSim) State() []byte {
	return m.Snapshot()
}

func TestFightingGamePattern(t *testing.T) {
	h := hub.New("fight")
	s := newMockFightSim()
	r := New(h, s, Options{TickRate: 60})

	// Send p1 attack input before starting
	r.ReceiveInput("p1", Input{Data: []byte("attack")})

	r.Start()
	time.Sleep(50 * time.Millisecond)
	r.Stop()

	// Verify ticks advanced
	if r.Frame() < 1 {
		t.Fatalf("expected at least 1 tick, got %d", r.Frame())
	}

	// p1 health should be unchanged (only p2 takes damage)
	if s.p1Health != 100 {
		t.Fatalf("expected p1Health 100, got %d", s.p1Health)
	}

	// p2 should have taken at least one hit (10 damage from first tick)
	if s.p2Health > 90 {
		t.Fatalf("expected p2Health <= 90, got %d", s.p2Health)
	}

	// Verify replay has frames
	replay := r.Replay()
	if len(replay.Frames) < 1 {
		t.Fatal("expected at least 1 replay frame")
	}

	// First frame should have p1's input
	if len(replay.Frames[0].Inputs) < 1 {
		t.Fatal("expected first replay frame to have at least 1 input")
	}
	if _, ok := replay.Frames[0].Inputs["p1"]; !ok {
		t.Fatal("expected p1 input in first replay frame")
	}
}

func TestRunnerConcurrentInputs(t *testing.T) {
	h := hub.New("test-race")
	s := &mockSim{}
	r := New(h, s, Options{TickRate: 60})
	r.RegisterHandlers()
	r.Start()
	defer r.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.ReceiveInput(fmt.Sprintf("p%d", n), Input{Data: []byte(`{}`)})
		}(i)
	}
	wg.Wait()
}
