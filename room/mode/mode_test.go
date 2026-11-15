package mode

import (
	"errors"
	"testing"
	"time"

	"m31labs.dev/gosx/sim"
)

type nopSim struct{}

func (nopSim) Tick(map[string]sim.Input) {}
func (nopSim) Snapshot() []byte          { return nil }
func (nopSim) Restore([]byte)            {}
func (nopSim) State() []byte             { return nil }

func TestNormalizeDefaultsAndErrors(t *testing.T) {
	m, err := Mode{Name: "m", Seats: 2, NewSim: func(uint64) sim.Simulation { return nopSim{} }}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if m.TickRate != DefaultTickRate || m.Countdown != DefaultCountdown || m.InputSize != DefaultInputSize {
		t.Fatalf("defaults %+v", m)
	}
	if m.Period() != time.Second/DefaultTickRate || len(m.Bot(0).Input(BotContext{}, nil)) != 0 || m.Role(5) != "" {
		t.Fatal("period, default bot or role")
	}
	skip, _ := Mode{Name: "s", Seats: 1, Countdown: -1, NewSim: m.NewSim}.Normalize()
	if skip.Countdown != 0 {
		t.Fatal("negative countdown should skip")
	}
	for name, bad := range map[string]Mode{
		"no name":   {Seats: 1, NewSim: m.NewSim},
		"no seats":  {Name: "x", NewSim: m.NewSim},
		"no sim":    {Name: "x", Seats: 1},
		"roles":     {Name: "x", Seats: 2, Roles: []string{"a"}, NewSim: m.NewSim},
		"big input": {Name: "x", Seats: 1, InputSize: 65, NewSim: m.NewSim},
	} {
		if _, err := bad.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPlayerIDRoundTrip(t *testing.T) {
	for _, seat := range []int{0, 1, 9, 63} {
		if got := SeatOf(PlayerID(seat)); got != seat {
			t.Fatalf("seat %d -> %q -> %d", seat, PlayerID(seat), got)
		}
	}
	for _, id := range []string{"", "p", "q1", "p01", "p-1", "p64"} {
		if SeatOf(id) != -1 {
			t.Fatalf("SeatOf(%q) accepted", id)
		}
	}
	if n := testing.AllocsPerRun(100, func() { _ = PlayerID(7) }); n != 0 {
		t.Fatalf("PlayerID allocates %v", n)
	}
}

func TestInputsReuseStorage(t *testing.T) {
	in := NewInputs(2, 4)
	in.Set(0, []byte{1, 2, 3, 4, 5, 6}) // cut to 4
	if got := in.Map()["p0"].Data; len(got) != 4 {
		t.Fatalf("input not cut: %v", got)
	}
	n := testing.AllocsPerRun(100, func() {
		buf := append(in.Buffer(1), 9, 9)
		in.Set(1, buf)
		in.Set(0, nil)
	})
	if n != 0 {
		t.Fatalf("Inputs allocate %v per tick", n)
	}
	if in.Seats() != 2 || len(in.Get(0)) != 0 || len(in.Get(1)) != 2 {
		t.Fatal("contents")
	}
}
