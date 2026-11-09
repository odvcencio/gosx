package voice

import (
	"errors"
	"fmt"

	"m31labs.dev/gosx/game/audio"
	"m31labs.dev/gosx/game/audio/speech"
)

// Region is the Clip.Handle LoadClips stores: a span of a decoded atlas.
type Region struct {
	Buffer audio.Buffer
	// Offset and Duration are in seconds of the atlas.
	Offset, Duration float64
}

// DuckTarget names a bus to lower while speech plays.
type DuckTarget struct {
	Bus    string
	Amount float64 // linear factor in [0,1]
}

// Config tunes a Player. Zero fields take defaults.
type Config struct {
	speech.SequencerConfig
	// Duck lists the buses lowered while a line plays. Default: music to 0.4
	// and sfx to 0.7. Use a non-nil empty slice to duck nothing.
	Duck []DuckTarget
	// DuckSeconds and RecoverSeconds are the ramp times. Defaults 0.08 and 0.5.
	DuckSeconds, RecoverSeconds float64
	// DuckKey names the duck, so it never clobbers another caller's duck.
	// Default "speech".
	DuckKey string
}

type liveSeg struct {
	line   uint64
	source audio.SourceNode
	gain   audio.GainNode
	endSec float64
	used   bool
}

type liveLine struct {
	id     uint64
	gain   audio.GainNode
	endSec float64
	used   bool
}

const (
	maxLiveSegs  = 96
	maxLiveLines = 6
)

// Player drives a Sequencer onto the mixer's voice bus.
type Player struct {
	mixer *audio.Mixer
	host  audio.Host
	bus   *audio.Bus
	rate  float64
	seq   *speech.Sequencer
	cfg   Config

	segs    [maxLiveSegs]liveSeg
	lines   [maxLiveLines]liveLine
	ducking bool
	// Errors counts scheduling calls the host refused (for example a source
	// that cannot play a region). The line still plays what it can.
	Errors int
}

// New builds a Player. lib's clips must carry Region handles (LoadClips).
func New(mixer *audio.Mixer, lib *speech.Library, cfg Config) (*Player, error) {
	bus := mixer.Bus(audio.VoiceBus)
	if bus == nil {
		return nil, errors.New("voice: the mixer has no voice bus")
	}
	if cfg.Duck == nil {
		cfg.Duck = []DuckTarget{{audio.MusicBus, 0.4}, {audio.SFXBus, 0.7}}
	}
	if cfg.DuckSeconds == 0 {
		cfg.DuckSeconds = 0.08
	}
	if cfg.RecoverSeconds == 0 {
		cfg.RecoverSeconds = 0.5
	}
	if cfg.DuckKey == "" {
		cfg.DuckKey = "speech"
	}
	p := &Player{mixer: mixer, host: mixer.Host(), bus: bus, rate: float64(lib.Rate()), cfg: cfg}
	p.seq = speech.NewSequencer(lib, p, cfg.SequencerConfig)
	return p, nil
}

// Sequencer returns the underlying sequencer for Status and Stats.
func (p *Player) Sequencer() *speech.Sequencer { return p.seq }

// Frame is the current audio-clock time in sample frames.
func (p *Player) Frame() int64 { return int64(p.host.CurrentTime()*p.rate + 0.5) }

// Playing reports what the voice is doing now, for Planner.Submit.
func (p *Player) Playing() speech.Playing { return p.seq.Playing(p.Frame()) }

// Status reports the line on air.
func (p *Player) Status() speech.Status { return p.seq.Status(p.Frame()) }

// Say carries out a planner decision now. It reports whether a line started.
func (p *Player) Say(d *speech.Decision) bool { return p.seq.Apply(d, p.Frame()) }

// Update releases finished nodes and ends the duck when the line is over.
// Call it once per frame.
func (p *Player) Update() {
	now := p.host.CurrentTime()
	for i := range p.segs {
		s := &p.segs[i]
		if s.used && now >= s.endSec {
			s.source.Disconnect()
			s.gain.Disconnect()
			*s = liveSeg{}
		}
	}
	for i := range p.lines {
		l := &p.lines[i]
		if l.used && now >= l.endSec {
			l.gain.Disconnect()
			*l = liveLine{}
		}
	}
	if p.ducking && !p.Playing().Active {
		p.ducking = false
		for _, d := range p.cfg.Duck {
			p.mixer.Unduck(d.Bus, p.cfg.DuckKey, p.cfg.RecoverSeconds)
		}
	}
}

func (p *Player) sec(frame int64) float64 { return float64(frame) / p.rate }

// BeginLine implements speech.Sink.
func (p *Player) BeginLine(id uint64) {
	slot := p.freeLine()
	g := p.host.CreateGain()
	g.Connect(p.bus.Input())
	p.lines[slot] = liveLine{id: id, gain: g, used: true}
	if !p.ducking {
		p.ducking = true
		for _, d := range p.cfg.Duck {
			p.mixer.Duck(d.Bus, p.cfg.DuckKey, d.Amount, p.cfg.DuckSeconds)
		}
	}
}

func (p *Player) freeLine() int {
	oldest := 0
	for i := range p.lines {
		if !p.lines[i].used {
			return i
		}
		if p.lines[i].id < p.lines[oldest].id {
			oldest = i
		}
	}
	// Reclaim the oldest line; its sounds are long finished or cut.
	l := &p.lines[oldest]
	l.gain.Disconnect()
	*l = liveLine{}
	return oldest
}

func (p *Player) freeSeg() int {
	oldest := 0
	for i := range p.segs {
		if !p.segs[i].used {
			return i
		}
		if p.segs[i].endSec < p.segs[oldest].endSec {
			oldest = i
		}
	}
	s := &p.segs[oldest]
	s.source.Disconnect()
	s.gain.Disconnect()
	*s = liveSeg{}
	return oldest
}

func (p *Player) line(id uint64) *liveLine {
	for i := range p.lines {
		if p.lines[i].used && p.lines[i].id == id {
			return &p.lines[i]
		}
	}
	return nil
}

// Schedule implements speech.Sink.
func (p *Player) Schedule(id uint64, seg speech.Segment) {
	ln := p.line(id)
	region, ok := seg.Clip.Handle.(Region)
	if ln == nil || !ok {
		p.Errors++
		return
	}
	src := p.host.CreateSource(region.Buffer)
	g := p.host.CreateGain()
	src.Connect(g)
	g.Connect(ln.gain)
	t0, t1 := p.sec(seg.Start), p.sec(seg.End())
	if ag, ok := g.(audio.AutomatedGainNode); ok {
		par := ag.Gain()
		if seg.FadeIn > 0 {
			g.SetGain(0)
			p.check(par.SetAt(0, t0))
			p.check(par.LinearRamp(1, t0+p.sec(int64(seg.FadeIn))))
		}
		if seg.FadeOut > 0 {
			p.check(par.SetAt(1, t1-p.sec(int64(seg.FadeOut))))
			p.check(par.LinearRamp(0, t1))
		}
	}
	p.check(audio.StartRegion(src, t0, region.Offset, region.Duration))
	src.Stop(t1)
	if t1 > ln.endSec {
		ln.endSec = t1
	}
	slot := p.freeSeg()
	p.segs[slot] = liveSeg{line: id, source: src, gain: g, endSec: t1 + 0.05, used: true}
}

func (p *Player) check(err error) {
	if err != nil {
		p.Errors++
	}
}

// CutLine implements speech.Sink.
func (p *Player) CutLine(id uint64, at int64, fade int32) {
	ln := p.line(id)
	if ln == nil {
		return
	}
	t, tEnd := p.sec(at), p.sec(at+int64(fade))
	if ag, ok := ln.gain.(audio.AutomatedGainNode); ok {
		par := ag.Gain()
		p.check(par.SetAt(1, t))
		p.check(par.LinearRamp(0, tEnd))
	}
	ln.endSec = tEnd + 0.05
	for i := range p.segs {
		s := &p.segs[i]
		if s.used && s.line == id {
			s.source.Stop(tEnd)
			s.endSec = tEnd + 0.05
		}
	}
}

// LoadClips decodes each atlas in m through the host and attaches every clip
// the library's keys ask for. read returns an atlas file's bytes; in the
// browser it wraps fetch. Keys the grammar never interned are ignored.
func LoadClips(host audio.Host, lib *speech.Library, m *speech.ClipManifest, read func(file string) ([]byte, error)) (attached int, err error) {
	buffers := make([]audio.Buffer, len(m.Atlases))
	for i, a := range m.Atlases {
		data, err := read(a.File)
		if err != nil {
			return 0, fmt.Errorf("voice: read atlas %q: %w", a.File, err)
		}
		if buffers[i], err = host.DecodeAudioData(data); err != nil {
			return 0, fmt.Errorf("voice: decode atlas %q: %w", a.File, err)
		}
	}
	rate := float64(m.Rate)
	attached, _, err = lib.Attach(m, func(a *speech.Atlas, c *speech.ClipEntry) any {
		return Region{Buffer: buffers[atlasIndex(m, a)], Offset: float64(c.Offset) / rate, Duration: float64(c.Frames) / rate}
	})
	return attached, err
}

func atlasIndex(m *speech.ClipManifest, a *speech.Atlas) int {
	for i := range m.Atlases {
		if &m.Atlases[i] == a {
			return i
		}
	}
	return 0
}
