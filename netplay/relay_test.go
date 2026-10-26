package netplay

import (
	"errors"
	"testing"
	"time"
)

// rig is p0 <-> relay <-> p1 over two separate simulated links.
type rig struct {
	t        *testing.T
	nets     [2]*LossyNet
	s        [2]*Session[pad]
	g        [2]*fightGame
	relay    *Relay[pad]
	spec     *Spectator[pad]
	specNet  *LossyNet
	sums     [2]map[int64]uint64
	script   [][2]pad
	now      time.Duration
	next     [3]time.Duration // p0, p1, relay
	outcomes [2][]Outcome
	mutate   func(i int, c *Config[pad])
}

type rigOpts struct {
	links  [2]NetConfig
	delay  [2]int
	faults [2][]int32
	relay  func(c *RelayConfig[pad])
	spec   bool
	id     [32]byte
}

func newRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	r := &rig{t: t}
	r.script = randomInputs(o.links[0].Seed+77, 6000)
	rc := RelayConfig[pad]{Identity: o.id, NewGame: func() Game[pad] { return newFightGame() }, Codec: padCodec{}}
	for i := 0; i < 2; i++ {
		r.nets[i] = NewLossyNet(o.links[i])
		r.g[i] = newFightGame()
		r.g[i].faultFrames = o.faults[i]
		r.sums[i] = map[int64]uint64{}
		i := i
		s, err := NewSession(Config[pad]{
			Game: r.g[i], Codec: padCodec{}, Transport: r.nets[i].Endpoint(0), LocalPlayer: i,
			InputDelay: o.delay[i], Relay: true, Identity: o.id,
			OnChecksum: func(f int64, c uint64) { r.sums[i][f] = c },
		})
		if err != nil {
			t.Fatal(err)
		}
		r.s[i] = s
		rc.Peers[i] = r.nets[i].Endpoint(1)
	}
	if o.spec {
		r.specNet = NewLossyNet(NetConfig{Seed: 5})
		r.spec = NewSpectator(SpectatorConfig[pad]{Game: newFightGame(), Codec: padCodec{}, Transport: r.specNet.Endpoint(0)})
	}
	if o.relay != nil {
		o.relay(&rc)
	}
	var err error
	if r.relay, err = NewRelay(rc); err != nil {
		t.Fatal(err)
	}
	if o.spec {
		r.relay.AddSpectator(r.specNet.Endpoint(1))
	}
	return r
}

func (r *rig) setNow() {
	for _, n := range r.nets {
		n.SetNow(r.now)
	}
	if r.specNet != nil {
		r.specNet.SetNow(r.now)
	}
}

func (r *rig) run(d time.Duration) {
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
			r.relay.Tick(r.now)
		default:
			r.next[k] += frameDur
			s := r.s[k]
			var in pad
			if f := s.Frame(); f < int64(len(r.script)) {
				in = r.script[f][k]
			}
			r.outcomes[k] = append(r.outcomes[k], s.Advance(r.now, in))
			if r.spec != nil && k == 0 {
				r.spec.Poll(r.now)
			}
		}
	}
}

func (r *rig) agreeAfter(from int64, minCommon int) {
	r.t.Helper()
	common := 0
	for f, a := range r.sums[0] {
		if f < from {
			continue
		}
		if b, ok := r.sums[1][f]; ok {
			common++
			if a != b {
				r.t.Fatalf("checksum differs at frame %d", f)
			}
		}
	}
	if common < minCommon {
		r.t.Fatalf("%d common checksums after frame %d, want >= %d", common, from, minCommon)
	}
}

var link60 = NetConfig{Seed: 1, Delay: [2]time.Duration{40 * time.Millisecond, 40 * time.Millisecond}, Jitter: 5 * time.Millisecond, Loss: 0.03}

func TestRelayForwardsLogsAndVerifies(t *testing.T) {
	l1 := link60
	l1.Seed = 2
	r := newRig(t, rigOpts{links: [2]NetConfig{link60, l1}})
	r.run(10 * time.Second)
	rep := r.relay.Report()
	if rep.Ended || len(rep.DesyncFrames) != 0 || rep.Resyncs != 0 {
		t.Fatalf("clean match reported %+v", rep)
	}
	if rep.VerifiedFrames < 500 || rep.ConfirmedFrames < rep.VerifiedFrames {
		t.Fatalf("relay verified %d of %d confirmed", rep.VerifiedFrames, rep.ConfirmedFrames)
	}
	r.agreeAfter(0, 15)
	for i := 0; i < 2; i++ {
		if r.s[i].Err() != nil {
			t.Fatal(r.s[i].Err())
		}
		if rep.Players[i].Flagged {
			t.Fatalf("honest player %d flagged: %+v", i, rep.Players[i])
		}
	}
}

// inputPacket builds an Input packet carrying frames [start, start+n) of
// constant value v.
func inputPacket(tag uint64, delay byte, seq uint32, start, n int, v byte) []byte {
	b := make([]byte, inputHdr+n)
	putHeader(b, ptInput, tag, &header{typ: ptInput, delay: delay, seq: seq})
	b[offStart] = byte(start)
	b[offCount] = byte(n)
	for i := 0; i < n; i++ {
		b[inputHdr+i] = v
	}
	return b
}

func TestRelayDropsOnlyDuplicateEntriesNeverWholePackets(t *testing.T) {
	r := newRig(t, rigOpts{})
	tag := idTag(&[32]byte{})
	tx := r.nets[0].Endpoint(0) // player 0's side
	rx := r.nets[1].Endpoint(0) // player 1's side
	_ = tx.Send(inputPacket(tag, 0, 1, 0, 5, 7))
	_ = tx.Send(inputPacket(tag, 0, 2, 3, 5, 7)) // overlaps frames 3,4
	_ = tx.Send(inputPacket(tag, 0, 3, 0, 8, 7)) // entirely duplicate entries
	_ = tx.Send(inputPacket(tag, 0, 4, 0, 8, 9)) // conflicting duplicates
	r.relay.Tick(time.Millisecond)
	rep := r.relay.Report()
	p := rep.Players[0]
	if p.FramesLogged != 8 {
		t.Fatalf("logged %d frames, want 8", p.FramesLogged)
	}
	if p.DuplicateEntries != 2+8+8 || p.ConflictingEntries != 8 {
		t.Fatalf("dups=%d conflicts=%d", p.DuplicateEntries, p.ConflictingEntries)
	}
	got := 0
	buf := make([]byte, MaxPacketSize)
	for {
		if _, ok := rx.Recv(buf); !ok {
			break
		}
		got++
	}
	if got != 4 {
		t.Fatalf("player 1 received %d packets, want all 4 forwarded whole", got)
	}
}

func TestRelayDoesNotForwardOtherIdentity(t *testing.T) {
	r := newRig(t, rigOpts{})
	var other [32]byte
	other[0] = 9
	hello := make([]byte, helloLen)
	putHeader(hello, ptHello, idTag(&other), &header{typ: ptHello})
	copy(hello[offHelloID:], other[:])
	_ = r.nets[0].Endpoint(0).Send(hello)
	r.relay.Tick(time.Millisecond)
	if _, ok := r.nets[1].Endpoint(0).Recv(make([]byte, MaxPacketSize)); ok {
		t.Fatal("hello with another identity was forwarded")
	}
}

func TestForcedDesyncIsRepairedFromRelayBlob(t *testing.T) {
	l1 := link60
	l1.Seed = 2
	// Peer 1's build mis-simulates frame 100, as a platform quirk would.
	r := newRig(t, rigOpts{links: [2]NetConfig{link60, l1}, faults: [2][]int32{nil, {100}}})
	r.run(15 * time.Second)
	rep := r.relay.Report()
	if rep.Ended {
		t.Fatalf("match ended: %+v", rep)
	}
	if len(rep.DesyncFrames) != 1 || rep.Resyncs != 1 {
		t.Fatalf("desyncs %v resyncs %d, want one of each", rep.DesyncFrames, rep.Resyncs)
	}
	c := rep.DesyncFrames[0]
	if c <= 100 || c > 130 {
		t.Fatalf("desync detected at checksum frame %d, want the first one after 100", c)
	}
	if r.s[1].Stats().Resyncs != 1 || r.s[0].Stats().Resyncs != 0 {
		t.Fatalf("resyncs applied: p0 %d p1 %d", r.s[0].Stats().Resyncs, r.s[1].Stats().Resyncs)
	}
	for i := 0; i < 2; i++ {
		if r.s[i].Err() != nil {
			t.Fatalf("peer %d: %v", i, r.s[i].Err())
		}
	}
	// Catch-up budget: at most 30 frames per tick, and under one second.
	st := r.s[1].Stats()
	if st.MaxCatchUpPerTick > 30 || st.CatchUpTicks == 0 || st.CatchUpTicks > 60 {
		t.Fatalf("catch-up: %+v", st)
	}
	r.agreeAfter(c+60, 10)
	// And the peers keep playing: frames advance well past the repair.
	if r.s[1].Frame() < 700 {
		t.Fatalf("peer 1 at frame %d", r.s[1].Frame())
	}
}

func TestSecondDesyncEndsMatchWithReportAndReplay(t *testing.T) {
	l1 := link60
	l1.Seed = 2
	r := newRig(t, rigOpts{links: [2]NetConfig{link60, l1}, faults: [2][]int32{nil, {100, 500}}})
	r.run(20 * time.Second)
	rep := r.relay.Report()
	if !rep.Ended || rep.EndReason != EndDesync || len(rep.DesyncFrames) != 2 {
		t.Fatalf("report %+v", rep)
	}
	if rep.Replay == nil || rep.Replay.Frames < 500 || len(rep.Replay.Checks) == 0 {
		t.Fatalf("replay not attached: %+v", rep.Replay)
	}
	for i := 0; i < 2; i++ {
		if !errors.Is(r.s[i].Err(), ErrRemoteEnded) {
			t.Fatalf("peer %d err = %v", i, r.s[i].Err())
		}
	}
}

func TestRelaySimulationRespectsCatchUpBudget(t *testing.T) {
	r := newRig(t, rigOpts{})
	tag := idTag(&[32]byte{})
	// Both players deliver 200 frames in one burst.
	_ = r.nets[0].Endpoint(0).Send(inputPacket(tag, 0, 1, 0, 200, 1))
	_ = r.nets[1].Endpoint(0).Send(inputPacket(tag, 0, 1, 0, 200, 2))
	prev := int64(0)
	ticks := 0
	for r.relay.SimFrame() < 200 && ticks < 20 {
		r.now += time.Millisecond
		r.relay.Tick(r.now)
		ticks++
		if d := r.relay.SimFrame() - prev; d > 30 {
			t.Fatalf("relay simulated %d frames in one tick", d)
		}
		prev = r.relay.SimFrame()
	}
	if ticks != 7 {
		t.Fatalf("took %d ticks for 200 frames, want 7 at 30 per tick", ticks)
	}
}

func TestLateInputsAreFlaggedForTheLatePlayerOnly(t *testing.T) {
	// Player 1's packets get 25 ms later every second.
	slow := NetConfig{Seed: 2, DelayRamp: [2]time.Duration{25 * time.Millisecond, 0}}
	r := newRig(t, rigOpts{links: [2]NetConfig{{Seed: 1}, slow}})
	r.run(40 * time.Second)
	rep := r.relay.Report()
	if !rep.Players[1].Flagged || rep.Players[1].LateInputs < 10 {
		t.Fatalf("slow player not flagged: %+v", rep.Players[1])
	}
	if rep.Players[0].Flagged {
		t.Fatalf("stalled honest player flagged: %+v", rep.Players[0])
	}
}

func TestSpectatorFollowsAfterDelay(t *testing.T) {
	r := newRig(t, rigOpts{links: [2]NetConfig{link60, link60}, spec: true,
		relay: func(c *RelayConfig[pad]) { c.SpectatorDelay = 3 * time.Second }})
	r.run(2500 * time.Millisecond)
	if r.spec.Frame() != 0 {
		t.Fatalf("spectator saw frame %d before the delay", r.spec.Frame())
	}
	r.run(8 * time.Second)
	if r.spec.Frame() < 200 {
		t.Fatalf("spectator at frame %d", r.spec.Frame())
	}
	// It must be behind the live match and exactly reproduce its state.
	if r.spec.Frame() > r.s[0].ConfirmedFrame()-100 {
		t.Fatalf("spectator %d not delayed vs %d", r.spec.Frame(), r.s[0].ConfirmedFrame())
	}
	ref := newFightGame()
	runGame(ref, r.script[:0])
	buf := make([]pad, 2)
	for f := int64(0); f < r.spec.Frame(); f++ {
		buf[0], buf[1] = r.inputAt(0, f), r.inputAt(1, f)
		ref.Step(buf)
	}
	if Checksum(ref.Save(nil)) != r.spec.Checksum() {
		t.Fatal("spectator state differs from re-simulation of the logged inputs")
	}
}

// inputAt is the input the relay logged for player p at frame f.
func (r *rig) inputAt(p int, f int64) pad {
	return pad(r.relay.log[p][f])
}
