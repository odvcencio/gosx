package netplay

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// runHook is rig.run with hooks around each relay Tick.
func (r *rig) runHook(d time.Duration, before, after func()) {
	end := r.now + d
	for r.now < end {
		k := 0
		for j := 1; j < 3; j++ {
			if r.next[j] < r.next[k] {
				k = j
			}
		}
		r.now = r.next[k]
		r.setNow()
		switch k {
		case 2:
			r.next[2] += 4 * time.Millisecond
			if before != nil {
				before()
			}
			r.relay.Tick(r.now)
			if after != nil {
				after()
			}
		default:
			r.next[k] += frameDur
			s := r.s[k]
			var in pad
			if f := s.Frame(); f < int64(len(r.script)) {
				in = r.script[f][k]
			}
			r.outcomes[k] = append(r.outcomes[k], s.Advance(r.now, in))
		}
	}
}

// forged builds an input packet with explicit ack and entries.
func forged(tag uint64, ack uint32, start int64, entries []byte) []byte {
	b := make([]byte, inputHdr+len(entries))
	putHeader(b, ptInput, tag, &header{typ: ptInput, delay: 0, seq: 1})
	binary.LittleEndian.PutUint32(b[offAck:], ack)
	binary.LittleEndian.PutUint32(b[offStart:], uint32(start))
	binary.LittleEndian.PutUint16(b[offCount:], uint16(len(entries)))
	copy(b[inputHdr:], entries)
	return b
}

func d20() [2]time.Duration { return [2]time.Duration{20 * time.Millisecond, 20 * time.Millisecond} }

// Honest peers on 200-400 ms links with loss, jitter and reordering are never
// flagged and never violate.
func TestHonestHighLatencyPlayersAreNeverFlagged(t *testing.T) {
	for _, rtt := range []time.Duration{200, 300, 400} {
		for _, loss := range []float64{0.05, 0.20} {
			for _, asym := range []bool{false, true} {
				for seed := uint64(1); seed <= 2; seed++ {
					ow := rtt * time.Millisecond / 2
					hi := NetConfig{Seed: seed, Delay: [2]time.Duration{ow, ow}, Jitter: 20 * time.Millisecond, Loss: loss, Reorder: 0.05}
					lo := hi
					lo.Seed = seed + 100
					if asym {
						lo = NetConfig{Seed: seed + 100, Delay: [2]time.Duration{5 * time.Millisecond, 5 * time.Millisecond}}
					}
					r := newRig(t, rigOpts{links: [2]NetConfig{hi, lo}})
					r.run(20 * time.Second)
					rep := r.relay.Report()
					for i, p := range rep.Players {
						if p.AckLagFlagged || p.Violations != 0 || p.CrawlFlagged || p.SlowGaps != 0 {
							t.Errorf("rtt %v loss %.2f asym %v seed %d: honest player %d flagged: acklag %v (max %d) violations %d %v crawl %v slow gaps %d (max gap %v)",
								rtt, loss, asym, seed, i, p.AckLagFlagged, p.MaxAckLag, p.Violations, p.ViolationCounts, p.CrawlFlagged, p.SlowGaps, p.MaxLogGap)
						}
					}
					for i := 0; i < 2; i++ {
						if r.s[i].Err() != nil {
							t.Errorf("rtt %v loss %.2f seed %d: peer %d ended: %v", rtt, loss, seed, i, r.s[i].Err())
						}
						if st := r.s[i].Stats(); st.FutureInputs != 0 || st.ImpossibleAcks != 0 {
							t.Errorf("peer %d dropped honest inputs: %+v", i, st)
						}
					}
				}
			}
		}
	}
}

// An impossible ack is a violation, is never trusted, and does not stall the
// honest peer.
func TestImpossibleAckIsFlaggedAndDoesNotStallTheHonestPeer(t *testing.T) {
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d20()}, {Seed: 2, Delay: d20()}}})
	r.run(2 * time.Second)
	tag := idTag(&[32]byte{})
	before := r.relay.Report()
	_ = r.nets[1].Endpoint(0).Send(forged(tag, 0xFFFFFF00, 0, nil))
	r.run(4 * time.Second)
	rep := r.relay.Report()
	p0, p1 := rep.Players[0], rep.Players[1]
	if p1.ImpossibleAcks != 1 || p1.ViolationCounts[ViolationImpossibleAck] != 1 || p1.FirstViolation != ViolationImpossibleAck ||
		p1.FirstViolationFrame != 0xFFFFFF00 {
		t.Fatalf("impossible ack not recorded: %+v", p1)
	}
	if r.relay.ack[1] > r.relay.logN[0] || p1.AckedFrame > r.relay.logN[0] || p1.ForwardedAck > r.relay.logN[0] {
		t.Fatalf("ack above the frames logged: relay ack %d acked %d forwarded %d, logN[0] %d", r.relay.ack[1], p1.AckedFrame, p1.ForwardedAck, r.relay.logN[0])
	}
	if p0.Violations != 0 || p0.FramesLogged < before.Players[0].FramesLogged+200 || p1.FramesLogged < before.Players[1].FramesLogged+200 {
		t.Fatalf("honest peer stalled: before %+v after %+v", before.Players[0], p0)
	}
	for i := 0; i < 2; i++ {
		if r.s[i].Err() != nil {
			t.Fatalf("peer %d: %v", i, r.s[i].Err())
		}
	}
}

// A burst of early inputs from one player is rejected, so the other player
// is not flagged for ack lag.
func TestInputFloodIsRejectedAndDoesNotFlagTheHonestPeer(t *testing.T) {
	d := [2]time.Duration{40 * time.Millisecond, 40 * time.Millisecond}
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d}, {Seed: 2, Delay: d}}})
	r.run(2 * time.Second)
	tag := idTag(&[32]byte{})
	start := r.relay.logN[1]
	ents := make([]byte, 60)
	for i := range ents {
		ents[i] = byte(r.script[start+int64(i)][1])
	}
	_ = r.nets[1].Endpoint(0).Send(forged(tag, uint32(r.relay.ack[1]), start, ents))
	r.run(3 * time.Second)
	rep := r.relay.Report()
	p0, p1 := rep.Players[0], rep.Players[1]
	if p0.AckLagFlagged || p0.Violations != 0 {
		t.Fatalf("honest player flagged: %+v", p0)
	}
	if p1.ViolationCounts[ViolationFuture] != 1 || p1.FirstViolationFrame != start {
		t.Fatalf("flood not recorded: %+v", p1)
	}
	// Even a flood that fits the bound is not counted against the receiver until
	// it is a second old.
	r2 := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d}, {Seed: 2, Delay: d}},
		relay: func(c *RelayConfig[pad]) { c.PredictionWindow = 100; c.AckLagSlack = AckLagSlackNone }})
	r2.run(time.Second)
	if r2.relay.AckLagLimit() != 100+3 {
		t.Fatalf("AckLagSlackNone: limit %d", r2.relay.AckLagLimit())
	}
	start = r2.relay.logN[1]
	ents = ents[:60]
	for i := range ents {
		ents[i] = byte(r2.script[start+int64(i)][1])
	}
	_ = r2.nets[1].Endpoint(0).Send(forged(tag, uint32(r2.relay.ack[1]), start, ents))
	r2.run(3 * time.Second)
	if p := r2.relay.Report().Players[0]; p.AckLagFlagged {
		t.Fatalf("honest player flagged by an early burst: %+v", p)
	}
}

// Equivocation: the relay forwards what it logged, so the honest peer is
// never given a conflicting copy and the conflicting sender is the one on
// record.
func TestEquivocationIsAttributedToTheSender(t *testing.T) {
	l0 := NetConfig{Seed: 1, Delay: d20()}
	l1 := NetConfig{Seed: 2, Delay: [2]time.Duration{0, 20 * time.Millisecond}}
	r := newRig(t, rigOpts{links: [2]NetConfig{l0, l1}})
	r.run(2 * time.Second)
	tag := idTag(&[32]byte{})
	f := r.relay.logN[1] + 3
	r.nets[0].SetLoss(1)
	phase := 0
	r.runHook(2*time.Second, nil, func() {
		if phase == 0 && r.relay.logN[1] > f+1 {
			phase = 1
			from := r.s[0].rc
			var ents []byte
			for g := from; g < f; g++ {
				ents = append(ents, r.relay.log[1][g])
			}
			ents = append(ents, byte(r.script[f][1]^padLeft))
			_ = r.nets[1].Endpoint(0).Send(forged(tag, uint32(r.relay.ack[1]), from, ents))
			r.nets[0].SetLoss(0)
			r.relay.Tick(r.now)
		}
	})
	r.run(3 * time.Second)
	if phase != 1 {
		t.Fatal("scenario did not run")
	}
	rep := r.relay.Report()
	p0, p1 := rep.Players[0], rep.Players[1]
	if p0.Desyncs != 0 || len(rep.DesyncFrames) != 0 || rep.Ended {
		t.Fatalf("honest peer was desynced by a forwarded conflicting copy: %v %+v", rep.DesyncFrames, p0)
	}
	if p1.ConflictingEntries == 0 || p1.ViolationCounts[ViolationConflict] == 0 || p0.ConflictingEntries != 0 {
		t.Fatalf("conflict not attributed to the sender: p0 %+v p1 %+v", p0, p1)
	}
}

// A normal end survives a loss burst longer than the immediate copies and the
// first repeats, because repeats are paced by relay time.
func TestEndSurvivesALossBurstOf300ms(t *testing.T) {
	for _, burst := range []time.Duration{60 * time.Millisecond, 300 * time.Millisecond, 900 * time.Millisecond} {
		r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d20()}, {Seed: 2, Delay: d20()}}})
		r.run(2 * time.Second)
		r.nets[0].SetLoss(1)
		r.relay.Finish()
		r.run(burst) // the relay keeps ticking
		r.nets[0].SetLoss(0)
		r.run(6 * time.Second)
		for i := 0; i < 2; i++ {
			if !r.s[i].MatchOver() || errors.Is(r.s[i].Err(), ErrDisconnect) {
				t.Fatalf("burst %v: peer %d err %v", burst, i, r.s[i].Err())
			}
		}
		if r.relay.Draining() {
			t.Fatalf("burst %v: relay still draining after 6 s", burst)
		}
	}
}

// Bad tag, bad length and a flood of high acks in bad packets are counted
// against the sender and change no ack fact.
func TestBadTagAckFloodChangesNothing(t *testing.T) {
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d20()}, {Seed: 2, Delay: d20()}}})
	r.run(time.Second)
	tag := idTag(&[32]byte{})
	before := r.relay.Report().Players[1]
	for i := 0; i < 500; i++ {
		_ = r.nets[1].Endpoint(0).Send(forged(tag^1, 0xFFFFFFF0, 0, []byte{1, 2, 3})) // wrong tag
		short := forged(tag, 0xFFFFFFF0, 0, []byte{1, 2, 3})
		_ = r.nets[1].Endpoint(0).Send(short[:len(short)-1]) // wrong length
	}
	r.run(2 * time.Second)
	rep := r.relay.Report()
	p0, p1 := rep.Players[0], rep.Players[1]
	if p1.ViolationCounts[ViolationBadPacket] < 900 || p1.FirstViolation != ViolationBadPacket {
		t.Fatalf("bad packets not counted: %+v", p1)
	}
	if p1.ImpossibleAcks != 0 || p1.AckedFrame > r.relay.logN[0] || p1.ForwardedAck > r.relay.logN[0] || p1.AckedFrame < before.AckedFrame {
		t.Fatalf("acks changed by bad packets: %+v", p1)
	}
	if p0.Violations != 0 || p0.FramesLogged < 150 {
		t.Fatalf("honest player hurt: %+v", p0)
	}
	for i := 0; i < 2; i++ {
		if r.s[i].Err() != nil {
			t.Fatalf("peer %d: %v", i, r.s[i].Err())
		}
	}
}

// A packet 552 frames ahead is rejected whole and never reaches the honest
// peer's ring.
func TestFarFuturePacketIsRejected(t *testing.T) {
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d20()}, {Seed: 2, Delay: d20()}}})
	r.run(2 * time.Second)
	tag := idTag(&[32]byte{})
	n := r.relay.logN[1]
	ents := make([]byte, 552)
	_ = r.nets[1].Endpoint(0).Send(forged(tag, 0, n, ents))
	r.run(2 * time.Second)
	rep := r.relay.Report()
	if rep.Players[1].ViolationCounts[ViolationFuture] != 1 || rep.Players[1].FirstViolationFrame != n {
		t.Fatalf("far-future packet not recorded: %+v", rep.Players[1])
	}
	if r.relay.logN[1] < n || r.relay.logN[1] > n+200 {
		t.Fatalf("log grew from %d to %d: entries from the packet were logged", n, r.relay.logN[1])
	}
	for i := 0; i < 2; i++ {
		if r.s[i].Err() != nil || r.s[i].Stats().FutureInputs != 0 || len(rep.DesyncFrames) != 0 {
			t.Fatalf("peer %d: %v %+v", i, r.s[i].Err(), r.s[i].Stats())
		}
	}
}

// A sender that sends each frame once and ignores the other's acks cannot
// freeze the honest peer behind a lost forwarded packet: the relay resends
// from its own log.
func TestNeverResendingPeerCannotStrandTheHonestPeer(t *testing.T) {
	lossy := NetConfig{Seed: 1, Delay: d20(), Loss: 0.30, Jitter: 5 * time.Millisecond, Reorder: 0.05}
	r := newRig(t, rigOpts{links: [2]NetConfig{lossy, {Seed: 2, Delay: d20()}}, delay: [2]int{2, 2}})
	tag := idTag(&[32]byte{})
	r.next[1] = 1 << 62 // player 1's session never runs; the cheater below speaks for it
	hello := make([]byte, helloLen)
	putHeader(hello, ptHello, tag, &header{typ: ptHello, delay: 2, seq: 1})
	hello[offHelloPlayer] = 1
	_ = r.nets[1].Endpoint(0).Send(hello)
	var cf int64 // the next frame the cheater sends, once
	var nextSend time.Duration
	r.runHook(20*time.Second, func() {
		if r.now < nextSend {
			return
		}
		nextSend += frameDur
		if cf >= r.relay.logN[0]+4 || cf >= int64(len(r.script)) {
			return // the cheater keeps pace with player 0
		}
		pk := forged(tag, uint32(r.relay.logN[0]), cf, []byte{byte(r.script[cf][1])})
		binary.LittleEndian.PutUint32(pk[offSeq:], uint32(cf+2))
		_ = r.nets[1].Endpoint(0).Send(pk)
		cf++
	}, nil)
	rep := r.relay.Report()
	if r.s[0].Err() != nil {
		t.Fatalf("honest peer ended: %v", r.s[0].Err())
	}
	if r.s[0].Frame() < 600 || cf-r.s[0].Frame() > 12 {
		t.Fatalf("honest peer reached only frame %d (cheater sent %d): stranded behind a lost packet", r.s[0].Frame(), cf)
	}
	if rep.Players[1].ViolationCounts[ViolationNoResend] == 0 || rep.Players[0].Violations != 0 {
		t.Fatalf("violations: p0 %+v p1 %+v", rep.Players[0], rep.Players[1])
	}
}

// A client that crawls (one tick every 2 s) never stalls the other player
// long enough to time out the stall, but it collects slow gaps.
func TestCrawlingClientIsFlagged(t *testing.T) {
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1, Delay: d20()}, {Seed: 2, Delay: d20()}}})
	r.run(2 * time.Second)
	if p := r.relay.Report().Players[1]; p.CrawlFlagged || p.SlowGaps != 0 {
		t.Fatalf("flagged before crawling: %+v", p)
	}
	for r.now < 24*time.Second {
		r.next[1] += 2*time.Second - frameDur // one tick every 2 s
		r.run(2 * time.Second)
		if r.s[0].Err() != nil {
			break
		}
	}
	rep := r.relay.Report()
	p0, p1 := rep.Players[0], rep.Players[1]
	if !p1.CrawlFlagged || p1.SlowGaps < 3 || p1.MaxLogGap < 1500*time.Millisecond {
		t.Fatalf("crawler not flagged: %+v", p1)
	}
	if p0.CrawlFlagged || p0.SlowGaps != 0 {
		t.Fatalf("honest waiting player flagged: %+v", p0)
	}
}

// A player whose scene loads late: the handshake outlives the stall timeout
// and the session still starts.
func TestLateHandshakeDoesNotRestartTheSession(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 5, Delay: [2]time.Duration{20 * time.Millisecond, 20 * time.Millisecond}}, offset: 12 * time.Second})
	p.run(20 * time.Second)
	for i := 0; i < 2; i++ {
		if p.s[i].Err() != nil || p.s[i].Frame() < 300 {
			t.Fatalf("peer %d: err %v frame %d", i, p.s[i].Err(), p.s[i].Frame())
		}
	}
	// The handshake still has a limit.
	q := newPair(t, pairOpts{net: NetConfig{Seed: 5}, offset: time.Hour, mutate: func(_ int, c *Config[pad]) { c.HandshakeTimeout = 2 * time.Second }})
	q.run(5 * time.Second)
	if !errors.Is(q.s[0].Err(), ErrDisconnect) {
		t.Fatalf("handshake never timed out: %v", q.s[0].Err())
	}
}

// A restarted session must not take the peer's echo of the previous session's
// timestamps as a round-trip sample.
func TestRestartedSessionIgnoresStaleEchoForRTT(t *testing.T) {
	d := 10 * time.Millisecond
	p := newPair(t, pairOpts{net: NetConfig{Seed: 7, Delay: [2]time.Duration{d, d}}, delay: [2]int{AutoDelay, AutoDelay}})
	p.run(1500 * time.Millisecond)
	// Peer 0 restarts; peer 1 keeps echoing timestamps of the old session.
	p.g[0] = newFightGame()
	s, err := NewSession(Config[pad]{Game: p.g[0], Codec: padCodec{}, Transport: p.net.Endpoint(0), LocalPlayer: 0, InputDelay: AutoDelay})
	if err != nil {
		t.Fatal(err)
	}
	p.s[0] = s
	p.run(time.Second)
	if st := s.Stats(); st.StaleEchoes == 0 {
		t.Fatalf("no stale echo seen: %+v", st)
	}
	if s.RTT() > 100*time.Millisecond || s.InputDelay() != 0 {
		t.Fatalf("rtt %v delay %d after restart, want about 20 ms and delay 0", s.RTT(), s.InputDelay())
	}
}

// Session-side bounds: a peer packet far ahead is dropped and counted, and an
// ack for frames not yet produced is ignored.
func TestSessionDropsFutureInputsAndImpossibleAcks(t *testing.T) {
	p := newPair(t, pairOpts{net: NetConfig{Seed: 3, Delay: d20()}, delay: [2]int{2, 2}})
	p.run(time.Second)
	s0 := p.s[0]
	rc, next, ack := s0.rc, s0.localNext, s0.peerAck
	tag := idTag(&[32]byte{})
	pk := forged(tag, 0xFFFFFF00, rc, make([]byte, 552))
	binary.LittleEndian.PutUint32(pk[offSeq:], 1<<20)
	_ = p.net.Endpoint(1).Send(pk)
	p.run(100 * time.Millisecond)
	st := s0.Stats()
	if st.FutureInputs == 0 || st.ImpossibleAcks == 0 {
		t.Fatalf("not counted: %+v", st)
	}
	if s0.rc > rc+int64(8+3+4)+30 || s0.peerAck > s0.localNext || s0.peerAck < ack || s0.localNext < next {
		t.Fatalf("session state moved: rc %d->%d peerAck %d->%d localNext %d->%d", rc, s0.rc, ack, s0.peerAck, next, s0.localNext)
	}
	if s0.Err() != nil {
		t.Fatal(s0.Err())
	}
}

// handshakePeer is a hand-driven peer that speaks only hello packets, to test
// the auto-delay handshake against a peer that cheats on echoes.
type handshakePeer struct {
	ep  Transport
	seq uint32
	// echo decides what the peer puts in its echo fields for a hello it got
	// from the session: ok=false sends no echo flag at all.
	echo func(n int, sendUs uint32) (echoUs uint32, ok bool)
	got  int
}

func (h *handshakePeer) step(now time.Duration) {
	buf := make([]byte, MaxPacketSize)
	var last *header
	for {
		n, ok := h.ep.Recv(buf)
		if !ok {
			break
		}
		if n == helloLen && buf[1] == ptHello {
			hd := getHeader(buf[:n])
			last = &hd
			h.got++
		}
	}
	h.seq++
	hd := header{typ: ptHello, seq: h.seq, sendUs: micros(now), delay: 0}
	if last != nil {
		if echoUs, ok := h.echo(h.got, last.sendUs); ok {
			hd.hasEcho, hd.echoUs = true, echoUs
		}
	}
	b := make([]byte, helloLen)
	putHeader(b, ptHello, idTag(&[32]byte{}), &hd)
	b[offHelloPlayer] = 1
	_ = h.ep.Send(b)
}

func runHandshake(t *testing.T, echo func(n int, sendUs uint32) (uint32, bool), d time.Duration) *Session[pad] {
	t.Helper()
	net := NewLossyNet(NetConfig{Seed: 1, Delay: [2]time.Duration{5 * time.Millisecond, 5 * time.Millisecond}})
	s, err := NewSession(Config[pad]{Game: newFightGame(), Codec: padCodec{}, Transport: net.Endpoint(0), InputDelay: AutoDelay})
	if err != nil {
		t.Fatal(err)
	}
	peer := &handshakePeer{ep: net.Endpoint(1), echo: echo}
	for now := time.Duration(0); now < d; now += frameDur {
		net.SetNow(now)
		s.Advance(now, 0)
		peer.step(now)
	}
	return s
}

// A peer that never sets the echo flag cannot hold an auto-delay session in
// the handshake: after RTTWait it uses the fallback delay.
func TestPeerThatNeverEchoesGetsTheFallbackDelay(t *testing.T) {
	s := runHandshake(t, func(int, uint32) (uint32, bool) { return 0, false }, 3*time.Second)
	if s.Status() != StatusRunning {
		t.Fatalf("session still %v after 3 s: err %v", s.Status(), s.Err())
	}
	if s.InputDelay() != 1 || s.Stats().DelayFallbacks != 1 {
		t.Fatalf("delay %d fallbacks %d, want the fallback delay 1", s.InputDelay(), s.Stats().DelayFallbacks)
	}
	// Not before RTTWait.
	early := runHandshake(t, func(int, uint32) (uint32, bool) { return 0, false }, 600*time.Millisecond)
	if early.Status() != StatusHandshake {
		t.Fatal("started before RTTWait ran out")
	}
}

// A peer that subtracts time from the timestamps it echoes cannot force the
// maximum delay: samples above the cap are ignored and the fallback applies,
// and an honest echo among fakes sets the delay from the minimum.
func TestPeerThatFakesEchoesCannotForceTheMaximumDelay(t *testing.T) {
	fakeAll := func(n int, sendUs uint32) (uint32, bool) { return sendUs - 700_000, true } // 0.7 s too early
	s := runHandshake(t, fakeAll, 3*time.Second)
	if s.Status() != StatusRunning || s.InputDelay() != 1 {
		t.Fatalf("status %v delay %d, want running with the fallback delay 1", s.Status(), s.InputDelay())
	}
	if st := s.Stats(); st.RTTIgnored == 0 || st.StaleEchoes == 0 || st.DelayFallbacks != 1 {
		t.Fatalf("fake samples not reported: %+v", st)
	}
	mixed := func(n int, sendUs uint32) (uint32, bool) {
		if n%3 == 0 {
			return sendUs, true // honest
		}
		return sendUs - 700_000, true
	}
	m := runHandshake(t, mixed, 3*time.Second)
	if m.Status() != StatusRunning || m.InputDelay() != 0 || m.Stats().RTTIgnored+m.Stats().StaleEchoes == 0 || m.Stats().DelayFallbacks != 0 {
		t.Fatalf("mixed: status %v delay %d stats %+v, want delay 0 from the honest samples", m.Status(), m.InputDelay(), m.Stats())
	}
}
