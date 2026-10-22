package netplay

import (
	"testing"
	"time"
)

// loopPair is two sessions on a loopback with a pre-built input script.
type loopPair struct {
	s      [2]*Session[pad]
	script [][2]pad
	now    time.Duration
	i      int
}

func newLoopPair(tb testing.TB, delay int) *loopPair {
	a, b := NewLoopbackPair()
	p := &loopPair{script: randomInputs(5, 4096)}
	for i, tr := range []Transport{a, b} {
		s, err := NewSession(Config[pad]{Game: newFightGame(), Codec: padCodec{}, Transport: tr, LocalPlayer: i, InputDelay: delay})
		if err != nil {
			tb.Fatal(err)
		}
		p.s[i] = s
	}
	return p
}

// tick runs one tick of both peers. Peer 1 ticks first so that peer 0 sees
// peer 1's input in time while peer 1 predicts peer 0, which forces a
// one-frame rollback whenever its prediction misses.
func (p *loopPair) tick() {
	in := p.script[p.i&4095]
	p.i++
	p.s[1].Advance(p.now, in[1])
	p.s[0].Advance(p.now, in[0])
	p.now += frameDur
}

func TestSteadyStateFrameDoesNotAllocate(t *testing.T) {
	for _, delay := range []int{0, 2} {
		p := newLoopPair(t, delay)
		for i := 0; i < 600; i++ { // warm every buffer, cross several checksum frames
			p.tick()
		}
		before := p.s[0].Stats().Rollbacks + p.s[1].Stats().Rollbacks
		allocs := testing.AllocsPerRun(500, p.tick)
		after := p.s[0].Stats().Rollbacks + p.s[1].Stats().Rollbacks
		if allocs != 0 {
			t.Errorf("delay %d: %v allocations per tick of two sessions", delay, allocs)
		}
		if delay == 0 && after-before < 20 {
			t.Errorf("only %d rollbacks in the measured ticks; the rollback path was not covered", after-before)
		}
	}
}

func TestWorstCaseRollbackDoesNotAllocate(t *testing.T) {
	p := newLoopPair(t, 0)
	for i := 0; i < 300; i++ {
		p.tick()
	}
	s := p.s[0]
	from := s.frame - 8
	allocs := testing.AllocsPerRun(200, func() { s.rollback(from) })
	if allocs != 0 {
		t.Fatalf("%v allocations per 8-frame rollback", allocs)
	}
}

func BenchmarkTickPair(b *testing.B) {
	p := newLoopPair(b, 0)
	for i := 0; i < 300; i++ {
		p.tick()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.tick()
	}
}

// BenchmarkRollback8 is the worst case the protocol allows: load a state 8
// frames back and re-simulate to the present with the test game.
func BenchmarkRollback8(b *testing.B) {
	p := newLoopPair(b, 0)
	for i := 0; i < 300; i++ {
		p.tick()
	}
	s := p.s[0]
	from := s.frame - 8
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.rollback(from)
	}
}

func BenchmarkGameStep(b *testing.B) {
	g := newFightGame()
	in := []pad{padLeft | padAttack, padRight}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		g.Step(in)
	}
}
