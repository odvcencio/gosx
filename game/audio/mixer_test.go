package audio

import (
	"testing"

	"m31labs.dev/gosx/game"
)

// fakeTargetGain records SetGainTarget calls so tests see click-free ducking.
type fakeTargetGain struct {
	*fakeGain
	targets []float64
	consts  []float64
}

func (g *fakeTargetGain) SetGainTarget(value, at, tc float64) error {
	g.targets = append(g.targets, value)
	g.consts = append(g.consts, tc)
	g.gain = value
	return nil
}

type targetHost struct{ *fakeHost }

func (h targetHost) CreateGain() GainNode {
	g := h.fakeHost.CreateGain().(*fakeGain)
	return &fakeTargetGain{fakeGain: g}
}

func TestMixerHasStandardBusesUnderMaster(t *testing.T) {
	host := newFakeHost()
	m := NewMixer(host)
	master := m.Bus(MasterBus)
	if master == nil {
		t.Fatal("no master bus")
	}
	for _, id := range StandardBuses()[1:] {
		b := m.Bus(id)
		if b == nil {
			t.Fatalf("missing bus %q", id)
		}
		g := b.gain.(*fakeGain)
		if len(g.connectedTo) != 1 || g.connectedTo[0] != master.gain.(*fakeGain).fakeNode {
			t.Errorf("bus %q does not feed master", id)
		}
	}
}

func TestMixerVolumeMuteAndSettingsRoundTrip(t *testing.T) {
	host := newFakeHost()
	m := NewMixer(host)
	m.SetVolume(MusicBus, 0.4)
	m.SetVolume(SFXBus, 0.7)
	m.SetMuted(VoiceBus, true)
	saved := m.Settings()
	if saved[MusicBus].Volume != 0.4 || saved[VoiceBus].Muted != true {
		t.Fatalf("settings = %+v", saved)
	}

	m2 := NewMixer(newFakeHost())
	applied := m2.ApplySettings(saved)
	if len(applied) != 5 {
		t.Fatalf("applied %v", applied)
	}
	if got := m2.Bus(MusicBus).gain.(*fakeGain).gain; got != 0.4 {
		t.Fatalf("restored music gain = %v", got)
	}
	if got := m2.Bus(VoiceBus).gain.(*fakeGain).gain; got != 0 {
		t.Fatalf("restored muted voice gain = %v, want 0", got)
	}
	if m2.SetVolume("nope", 1) {
		t.Fatal("unknown bus reported success")
	}
}

func TestMixerVolumeClampsToUnitRange(t *testing.T) {
	m := NewMixer(newFakeHost())
	m.SetVolume(UIBus, 3)
	if m.Volume(UIBus) != 1 {
		t.Fatalf("volume = %v", m.Volume(UIBus))
	}
	m.SetVolume(UIBus, -1)
	if m.Volume(UIBus) != 0 {
		t.Fatalf("volume = %v", m.Volume(UIBus))
	}
}

func TestMixerDuckLowestFactorWinsAndUnduckRestores(t *testing.T) {
	host := targetHost{newFakeHost()}
	m := NewMixer(host)
	m.SetVolume(MusicBus, 0.8)
	g := m.Bus(MusicBus).gain.(*fakeTargetGain)

	m.Duck(MusicBus, "voice-line", 0.5, 0.1)
	if g.gain != 0.4 {
		t.Fatalf("ducked gain = %v, want 0.4", g.gain)
	}
	m.Duck(MusicBus, "stinger", 0.25, 0.1)
	if g.gain != 0.2 {
		t.Fatalf("gain with two ducks = %v, want 0.2 (lowest wins)", g.gain)
	}
	m.Unduck(MusicBus, "stinger", 0.4)
	if g.gain != 0.4 {
		t.Fatalf("gain after releasing one duck = %v, want 0.4", g.gain)
	}
	if last := g.consts[len(g.consts)-1]; last <= 0 {
		t.Fatalf("release used time constant %v, want a smooth ramp", last)
	}
	m.Unduck(MusicBus, "voice-line", 0.4)
	if g.gain != 0.8 {
		t.Fatalf("gain after all ducks = %v, want 0.8", g.gain)
	}
	if m.Volume(MusicBus) != 0.8 {
		t.Fatal("ducking changed the stored volume")
	}
}

func TestMixerKeepsGameBusesFromManifest(t *testing.T) {
	m := NewMixer(newFakeHost(), game.AudioManifest{Buses: []game.AudioBus{
		{ID: "ambience", Parent: MusicBus, Volume: 0.3},
		{ID: SFXBus, Volume: 0.1}, // standard buses cannot be redefined
	}})
	if m.Bus("ambience") == nil || m.Volume("ambience") != 0.3 {
		t.Fatal("custom bus missing")
	}
	if m.Volume(SFXBus) != 1 {
		t.Fatalf("sfx volume = %v, want default 1", m.Volume(SFXBus))
	}
}

// sharedHost implements SharedMixer, as the browser host does.
type sharedBus struct {
	node          *fakeNode
	vol           float64
	muted         bool
	ducks         map[string]float64
	volumeSetters int
}

func (b *sharedBus) Input() Node         { return b.node }
func (b *sharedBus) Volume() float64     { return b.vol }
func (b *sharedBus) SetVolume(v float64) { b.vol = v; b.volumeSetters++ }
func (b *sharedBus) Muted() bool         { return b.muted }
func (b *sharedBus) SetMuted(m bool)     { b.muted = m }
func (b *sharedBus) Duck(k string, a, s float64) {
	if b.ducks == nil {
		b.ducks = map[string]float64{}
	}
	b.ducks[k] = a
}
func (b *sharedBus) Unduck(k string, s float64) { delete(b.ducks, k) }

type sharedHost struct {
	*fakeHost
	buses map[string]*sharedBus
}

func (h *sharedHost) MixerBus(id, parent string) (MixerBus, bool) {
	if h.buses == nil {
		h.buses = map[string]*sharedBus{}
	}
	b := h.buses[id]
	if b == nil {
		b = &sharedBus{node: &fakeNode{name: id}, vol: 1}
		h.buses[id] = b
	}
	return b, true
}

func TestMixerOnSharedHostUsesPageBusesAndNoPrivateGraph(t *testing.T) {
	host := &sharedHost{fakeHost: newFakeHost()}
	m := NewMixer(host)
	if host.nodes != 0 {
		t.Fatalf("shared mixer created %d private nodes", host.nodes)
	}
	m.SetVolume(MusicBus, 0.3)
	m.SetMuted(SFXBus, true)
	m.Duck(MusicBus, "v", 0.5, 0.1)
	if host.buses[MusicBus].vol != 0.3 || !host.buses[SFXBus].muted || host.buses[MusicBus].ducks["v"] != 0.5 {
		t.Fatal("page mixer did not receive state")
	}
	if m.Volume(MusicBus) != 0.3 || !m.Muted(SFXBus) {
		t.Fatal("reads do not reflect page mixer")
	}
	if m.Bus(MusicBus).Input() != Node(host.buses[MusicBus].node) {
		t.Fatal("bus input is not the page bus node")
	}
}
