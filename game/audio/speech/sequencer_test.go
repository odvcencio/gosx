package speech

import (
	"math"
	"testing"
)

const testRate = 48000

// seqFixture builds a library whose clips have distinct lengths per position
// (start 1000, mid 900, end 800 frames, scaled by 1 for every fragment) and
// constant sample values, so a rendered buffer shows which clip sounds when.
type seqFixture struct {
	lib *Library
	mix *Mix
	seq *Sequencer
	c   *Compiled
}

func newSeqFixture(t testing.TB, cfg SequencerConfig) *seqFixture {
	t.Helper()
	c, lib := compileRacing(t)
	for id := FragID(1); int(id) <= lib.Len(); id++ {
		for pos, n := range map[Pos]int{PosStart: 1000, PosMid: 900, PosEnd: 800} {
			data := make([]float32, n)
			for i := range data {
				data[i] = 1
			}
			lib.Set(id, pos, Clip{Frames: int32(n), Handle: data})
		}
	}
	mix := &Mix{}
	return &seqFixture{lib: lib, mix: mix, seq: NewSequencer(lib, mix, cfg), c: c}
}

func (f *seqFixture) utt(t testing.TB, kind string, fill func(*Event)) Utterance {
	t.Helper()
	p := NewPlanner(f.c, PlannerConfig{Seed: 1})
	var ev Event
	ev.Reset(kind)
	fill(&ev)
	d := p.Submit(&ev, 0, Playing{})
	if d.Action != Play {
		t.Fatalf("%s: %v/%v", kind, d.Action, d.Reason)
	}
	return d.Utt
}

func TestSequencerPlacesFragmentsOnWholeFrames(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{CrossfadeMs: 5, LeadMs: 20})
	u := f.utt(t, "gap", func(e *Event) { e.Str("driver", "vance").Num("gap", 1) }) // two fragments
	if u.N != 2 {
		t.Fatalf("fragments = %d", u.N)
	}
	if !f.seq.Play(&u, 1000) {
		t.Fatal("Play refused on an idle voice")
	}
	lead := int64(0.020 * testRate) // 960
	xf := int64(0.005 * testRate)   // 240
	first, second := f.mix.segs[0].seg, f.mix.segs[1].seg
	if first.Start != 1000+lead || first.Pos != PosStart || first.Clip.Frames != 1000 {
		t.Errorf("first = start %d pos %v frames %d", first.Start, first.Pos, first.Clip.Frames)
	}
	if second.Pos != PosEnd || second.Clip.Frames != 800 {
		t.Errorf("second pos %v frames %d, want end/800", second.Pos, second.Clip.Frames)
	}
	if want := first.End() - xf; second.Start != want {
		t.Errorf("second starts at %d, want %d (end minus crossfade)", second.Start, want)
	}
	if first.FadeIn != int32(xf) || first.FadeOut != int32(xf) || second.FadeIn != int32(xf) {
		t.Errorf("fades = %d/%d/%d, want %d", first.FadeIn, first.FadeOut, second.FadeIn, xf)
	}

	// The crossfade sums to unity across the overlap.
	out := f.mix.Render(second.Start, int(xf))
	for i, v := range out {
		if math.Abs(float64(v)-1) > 2e-3 {
			t.Fatalf("crossfade sample %d = %v, want 1", i, v)
		}
	}
	// Silence before the line and after it.
	if v := f.mix.Render(0, 1000+int(lead)); maxAbs(v) != 0 {
		t.Error("audio before the lead")
	}
	if v := f.mix.Render(second.End(), 100); maxAbs(v) != 0 {
		t.Error("audio after the line")
	}
	// The first sample of the line is zero (fade in), the second is not.
	head := f.mix.Render(first.Start, 2)
	if head[0] != 0 || head[1] <= 0 {
		t.Errorf("fade-in head = %v", head)
	}
}

func TestSequencerGapAddsSilence(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{CrossfadeMs: 5, GapMs: 30})
	u := f.utt(t, "gap", func(e *Event) { e.Str("driver", "vance").Num("gap", 1) })
	f.seq.Play(&u, 0)
	a, b := f.mix.segs[0].seg, f.mix.segs[1].seg
	if got, want := b.Start-a.End(), int64(30*testRate/1000); got != want {
		t.Errorf("gap = %d frames, want %d", got, want)
	}
	mid := f.mix.Render(a.End(), int(b.Start-a.End()))
	if maxAbs(mid) != 0 {
		t.Error("the gap is not silent")
	}
}

func TestSequencerPausePicksClausePositions(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{})
	// Four fragments with a 200 ms pause after the first: a pause closes a
	// clause, so the first takes the end variant and the second starts a new clause.
	u := Utterance{N: 4, Interruptible: true}
	u.Frags[0] = FragRef{ID: 1, GapMs: 200}
	u.Frags[1], u.Frags[2], u.Frags[3] = FragRef{ID: 2}, FragRef{ID: 3}, FragRef{ID: 4}
	f.seq.Play(&u, 0)
	var pos []Pos
	for _, s := range f.mix.segs {
		pos = append(pos, s.seg.Pos)
	}
	want := []Pos{PosEnd, PosStart, PosMid, PosEnd}
	for i := range want {
		if pos[i] != want[i] {
			t.Fatalf("positions = %v, want %v", pos, want)
		}
	}
	if gap := f.mix.segs[1].seg.Start - f.mix.segs[0].seg.End(); gap != int64(0.2*testRate) {
		t.Errorf("explicit pause = %d frames", gap)
	}
}

func TestSequencerSingleFragmentIsEnd(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{})
	u := Utterance{N: 1, Interruptible: true}
	u.Frags[0] = FragRef{ID: 1}
	f.seq.Play(&u, 0)
	if f.mix.segs[0].seg.Pos != PosEnd {
		t.Errorf("single fragment pos = %v", f.mix.segs[0].seg.Pos)
	}
}

func TestSequencerFallsBackToAvailableVariant(t *testing.T) {
	c, lib := compileRacing(t)
	_ = c
	lib.Set(1, PosMid, Clip{Frames: 500})
	mix := &Mix{}
	seq := NewSequencer(lib, mix, SequencerConfig{})
	u := Utterance{N: 2}
	u.Frags[0], u.Frags[1] = FragRef{ID: 1}, FragRef{ID: 1}
	seq.Play(&u, 0)
	for _, s := range mix.segs {
		if s.seg.Clip.Frames != 500 {
			t.Errorf("fallback clip frames = %d", s.seg.Clip.Frames)
		}
	}
}

func TestSequencerSkipsMissingClips(t *testing.T) {
	c, lib := compileRacing(t)
	_ = c
	lib.Set(1, PosMid, Clip{Frames: 500})
	lib.Set(3, PosMid, Clip{Frames: 500})
	mix := &Mix{}
	seq := NewSequencer(lib, mix, SequencerConfig{})
	u := Utterance{N: 3}
	u.Frags[0], u.Frags[1], u.Frags[2] = FragRef{ID: 1}, FragRef{ID: 2}, FragRef{ID: 3}
	if !seq.Play(&u, 0) {
		t.Fatal("line with a missing clip refused")
	}
	if len(mix.segs) != 2 || seq.Stats().MissingClips != 1 {
		t.Errorf("segments %d, missing %d", len(mix.segs), seq.Stats().MissingClips)
	}
	none := Utterance{N: 1}
	none.Frags[0] = FragRef{ID: 2}
	if seq.Play(&none, 1<<30) {
		t.Error("a line with no playable clip started")
	}
}

func TestSequencerRejectsWhileBusy(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{})
	u := f.utt(t, "gap", func(e *Event) { e.Str("driver", "vance").Num("gap", 1) })
	f.seq.Play(&u, 0)
	if f.seq.Play(&u, 100) {
		t.Error("Play started over a line on air")
	}
	if f.seq.Stats().Rejected != 1 {
		t.Errorf("rejected = %d", f.seq.Stats().Rejected)
	}
	end := f.mix.segs[1].seg.End()
	if f.seq.Playing(end-1).Active != true || f.seq.Playing(end).Active {
		t.Error("Playing does not flip at the line's last frame")
	}
	if !f.seq.Play(&u, end) {
		t.Error("Play refused right after the line ended")
	}
}

func TestSequencerInterruptCutsOldLine(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{CrossfadeMs: 10, CutFadeMs: 5, LeadMs: 0.0001})
	long := Utterance{N: 3, Interruptible: true, Priority: 1}
	long.Frags[0], long.Frags[1], long.Frags[2] = FragRef{ID: 1}, FragRef{ID: 2}, FragRef{ID: 3}
	f.seq.Play(&long, 0)
	long2 := f.mix.segs[2].seg
	if long2.Start <= 740 {
		t.Fatalf("setup: third segment starts at %d", long2.Start)
	}
	urgent := Utterance{N: 1, Priority: 9}
	urgent.Frags[0] = FragRef{ID: 4}
	cutAt := int64(500)
	if !f.seq.Interrupt(&urgent, cutAt) {
		t.Fatal("interrupt refused")
	}
	fade := int64(0.005 * testRate)
	// The old line is silent from cutAt+fade (third segment cancelled), and
	// the new line is the only sound there.
	newSeg := f.mix.segs[len(f.mix.segs)-1].seg
	if newSeg.Frag != 4 || newSeg.Start != cutAt+int64(math.Round(0.0001*testRate/1000)) {
		t.Logf("new segment start %d", newSeg.Start)
	}
	after := f.mix.Render(newSeg.End()+fade, 2000)
	if maxAbs(after) != 0 {
		t.Error("old line is still audible after the cut")
	}
	// During the fade the old line ramps down: the sum of old and new gain stays near unity.
	out := f.mix.Render(newSeg.Start, int(fade))
	if out[0] < 0.9 || out[0] > 1.1 {
		t.Errorf("onset of the new line sums to %v", out[0])
	}
	if f.seq.Stats().Interrupts != 1 {
		t.Errorf("interrupts = %d", f.seq.Stats().Interrupts)
	}
	st := f.seq.Status(newSeg.Start + 1)
	if !st.Active || st.Priority != 9 {
		t.Errorf("status after interrupt = %+v", st)
	}
}

func TestSequencerInterruptRules(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{})
	hold := Utterance{N: 1, Interruptible: false, Priority: 5}
	hold.Frags[0] = FragRef{ID: 1}
	f.seq.Play(&hold, 0)
	big := Utterance{N: 1, Priority: 9}
	big.Frags[0] = FragRef{ID: 2}
	if f.seq.Interrupt(&big, 100) {
		t.Error("cut an uninterruptible line")
	}
	f2 := newSeqFixture(t, SequencerConfig{})
	soft := Utterance{N: 1, Interruptible: true, Priority: 5}
	soft.Frags[0] = FragRef{ID: 1}
	f2.seq.Play(&soft, 0)
	same := Utterance{N: 1, Priority: 5}
	same.Frags[0] = FragRef{ID: 2}
	if f2.seq.Interrupt(&same, 100) {
		t.Error("an equal priority line cut another")
	}
}

func TestStatusReportsSoundingFragment(t *testing.T) {
	f := newSeqFixture(t, SequencerConfig{CrossfadeMs: 10, LeadMs: 0.0001})
	u := Utterance{N: 2, Interruptible: true, Priority: 3, Phrase: 2, Variant: 1}
	u.Frags[0], u.Frags[1] = FragRef{ID: 7}, FragRef{ID: 8}
	f.seq.Play(&u, 0)
	a, b := f.mix.segs[0].seg, f.mix.segs[1].seg
	st := f.seq.Status(a.Start + 10)
	if !st.Active || st.Index != 0 || st.Frag != 7 || st.Phrase != 2 || st.Variant != 1 || st.Priority != 3 {
		t.Errorf("first status = %+v", st)
	}
	st = f.seq.Status(b.Start + 10)
	if st.Index != 1 || st.Frag != 8 || st.Pos != PosEnd {
		t.Errorf("second status = %+v", st)
	}
	if st.Remaining != b.End()-(b.Start+10) {
		t.Errorf("remaining = %d", st.Remaining)
	}
	if st := f.seq.Status(b.End()); st.Active || st.Index != -1 {
		t.Errorf("status after end = %+v", st)
	}
}

type nopSink struct{ n int }

func (s *nopSink) BeginLine(uint64)         {}
func (s *nopSink) Schedule(uint64, Segment) { s.n++ }
func (s *nopSink) CutLine(uint64, int64, int32) {
	s.n++
}

func TestSequencerZeroAllocsPerEvent(t *testing.T) {
	c, lib := compileRacing(t)
	for id := FragID(1); int(id) <= lib.Len(); id++ {
		lib.Set(id, PosMid, Clip{Frames: 700, Handle: []float32(nil)})
	}
	sink := &nopSink{}
	seq := NewSequencer(lib, sink, SequencerConfig{})
	p := NewPlanner(c, PlannerConfig{Seed: 9})
	names := [...]string{"okafor", "vance", "reyes", "lindqvist"}
	var now int64
	i := 0
	allocs := testing.AllocsPerRun(400, func() {
		i++
		now += 90000
		var ev Event
		ev.Reset("lead_change")
		ev.Str("driver", names[i%4])
		d := p.Submit(&ev, float64(now)/testRate, seq.Playing(now))
		seq.Apply(&d, now)
		ev.Reset("crash")
		ev.Priority = 100 + i
		ev.Str("driver", names[(i+1)%4]).Str("corner", "chicane")
		d = p.Submit(&ev, float64(now)/testRate+0.01, seq.Playing(now+10))
		seq.Apply(&d, now+10)
		_ = seq.Status(now + 20)
	})
	if allocs != 0 {
		t.Errorf("event path allocates %v times per run", allocs)
	}
	if sink.n == 0 {
		t.Fatal("nothing was scheduled")
	}
}

func maxAbs(v []float32) float32 {
	var m float32
	for _, x := range v {
		if x < 0 {
			x = -x
		}
		if x > m {
			m = x
		}
	}
	return m
}

func TestSequencerShortClipsKeepComplementaryFades(t *testing.T) {
	c, lib := compileRacing(t)
	_ = c
	short := make([]float32, 300) // half is 150 frames, below the 8 ms crossfade (384)
	for i := range short {
		short[i] = 1
	}
	lib.Set(1, PosMid, Clip{Frames: 300, Handle: short})
	mix := &Mix{}
	seq := NewSequencer(lib, mix, SequencerConfig{})
	u := Utterance{N: 3}
	u.Frags[0], u.Frags[1], u.Frags[2] = FragRef{ID: 1}, FragRef{ID: 1}, FragRef{ID: 1}
	seq.Play(&u, 0)
	b := mix.segs[1].seg
	out := mix.Render(b.Start, int(b.FadeIn))
	for i, v := range out {
		if math.Abs(float64(v)-1) > 1e-6 {
			t.Fatalf("overlap sample %d = %v, want 1", i, v)
		}
	}
	if b.FadeIn != 150 {
		t.Errorf("short clip fade = %d, want 150", b.FadeIn)
	}
}
