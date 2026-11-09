package audio

import (
	"sort"

	"m31labs.dev/gosx/game"
)

// The standard bus names every GoSX game mixer has. Clips and cues without a
// bus play on master. Music, sfx, voice and ui each feed master.
const (
	MasterBus = "master"
	MusicBus  = "music"
	SFXBus    = "sfx"
	VoiceBus  = "voice"
	UIBus     = "ui"
)

// StandardBuses lists the standard buses in a stable order, master first.
func StandardBuses() []string {
	return []string{MasterBus, MusicBus, SFXBus, VoiceBus, UIBus}
}

// StandardManifest returns an AudioManifest with the five standard buses at
// full volume. Add clips to it and pass it to Player.LoadManifest.
func StandardManifest() game.AudioManifest {
	return game.AudioManifest{Buses: []game.AudioBus{
		{ID: MusicBus, Parent: MasterBus},
		{ID: SFXBus, Parent: MasterBus},
		{ID: VoiceBus, Parent: MasterBus},
		{ID: UIBus, Parent: MasterBus},
	}}
}

// Mixer is the game's one mixer: the standard buses plus any the game adds.
// Build it once with NewMixer and give the same Mixer to the Player, the SFX
// player and the Cicada music host. On the browser host it shares the page's
// AudioContext and buses with clips played through window.__gosx.audio, the
// arcade synth (scene.AudioCue) and streamed audio, so a volume set here
// reaches all of them.
type Mixer struct {
	host  Host
	graph *BusGraph
}

// NewMixer builds a mixer on host. extra may add game-specific buses and
// clips; the standard buses are always present.
func NewMixer(host Host, extra ...game.AudioManifest) *Mixer {
	manifest := StandardManifest()
	for _, m := range extra {
		if m.MasterVolume != 0 {
			manifest.MasterVolume = m.MasterVolume
		}
		for _, b := range m.Buses {
			if isStandardBus(b.ID) {
				continue
			}
			manifest.Buses = append(manifest.Buses, b)
		}
	}
	return &Mixer{host: host, graph: NewBusGraph(host, manifest)}
}

func isStandardBus(id string) bool {
	for _, s := range StandardBuses() {
		if s == id {
			return true
		}
	}
	return false
}

// Host returns the host the mixer was built on.
func (m *Mixer) Host() Host { return m.host }

// Graph returns the underlying bus graph, for NewPlayer.
func (m *Mixer) Graph() *BusGraph { return m.graph }

// Bus returns the named bus, or nil if the mixer lacks it.
func (m *Mixer) Bus(id string) *Bus { return m.graph.Bus(id) }

// SetVolume sets bus id's volume in [0,1]. It reports false for an unknown bus.
func (m *Mixer) SetVolume(id string, volume float64) bool {
	b := m.graph.Bus(id)
	if b == nil {
		return false
	}
	b.SetVolume(volume)
	return true
}

// Volume returns bus id's volume, or 0 for an unknown bus.
func (m *Mixer) Volume(id string) float64 {
	if b := m.graph.Bus(id); b != nil {
		return b.Volume()
	}
	return 0
}

// SetMuted mutes or unmutes bus id. It reports false for an unknown bus.
func (m *Mixer) SetMuted(id string, muted bool) bool {
	b := m.graph.Bus(id)
	if b == nil {
		return false
	}
	b.SetMuted(muted)
	return true
}

// Muted reports whether bus id is muted.
func (m *Mixer) Muted(id string) bool {
	if b := m.graph.Bus(id); b != nil {
		return b.Muted()
	}
	return false
}

// Duck lowers bus id to the linear factor amount over seconds under key. Use
// it to pull music down while a voice line plays; call Unduck with the same key.
func (m *Mixer) Duck(id, key string, amount, seconds float64) {
	m.graph.Bus(id).Duck(key, amount, seconds)
}

// Unduck lets bus id recover from key's duck over seconds.
func (m *Mixer) Unduck(id, key string, seconds float64) {
	m.graph.Bus(id).Unduck(key, seconds)
}

// BusSettings is one bus's persistable state.
type BusSettings struct {
	Volume float64 `json:"volume"`
	Muted  bool    `json:"muted,omitempty"`
}

// Settings returns every bus's volume and mute flag, keyed by bus ID. It
// marshals to JSON, so a settings screen can persist it.
func (m *Mixer) Settings() map[string]BusSettings {
	out := make(map[string]BusSettings, len(m.graph.buses))
	for id, b := range m.graph.buses {
		out[id] = BusSettings{Volume: b.Volume(), Muted: b.Muted()}
	}
	return out
}

// ApplySettings restores saved settings. Unknown buses are skipped, and the
// IDs it applied are returned in sorted order.
func (m *Mixer) ApplySettings(settings map[string]BusSettings) []string {
	var applied []string
	for id, s := range settings {
		b := m.graph.Bus(id)
		if b == nil {
			continue
		}
		b.SetVolume(s.Volume)
		b.SetMuted(s.Muted)
		applied = append(applied, id)
	}
	sort.Strings(applied)
	return applied
}
