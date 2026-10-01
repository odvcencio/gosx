package audio

import "m31labs.dev/gosx/game"

// Bus is one named mixer bus: a gain node plus the volume/mute state that
// produced its current gain value, so SetVolume and SetMuted can be called
// independently without one clobbering the other.
type Bus struct {
	id     string
	gain   GainNode
	volume float64
	muted  bool
}

// ID returns the bus's manifest ID.
func (b *Bus) ID() string { return b.id }

// SetVolume changes the bus's volume in [0,1]. It has no audible effect
// while the bus is muted, but is remembered for when SetMuted(false) runs.
func (b *Bus) SetVolume(volume float64) {
	if b == nil {
		return
	}
	b.volume = volume
	b.apply()
}

// SetMuted mutes or unmutes the bus without discarding its volume.
func (b *Bus) SetMuted(muted bool) {
	if b == nil {
		return
	}
	b.muted = muted
	b.apply()
}

// Volume returns the bus's currently set volume, regardless of mute state.
func (b *Bus) Volume() float64 { return b.volume }

// Muted reports whether the bus is currently muted.
func (b *Bus) Muted() bool { return b.muted }

func (b *Bus) apply() {
	if b.muted {
		b.gain.SetGain(0)
		return
	}
	b.gain.SetGain(b.volume)
}

// BusGraph is a game's WebAudio bus tree: one master bus feeding a
// compressor feeding the host's destination, with named child buses from a
// game.AudioManifest connected either to master or to another named bus, per
// game.AudioBus.Parent.
type BusGraph struct {
	host       Host
	master     GainNode
	compressor CompressorNode
	buses      map[string]*Bus
}

// NewBusGraph builds the graph described by manifest against host.
func NewBusGraph(host Host, manifest game.AudioManifest) *BusGraph {
	g := &BusGraph{host: host, buses: make(map[string]*Bus, len(manifest.Buses))}

	g.master = host.CreateGain()
	g.master.SetGain(orOne(manifest.MasterVolume))
	g.compressor = host.CreateCompressor()
	// These defaults match a widely used game/music mastering compressor
	// shape: a gentle 3:1 ratio starting at -12dB, fast enough attack to
	// catch transients without audibly pumping, and a release long enough to
	// stay musical.
	g.compressor.Configure(-12, 3, 0.008, 0.2)
	g.master.Connect(g.compressor)
	g.compressor.Connect(host.Destination())

	for _, spec := range manifest.Buses {
		if spec.ID == "" {
			continue
		}
		gain := host.CreateGain()
		bus := &Bus{id: spec.ID, gain: gain, volume: orOne(spec.Volume), muted: spec.Muted}
		bus.apply()
		g.buses[spec.ID] = bus
	}
	for _, spec := range manifest.Buses {
		bus, ok := g.buses[spec.ID]
		if !ok {
			continue
		}
		var parent Node = g.master
		if spec.Parent != "" {
			if p, ok := g.buses[spec.Parent]; ok {
				parent = p.gain
			}
		}
		bus.gain.Connect(parent)
	}
	return g
}

// Bus returns the named bus, or nil if manifest did not declare it.
func (g *BusGraph) Bus(id string) *Bus {
	if g == nil {
		return nil
	}
	return g.buses[id]
}

// Master returns the master gain node, mainly for inspection in tests.
func (g *BusGraph) Master() GainNode { return g.master }

func orOne(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
