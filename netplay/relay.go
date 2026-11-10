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
	// PredictionWindow and MaxInputDelay are the values the sessions use
	// (defaults 8 and 3). They must be at least as large as the sessions':
	// the relay rejects input packets that run further ahead than they allow,
	// and flags a player whose acknowledgement trails by more than
	// PredictionWindow + MaxInputDelay + AckLagSlack frames.
	PredictionWindow int
	MaxInputDelay    int
	// AckLagSlack is the extra frames of ack lag tolerated for network delay
	// and loss (default 30). Set AckLagSlackNone for exactly 0.
	AckLagSlack int
	// AckLagGrace is how old a forwarded frame must be before it counts
	// toward ack lag (default 1 s), so a burst of early inputs from the other
	// player cannot flag an honest one.
	AckLagGrace time.Duration
	// InputSlack is the extra frames an input packet may run past
	// the other player's log + PredictionWindow + the sender's delay before
	// the relay rejects it (default 8).
	InputSlack int
	// CrawlAfter is the gap between two growths of a player's log, while
	// the other player's log kept growing, that counts as a slow gap
	// (default 1.5 s). CrawlGaps slow gaps set CrawlFlagged (default 3).
	CrawlAfter time.Duration
	CrawlGaps  int
	// EndRepeatEvery and EndRepeats pace the End packets sent after the
	// match ends (defaults 100 ms and 20), by relay time, one per Tick at most.
	EndRepeatEvery time.Duration
	EndRepeats     int
}

// AckLagSlackNone, in RelayConfig.AckLagSlack, means no slack at all.
const AckLagSlackNone = -1

// ViolationKind says what a player's malformed or impossible packet did.
type ViolationKind uint8

const (
	ViolationNone ViolationKind = iota
	// ViolationBadPacket: an Input packet with the wrong tag or length.
	// The relay drops it and trusts nothing in it.
	ViolationBadPacket
	// ViolationNoResend: the packet starts above the highest ack the relay
	// has forwarded from the other player. An honest sender resends from that
	// ack. The relay drops its ack but logs entries that extend the log.
	ViolationNoResend
	// ViolationFuture: the packet carries frames beyond the other player's
	// log + prediction window + the sender's delay + slack. Dropped whole.
	ViolationFuture
	// ViolationImpossibleAck: the ack is above the frames the relay has
	// logged for the other player. The ack is ignored.
	ViolationImpossibleAck
	// ViolationConflict: an entry for a frame already logged has other bytes.
	ViolationConflict
	// NumViolationKinds is the length of PlayerReport.ViolationCounts.
	NumViolationKinds
)

func (k ViolationKind) String() string {
	switch k {
	case ViolationBadPacket:
		return "bad packet"
	case ViolationNoResend:
		return "no resend"
	case ViolationFuture:
		return "future frames"
	case ViolationImpossibleAck:
		return "impossible ack"
	case ViolationConflict:
		return "conflicting entry"
	}
	return "none"
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

	// Desyncs counts checksum frames where this player's checksum disagreed
	// with the relay's native copy; each started a resync to this player.
	// Resyncs counts resyncs this player acknowledged. FirstDesyncFrame is
	// the first such checksum frame, or 0 if none (frame 0 is never judged).
	//
	// Desyncs alone do not say whose build diverged: an input the relay
	// forwarded is always the one it logged, but a player who sent
	// conflicting inputs (ConflictingEntries, Violations) is the suspect
	// whatever the Desyncs counts say.
	Desyncs          int
	Resyncs          int
	FirstDesyncFrame int64

	// Violations counts this player's rejected or distrusted Input packets.
	// ViolationCounts splits them by ViolationKind. FirstViolation is the
	// kind of the first one, FirstViolationFrame the start frame of that
	// packet (the ack value for ViolationImpossibleAck, 0 for a bad packet),
	// and FirstViolationAt and FirstViolationTick the relay clock and Tick
	// count. ImpossibleAcks counts acks above what the relay had logged.
	// ConflictingEntries (above) is attributed to the sender.
	Violations          int
	ViolationCounts     [NumViolationKinds]int
	FirstViolation      ViolationKind
	FirstViolationFrame int64
	FirstViolationAt    time.Duration
	FirstViolationTick  int64
	ImpossibleAcks      int

	// AckedFrame is the highest ack seen in this player's packets that passed
	// validation: the number of the other player's frames it holds.
	// ForwardedAck is the ack the relay put in the last packet it sent to the
	// other player: the ack that player acts on (it never exceeds the frames
	// the relay logged for that player).
	AckedFrame   int64
	ForwardedAck int64

	// AckLag is the most recent distance, in frames, between the other
	// player's frames that the relay forwarded at least AckLagGrace ago and
	// the frames this player acknowledged. MaxAckLag is its largest value.
	// AckLagFlagged is set once the lag exceeds the limit
	// (Relay.AckLagLimit). It is measured when this player's valid packets
	// arrive, so a silent player shows up in LastPacketAt and LastLogAt.
	AckLag        int
	MaxAckLag     int
	AckLagFlagged bool

	// LogGap is how long ago this player's log last grew, at the last
	// Tick, counted only while the other player's log is longer (this
	// player holds the match back; a player waiting for the other has the
	// longer log). MaxLogGap is the longest such gap, open or
	// closed. SlowGaps counts closed gaps longer than CrawlAfter, and
	// CrawlFlagged is set after CrawlGaps of them. A client that crawls, one
	// tick every 2 s, never goes silent long enough for a stall timeout, but
	// it collects slow gaps. One long gap alone (an outage that hit both
	// players) does not set the flag.
	LogGap       time.Duration
	MaxLogGap    time.Duration
	SlowGaps     int
	CrawlFlagged bool

	// LastLogAt is the relay clock (the `now` given to Tick) when this
	// player's log last grew, and LastLogTick is the Tick count then. Both
	// are meaningful only when FramesLogged > 0. The player whose log
	// stopped growing first stalled first. These are wall-clock facts for
	// the app; never feed them to the simulation.
	LastLogAt   time.Duration
	LastLogTick int64
	// LastPacketAt is the relay clock when any valid packet from this player
	// last arrived, and LastPacketTick the Tick count then (0 until one
	// arrives).
	LastPacketAt   time.Duration
	LastPacketTick int64
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
	// Ticks is the number of Tick calls so far.
	Ticks int64
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

	ticks int64
	ack   [2]int64 // highest validated ack seen from each player
	logAt [2][]time.Duration
	elig  [2]int64 // frames of each player forwarded at least AckLagGrace ago
	fbuf  [MaxPacketSize]byte

	ended     bool
	endReason uint8
	endLeft   int
	endNext   time.Duration
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
	if cfg.PredictionWindow == 0 {
		cfg.PredictionWindow = 8
	}
	if cfg.MaxInputDelay == 0 {
		cfg.MaxInputDelay = 3
	}
	if cfg.AckLagSlack == 0 {
		cfg.AckLagSlack = 30
	} else if cfg.AckLagSlack < 0 {
		cfg.AckLagSlack = 0
	}
	if cfg.AckLagGrace == 0 {
		cfg.AckLagGrace = time.Second
	}
	if cfg.InputSlack == 0 {
		cfg.InputSlack = 8
	}
	if cfg.CrawlAfter == 0 {
		cfg.CrawlAfter = 1500 * time.Millisecond
	}
	if cfg.CrawlGaps == 0 {
		cfg.CrawlGaps = 3
	}
	if cfg.EndRepeatEvery == 0 {
		cfg.EndRepeatEvery = 100 * time.Millisecond
	}
	if cfg.EndRepeats == 0 {
		cfg.EndRepeats = 20
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
	r.ticks++
	if r.ended {
		if r.endLeft > 0 && r.now >= r.endNext {
			r.endLeft--
			r.endNext = r.now + r.cfg.EndRepeatEvery
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
	r.updateGaps()
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
		r.notePacket(p)
		_ = r.cfg.Peers[1-p].Send(b)
	case ptInput:
		pk, tag, ok := parseInput(b, r.isz)
		if !ok || tag != r.tag {
			r.violate(p, ViolationBadPacket, 0) // nothing in it is trusted
			return
		}
		r.handleInput(p, &pk)
	case ptResyncAck:
		tag, epoch, _, ok := parseSmall(b, ptResyncAck)
		if ok && tag == r.tag && r.rs[p].active && epoch == r.rs[p].epoch {
			r.rs[p].active = false
			r.ackedEpoch[p] = epoch
			r.resyncs++
			r.players[p].Resyncs++
			r.notePacket(p)
			for i := range r.reported[p] {
				r.reported[p][i].valid = false
			}
		}
	}
}

func (r *Relay[I]) notePacket(p int) {
	r.players[p].LastPacketAt, r.players[p].LastPacketTick = r.now, r.ticks
}

// violate records one violation by player p. frame says where (see
// PlayerReport.FirstViolationFrame).
func (r *Relay[I]) violate(p int, k ViolationKind, frame int64) {
	pr := &r.players[p]
	if pr.Violations == 0 {
		pr.FirstViolation, pr.FirstViolationFrame = k, frame
		pr.FirstViolationAt, pr.FirstViolationTick = r.now, r.ticks
	}
	pr.Violations++
	pr.ViolationCounts[k]++
}

// AckLagLimit is the lag, in frames, above which a player is flagged.
func (r *Relay[I]) AckLagLimit() int {
	return r.cfg.PredictionWindow + r.cfg.MaxInputDelay + r.cfg.AckLagSlack
}

// handleInput validates a well-formed Input packet from player p, logs it and
// forwards a packet built from the relay's own log to the other player. The
// sender's bytes are never forwarded, so a player cannot change what the
// other receives after the relay logged it.
func (r *Relay[I]) handleInput(p int, pk *inputPkt) {
	q := 1 - p
	r.notePacket(p)
	start := int64(pk.start)
	end := start + int64(pk.count)
	d := r.delay[p]
	if pk.delay != delayAuto {
		d = int(pk.delay)
	}
	if end > r.logN[q]+int64(r.cfg.PredictionWindow+d+r.cfg.InputSlack) {
		// Frames this far ahead cannot come from a session that waits for the
		// other player; they would also wrap the receiver's history ring.
		r.violate(p, ViolationFuture, start)
		return
	}
	ack := int64(pk.ack)
	trust := true
	if ack > r.logN[q] {
		r.players[p].ImpossibleAcks++
		r.violate(p, ViolationImpossibleAck, ack)
		trust = false
	}
	if start > r.ack[q] {
		// An honest sender resends from the ack it received, which the relay
		// built from r.ack[q]. The entries still extend the log if they are
		// contiguous with it, and the relay resends from its own log.
		r.violate(p, ViolationNoResend, start)
		trust = false
	}
	r.logInputs(p, pk)
	if trust {
		r.noteAck(p, ack)
	}
	for i := 0; i < pk.nsums; i++ {
		r.onReport(p, int64(pk.sums[i].frame), pk.sums[i].sum, pk.sums[i].epoch)
	}
	r.forward(p, pk)
}

// noteAck records a validated ack from player p and measures how far it
// trails the other player's frames that were forwarded at least AckLagGrace
// ago. ack is at most the frames logged for the other player.
func (r *Relay[I]) noteAck(p int, ack int64) {
	q := 1 - p
	if ack > r.ack[p] {
		r.ack[p] = ack
	}
	pr := &r.players[p]
	pr.AckedFrame = r.ack[p]
	for r.elig[q] < r.logN[q] && r.logAt[q][r.elig[q]]+r.cfg.AckLagGrace <= r.now {
		r.elig[q]++
	}
	lag := int(r.elig[q] - r.ack[p])
	if lag < 0 {
		lag = 0
	}
	pr.AckLag = lag
	if lag > pr.MaxAckLag {
		pr.MaxAckLag = lag
	}
	if lag > r.AckLagLimit() {
		pr.AckLagFlagged = true
	}
}

// forward sends the other player a packet with player p's header, checksum
// reports and ack, and p's logged inputs from the other player's validated
// ack on.
func (r *Relay[I]) forward(p int, pk *inputPkt) {
	q := 1 - p
	start := r.ack[q]
	count := r.logN[p] - start
	if m := int64(maxEntries(r.isz)); count > m {
		count = m
	}
	if count < 0 {
		count = 0
	}
	b := r.fbuf[:inputHdr]
	h := pk.header
	putHeader(b, ptInput, r.tag, &h)
	le := binary.LittleEndian
	le.PutUint32(b[offFrame:], pk.frame)
	le.PutUint32(b[offAck:], uint32(r.ack[p]))
	le.PutUint32(b[offStart:], uint32(start))
	le.PutUint16(b[offCount:], uint16(count))
	b = b[:inputHdr+int(count)*r.isz]
	copy(b[inputHdr:], r.log[p][start*int64(r.isz):(start+count)*int64(r.isz)])
	b[offNSums] = byte(pk.nsums)
	for i := 0; i < pk.nsums; i++ {
		o := len(b)
		b = b[:o+sumLen]
		le.PutUint32(b[o:], pk.sums[i].frame)
		le.PutUint64(b[o+4:], pk.sums[i].sum)
		b[o+12] = pk.sums[i].epoch
	}
	r.players[p].ForwardedAck = r.ack[p]
	_ = r.cfg.Peers[q].Send(b)
}

// updateGaps measures how long each player's log has been silent while the
// other player's log grew.
func (r *Relay[I]) updateGaps() {
	for p := 0; p < 2; p++ {
		q := 1 - p
		pr := &r.players[p]
		var gap time.Duration
		if r.hasLog[p] && r.hasLog[q] && r.logN[q] > r.logN[p] {
			gap = r.now - pr.LastLogAt
		}
		pr.LogGap = gap
		if gap > pr.MaxLogGap {
			pr.MaxLogGap = gap
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
				r.violate(p, ViolationConflict, f)
			}
		case f == r.logN[p]:
			if !r.hasLog[p] {
				r.hasLog[p], r.first[p] = true, r.now
			}
			r.log[p] = append(r.log[p], raw...)
			r.logN[p]++
			r.logAt[p] = append(r.logAt[p], r.now)
			pr := &r.players[p]
			if r.logN[p] > 1 && r.hasLog[q] && r.logN[q] > f && r.now > pr.LastLogAt {
				// The other player was ahead while this log was silent: this
				// player held the match back.
				gap := r.now - pr.LastLogAt
				if gap > pr.MaxLogGap {
					pr.MaxLogGap = gap
				}
				if gap > r.cfg.CrawlAfter {
					pr.SlowGaps++
					if pr.SlowGaps >= r.cfg.CrawlGaps {
						pr.CrawlFlagged = true
					}
				}
			}
			pr.LastLogAt, pr.LastLogTick = r.now, r.ticks
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
	if pr := &r.players[p]; pr.Desyncs == 0 {
		pr.FirstDesyncFrame = f
		pr.Desyncs = 1
	} else {
		pr.Desyncs++
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

// endBurst is how many End packets go out at once, before the repeats that
// later Tick calls send. Copies in one burst do not help against a burst of
// loss, so the app should still keep ticking (see Draining).
const endBurst = 4

func (r *Relay[I]) endMatch(reason uint8) {
	r.ended, r.endReason = true, reason
	r.endLeft, r.endNext = r.cfg.EndRepeats, r.now+r.cfg.EndRepeatEvery
	for p := 0; p < 2; p++ {
		r.rs[p].active = false
		for i := 0; i < endBurst; i++ {
			_ = r.cfg.Peers[p].Send(putSmall(r.pktBuf(), ptEnd, r.tag, reason, 0))
		}
	}
}

// Finish ends the match as over (EndMatchOver) and sends the End packet to
// both players at once. The app calls it when its game rules decide the
// match is finished, so the clients end normally (ErrMatchOver) instead of
// stalling into ErrDisconnect. It does nothing if the match already ended.
//
// Call Finish on the goroutine that calls Tick; the relay is not safe for
// concurrent use. The relay repeats End by relay time (EndRepeats copies,
// EndRepeatEvery apart, defaults 20 and 100 ms), one per Tick at most, because
// a datagram can be lost. Keep calling Tick until Draining reports false.
func (r *Relay[I]) Finish() {
	if !r.ended {
		r.endMatch(EndMatchOver)
	}
}

// Draining reports whether the relay still owes End repeats. While it is
// true, call Tick (at the usual rate) so every client sees the End packet.
func (r *Relay[I]) Draining() bool { return r.ended && r.endLeft > 0 }

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
		Resyncs: r.resyncs, Ended: r.ended, EndReason: r.endReason, Ticks: r.ticks,
	}
	for p := 0; p < 2; p++ {
		rep.Players[p].FramesLogged = int(r.logN[p])
	}
	r.updateGaps()
	rep.Players = r.players
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
