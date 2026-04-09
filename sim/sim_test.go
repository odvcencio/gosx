package sim

import (
	"testing"

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
