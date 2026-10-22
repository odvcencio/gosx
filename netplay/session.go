package netplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// AutoDelay in Config.InputDelay picks the input delay from the handshake
// round trip.
const AutoDelay = -1

// Session errors. Test with errors.Is.
var (
	ErrDisconnect       = errors.New("netplay: peer stalled past the stall timeout")
	ErrIdentityMismatch = errors.New("netplay: simulation identity differs")
	ErrDesync           = errors.New("netplay: desync")
	ErrRemoteEnded      = errors.New("netplay: match ended by relay")
	ErrCatchUpTimeout   = errors.New("netplay: desync catch-up exceeded its deadline")
	ErrBadResync        = errors.New("netplay: unusable resync blob")
)

// DesyncError describes a checksum mismatch on a confirmed frame.
type DesyncError struct {
	Frame  int64
	Local  uint64
	Remote uint64
	State  []byte // local Save bytes at Frame, for the match report
}

func (e *DesyncError) Error() string {
	return fmt.Sprintf("netplay: desync at frame %d (local %016x, remote %016x)", e.Frame, e.Local, e.Remote)
}

// Is makes errors.Is(err, ErrDesync) true.
func (e *DesyncError) Is(target error) bool { return target == ErrDesync }

// Status is the lifecycle state of a Session.
type Status int

// Session lifecycle states.
const (
	StatusHandshake Status = iota
	StatusRunning
	StatusEnded
)

// Outcome is what one call to Advance did.
type Outcome int

// Outcomes of Advance.
const (
	OutcomeHandshake Outcome = iota // still waiting for the peer
	OutcomeAdvanced                 // simulated one new frame
	OutcomeStalled                  // at the prediction window; waiting for inputs
	OutcomeWaited                   // skipped a frame to rebalance frame advantage
	OutcomeCatchUp                  // re-simulating after a relay resync
	OutcomeEnded
)

// Config configures a Session. Game, Codec and Transport are required.
type Config[I any] struct {
	Game      Game[I]
	Codec     Codec[I]
	Transport Transport

	// LocalPlayer is this peer's index, 0 or 1.
	LocalPlayer int
	// Identity is the simulation identity; peers must match.
	Identity [32]byte
	// InputDelay is D, 0 to MaxInputDelay, or AutoDelay.
	InputDelay int
	// MaxInputDelay is the app's limit on D (default 3).
	MaxInputDelay int
	// MaxRollback is the rollback depth AutoDelay aims for (default 4).
	MaxRollback int

	Predictor        Predictor[I] // default RepeatLast
	ChecksumInterval int          // K, default 30
	PredictionWindow int          // default 8
	StallTimeout     time.Duration
	// SyncInterval is N: a peer ahead by more than one frame waits one
	// frame at most every N ticks (default 4).
	SyncInterval  int
	FrameDuration time.Duration // default 1/60 s

	// Relay is true when a relay arbitrates desyncs. Without one, a checksum
	// mismatch ends the match.
	Relay          bool
	CatchUpPerTick int           // frames re-simulated per tick after a resync (default 30)
	CatchUpTimeout time.Duration // default 1 s

	// OnChecksum, if set, is called for each confirmed-frame checksum.
	OnChecksum func(frame int64, sum uint64)
}

// Stats are counters for diagnostics and tests.
type Stats struct {
	Rollbacks          int
	RolledBackFrames   int
	MaxRollbackDepth   int
	StallTicks         int
	WaitTicks          int
	DuplicateEntries   int
	ConflictingEntries int
	BadPackets         int
	PacketsSent        int
	PacketsReceived    int
	Checksums          int
	Agreed             int
	Mismatches         int
	Resyncs            int
	CatchUpTicks       int
	CatchUpFrames      int
	MaxCatchUpPerTick  int
}

const (
	historyFrames = 256 // retained inputs; spec minimum is 120
	cpSlots       = 8
	noFrame       = int64(-1)
)

type cpSlot struct {
	frame  int64
	sum    uint64
	epoch  uint8
	valid  bool
	judged bool
	state  []byte
}

type remSum struct {
	frame int64
	sum   uint64
	epoch uint8
	valid bool
}

// Session is one peer of a two-player rollback match. It is not safe for
// concurrent use. In steady state Advance does not allocate.
type Session[I any] struct {
	cfg    Config[I]
	me     int
	peer   int
	codec  Codec[I]
	isz    int
	game   Game[I]
	pred   Predictor[I]
	tr     Transport
	tag    uint64
	k      int64
	window int64
	nring  int

	status Status
	err    error
	stats  Stats
	now    time.Duration

	delay      int
	delayKnown bool
	peerSeen   bool
	hsStart    time.Duration
	hsStarted  bool

	frame     int64 // next frame to simulate; the re-simulation cursor during catch-up
	rc        int64 // remote inputs known for frames [0, rc)
	localNext int64 // local inputs committed for frames [0, localNext)
	peerAck   int64 // peer holds our inputs for frames [0, peerAck)
	firstBad  int64

	local  []I // ring of historyFrames
	remote []I
	used   []I // remote value used when frame was last simulated

	states  [][]byte // ring of nring saved states; states[f%nring] is the state at the start of f
	stFrame []int64
	evs     [][]Event
	in      [2]I
	inSlice []I
	zero    I

	changedFrom int64
	checkBuf    [2][]byte

	// network and clocks
	seq       uint32
	recvSeq   uint32
	haveRecv  bool
	echoUs    uint32
	echoAt    time.Duration
	haveEcho  bool
	rtt       time.Duration
	haveRTT   bool
	adv       float64
	haveAdv   bool
	sinceWait int
	pkt       [MaxPacketSize]byte
	rbuf      [MaxPacketSize]byte

	// stall
	stalling   bool
	stallSince time.Duration

	// checksums
	nextCheck int64
	cp        [cpSlots]cpSlot
	rem       [cpSlots]remSum
	epoch     uint8
	agreed    int64

	// resync (relay assisted)
	rsEpoch  uint8
	rsFrame  int64
	rsTotal  int
	rsBuf    []byte
	rsGot    []bool
	rsHave   int
	rsSum    uint64
	catching bool
	catchEnd int64
	catchBy  time.Duration
}

// NewSession validates cfg and returns a Session in the handshake state.
func NewSession[I any](cfg Config[I]) (*Session[I], error) {
	if cfg.Game == nil || cfg.Codec == nil || cfg.Transport == nil {
		return nil, errors.New("netplay: Game, Codec and Transport are required")
	}
	if cfg.LocalPlayer != 0 && cfg.LocalPlayer != 1 {
		return nil, errors.New("netplay: LocalPlayer must be 0 or 1")
	}
	if cfg.Codec.Size() < 1 || cfg.Codec.Size() > 64 {
		return nil, errors.New("netplay: input size must be 1 to 64 bytes")
	}
	if cfg.MaxInputDelay == 0 {
		cfg.MaxInputDelay = 3
	}
	if cfg.MaxRollback == 0 {
		cfg.MaxRollback = 4
	}
	if cfg.InputDelay > cfg.MaxInputDelay || cfg.InputDelay < AutoDelay {
		return nil, errors.New("netplay: InputDelay outside 0..MaxInputDelay")
	}
	if cfg.Predictor == nil {
		cfg.Predictor = RepeatLast[I]
	}
	if cfg.ChecksumInterval == 0 {
		cfg.ChecksumInterval = 30
	}
	if cfg.PredictionWindow == 0 {
		cfg.PredictionWindow = 8
	}
	if cfg.StallTimeout == 0 {
		cfg.StallTimeout = 5 * time.Second
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 4
	}
	if cfg.FrameDuration == 0 {
		cfg.FrameDuration = time.Second / 60
	}
	if cfg.CatchUpPerTick == 0 {
		cfg.CatchUpPerTick = 30
	}
	if cfg.CatchUpTimeout == 0 {
		cfg.CatchUpTimeout = time.Second
	}
	nring := cfg.PredictionWindow + cfg.MaxInputDelay + 4
	s := &Session[I]{
		cfg: cfg, me: cfg.LocalPlayer, peer: 1 - cfg.LocalPlayer,
		codec: cfg.Codec, isz: cfg.Codec.Size(), game: cfg.Game, pred: cfg.Predictor,
		tr: cfg.Transport, tag: idTag(&cfg.Identity),
		k: int64(cfg.ChecksumInterval), window: int64(cfg.PredictionWindow), nring: nring,
		firstBad: noFrame, changedFrom: noFrame, agreed: noFrame,
		local: make([]I, historyFrames), remote: make([]I, historyFrames), used: make([]I, historyFrames),
		states: make([][]byte, nring), stFrame: make([]int64, nring), evs: make([][]Event, nring),
		inSlice: make([]I, 2),
	}
	for i := range s.states {
		s.states[i] = make([]byte, 0, 512)
		s.evs[i] = make([]Event, 0, 8)
		s.stFrame[i] = noFrame
	}
	s.checkBuf[0] = make([]byte, s.isz)
	s.checkBuf[1] = make([]byte, s.isz)
	s.nextCheck = s.k
	s.inSlice = s.inSlice[:2]
	if cfg.InputDelay != AutoDelay {
		s.delay, s.delayKnown = cfg.InputDelay, true
	}
	return s, nil
}

// SuggestDelay returns the smallest input delay that keeps the expected
// rollback depth (one-way latency in frames minus the delay) at or under
// maxRollback, capped at maxDelay.
func SuggestDelay(rtt, frameDur time.Duration, maxRollback, maxDelay int) int {
	oneWay := int(math.Ceil(float64(rtt/2)/float64(frameDur) - 0.05)) // frames, rounded up
	d := oneWay - maxRollback
	if d < 0 {
		d = 0
	}
	if d > maxDelay {
		d = maxDelay
	}
	return d
}

// Frame is the next frame to be simulated.
func (s *Session[I]) Frame() int64 { return s.frame }

// Status reports the lifecycle state.
func (s *Session[I]) Status() Status { return s.status }

// Err is why the session ended, or nil.
func (s *Session[I]) Err() error { return s.err }

// RTT is the smoothed round trip, from echoed timestamps.
func (s *Session[I]) RTT() time.Duration { return s.rtt }

// InputDelay is the delay D in use (valid once the handshake is done).
func (s *Session[I]) InputDelay() int { return s.delay }

// FrameAdvantage is the smoothed local frame minus the estimated remote
// frame, in frames. Positive means this peer is ahead.
func (s *Session[I]) FrameAdvantage() float64 { return s.adv }

// ConfirmedFrame is the number of leading frames whose inputs are all known.
func (s *Session[I]) ConfirmedFrame() int64 {
	c := s.rc
	if l := s.localNext; l < c {
		c = l
	}
	return c
}

// Stats returns a copy of the counters.
func (s *Session[I]) Stats() Stats { return s.stats }

// ChangedFrom is the first frame whose events may differ since the last
// Advance: the rolled-back frame, or the newly simulated frame. It is -1 if
// nothing was simulated.
func (s *Session[I]) ChangedFrom() int64 { return s.changedFrom }

// EventsFrom appends the events of frames [from, Frame()) that are still
// retained (the last few frames), in frame then emission order.
func (s *Session[I]) EventsFrom(from int64, dst []Event) []Event {
	if lo := s.frame - int64(s.nring) + 1; from < lo {
		from = lo
	}
	if from < 0 {
		from = 0
	}
	for f := from; f < s.frame; f++ {
		if s.stFrame[f%int64(s.nring)] == f {
			dst = append(dst, s.evs[f%int64(s.nring)]...)
		}
	}
	return dst
}

func (s *Session[I]) end(err error) {
	if s.status != StatusEnded {
		s.status, s.err = StatusEnded, err
	}
}

// Advance runs one tick at time now with the local player's sampled input.
// Call it at the frame rate. It receives packets, rolls back if a prediction
// was wrong, then simulates one frame unless it must stall or wait, and sends
// one packet. now is any monotonic clock; only differences matter.
func (s *Session[I]) Advance(now time.Duration, local I) Outcome {
	if s.status == StatusEnded {
		return OutcomeEnded
	}
	s.now = now
	s.changedFrom = noFrame
	s.sinceWait++
	s.poll(now)
	if s.status == StatusEnded {
		return OutcomeEnded
	}
	if s.status == StatusHandshake {
		if !s.hsStarted {
			s.hsStarted, s.hsStart = true, now
		}
		if now-s.hsStart > s.cfg.StallTimeout {
			s.end(ErrDisconnect)
			return OutcomeEnded
		}
		s.sendHello(now)
		return OutcomeHandshake
	}
	if s.catching {
		return s.catchUp()
	}
	if s.firstBad != noFrame && s.firstBad < s.frame {
		s.rollback(s.firstBad)
	}
	s.firstBad = noFrame
	s.tryCheckpoints(s.frame - 1)
	if s.status == StatusEnded {
		return OutcomeEnded
	}

	// Stall at the prediction window, or if our unacked inputs would outrun
	// the retained history.
	if s.frame-s.rc >= s.window || s.localNext-s.peerAck >= historyFrames-16 {
		if !s.stalling {
			s.stalling, s.stallSince = true, now
		}
		s.stats.StallTicks++
		if now-s.stallSince > s.cfg.StallTimeout {
			s.end(ErrDisconnect)
			return OutcomeEnded
		}
		s.sendInput(now)
		return OutcomeStalled
	}
	s.stalling = false

	if s.haveAdv && s.adv > 1 && s.sinceWait >= s.cfg.SyncInterval {
		s.sinceWait = 0
		s.stats.WaitTicks++
		s.sendInput(now)
		return OutcomeWaited
	}

	g := s.frame + int64(s.delay)
	s.local[g%historyFrames] = local
	s.localNext = g + 1
	s.stepFrame(s.frame)
	if s.changedFrom == noFrame { // a rollback this tick already set it, earlier
		s.changedFrom = s.frame
	}
	s.frame++
	s.sendInput(now)
	return OutcomeAdvanced
}

// stepFrame saves the state at the start of frame g and simulates g with the
// best known inputs.
func (s *Session[I]) stepFrame(g int64) {
	i := g % int64(s.nring)
	s.states[i] = s.game.Save(s.states[i][:0])
	s.stFrame[i] = g
	s.in[s.me] = s.local[g%historyFrames]
	var v I
	if g < s.rc {
		v = s.remote[g%historyFrames]
	} else {
		last := s.zero
		if s.rc > 0 {
			last = s.remote[(s.rc-1)%historyFrames]
		}
		v = s.pred(s.peer, last, g)
	}
	s.used[g%historyFrames] = v
	s.in[s.peer] = v
	s.inSlice[0], s.inSlice[1] = s.in[0], s.in[1]
	s.game.Step(s.inSlice)
	evs := s.game.Events(s.evs[i][:0])
	for k := range evs {
		evs[k].Frame = g
	}
	s.evs[i] = evs
	s.tryCheckpoints(g)
}

// rollback loads the saved state of frame f and re-simulates to s.frame.
func (s *Session[I]) rollback(f int64) {
	i := f % int64(s.nring)
	if s.stFrame[i] != f {
		s.end(fmt.Errorf("netplay: internal: no saved state for frame %d", f))
		return
	}
	if err := s.game.Load(s.states[i]); err != nil {
		s.end(err)
		return
	}
	depth := int(s.frame - f)
	s.stats.Rollbacks++
	s.stats.RolledBackFrames += depth
	if depth > s.stats.MaxRollbackDepth {
		s.stats.MaxRollbackDepth = depth
	}
	for g := f; g < s.frame; g++ {
		s.stepFrame(g)
	}
	s.changedFrom = f
}

// catchUp re-simulates a bounded number of frames after a resync.
func (s *Session[I]) catchUp() Outcome {
	n := 0
	for ; n < s.cfg.CatchUpPerTick && s.frame < s.catchEnd; n++ {
		s.stepFrame(s.frame)
		s.frame++
	}
	s.stats.CatchUpTicks++
	s.stats.CatchUpFrames += n
	if n > s.stats.MaxCatchUpPerTick {
		s.stats.MaxCatchUpPerTick = n
	}
	s.sendInput(s.now)
	if s.frame >= s.catchEnd {
		s.catching = false
		s.changedFrom = s.catchEnd - int64(s.nring) + 1
		if s.changedFrom < 0 {
			s.changedFrom = 0
		}
		return OutcomeCatchUp
	}
	if s.now > s.catchBy {
		s.end(ErrCatchUpTimeout)
		return OutcomeEnded
	}
	return OutcomeCatchUp
}

// ---- receiving ----

func (s *Session[I]) poll(now time.Duration) {
	for n := 0; n < 512; n++ {
		m, ok := s.tr.Recv(s.rbuf[:])
		if !ok {
			return
		}
		s.stats.PacketsReceived++
		s.handle(now, s.rbuf[:m])
		if s.status == StatusEnded {
			return
		}
	}
}

func (s *Session[I]) handle(now time.Duration, b []byte) {
	if len(b) < 2 || b[0] != protoVersion {
		s.stats.BadPackets++
		return
	}
	switch b[1] {
	case ptHello:
		s.handleHello(now, b)
	case ptInput:
		s.handleInput(now, b)
	case ptResync:
		s.handleResync(now, b)
	case ptEnd:
		if tag, _, _, ok := parseSmall(b, ptEnd); ok && tag == s.tag && s.cfg.Relay {
			s.end(ErrRemoteEnded)
		} else {
			s.stats.BadPackets++
		}
	default:
		s.stats.BadPackets++
	}
}

func (s *Session[I]) handleHello(now time.Duration, b []byte) {
	if len(b) != helloLen {
		s.stats.BadPackets++
		return
	}
	if !bytes.Equal(b[offHelloID:], s.cfg.Identity[:]) {
		s.sendHello(now) // tell the peer, which then sees the mismatch too
		s.end(ErrIdentityMismatch)
		return
	}
	h := getHeader(b)
	s.noteHeader(now, &h)
	s.peerSeen = true
	s.maybeStart(now)
}

func seqNewer(a, b uint32) bool { return int32(a-b) > 0 }

// noteHeader records echo and RTT data common to Hello and Input.
func (s *Session[I]) noteHeader(now time.Duration, h *header) bool {
	newest := !s.haveRecv || seqNewer(h.seq, s.recvSeq)
	if newest {
		s.haveRecv, s.recvSeq = true, h.seq
		s.echoUs, s.echoAt, s.haveEcho = h.sendUs, now, true
	}
	if h.hasEcho {
		d := micros(now) - h.echoUs - h.holdUs // wraps correctly
		if d < 1<<30 {
			sample := time.Duration(d) * time.Microsecond
			if !s.haveRTT {
				s.rtt, s.haveRTT = sample, true
			} else {
				s.rtt += (sample - s.rtt) / 8
			}
		}
	}
	return newest
}

func (s *Session[I]) maybeStart(now time.Duration) {
	if s.status != StatusHandshake || !s.peerSeen {
		return
	}
	if !s.delayKnown {
		if !s.haveRTT {
			return
		}
		s.delay = SuggestDelay(s.rtt, s.cfg.FrameDuration, s.cfg.MaxRollback, s.cfg.MaxInputDelay)
		s.delayKnown = true
	}
	s.status = StatusRunning
	s.localNext = int64(s.delay) // frames before D carry the zero input
	s.hsStart = now
}

func (s *Session[I]) handleInput(now time.Duration, b []byte) {
	p, tag, ok := parseInput(b, s.isz)
	if !ok || tag != s.tag {
		s.stats.BadPackets++
		return
	}
	newest := s.noteHeader(now, &p.header)
	if s.status == StatusHandshake {
		s.peerSeen = true
		s.maybeStart(now)
		if s.status == StatusHandshake {
			return
		}
	}
	if newest {
		rttFrames := float64(s.rtt) / float64(s.cfg.FrameDuration)
		sample := float64(s.frame) - (float64(p.frame) + rttFrames/2)
		if !s.haveAdv {
			s.adv, s.haveAdv = sample, true
		} else {
			s.adv += (sample - s.adv) * 0.25
		}
	}
	if a := int64(p.ack); a > s.peerAck {
		s.peerAck = a
	}
	for i := 0; i < p.count; i++ {
		f := int64(p.start) + int64(i)
		raw := p.entries[i*s.isz : (i+1)*s.isz]
		switch {
		case f < s.rc:
			s.stats.DuplicateEntries++
			s.codec.Put(s.checkBuf[0], s.remote[f%historyFrames])
			if !bytes.Equal(s.checkBuf[0], raw) {
				s.stats.ConflictingEntries++
			}
		case f == s.rc:
			in := s.codec.Get(raw)
			s.remote[f%historyFrames] = in
			if f < s.frame {
				s.codec.Put(s.checkBuf[0], s.used[f%historyFrames])
				if !bytes.Equal(s.checkBuf[0], raw) && (s.firstBad == noFrame || f < s.firstBad) {
					s.firstBad = f
				}
			}
			s.rc++
		default:
			s.stats.BadPackets++ // gap: sender must resend from our ack
		}
	}
	for i := 0; i < p.nsums; i++ {
		s.onRemoteSum(int64(p.sums[i].frame), p.sums[i].sum, p.sums[i].epoch)
	}
}

// ---- checksums ----

func (s *Session[I]) tryCheckpoints(limit int64) {
	for s.nextCheck <= limit && s.nextCheck <= s.rc {
		f := s.nextCheck
		i := f % int64(s.nring)
		if s.stFrame[i] != f {
			if limit-f >= int64(s.nring) { // fell out of the ring; skip it
				s.nextCheck += s.k
				continue
			}
			return
		}
		s.nextCheck += s.k
		sum := Checksum(s.states[i])
		slot := &s.cp[(f/s.k)%cpSlots]
		slot.frame, slot.sum, slot.epoch, slot.valid, slot.judged = f, sum, s.epoch, true, false
		slot.state = append(slot.state[:0], s.states[i]...)
		s.stats.Checksums++
		if s.cfg.OnChecksum != nil {
			s.cfg.OnChecksum(f, sum)
		}
		s.judge(f)
		if s.status == StatusEnded {
			return
		}
	}
}

func (s *Session[I]) onRemoteSum(f int64, sum uint64, epoch uint8) {
	if f <= 0 || f%s.k != 0 {
		s.stats.BadPackets++
		return
	}
	r := &s.rem[(f/s.k)%cpSlots]
	if r.valid && r.frame == f && r.epoch == epoch {
		return
	}
	*r = remSum{frame: f, sum: sum, epoch: epoch, valid: true}
	s.judge(f)
}

func (s *Session[I]) judge(f int64) {
	c := &s.cp[(f/s.k)%cpSlots]
	r := &s.rem[(f/s.k)%cpSlots]
	if !c.valid || !r.valid || c.frame != f || r.frame != f || c.judged || c.epoch != r.epoch {
		return
	}
	c.judged = true
	if c.sum == r.sum {
		s.stats.Agreed++
		if f > s.agreed {
			s.agreed = f
		}
		return
	}
	s.stats.Mismatches++
	if !s.cfg.Relay {
		st := append([]byte(nil), c.state...)
		s.end(&DesyncError{Frame: f, Local: c.sum, Remote: r.sum, State: st})
	}
}

// ---- resync ----

func (s *Session[I]) handleResync(now time.Duration, b []byte) {
	p, tag, ok := parseResync(b)
	if !ok || tag != s.tag || !s.cfg.Relay || s.status != StatusRunning {
		s.stats.BadPackets++
		return
	}
	if p.epoch == s.epoch && s.epoch != 0 {
		s.sendResyncAck() // our ack was lost; repeat it
		return
	}
	if p.epoch < s.epoch || p.total == 0 || p.total > 1<<24 {
		return
	}
	if s.rsTotal != int(p.total) || s.rsEpoch != p.epoch || s.rsFrame != int64(p.frame) || s.rsSum != p.sum {
		s.rsEpoch, s.rsFrame, s.rsTotal, s.rsSum = p.epoch, int64(p.frame), int(p.total), p.sum
		s.rsBuf = append(s.rsBuf[:0], make([]byte, p.total)...)
		s.rsGot = append(s.rsGot[:0], make([]bool, (int(p.total)+resyncChunk-1)/resyncChunk)...)
		s.rsHave = 0
	}
	if p.offset%resyncChunk != 0 || int(p.offset)+len(p.data) > s.rsTotal {
		return
	}
	ci := int(p.offset) / resyncChunk
	if !s.rsGot[ci] {
		copy(s.rsBuf[p.offset:], p.data)
		s.rsGot[ci] = true
		s.rsHave++
	}
	if s.rsHave == len(s.rsGot) {
		s.applyResync(now)
	}
}

func (s *Session[I]) applyResync(now time.Duration) {
	blob := s.rsBuf
	s.rsTotal = 0
	c := s.rsFrame
	if Checksum(blob) != s.rsSum || c <= 0 || c > s.frame || s.frame-c >= historyFrames-16 {
		s.end(ErrBadResync)
		return
	}
	if err := s.game.Load(blob); err != nil {
		s.end(fmt.Errorf("%w: %v", ErrBadResync, err))
		return
	}
	s.stats.Resyncs++
	s.epoch = s.rsEpoch
	s.catchEnd = s.frame
	s.frame = c
	s.catching = true
	s.catchBy = now + s.cfg.CatchUpTimeout
	s.firstBad = noFrame
	// Everything we computed after c came from the bad state.
	for i := range s.cp {
		s.cp[i].valid = false
		s.rem[i].valid = false
	}
	for i := range s.stFrame {
		if s.stFrame[i] >= c {
			s.stFrame[i] = noFrame
		}
	}
	s.nextCheck = c + s.k
	s.sendResyncAck()
}

func (s *Session[I]) sendResyncAck() {
	b := putSmall(s.pkt[:], ptResyncAck, s.tag, s.epoch, uint32(s.rsFrame))
	_ = s.tr.Send(b)
}

// ---- sending ----

func (s *Session[I]) header(typ byte, now time.Duration) header {
	s.seq++
	h := header{typ: typ, seq: s.seq, sendUs: micros(now)}
	if s.cfg.InputDelay == AutoDelay && !s.delayKnown {
		h.delay = delayAuto
	} else {
		h.delay = byte(s.delay)
	}
	if s.haveEcho {
		h.hasEcho, h.echoUs, h.holdUs = true, s.echoUs, micros(now-s.echoAt)
	}
	return h
}

func (s *Session[I]) sendHello(now time.Duration) {
	h := s.header(ptHello, now)
	b := s.pkt[:helloLen]
	putHeader(b, ptHello, s.tag, &h)
	b[offHelloPlayer] = byte(s.me)
	copy(b[offHelloID:], s.cfg.Identity[:])
	s.stats.PacketsSent++
	_ = s.tr.Send(b)
}

func (s *Session[I]) sendInput(now time.Duration) {
	h := s.header(ptInput, now)
	b := s.pkt[:inputHdr]
	putHeader(b, ptInput, s.tag, &h)
	start := s.peerAck
	if lo := s.localNext - (historyFrames - 16); start < lo {
		start = lo
	}
	if start < 0 {
		start = 0
	}
	count := int(s.localNext - start)
	if m := maxEntries(s.isz); count > m {
		count = m
	}
	if count < 0 {
		count = 0
	}
	le := binary.LittleEndian
	le.PutUint32(b[offFrame:], uint32(s.frame))
	le.PutUint32(b[offAck:], uint32(s.rc))
	le.PutUint32(b[offStart:], uint32(start))
	le.PutUint16(b[offCount:], uint16(count))
	b = b[:inputHdr+count*s.isz]
	for i := 0; i < count; i++ {
		s.codec.Put(b[inputHdr+i*s.isz:], s.local[(start+int64(i))%historyFrames])
	}
	// Newest checksums, newest first.
	var picked [maxSums]*cpSlot
	np := 0
	for i := range s.cp {
		c := &s.cp[i]
		if !c.valid || c.epoch != s.epoch {
			continue
		}
		j := np
		if np < maxSums {
			np++
		} else if c.frame <= picked[maxSums-1].frame {
			continue
		} else {
			j = maxSums - 1
		}
		for j > 0 && picked[j-1].frame < c.frame {
			picked[j] = picked[j-1]
			j--
		}
		picked[j] = c
	}
	b[offNSums] = byte(np)
	for i := 0; i < np; i++ {
		o := len(b)
		b = b[:o+sumLen]
		le.PutUint32(b[o:], uint32(picked[i].frame))
		le.PutUint64(b[o+4:], picked[i].sum)
		b[o+12] = picked[i].epoch
	}
	s.stats.PacketsSent++
	_ = s.tr.Send(b)
}
