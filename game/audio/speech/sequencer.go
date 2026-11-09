package speech

// Segment is one fragment placed on the timeline. All times are absolute
// sample frames on the voice clock (host.CurrentTime() * rate).
type Segment struct {
	Frag FragID
	Pos  Pos
	Clip Clip
	// Start is the first frame of the clip; the clip plays Clip.Frames frames.
	Start int64
	// FadeIn and FadeOut are linear gain ramps at the clip's edges, in frames.
	FadeIn, FadeOut int32
}

// End is the frame after the clip's last frame.
func (s *Segment) End() int64 { return s.Start + int64(s.Clip.Frames) }

// Sink receives the timeline a Sequencer builds. Implementations: Mix (PCM,
// for tests and offline rendering) and VoicePlayer (the browser's voice bus).
type Sink interface {
	// BeginLine opens a line. Segments for it follow.
	BeginLine(line uint64)
	// Schedule adds a segment to the line.
	Schedule(line uint64, seg Segment)
	// CutLine fades the whole line to silence, starting at frame at and
	// reaching zero fade frames later. Segments that start after at+fade are
	// cancelled.
	CutLine(line uint64, at int64, fade int32)
}

// SequencerConfig tunes a Sequencer. A zero field takes its default.
type SequencerConfig struct {
	// CrossfadeMs is the overlap between neighbouring fragments when the gap
	// is zero, and the edge fade on every fragment. Default 8.
	CrossfadeMs float64
	// GapMs is the silence left between fragments. Default 0, which joins
	// neighbours with a CrossfadeMs overlap instead. With a positive gap the
	// clips do not overlap; their edge fades still apply.
	GapMs float64
	// ClauseGapMs is the pause (default plus explicit) at or above which a
	// fragment counts as ending one clause and the next as starting another,
	// so the pitch variants follow the pause. Default 150.
	ClauseGapMs float64
	// CutFadeMs is the fade used when a line is interrupted. Default 12.
	CutFadeMs float64
	// LeadMs is how far ahead of "now" a line is scheduled, so its start is
	// never in the past by the time the audio thread sees it. Default 20.
	LeadMs float64
}

// Sequencer lays Utterances on a sample-accurate timeline. It does no audio
// work itself; it computes whole-frame start times and hands them to a Sink.
// It is not safe for concurrent use.
type Sequencer struct {
	lib   *Library
	sink  Sink
	cfg   seqFrames
	line  uint64
	cur   line
	stats Stats
}

type seqFrames struct {
	xfade, gap, clauseGap, cut, lead int64
}

type line struct {
	id            uint64
	valid         bool
	phrase, vari  int
	priority      int
	interruptible bool
	n             int
	segs          [MaxFragments]Segment
	start, end    int64
	cutAt         int64 // -1 when not cut
}

// Stats counts what the sequencer has done, for tests and diagnostics.
type Stats struct {
	Lines, Interrupts, Rejected int
	// MissingClips counts fragments skipped because the library had no
	// recording for them.
	MissingClips int
}

// NewSequencer builds a sequencer that reads clip lengths from lib and writes
// the timeline to sink.
func NewSequencer(lib *Library, sink Sink, cfg SequencerConfig) *Sequencer {
	def := func(v *float64, d float64) {
		if *v == 0 {
			*v = d
		}
	}
	def(&cfg.CrossfadeMs, 8)
	def(&cfg.ClauseGapMs, 150)
	def(&cfg.CutFadeMs, 12)
	def(&cfg.LeadMs, 20)
	ms := func(v float64) int64 { return int64(v*float64(lib.rate)/1000 + 0.5) }
	return &Sequencer{lib: lib, sink: sink, cfg: seqFrames{
		xfade: ms(cfg.CrossfadeMs), gap: ms(cfg.GapMs), clauseGap: ms(cfg.ClauseGapMs),
		cut: ms(cfg.CutFadeMs), lead: ms(cfg.LeadMs),
	}}
}

// Stats returns the counters.
func (s *Sequencer) Stats() Stats { return s.stats }

// Playing reports what the voice is doing at frame now, in the form
// Planner.Submit takes.
func (s *Sequencer) Playing(now int64) Playing {
	if !s.active(now) {
		return Playing{}
	}
	return Playing{Active: true, Priority: s.cur.priority, Interruptible: s.cur.interruptible}
}

func (s *Sequencer) active(now int64) bool {
	return s.cur.valid && now < s.cur.end
}

// Status describes the line on air.
type Status struct {
	Active        bool
	Phrase        int
	Variant       int
	Priority      int
	Interruptible bool
	// Cut is true while the line fades out after an interrupt.
	Cut bool
	// Index is the position of the sounding fragment in the line, or -1 in a
	// gap; Frag and Pos name it.
	Index int
	Frag  FragID
	Pos   Pos
	// Elapsed and Remaining are in frames since the line's first frame.
	Elapsed, Remaining int64
}

// Status reports what is playing at frame now. Index is the last fragment
// that has started.
func (s *Sequencer) Status(now int64) Status {
	if !s.active(now) {
		return Status{Index: -1}
	}
	c := &s.cur
	st := Status{Active: true, Phrase: c.phrase, Variant: c.vari, Priority: c.priority,
		Interruptible: c.interruptible, Cut: c.cutAt >= 0 && now >= c.cutAt, Index: -1,
		Elapsed: now - c.start, Remaining: c.end - now}
	for i := 0; i < c.n; i++ {
		if c.segs[i].Start > now {
			break
		}
		st.Index, st.Frag, st.Pos = i, c.segs[i].Frag, c.segs[i].Pos
	}
	return st
}

// Apply carries out a planner decision at frame now. It reports whether a
// line started.
func (s *Sequencer) Apply(d *Decision, now int64) bool {
	switch d.Action {
	case Play:
		return s.Play(&d.Utt, now)
	case Interrupt:
		return s.Interrupt(&d.Utt, now)
	}
	return false
}

// Play starts u at now plus the lead, or reports false when a line is still
// on air. Use Interrupt to cut one.
func (s *Sequencer) Play(u *Utterance, now int64) bool {
	if s.active(now) {
		s.stats.Rejected++
		return false
	}
	return s.start(u, now+s.cfg.lead)
}

// Interrupt fades out the line on air and starts u in its place. With
// nothing on air it behaves like Play. It reports false, and leaves the
// current line alone, when the current line is not interruptible or u does
// not outrank it.
func (s *Sequencer) Interrupt(u *Utterance, now int64) bool {
	if !s.active(now) {
		return s.start(u, now+s.cfg.lead)
	}
	if !s.cur.interruptible || u.Priority <= s.cur.priority {
		s.stats.Rejected++
		return false
	}
	at := now + s.cfg.lead
	return s.start(u, at)
}

func (s *Sequencer) start(u *Utterance, at int64) bool {
	// Resolve clips first so an utterance with no playable fragment leaves
	// the current line untouched.
	var ids [MaxFragments]FragID
	var gaps [MaxFragments]int64
	n := 0
	for i := 0; i < u.N; i++ {
		id := u.Frags[i].ID
		if _, ok := s.lib.Clip(id, PosMid); !ok {
			s.stats.MissingClips++
			if n > 0 {
				gaps[n-1] += s.gapFor(&u.Frags[i])
			}
			continue
		}
		ids[n] = id
		gaps[n] = s.gapFor(&u.Frags[i])
		n++
	}
	if n == 0 {
		return false
	}

	if s.cur.valid && s.cur.end > at && s.cur.cutAt < 0 {
		s.sink.CutLine(s.cur.id, at, int32(s.cfg.cut))
		s.cur.cutAt = at
		s.cur.end = at + s.cfg.cut
		s.stats.Interrupts++
	}

	s.line++
	c := &s.cur
	*c = line{id: s.line, valid: true, phrase: u.Phrase, vari: u.Variant, priority: u.Priority,
		interruptible: u.Interruptible, cutAt: -1, start: at}
	s.sink.BeginLine(c.id)

	// Pitch position: first of a clause is Start, last is End, others Mid. A
	// gap at or above ClauseGapMs closes a clause. One-fragment clauses take End.
	clauseStart := true
	var fin, fout [MaxFragments]int64
	for i := 0; i < n; i++ {
		last := i == n-1 || gaps[i] >= s.cfg.clauseGap
		pos := PosMid
		switch {
		case last:
			pos = PosEnd
		case clauseStart:
			pos = PosStart
		}
		clip, _ := s.lib.Clip(ids[i], pos)
		c.segs[i] = Segment{Frag: ids[i], Pos: pos, Clip: clip}
		fade := min(s.cfg.xfade, int64(clip.Frames)/2)
		fin[i], fout[i] = fade, fade
		clauseStart = last
	}
	c.n = n
	// A zero-gap join overlaps its two clips by one shared fade length, so
	// the complementary ramps sum to unity even when a short clip's own fade
	// is clamped.
	t := at
	for i := 0; i < n; i++ {
		seg := &c.segs[i]
		seg.Start = t
		if i+1 < n && gaps[i] <= 0 {
			o := min(fout[i], fin[i+1])
			fout[i], fin[i+1] = o, o
		}
		seg.FadeIn, seg.FadeOut = int32(fin[i]), int32(fout[i])
		end := seg.End()
		if gaps[i] > 0 {
			t = end + gaps[i]
		} else {
			t = end - fout[i]
		}
		c.end = end
	}
	for i := 0; i < n; i++ {
		s.sink.Schedule(c.id, c.segs[i])
	}
	s.stats.Lines++
	return true
}

// gapFor is the silence after a fragment: the default gap plus its own pause.
// Zero means a crossfade join.
func (s *Sequencer) gapFor(f *FragRef) int64 {
	return s.cfg.gap + int64(f.GapMs)*int64(s.lib.rate)/1000
}
