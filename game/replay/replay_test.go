package replay_test

import (
	"math/rand"
	"testing"
	"time"

	"m31labs.dev/gosx/game/replay"
	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/roomtest"
	"m31labs.dev/gosx/sim/simcore"
)

const (
	seats = 4
	props = 12
	rate  = 60
)

func newGame() *roomtest.Game { return roomtest.NewGame(seats, props, 7, rate) }

// play runs n ticks of seeded random input through a recorder and returns the
// live state hash after each tick (index 0 is the start).
func play(t testing.TB, n int, cfg replay.Config) (*replay.Recorder, []uint64) {
	t.Helper()
	g := newGame()
	cfg.TickRate = rate
	rec := replay.NewRecorder(g, cfg)
	rng := rand.New(rand.NewSource(1))
	hashes := []uint64{replay.Hash(g)}
	inputs := map[string]simcore.Input{}
	for i := 0; i < n; i++ {
		for k := range inputs {
			delete(inputs, k)
		}
		for s := 0; s < seats; s++ {
			if rng.Intn(3) > 0 { // some seats idle some ticks
				inputs[mode.PlayerID(s)] = simcore.Input{Data: []byte{byte(rng.Intn(32))}}
			}
		}
		rec.Tick(inputs)
		hashes = append(hashes, replay.Hash(g))
	}
	return rec, hashes
}

func TestSeekMatchesLiveState(t *testing.T) {
	rec, hashes := play(t, 1000, replay.Config{Interval: 64})
	g := newGame()
	p, err := replay.NewPlayer(g, rec.Recording())
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(2))
	ticks := []uint64{0, 1, 63, 64, 65, 999, 1000, 500, 10, 10}
	for i := 0; i < 300; i++ {
		ticks = append(ticks, uint64(rng.Intn(1001)))
	}
	for _, n := range ticks {
		if err := p.Seek(n); err != nil {
			t.Fatalf("Seek(%d): %v", n, err)
		}
		if got := replay.Hash(g); got != hashes[n] {
			t.Fatalf("tick %d: replay hash %x, live %x", n, got, hashes[n])
		}
		if p.Tick() != n {
			t.Fatalf("Tick() = %d, want %d", p.Tick(), n)
		}
	}
}

func TestSeekBoundsResim(t *testing.T) {
	rec, _ := play(t, 500, replay.Config{Interval: 50})
	p, _ := replay.NewPlayer(newGame(), rec.Recording())
	for _, n := range []uint64{499, 3, 250, 100} {
		_ = p.Seek(n)
		if p.LastResim() > 50 {
			t.Fatalf("Seek(%d) resimulated %d ticks, want <= 50", n, p.LastResim())
		}
	}
	if err := p.Seek(501); err == nil {
		t.Fatal("seek past the end succeeded")
	}
}

func TestStepAndReverse(t *testing.T) {
	rec, hashes := play(t, 200, replay.Config{Interval: 32})
	g := newGame()
	p, _ := replay.NewPlayer(g, rec.Recording())
	_ = p.Seek(100)
	if p.StepForward(3) != 3 || p.Tick() != 103 || replay.Hash(g) != hashes[103] {
		t.Fatalf("StepForward: tick %d", p.Tick())
	}
	if err := p.StepBack(5); err != nil || p.Tick() != 98 || replay.Hash(g) != hashes[98] {
		t.Fatalf("StepBack: tick %d err %v", p.Tick(), err)
	}
	if err := p.StepBack(1000); err != nil || p.Tick() != 0 {
		t.Fatalf("StepBack past start: tick %d err %v", p.Tick(), err)
	}
	_ = p.Seek(198)
	if n := p.StepForward(10); n != 2 || !p.AtEnd() {
		t.Fatalf("StepForward at end ran %d", n)
	}
}

func TestPlaybackSpeedAndPause(t *testing.T) {
	rec, hashes := play(t, 600, replay.Config{Interval: 60})
	g := newGame()
	p, _ := replay.NewPlayer(g, rec.Recording())
	if !p.Paused() {
		t.Fatal("player should start paused")
	}
	if p.Advance(time.Second) != 0 {
		t.Fatal("paused player advanced")
	}
	p.Resume()
	for _, c := range []struct {
		speed float64
		want  uint64
	}{{1, 60}, {0.25, 15}, {4, 240}, {2, 120}, {0.5, 30}} {
		_ = p.Seek(0)
		p.SetSpeed(c.speed)
		p.Resume()
		// Feed 1 second in uneven frames; the remainder must carry.
		for _, d := range []time.Duration{7, 13, 21, 400, 559} {
			p.Advance(d * time.Millisecond)
		}
		if p.Tick() != c.want {
			t.Errorf("speed %v: tick %d, want %d", c.speed, p.Tick(), c.want)
		}
		if replay.Hash(g) != hashes[p.Tick()] {
			t.Errorf("speed %v: state differs from live at tick %d", c.speed, p.Tick())
		}
	}
	p.SetSpeed(100)
	if p.Speed() != replay.MaxSpeed {
		t.Errorf("speed not clamped: %v", p.Speed())
	}
	p.SetSpeed(0)
	if p.Speed() != replay.MinSpeed {
		t.Errorf("speed not clamped: %v", p.Speed())
	}
	p.Pause()
	at := p.Tick()
	p.Advance(time.Second)
	if p.Tick() != at {
		t.Error("pause did not hold the tick")
	}
}

func TestEndAndLoop(t *testing.T) {
	rec, _ := play(t, 30, replay.Config{Interval: 10})
	p, _ := replay.NewPlayer(newGame(), rec.Recording())
	p.Resume()
	p.Advance(5 * time.Second)
	if !p.AtEnd() || !p.Paused() {
		t.Fatalf("expected to stop paused at end, tick %d paused %v", p.Tick(), p.Paused())
	}
	_ = p.Seek(0)
	p.SetLoop(true)
	p.Resume()
	p.Advance(700 * time.Millisecond) // 42 ticks: 30, wrap, 12 more
	if p.Paused() || p.Tick() == 0 {
		t.Fatalf("loop: tick %d paused %v", p.Tick(), p.Paused())
	}
}

func TestBoundedMemory(t *testing.T) {
	rec, hashes := play(t, 5000, replay.Config{Interval: 50, MaxBytes: 8 << 10})
	if rec.Bytes() > 8<<10+4<<10 {
		t.Fatalf("recorder holds %d bytes, cap 8192", rec.Bytes())
	}
	if rec.Dropped() == 0 || rec.Start() == 0 {
		t.Fatal("expected the oldest ticks to be dropped")
	}
	r := rec.Recording()
	if r.Start() != rec.Start() || r.End() != 5000 {
		t.Fatalf("range [%d,%d]", r.Start(), r.End())
	}
	g := newGame()
	p, err := replay.NewPlayer(g, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint64{r.Start(), r.Start() + 77, 4999, 5000} {
		if err := p.Seek(n); err != nil {
			t.Fatal(err)
		}
		if replay.Hash(g) != hashes[n] {
			t.Fatalf("tick %d differs after eviction", n)
		}
	}
	if err := p.Seek(r.Start() - 1); err == nil {
		t.Fatal("seek before the window succeeded")
	}
}

func TestRecordingIsStableWhileRecording(t *testing.T) {
	g := newGame()
	rec := replay.NewRecorder(g, replay.Config{Interval: 10})
	for i := 0; i < 25; i++ {
		rec.Tick(nil)
	}
	r := rec.Recording()
	end, h := r.End(), uint64(0)
	{
		g2 := newGame()
		p, _ := replay.NewPlayer(g2, r)
		_ = p.Seek(end)
		h = replay.Hash(g2)
	}
	for i := 0; i < 40; i++ {
		rec.Tick(map[string]simcore.Input{mode.PlayerID(0): {Data: []byte{1}}})
	}
	if r.End() != end {
		t.Fatal("recording changed after more play")
	}
	g3 := newGame()
	p, _ := replay.NewPlayer(g3, r)
	_ = p.Seek(end)
	if replay.Hash(g3) != h {
		t.Fatal("replayed state changed")
	}
}

func TestMemoryPerMinute(t *testing.T) {
	// 60 Hz, 4 seats, 12 props, 2 s keyframes.
	rec, _ := play(t, 60*60, replay.Config{Interval: 120})
	t.Logf("1 minute at %d Hz, %d bodies, keyframe every 120 ticks: %d bytes (%d keyframes)",
		rate, seats+props, rec.Bytes(), len(rec.Recording().Keyframes()))
}

func BenchmarkSeek(b *testing.B) {
	for _, iv := range []int{30, 120, 600} {
		rec, _ := play(b, 60*60, replay.Config{Interval: iv})
		r := rec.Recording()
		p, _ := replay.NewPlayer(newGame(), r)
		rng := rand.New(rand.NewSource(3))
		b.Run("interval="+itoa(iv), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = p.Seek(uint64(rng.Intn(3601)))
			}
		})
	}
}

func BenchmarkRecordTick(b *testing.B) {
	g := newGame()
	rec := replay.NewRecorder(g, replay.Config{Interval: 120, MaxBytes: 1 << 20})
	in := map[string]simcore.Input{mode.PlayerID(0): {Data: []byte{3}}, mode.PlayerID(1): {Data: []byte{4}}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec.Tick(in)
	}
}

func BenchmarkPlayTick(b *testing.B) {
	rec, _ := play(b, 3600, replay.Config{Interval: 120})
	p, _ := replay.NewPlayer(newGame(), rec.Recording())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p.AtEnd() {
			_ = p.Seek(0)
		}
		p.StepForward(1)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
