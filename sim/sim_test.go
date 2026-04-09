package sim

import (
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
