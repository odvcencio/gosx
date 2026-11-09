package voice

import (
	"math"
	"sort"
	"testing"

	"m31labs.dev/gosx/game/audio"
	"m31labs.dev/gosx/game/audio/speech"
)

const rate = 48000

// A small model of WebAudio, enough to render what Player schedules: buffer
// sources with regions, gain nodes with SetAt and LinearRamp automation, and
// summing at the destination.

type node struct {
	m    *model
	name string
	to   []*node
	disc int
}

func (n *node) Connect(dst audio.Node) {
	switch d := dst.(type) {
	case *gain:
		n.to = append(n.to, d.node)
	case *comp:
		n.to = append(n.to, d.node)
	case *node:
		n.to = append(n.to, d)
	}
}
func (n *node) Disconnect() { n.disc++ }

type event struct {
	ramp  bool
	value float64
	at    float64
}

type gain struct {
	*node
	initial float64
	events  []event
}

func (g *gain) SetGain(v float64)     { g.initial = v }
func (g *gain) Gain() audio.Parameter { return (*param)(g) }
func (g *gain) at(t float64) float64 {
	evs := append([]event(nil), g.events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].at < evs[j].at })
	pv, pt := g.initial, math.Inf(-1)
	for i, e := range evs {
		if e.at <= t {
			pv, pt = e.value, e.at
			continue
		}
		if e.ramp {
			if math.IsInf(pt, -1) {
				pt = 0
				if i > 0 {
					pt = evs[i-1].at
				}
			}
			return pv + (e.value-pv)*(t-pt)/(e.at-pt)
		}
		break
	}
	return pv
}

type param gain

func (p *param) SetAt(v, at float64) error {
	p.events = append(p.events, event{value: v, at: at})
	return nil
}
func (p *param) LinearRamp(v, at float64) error {
	p.events = append(p.events, event{ramp: true, value: v, at: at})
	return nil
}
func (p *param) ExponentialRamp(v, at float64) error { return nil }
func (p *param) HoldAt(at float64) error             { return nil }

type source struct {
	*node
	buf        *atlasBuf
	start, end float64
	offset     float64
	started    bool
	dest       *gain
}

func (s *source) SetLoop(bool)            {}
func (s *source) SetPlaybackRate(float64) {}
func (s *source) Start(at float64)        { s.start, s.started = at, true }
func (s *source) Stop(at float64)         { s.end = at }
func (s *source) OnEnded(func())          {}
func (s *source) StartRegion(at, offset, duration float64) error {
	s.start, s.offset, s.started = at, offset, true
	s.end = at + duration
	return nil
}
func (s *source) SetLoopRegion(a, b float64) error { return nil }

type atlasBuf struct{ pcm []float32 }

type model struct {
	now     float64
	gains   []*gain
	sources []*source
	dest    *node
	// feeds maps a node to the gain it connects into, built by walk.
}

func (m *model) CurrentTime() float64 { return m.now }
func (m *model) State() string        { return "running" }
func (m *model) Resume() error        { return nil }
func (m *model) CreateGain() audio.GainNode {
	g := &gain{node: &node{m: m, name: "gain"}, initial: 1}
	m.gains = append(m.gains, g)
	return g
}
type comp struct{ *node }

func (c *comp) Configure(a, b, d, e float64) {}

func (m *model) CreateCompressor() audio.CompressorNode { return &comp{node: &node{m: m, name: "comp"}} }
func (m *model) CreateSource(b audio.Buffer) audio.SourceNode {
	s := &source{node: &node{m: m, name: "source"}, buf: b.(*atlasBuf)}
	m.sources = append(m.sources, s)
	return s
}
func (m *model) Destination() audio.Node { return m.dest }
func (m *model) DecodeAudioData(data []byte) (audio.Buffer, error) {
	pcm := make([]float32, len(data))
	for i, b := range data {
		pcm[i] = float32(b) / 255
	}
	return &atlasBuf{pcm: pcm}, nil
}

// downstream follows connections from n to the destination bus and returns
// the product of gains along the way, evaluated at t, plus whether the path
// reaches busIn.
func (m *model) gainTo(n *node, busIn *node, t float64) (float64, bool) {
	if n == busIn {
		return 1, true
	}
	for _, next := range n.to {
		g := 1.0
		for _, gg := range m.gains {
			if gg.node == next {
				g = gg.at(t)
			}
		}
		if rest, ok := m.gainTo(next, busIn, t); ok {
			return g * rest, true
		}
	}
	return 0, false
}

func (m *model) render(busIn *node, from int64, n int) []float32 {
	out := make([]float32, n)
	for _, s := range m.sources {
		if !s.started {
			continue
		}
		for f := from; f < from+int64(n); f++ {
			t := float64(f) / rate
			if t < s.start || t >= s.end {
				continue
			}
			idx := int(math.Round(s.offset*rate)) + int(f-int64(math.Round(s.start*rate)))
			if idx < 0 || idx >= len(s.buf.pcm) {
				continue
			}
			g := 1.0
			if sg, ok := m.sourceGain(s); ok {
				g = sg.at(t)
			}
			rest, ok := m.gainTo(s.node.to[0], busIn, t)
			if !ok {
				continue
			}
			out[f-from] += s.buf.pcm[idx] * float32(g*rest)
		}
	}
	return out
}

func (m *model) sourceGain(s *source) (*gain, bool) {
	for _, g := range m.gains {
		if len(s.to) > 0 && g.node == s.to[0] {
			return g, true
		}
	}
	return nil, false
}

// fixture: a library of constant-valued clips in one atlas.
type fixture struct {
	m      *model
	mixer  *audio.Mixer
	lib    *speech.Library
	player *Player
	ref    *speech.Mix
	refSeq *speech.Sequencer
}

func newFixture(t *testing.T, cfg Config) *fixture {
	t.Helper()
	m := &model{dest: &node{name: "dest"}}
	mixer := audio.NewMixer(m)
	g, err := speech.ParseGrammar([]byte(`{"pools":{"d":["a","b","c"]},"phrases":[{"id":"x","event":"x","variants":[{"say":"{d} one {d}"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	lib := speech.NewLibrary(rate)
	if _, err := g.Compile(lib); err != nil {
		t.Fatal(err)
	}
	// Atlas: 5 clips of 1200 frames, each a constant byte value.
	const n = 1200
	atlas := make([]byte, 0, 6*n)
	var entries []speech.ClipEntry
	keys := []string{"pool/d/a", "pool/d/b", "pool/d/c", "lit/one"}
	for i, k := range keys {
		for f := 0; f < n; f++ {
			atlas = append(atlas, 255)
		}
		for _, v := range []string{"start", "mid", "end"} {
			entries = append(entries, speech.ClipEntry{Key: k, Variant: v, Offset: i * n, Frames: n})
		}
	}
	man := &speech.ClipManifest{Version: 1, Rate: rate, Atlases: []speech.Atlas{{File: "a.wav", Voice: "v", Frames: len(atlas)}}, Clips: entries}
	attached, err := LoadClips(m, lib, man, func(string) ([]byte, error) { return atlas, nil })
	if err != nil || attached != len(entries) {
		t.Fatalf("LoadClips: %d, %v", attached, err)
	}
	p, err := New(mixer, lib, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ref := &speech.Mix{}
	// Reference clips carry float32 data.
	ones := make([]float32, n)
	for i := range ones {
		ones[i] = 1
	}
	rlib := speech.NewLibrary(rate)
	if _, err := g.Compile(rlib); err != nil {
		t.Fatal(err)
	}
	for id := speech.FragID(1); int(id) <= rlib.Len(); id++ {
		for _, pos := range []speech.Pos{speech.PosStart, speech.PosMid, speech.PosEnd} {
			rlib.Set(id, pos, speech.Clip{Frames: n, Handle: ones})
		}
	}
	return &fixture{m: m, mixer: mixer, lib: lib, player: p, ref: ref, refSeq: speech.NewSequencer(rlib, ref, cfg.SequencerConfig)}
}

func (f *fixture) utt(ids ...string) speech.Utterance {
	u := speech.Utterance{Interruptible: true}
	for _, k := range ids {
		id, ok := f.lib.Lookup(k)
		if !ok {
			panic(k)
		}
		u.Frags[u.N] = speech.FragRef{ID: id}
		u.N++
	}
	return u
}

func TestPlayerMatchesReferenceMix(t *testing.T) {
	f := newFixture(t, Config{SequencerConfig: speech.SequencerConfig{CrossfadeMs: 4, GapMs: 0}})
	u := f.utt("pool/d/a", "lit/one", "pool/d/b")
	f.m.now = 0.5
	if !f.player.Say(&speech.Decision{Action: speech.Play, Utt: u}) {
		t.Fatal("Say refused")
	}
	f.refSeq.Play(&u, f.player.Frame())
	from := f.player.Frame()
	n := 4000
	got := f.m.render(f.player.bus.Input().(*gain).node, from, n)
	want := f.ref.Render(from, n)
	var worst float64
	for i := range got {
		worst = math.Max(worst, math.Abs(float64(got[i]-want[i])))
	}
	if worst > 1e-3 {
		t.Errorf("model render differs from the reference mix by %g", worst)
	}
	if maxAbs(got) == 0 {
		t.Fatal("rendered silence")
	}
}

func TestPlayerInterruptMatchesReference(t *testing.T) {
	cfg := Config{SequencerConfig: speech.SequencerConfig{CrossfadeMs: 4, CutFadeMs: 6}}
	f := newFixture(t, cfg)
	f.m.now = 1
	long := f.utt("pool/d/a", "lit/one", "pool/d/b")
	long.Priority = 1
	f.player.Say(&speech.Decision{Action: speech.Play, Utt: long})
	f.refSeq.Play(&long, f.player.Frame())
	f.m.now = 1.012
	urgent := f.utt("pool/d/c")
	urgent.Priority = 9
	if !f.player.Say(&speech.Decision{Action: speech.Interrupt, Utt: urgent}) {
		t.Fatal("interrupt refused")
	}
	f.refSeq.Interrupt(&urgent, f.player.Frame())
	from := int64(1 * rate)
	got := f.m.render(f.player.bus.Input().(*gain).node, from, 6000)
	want := f.ref.Render(from, 6000)
	var worst float64
	for i := range got {
		worst = math.Max(worst, math.Abs(float64(got[i]-want[i])))
	}
	if worst > 1e-3 {
		t.Errorf("interrupted render differs from the reference by %g", worst)
	}
	// The cut really fades: some sample of the old line lies strictly between 0 and 1.
	mid := false
	for _, v := range got {
		if v > 0.05 && v < 0.95 {
			mid = true
		}
	}
	if !mid {
		t.Error("no faded samples in the interrupted render")
	}
}

func TestPlayerDucksAndRecovers(t *testing.T) {
	f := newFixture(t, Config{})
	u := f.utt("pool/d/a")
	f.m.now = 0.1
	f.player.Say(&speech.Decision{Action: speech.Play, Utt: u})
	if !f.player.ducking {
		t.Fatal("no duck while a line is queued to play")
	}
	f.player.Update()
	if !f.player.ducking {
		t.Fatal("duck ended while the line is on air")
	}
	f.m.now = 0.1 + 0.2 // clip is 25 ms; the line is over
	f.player.Update()
	if f.player.ducking {
		t.Error("duck never ended")
	}
	for _, s := range f.m.sources {
		if s.disc == 0 {
			t.Error("a finished source was not disconnected")
		}
	}
}

func TestPlayerRejectsUnloadedClips(t *testing.T) {
	f := newFixture(t, Config{})
	u := speech.Utterance{N: 1}
	u.Frags[0] = speech.FragRef{ID: 1}
	f.player.BeginLine(99)
	f.player.Schedule(99, speech.Segment{Frag: 1, Clip: speech.Clip{Frames: 10}})
	if f.player.Errors == 0 {
		t.Error("a segment without a Region handle was not reported")
	}
	_ = u
}

func TestNewNeedsVoiceBus(t *testing.T) {
	if _, err := New(audio.NewMixer(&model{dest: &node{}}), speech.NewLibrary(rate), Config{}); err != nil {
		t.Fatalf("standard mixer: %v", err)
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
