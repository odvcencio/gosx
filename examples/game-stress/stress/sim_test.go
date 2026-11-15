package stress

import (
	"testing"
	"time"
)

// stepN runs n ticks with scripted inputs and returns the hash after each.
func stepN(s *Sim, n int, input func(uint64, int) byte) []uint64 {
	hashes := make([]uint64, n)
	in := make([]byte, s.Config().Seats)
	for i := 0; i < n; i++ {
		for seat := range in {
			in[seat] = input(s.Tick(), seat)
		}
		s.Step(in)
		hashes[i] = s.Hash()
	}
	return hashes
}

func TestSimIsDeterministic(t *testing.T) {
	a, b := New(DefaultConfig()), New(DefaultConfig())
	ha, hb := stepN(a, 600, ScriptedInput(7)), stepN(b, 600, ScriptedInput(7))
	for i := range ha {
		if ha[i] != hb[i] {
			t.Fatalf("tick %d: hashes differ %x != %x", i+1, ha[i], hb[i])
		}
	}
	other := stepN(New(DefaultConfig()), 600, ScriptedInput(8))
	if other[599] == ha[599] {
		t.Fatal("different inputs gave the same state: the inputs do not reach the simulation")
	}
}

// goldenHash600 is the state hash after 600 ticks of DefaultConfig with
// ScriptedInput(7). It must be the same on amd64, arm64 and js/wasm; the
// browser test checks the Web Worker and the main thread against it too.
const goldenHash600 = 0xce5e3bb3cedde9b2

func TestSimGoldenHash(t *testing.T) {
	h := stepN(New(DefaultConfig()), 600, ScriptedInput(7))[599]
	if goldenHash600 == 0 {
		t.Skipf("golden hash not recorded yet: %#x", h)
	}
	if h != goldenHash600 {
		t.Fatalf("hash after 600 ticks = %#x, want %#x: the simulation is not bit-identical on this platform", h, uint64(goldenHash600))
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	s := New(DefaultConfig())
	stepN(s, 100, ScriptedInput(1))
	snap := s.Snapshot(nil)
	if len(snap) != s.SnapshotSize() {
		t.Fatalf("snapshot is %d bytes, SnapshotSize says %d", len(snap), s.SnapshotSize())
	}
	ahead := stepN(s, 50, ScriptedInput(1))
	if err := s.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if s.Tick() != 100 {
		t.Fatalf("restored tick %d, want 100", s.Tick())
	}
	again := stepN(s, 50, ScriptedInput(1))
	for i := range ahead {
		if ahead[i] != again[i] {
			t.Fatalf("tick %d after restore: %x != %x", 101+i, again[i], ahead[i])
		}
	}
	if err := s.Restore(snap[:10]); err == nil {
		t.Fatal("a truncated snapshot restored without an error")
	}
}

func TestGameReplaysMatchAndTicksAt30Hz(t *testing.T) {
	cfg := DefaultGameConfig()
	cfg.Input = ScriptedInput(3)
	g := NewGame(cfg)
	for i := 0; i < 600; i++ { // 10 s of 60 fps frames
		if _, err := g.Step(time.Second / 60); err != nil {
			t.Fatal(err)
		}
	}
	tot := g.Totals()
	if tot.Ticks < 298 || tot.Ticks > 301 {
		t.Fatalf("%d ticks in 10 s at 30 Hz", tot.Ticks)
	}
	// 6 ticks every 3rd frame, once 6 ticks exist before the frame (frame 15 on).
	if want := 196 * cfg.ReplayTicks; tot.Replayed != want {
		t.Fatalf("replayed %d ticks, want %d (6 ticks every 3rd frame)", tot.Replayed, want)
	}
	if tot.Mismatches != 0 {
		t.Fatalf("%d replays gave a different state than the ticks they replaced", tot.Mismatches)
	}
	// A game without replays reaches the same state: replays do not change it.
	plain := cfg
	plain.ReplayEvery = 0
	p := NewGame(plain)
	for i := 0; i < 600; i++ {
		_, _ = p.Step(time.Second / 60)
	}
	if p.Sim().Tick() != g.Sim().Tick() || p.Sim().Hash() != g.Sim().Hash() {
		t.Fatalf("with replays tick %d hash %x, without tick %d hash %x", g.Sim().Tick(), g.Sim().Hash(), p.Sim().Tick(), p.Sim().Hash())
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	s := New(DefaultConfig())
	stepN(s, 30, ScriptedInput(1))
	in := []byte{1, 2}
	if n := testing.AllocsPerRun(200, func() { s.Step(in) }); n != 0 {
		t.Fatalf("Sim.Step allocates %.1f times per tick", n)
	}
	cfg := DefaultGameConfig()
	cfg.Input = ScriptedInput(1)
	cfg.Clock = func() float64 { return 0 }
	g := NewGame(cfg)
	for i := 0; i < 60; i++ {
		_, _ = g.Step(time.Second / 60)
	}
	if n := testing.AllocsPerRun(300, func() { _, _ = g.Step(time.Second / 60) }); n != 0 {
		t.Fatalf("Game.Step allocates %.1f times per frame (replays included)", n)
	}
}

func TestPoseInterpolates(t *testing.T) {
	g := NewGame(DefaultGameConfig())
	for i := 0; i < 10; i++ {
		_, _ = g.Step(time.Second / 30)
	}
	a := g.Sim().Agents()[3]
	p0, p1 := g.Pose(3, 0), g.Pose(3, 1)
	if p0.X != a.PX || p0.Z != a.PZ || p1.X != a.X || p1.Z != a.Z {
		t.Fatalf("pose at 0 = %+v and at 1 = %+v, agent %+v", p0, p1, a)
	}
}

// BenchmarkTick is one 30 Hz tick of the default workload, AI included.
func BenchmarkTick(b *testing.B) {
	s := New(DefaultConfig())
	in := []byte{1, 2}
	b.ReportAllocs()
	for b.Loop() {
		s.Step(in)
	}
}

// BenchmarkReplayFrame is the worst frame: one new tick plus a 6-tick replay.
func BenchmarkReplayFrame(b *testing.B) {
	cfg := DefaultGameConfig()
	cfg.ReplayEvery = 1
	g := NewGame(cfg)
	for i := 0; i < 30; i++ {
		_, _ = g.Step(time.Second / 30)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = g.Step(time.Second / 30)
	}
}
