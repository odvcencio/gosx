package roomtest

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/sim"
)

// goldenHash is the SHA-256 of the snapshot after 1000 scripted ticks with
// seed 42. Integer math makes it the same on every platform; a change here
// means the game's rules or encoding changed.
const goldenHash = "508c135e7932aa4853827d8d094552a0526586d1391550c22e6f0453a83a6a28"

func scriptedRun(t testing.TB, seed uint64, ticks int) *Game {
	g := NewGame(3, 5, seed, 30)
	inputs := mode.NewInputs(3, 1)
	for tick := 1; tick <= ticks; tick++ {
		inputs.Set(0, []byte{byte(tick/9) & 0x1F})
		inputs.Set(1, ChaseBot.Input(mode.BotContext{Tick: uint64(tick), Seat: 1, Sim: g}, inputs.Buffer(1)))
		inputs.Set(2, []byte{Left | Up})
		g.Tick(inputs.Map())
	}
	return g
}

func TestBodiesDeterministicGolden(t *testing.T) {
	a, b := scriptedRun(t, 42, 1000), scriptedRun(t, 42, 1000)
	sa, sb := a.Snapshot(), b.Snapshot()
	if string(sa) != string(sb) {
		t.Fatal("two runs with the same seed and inputs differ")
	}
	sum := sha256.Sum256(sa)
	if got := hex.EncodeToString(sum[:]); got != goldenHash {
		t.Fatalf("snapshot hash %s, want %s", got, goldenHash)
	}
	if a.World.Tick != 1000 {
		t.Fatalf("world tick %d", a.World.Tick)
	}
}

func TestBodiesSnapshotRestore(t *testing.T) {
	g := scriptedRun(t, 7, 200)
	snap := g.Snapshot()
	other := NewGame(3, 5, 99, 30)
	if err := other.RestoreSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if string(other.Snapshot()) != string(snap) {
		t.Fatal("restore did not reproduce the snapshot")
	}
	in := map[string]sim.Input{"p0": {Data: []byte{Right}}}
	g.Tick(in)
	other.Tick(in)
	if string(g.Snapshot()) != string(other.Snapshot()) {
		t.Fatal("restored game diverged on the next tick")
	}
	if err := NewGame(2, 5, 1, 30).RestoreSnapshot(snap); err == nil {
		t.Fatal("snapshot of another shape accepted")
	}
	if Of(nil) != nil || Of(g) != g {
		t.Fatal("Of")
	}
}

func TestModeResults(t *testing.T) {
	m := Mode(Options{Seats: 2})
	s := m.NewSim(1)
	if len(m.Results(s)) != 8 {
		t.Fatal("results payload")
	}
	if _, err := m.Normalize(); err != nil {
		t.Fatal(err)
	}
}
