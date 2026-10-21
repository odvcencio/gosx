package netplay

import (
	"errors"
	"testing"
	"time"
)

const frameDur = time.Second / 60

// pair is two sessions over one network with a scripted input source.
type pair struct {
	t      *testing.T
	net    *LossyNet
	s      [2]*Session[pad]
	g      [2]*fightGame
	script [][2]pad
	sums   [2]map[int64]uint64
	period [2]time.Duration
	next   [2]time.Duration
	now    time.Duration
	ticks  [2]int
}

type pairOpts struct {
	net     NetConfig
	delay   [2]int
	relay   bool
	period  [2]time.Duration // tick period per peer, default frameDur
	offset  time.Duration    // peer 1 starts this much later
	mutate  func(i int, c *Config[pad])
	frames  int
	scriptN int
}

func newPair(t *testing.T, o pairOpts) *pair {
	t.Helper()
	p := &pair{t: t, net: NewLossyNet(o.net)}
	n := o.scriptN
	if n == 0 {
		n = 4000
	}
	p.script = randomInputs(o.net.Seed+11, n)
	for i := 0; i < 2; i++ {
		p.g[i] = newFightGame()
		p.sums[i] = map[int64]uint64{}
		i := i
		cfg := Config[pad]{
			Game: p.g[i], Codec: padCodec{}, Transport: p.net.Endpoint(i),
			LocalPlayer: i, InputDelay: o.delay[i], Relay: o.relay,
			OnChecksum: func(f int64, s uint64) { p.sums[i][f] = s },
		}
		if o.mutate != nil {
			o.mutate(i, &cfg)
		}
		s, err := NewSession(cfg)
		if err != nil {
			t.Fatal(err)
		}
		p.s[i] = s
		p.period[i] = frameDur
		if o.period[i] != 0 {
			p.period[i] = o.period[i]
		}
	}
	p.next[1] = o.offset
	return p
}

// run advances virtual time for d, ticking each peer on its own period.
func (p *pair) run(d time.Duration) {
	end := p.now + d
	for p.now < end {
		i := 0
		if p.next[1] < p.next[0] {
			i = 1
		}
		p.now = p.next[i]
		p.next[i] += p.period[i]
		p.tick(i)
	}
}

func (p *pair) tick(i int) Outcome {
	p.net.SetNow(p.now)
	s := p.s[i]
	f := s.Frame() // local sample index: script by this peer's own frame
	var in pad
	if f >= 0 && int(f) < len(p.script) {
		in = p.script[f][i]
	}
	p.ticks[i]++
	return s.Advance(p.now, in)
}

func (p *pair) requireAgreement(minCommon int) {
	p.t.Helper()
	common := 0
	for f, a := range p.sums[0] {
		if b, ok := p.sums[1][f]; ok {
			common++
			if a != b {
				p.t.Fatalf("checksum mismatch at frame %d: %x vs %x", f, a, b)
			}
		}
	}
	if common < minCommon {
		p.t.Fatalf("only %d common checksums, want >= %d", common, minCommon)
	}
	for i := 0; i < 2; i++ {
		if p.s[i].Err() != nil {
			p.t.Fatalf("peer %d ended: %v", i, p.s[i].Err())
		}
		if st := p.s[i].Stats(); st.Mismatches != 0 {
			p.t.Fatalf("peer %d saw %d desyncs", i, st.Mismatches)
		}
	}
}

func TestPairOverLoopbackNoRollbackAtZeroLatency(t *testing.T) {
	a, b := NewLoopbackPair()
	var sums [2][]uint64
	var ss [2]*Session[pad]
	for i, tr := range []Transport{a, b} {
		i := i
		s, err := NewSession(Config[pad]{
			Game: newFightGame(), Codec: padCodec{}, Transport: tr, LocalPlayer: i,
			OnChecksum: func(f int64, c uint64) { sums[i] = append(sums[i], c) },
		})
		if err != nil {
			t.Fatal(err)
		}
		ss[i] = s
	}
	script := randomInputs(3, 600)
	var now time.Duration
	for k := 0; k < 600; k++ {
		for i := 0; i < 2; i++ {
			ss[i].Advance(now, script[ss[i].Frame()][i])
		}
		now += frameDur
	}
	for i := 0; i < 2; i++ {
		if ss[i].Frame() < 590 {
			t.Fatalf("peer %d only reached frame %d", i, ss[i].Frame())
		}
	}
	if len(sums[0]) < 15 || len(sums[0]) != len(sums[1]) {
		t.Fatalf("checksum counts %d %d", len(sums[0]), len(sums[1]))
	}
	for k := range sums[0] {
		if sums[0][k] != sums[1][k] {
			t.Fatalf("checksum %d differs", k)
		}
	}
	// Peer 1 starts one tick ahead of peer 0, so it predicts one frame.
	for i := 0; i < 2; i++ {
		if d := ss[i].Stats().MaxRollbackDepth; d > 1 {
			t.Fatalf("peer %d rolled back %d frames at zero latency", i, d)
		}
	}
}

func TestRollbackCorrectsMispredictionWithinOneTick(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 1, Delay: [2]time.Duration{4 * frameDur, 4 * frameDur}}})
	p.run(6 * time.Second)
	for i := 0; i < 2; i++ {
		st := p.s[i].Stats()
		if st.Rollbacks == 0 {
			t.Fatalf("peer %d never rolled back despite 4-frame latency", i)
		}
		if st.MaxRollbackDepth > 8 {
			t.Fatalf("peer %d rolled back %d frames, window is 8", i, st.MaxRollbackDepth)
		}
	}
	p.requireAgreement(8)
}

func TestPredictionWindowStallsAtEightFrames(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 2, Delay: [2]time.Duration{time.Second, time.Second}}})
	p.run(1500 * time.Millisecond)
	// Nothing has arrived yet; each peer must stop after 8 predicted frames.
	for i := 0; i < 2; i++ {
		if f := p.s[i].Frame(); f != 8 {
			t.Fatalf("peer %d frame %d, want 8 (stalled at the window)", i, f)
		}
		if p.s[i].Stats().StallTicks == 0 {
			t.Fatalf("peer %d reports no stall ticks", i)
		}
	}
}

func TestStallTimeoutReportsDisconnect(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 3}})
	p.run(time.Second)
	// Peer 1 goes silent (e.g. hidden tab): only peer 0 keeps ticking.
	start := p.now
	var lastOutcome Outcome
	for p.now < start+7*time.Second && p.s[0].Err() == nil {
		p.now += frameDur
		lastOutcome = p.tickOnly(0)
	}
	if !errors.Is(p.s[0].Err(), ErrDisconnect) {
		t.Fatalf("err = %v (outcome %v), want ErrDisconnect", p.s[0].Err(), lastOutcome)
	}
	if d := p.now - start; d < 5*time.Second || d > 5*time.Second+time.Second {
		t.Fatalf("disconnect after %v, want about 5s", d)
	}
	if p.s[0].Status() != StatusEnded {
		t.Fatal("status not ended")
	}
}

func (p *pair) tickOnly(i int) Outcome {
	p.net.SetNow(p.now)
	return p.s[i].Advance(p.now, 0)
}

func TestTimeSyncBoundsFrameDrift(t *testing.T) {
	// Peer 1's clock runs 4% fast; without sync it would gain 120 frames in 50 s.
	p := newPair(t, pairOpts{
		net:    NetConfig{Seed: 4, Delay: [2]time.Duration{2 * frameDur, 2 * frameDur}},
		period: [2]time.Duration{frameDur, frameDur * 100 / 104},
	})
	maxDiff := int64(0)
	for k := 0; k < 50; k++ {
		p.run(time.Second)
		d := p.s[1].Frame() - p.s[0].Frame()
		if d < 0 {
			d = -d
		}
		if k > 5 && d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff > 4 {
		t.Fatalf("frame drift reached %d frames", maxDiff)
	}
	if p.s[1].Stats().WaitTicks == 0 {
		t.Fatal("fast peer never waited")
	}
	p.requireAgreement(50)
}

func TestWaitsAreSpreadNotBursty(t *testing.T) {
	p := newPair(t, pairOpts{
		net:    NetConfig{Seed: 5},
		period: [2]time.Duration{frameDur, frameDur * 100 / 104},
	})
	run := 0
	maxRun := 0
	for k := 0; k < 3000; k++ {
		p.now += frameDur * 100 / 104
		p.net.SetNow(p.now)
		o := p.tick(1)
		if o == OutcomeWaited {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
		if k%26 == 0 { // peer 0 at its slower pace
			p.tick(0)
		}
	}
	if maxRun > 1 {
		t.Fatalf("consecutive waits: %d", maxRun)
	}
}

func TestRTTFromEchoedTimestamp(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 6, Delay: [2]time.Duration{50 * time.Millisecond, 30 * time.Millisecond}}})
	p.run(3 * time.Second)
	for i := 0; i < 2; i++ {
		rtt := p.s[i].RTT()
		if rtt < 70*time.Millisecond || rtt > 100*time.Millisecond {
			t.Fatalf("peer %d rtt = %v, want about 80ms", i, rtt)
		}
	}
}

func TestPacketsStayUnder1200BytesAndRedundantInputsAreIncluded(t *testing.T) {
	var maxLen int
	var tr [2]Transport
	net := NewLossyNet(NetConfig{Seed: 7, Delay: [2]time.Duration{6 * frameDur, 6 * frameDur}})
	for i := range tr {
		tr[i] = &spyTransport{Transport: net.Endpoint(i), max: &maxLen}
	}
	p := newPair(t, pairOpts{net: NetConfig{Seed: 7}, mutate: func(i int, c *Config[pad]) { c.Transport = tr[i] }})
	p.net = net
	p.run(4 * time.Second)
	if maxLen == 0 || maxLen > MaxPacketSize {
		t.Fatalf("largest packet %d bytes", maxLen)
	}
	// With a 12-frame round trip, packets carry the unacked tail.
	if maxLen < inputHdr+8 {
		t.Fatalf("largest packet %d bytes: no redundancy visible", maxLen)
	}
}

type spyTransport struct {
	Transport
	max *int
}

func (s *spyTransport) Send(p []byte) error {
	if len(p) > *s.max {
		*s.max = len(p)
	}
	return s.Transport.Send(p)
}

func TestIdentityMismatchRefusesToStart(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 8}, mutate: func(i int, c *Config[pad]) { c.Identity[0] = byte(i) }})
	p.run(time.Second)
	for i := 0; i < 2; i++ {
		if !errors.Is(p.s[i].Err(), ErrIdentityMismatch) {
			t.Fatalf("peer %d err = %v", i, p.s[i].Err())
		}
		if p.s[i].Frame() != 0 {
			t.Fatalf("peer %d started a match", i)
		}
	}
}

func TestAutoInputDelayFromHandshakeRTT(t *testing.T) {
	d := 125 * time.Millisecond
	p := newPair(t, pairOpts{net: NetConfig{Seed: 9, Delay: [2]time.Duration{d, d}}, delay: [2]int{AutoDelay, AutoDelay}})
	p.run(2 * time.Second)
	for i := 0; i < 2; i++ {
		if got := p.s[i].InputDelay(); got != 3 {
			t.Fatalf("peer %d delay = %d, want 3 for 250ms rtt", i, got)
		}
	}
	p.requireAgreement(3)
}

func TestSuggestDelay(t *testing.T) {
	cases := []struct {
		rtt  time.Duration
		want int
	}{{0, 0}, {50 * time.Millisecond, 0}, {120 * time.Millisecond, 0}, {200 * time.Millisecond, 2}, {250 * time.Millisecond, 3}, {400 * time.Millisecond, 3}}
	for _, c := range cases {
		if got := SuggestDelay(c.rtt, frameDur, 4, 3); got != c.want {
			t.Errorf("SuggestDelay(%v) = %d, want %d", c.rtt, got, c.want)
		}
	}
}

func TestDuplicateEntriesAreIgnoredAndConflictsCounted(t *testing.T) {
	// Heavy reordering and a long delay make every packet carry many
	// entries the receiver already holds.
	p := newPair(t, pairOpts{net: NetConfig{Seed: 10, Delay: [2]time.Duration{5 * frameDur, 5 * frameDur}, Reorder: 0.3}})
	p.run(3 * time.Second)
	st := p.s[0].Stats()
	if st.DuplicateEntries == 0 {
		t.Fatal("no duplicate entries seen")
	}
	if st.ConflictingEntries != 0 {
		t.Fatalf("honest peers sent %d conflicting duplicates", st.ConflictingEntries)
	}
	p.requireAgreement(3)
}

func TestNoPauseMessage(t *testing.T) {
	// The wire has no pause packet; a packet of an unknown type is ignored.
	p := newPair(t, pairOpts{net: NetConfig{Seed: 12}})
	p.run(500 * time.Millisecond)
	tr := p.net.Endpoint(1)
	junk := make([]byte, 64)
	junk[0], junk[1] = protoVersion, 9 // would-be "pause"
	before := p.s[0].Frame()
	for i := 0; i < 5; i++ {
		_ = tr.Send(junk)
	}
	p.run(500 * time.Millisecond)
	if p.s[0].Frame() <= before {
		t.Fatal("session paused on an unknown packet")
	}
	if p.s[0].Stats().BadPackets == 0 {
		t.Fatal("unknown packet not counted")
	}
	// A peer that stops sending stalls the other one; nothing pauses it.
	last := p.s[0].Frame()
	for k := 0; k < 120; k++ {
		p.now += frameDur
		p.tickOnly(0)
	}
	if got := p.s[0].Frame(); got > last+8 {
		t.Fatalf("peer 0 ran %d frames past a silent peer", got-last)
	}
}
