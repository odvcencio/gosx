package netplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

// RelayConfig configures a Relay.
type RelayConfig[I any] struct {
	// NewGame returns a fresh native copy of the simulation at frame 0.
	NewGame func() Game[I]
	Codec   Codec[I]
	// Identity is the simulation identity this relay verifies.
	Identity [32]byte
	// Peers are the relay's ends of the two player links.
	Peers [2]Transport

	ChecksumInterval int           // K, default 30
	FrameDuration    time.Duration // default 1/60 s
	// CatchUpPerTick bounds native frames simulated per Tick (default 30).
	CatchUpPerTick int
	// SpectatorDelay is how long a confirmed frame waits before it is
	// streamed to spectators (default 3 s).
	SpectatorDelay time.Duration
	// LateSlack and LateFlagCount tune the late-input flag: a player is
	// flagged once LateFlagCount inputs (default 10) arrive more than
	// LateSlack (default 2 frames) after their due time plus input delay.
	LateSlack     time.Duration
	LateFlagCount int
}

// PlayerReport is the relay's view of one player.
type PlayerReport struct {
	InputDelay         int
	FramesLogged       int
	LateInputs         int
	MaxLateness        time.Duration
	Flagged            bool
	DuplicateEntries   int
	ConflictingEntries int
}

// MatchReport summarizes a match from the relay's side.
type MatchReport struct {
	Identity        [32]byte
	ConfirmedFrames int64
	VerifiedFrames  int64 // frames the native copy has simulated
	Players         [2]PlayerReport
	DesyncFrames    []int64 // checksum frames where a peer disagreed with the relay
	Resyncs         int
	Ended           bool
	EndReason       uint8
	// Replay is attached when the match ended in a desync.
	Replay *Replay
}

const (
	relayCPSlots = 16
	specMaxHdr   = 16
)

type relayCP struct {
	frame int64
	sum   uint64
	valid bool
	blob  []byte
}

type relayRep struct {
	frame  int64
	sum    uint64
	epoch  uint8
	valid  bool
	judged bool
}

type relayResync struct {
	active bool
	epoch  uint8
	frame  int64
	sum    uint64
	blob   []byte
	last   time.Duration
}

type relaySpec struct {
	tr   Transport
	next int64
	last time.Duration
	sent bool
}

// Relay forwards packets between two sessions, logs their confirmed inputs,
// verifies checksums with a native copy of the game and repairs a desync by
// sending its Save blob.
type Relay[I any] struct {
	cfg   RelayConfig[I]
	codec Codec[I]
	isz   int
	tag   uint64
	k     int64
	now   time.Duration

	log     [2][]byte // encoded inputs, isz bytes per frame
	logN    [2]int64
	first   [2]time.Duration
	hasLog  [2]bool
	delay   [2]int
	players [2]PlayerReport

	game     Game[I]
	inputs   []I
	simFrame int64
	cps      [relayCPSlots]relayCP
	checks   []ReplayCheck
	reported [2][relayCPSlots]relayRep

	confirmedAt []time.Duration
	confN       int64

	sentEpoch  [2]uint8
	ackedEpoch [2]uint8
	rs         [2]relayResync
	desyncs    []int64
	resyncs    int

	specs   []*relaySpec
	specVis int64

	ended     bool
	endReason uint8
	endRepeat int
	scratch   []byte
	rbuf      [MaxPacketSize]byte
	cmp       []byte
}

// NewRelay validates cfg and returns a Relay.
func NewRelay[I any](cfg RelayConfig[I]) (*Relay[I], error) {
	if cfg.NewGame == nil || cfg.Codec == nil || cfg.Peers[0] == nil || cfg.Peers[1] == nil {
		return nil, errors.New("netplay: NewGame, Codec and both Peers are required")
	}
	if cfg.ChecksumInterval == 0 {
		cfg.ChecksumInterval = 30
	}
	if cfg.FrameDuration == 0 {
		cfg.FrameDuration = time.Second / 60
	}
	if cfg.CatchUpPerTick == 0 {
		cfg.CatchUpPerTick = 30
	}
	if cfg.SpectatorDelay == 0 {
		cfg.SpectatorDelay = 3 * time.Second
	}
	if cfg.LateSlack == 0 {
		cfg.LateSlack = 2 * cfg.FrameDuration
	}
	if cfg.LateFlagCount == 0 {
		cfg.LateFlagCount = 10
	}
	r := &Relay[I]{
		cfg: cfg, codec: cfg.Codec, isz: cfg.Codec.Size(), tag: idTag(&cfg.Identity),
		k: int64(cfg.ChecksumInterval), game: cfg.NewGame(), inputs: make([]I, 2),
		cmp: make([]byte, cfg.Codec.Size()),
	}
	return r, nil
}

// AddSpectator attaches a spectator link. Spectators receive confirmed inputs
// after the spectator delay and acknowledge with their next wanted frame.
func (r *Relay[I]) AddSpectator(t Transport) { r.specs = append(r.specs, &relaySpec{tr: t}) }

// Ended reports whether the relay ended the match.
func (r *Relay[I]) Ended() bool { return r.ended }

// SimFrame is the number of frames the native copy has simulated.
func (r *Relay[I]) SimFrame() int64 { return r.simFrame }

// Tick processes packets and advances the native copy at time now. Call it
// often (every few milliseconds); it simulates at most CatchUpPerTick frames.
func (r *Relay[I]) Tick(now time.Duration) {
	r.now = now
	if r.ended {
		if r.endRepeat > 0 {
			r.endRepeat--
			for p := 0; p < 2; p++ {
				_ = r.cfg.Peers[p].Send(putSmall(r.pktBuf(), ptEnd, r.tag, r.endReason, 0))
			}
		}
		return
	}
	for p := 0; p < 2; p++ {
		for n := 0; n < 512; n++ {
			m, ok := r.cfg.Peers[p].Recv(r.rbuf[:])
			if !ok {
				break
			}
			r.handle(p, r.rbuf[:m])
			if r.ended {
				return
			}
		}
	}
	r.simulate()
	r.sendResyncs()
	r.serveSpectators()
}

func (r *Relay[I]) pktBuf() []byte {
	if r.scratch == nil {
		r.scratch = make([]byte, MaxPacketSize)
	}
	return r.scratch
}

func (r *Relay[I]) handle(p int, b []byte) {
	if len(b) < 2 || b[0] != protoVersion {
		return
	}
	switch b[1] {
	case ptHello:
		if len(b) != helloLen || !bytes.Equal(b[offHelloID:], r.cfg.Identity[:]) {
			return // a peer with another identity never reaches the other side
		}
		_ = r.cfg.Peers[1-p].Send(b)
	case ptInput:
		pk, tag, ok := parseInput(b, r.isz)
		if !ok || tag != r.tag {
			return
		}
		r.logInputs(p, &pk)
		for i := 0; i < pk.nsums; i++ {
			r.onReport(p, int64(pk.sums[i].frame), pk.sums[i].sum, pk.sums[i].epoch)
		}
		_ = r.cfg.Peers[1-p].Send(b) // whole packet, always: redundancy depends on it
	case ptResyncAck:
		tag, epoch, _, ok := parseSmall(b, ptResyncAck)
		if ok && tag == r.tag && r.rs[p].active && epoch == r.rs[p].epoch {
			r.rs[p].active = false
			r.ackedEpoch[p] = epoch
			r.resyncs++
			for i := range r.reported[p] {
				r.reported[p][i].valid = false
			}
		}
	}
}

// logInputs records first-seen entries. Duplicate entries for frames the
// relay already holds are dropped from the log only, never from forwarding.
func (r *Relay[I]) logInputs(p int, pk *inputPkt) {
	if pk.delay != delayAuto {
		r.delay[p] = int(pk.delay)
		r.players[p].InputDelay = int(pk.delay)
	}
	q := 1 - p
	for i := 0; i < pk.count; i++ {
		f := int64(pk.start) + int64(i)
		raw := pk.entries[i*r.isz : (i+1)*r.isz]
		switch {
		case f < r.logN[p]:
			r.players[p].DuplicateEntries++
			if !bytes.Equal(r.log[p][f*int64(r.isz):(f+1)*int64(r.isz)], raw) {
				r.players[p].ConflictingEntries++
			}
		case f == r.logN[p]:
			if !r.hasLog[p] {
				r.hasLog[p], r.first[p] = true, r.now
			}
			r.log[p] = append(r.log[p], raw...)
			r.logN[p]++
			r.noteLateness(p, q, f)
		}
	}
	for r.confN < r.logN[0] && r.confN < r.logN[1] {
		r.confirmedAt = append(r.confirmedAt, r.now)
		r.confN++
	}
}

// noteLateness compares the arrival of player p's input for frame f with its
// due time plus the input delay. A late input counts only if the other
// player had already sent frame f: a player who waits for the other (a stall,
// or a time-sync wait) arrives late too, but is not the cause.
func (r *Relay[I]) noteLateness(p, q int, f int64) {
	due := r.first[p] + time.Duration(f+int64(r.delay[p]))*r.cfg.FrameDuration + r.cfg.LateSlack
	late := r.now - due
	if late <= 0 || r.logN[q] <= f {
		return
	}
	pr := &r.players[p]
	pr.LateInputs++
	if late > pr.MaxLateness {
		pr.MaxLateness = late
	}
	if pr.LateInputs >= r.cfg.LateFlagCount {
		pr.Flagged = true
	}
}

func (r *Relay[I]) onReport(p int, f int64, sum uint64, epoch uint8) {
	if f <= 0 || f%r.k != 0 {
		return
	}
	rep := &r.reported[p][(f/r.k)%relayCPSlots]
	if rep.valid && rep.frame == f && rep.epoch == epoch {
		return
	}
	*rep = relayRep{frame: f, sum: sum, epoch: epoch, valid: true}
	r.judge(p, f)
}

// simulate steps the native copy over confirmed frames, within the budget.
func (r *Relay[I]) simulate() {
	confirmed := r.logN[0]
	if r.logN[1] < confirmed {
		confirmed = r.logN[1]
	}
	for n := 0; n < r.cfg.CatchUpPerTick && r.simFrame < confirmed; n++ {
		f := r.simFrame
		r.inputs[0] = r.codec.Get(r.log[0][f*int64(r.isz):])
		r.inputs[1] = r.codec.Get(r.log[1][f*int64(r.isz):])
		r.game.Step(r.inputs)
		r.simFrame++
		if r.simFrame%r.k == 0 {
			r.saveCP(r.simFrame)
		}
		if r.ended {
			return
		}
	}
}

func (r *Relay[I]) saveCP(f int64) {
	c := &r.cps[(f/r.k)%relayCPSlots]
	c.blob = r.game.Save(c.blob[:0])
	c.frame, c.sum, c.valid = f, Checksum(c.blob), true
	r.checks = append(r.checks, ReplayCheck{Frame: uint32(f), Sum: c.sum})
	r.judge(0, f)
	r.judge(1, f)
}

func (r *Relay[I]) judge(p int, f int64) {
	c := &r.cps[(f/r.k)%relayCPSlots]
	rep := &r.reported[p][(f/r.k)%relayCPSlots]
	if !c.valid || c.frame != f || !rep.valid || rep.frame != f || rep.judged {
		return
	}
	// Ignore reports from before a pending or completed resync.
	if r.sentEpoch[p] != r.ackedEpoch[p] || rep.epoch != r.ackedEpoch[p] {
		return
	}
	rep.judged = true
	if rep.sum == c.sum {
		return
	}
	known := false
	for _, d := range r.desyncs {
		if d == f {
			known = true
		}
	}
	if !known {
		r.desyncs = append(r.desyncs, f)
	}
	if len(r.desyncs) >= 2 {
		r.endMatch(EndDesync)
		return
	}
	r.sentEpoch[p]++
	r.rs[p] = relayResync{active: true, epoch: r.sentEpoch[p], frame: f, sum: c.sum,
		blob: append(r.rs[p].blob[:0], c.blob...), last: r.now - r.cfg.FrameDuration}
}

func (r *Relay[I]) endMatch(reason uint8) {
	r.ended, r.endReason, r.endRepeat = true, reason, 10
	for p := 0; p < 2; p++ {
		r.rs[p].active = false
		_ = r.cfg.Peers[p].Send(putSmall(r.pktBuf(), ptEnd, r.tag, reason, 0))
	}
}

// sendResyncs sends each pending blob in fragments, once per frame time,
// until the peer acknowledges it.
func (r *Relay[I]) sendResyncs() {
	for p := 0; p < 2; p++ {
		rs := &r.rs[p]
		if !rs.active || r.now-rs.last < r.cfg.FrameDuration {
			continue
		}
		rs.last = r.now
		for off := 0; off < len(rs.blob); off += resyncChunk {
			end := off + resyncChunk
			if end > len(rs.blob) {
				end = len(rs.blob)
			}
			pk := resyncPkt{epoch: rs.epoch, frame: uint32(rs.frame), total: uint32(len(rs.blob)),
				offset: uint32(off), sum: Checksum(rs.blob), data: rs.blob[off:end]}
			_ = r.cfg.Peers[p].Send(putResync(r.pktBuf(), r.tag, &pk))
		}
	}
}

func (r *Relay[I]) serveSpectators() {
	for r.specVis < r.confN && r.confirmedAt[r.specVis]+r.cfg.SpectatorDelay <= r.now {
		r.specVis++
	}
	per := (MaxPacketSize - specMaxHdr) / (2 * r.isz)
	for _, s := range r.specs {
		for n := 0; n < 64; n++ {
			m, ok := s.tr.Recv(r.rbuf[:])
			if !ok {
				break
			}
			if tag, _, next, ok := parseSmall(r.rbuf[:m], ptSpecAck); ok && tag == r.tag && int64(next) > s.next {
				s.next = int64(next)
			}
		}
		if s.next >= r.specVis || (s.sent && r.now-s.last < r.cfg.FrameDuration) {
			continue
		}
		s.sent, s.last = true, r.now
		count := int(r.specVis - s.next)
		if count > per {
			count = per
		}
		b := r.pktBuf()[:specMaxHdr+count*2*r.isz]
		b[0], b[1] = protoVersion, ptSpectate
		binary.LittleEndian.PutUint64(b[2:], r.tag)
		binary.LittleEndian.PutUint32(b[10:], uint32(s.next))
		binary.LittleEndian.PutUint16(b[14:], uint16(count))
		for i := 0; i < count; i++ {
			f := (s.next + int64(i)) * int64(r.isz)
			o := specMaxHdr + i*2*r.isz
			copy(b[o:], r.log[0][f:f+int64(r.isz)])
			copy(b[o+r.isz:], r.log[1][f:f+int64(r.isz)])
		}
		_ = s.tr.Send(b)
	}
}

// Report returns the match report so far.
func (r *Relay[I]) Report() MatchReport {
	rep := MatchReport{
		Identity: r.cfg.Identity, ConfirmedFrames: r.confN, VerifiedFrames: r.simFrame,
		Players: r.players, DesyncFrames: append([]int64(nil), r.desyncs...),
		Resyncs: r.resyncs, Ended: r.ended, EndReason: r.endReason,
	}
	for p := 0; p < 2; p++ {
		rep.Players[p].FramesLogged = int(r.logN[p])
	}
	if r.ended && r.endReason == EndDesync {
		rep.Replay = r.Replay()
	}
	return rep
}

// Replay builds a replay of the confirmed frames logged so far.
func (r *Relay[I]) Replay() *Replay {
	n := int(r.confN)
	rp := &Replay{Identity: r.cfg.Identity, InputSize: r.isz, Interval: int(r.k), Frames: n,
		Inputs: make([]byte, 0, n*2*r.isz)}
	for f := 0; f < n; f++ {
		rp.Inputs = append(rp.Inputs, r.log[0][f*r.isz:(f+1)*r.isz]...)
		rp.Inputs = append(rp.Inputs, r.log[1][f*r.isz:(f+1)*r.isz]...)
	}
	for _, c := range r.checks {
		if int(c.Frame) <= n {
			rp.Checks = append(rp.Checks, c)
		}
	}
	return rp
}

// ---- spectator ----

// SpectatorConfig configures a Spectator.
type SpectatorConfig[I any] struct {
	Game      Game[I]
	Codec     Codec[I]
	Transport Transport
	Identity  [32]byte
	// MaxFramesPerPoll bounds simulation per Poll (default 30).
	MaxFramesPerPoll int
}

// Spectator plays a match from the relay's delayed stream of confirmed
// inputs. It never predicts and never rolls back.
type Spectator[I any] struct {
	cfg    SpectatorConfig[I]
	isz    int
	tag    uint64
	frame  int64
	buf    []byte // received but unapplied frames, 2*isz bytes each, from base
	base   int64
	in     []I
	pkt    [MaxPacketSize]byte
	rbuf   [MaxPacketSize]byte
	events []Event
}

// NewSpectator returns a Spectator that starts at frame 0.
func NewSpectator[I any](cfg SpectatorConfig[I]) *Spectator[I] {
	if cfg.MaxFramesPerPoll == 0 {
		cfg.MaxFramesPerPoll = 30
	}
	return &Spectator[I]{cfg: cfg, isz: cfg.Codec.Size(), tag: idTag(&cfg.Identity), in: make([]I, 2)}
}

// Frame is the number of frames simulated.
func (s *Spectator[I]) Frame() int64 { return s.frame }

// Checksum hashes the current state.
func (s *Spectator[I]) Checksum() uint64 { return Checksum(s.cfg.Game.Save(nil)) }

// Poll receives stream packets, simulates available frames and acknowledges.
// It returns the number of frames simulated.
func (s *Spectator[I]) Poll(now time.Duration) int {
	for n := 0; n < 512; n++ {
		m, ok := s.cfg.Transport.Recv(s.rbuf[:])
		if !ok {
			break
		}
		b := s.rbuf[:m]
		if len(b) < specMaxHdr || b[0] != protoVersion || b[1] != ptSpectate ||
			binary.LittleEndian.Uint64(b[2:]) != s.tag {
			continue
		}
		start := int64(binary.LittleEndian.Uint32(b[10:]))
		count := int(binary.LittleEndian.Uint16(b[14:]))
		if len(b) != specMaxHdr+count*2*s.isz || start > s.base+int64(len(s.buf)/(2*s.isz)) {
			continue
		}
		for i := 0; i < count; i++ {
			f := start + int64(i)
			if f < s.base+int64(len(s.buf)/(2*s.isz)) {
				continue // already held
			}
			s.buf = append(s.buf, b[specMaxHdr+i*2*s.isz:specMaxHdr+(i+1)*2*s.isz]...)
		}
	}
	stepped := 0
	for stepped < s.cfg.MaxFramesPerPoll && s.frame < s.base+int64(len(s.buf)/(2*s.isz)) {
		o := int(s.frame-s.base) * 2 * s.isz
		s.in[0] = s.cfg.Codec.Get(s.buf[o:])
		s.in[1] = s.cfg.Codec.Get(s.buf[o+s.isz:])
		s.cfg.Game.Step(s.in)
		s.frame++
		stepped++
	}
	_ = s.cfg.Transport.Send(putSmall(s.pkt[:], ptSpecAck, s.tag, 0, uint32(s.base+int64(len(s.buf)/(2*s.isz)))))
	return stepped
}
