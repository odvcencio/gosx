package audio

import "m31labs.dev/gosx/game"

// Bus is one named mixer bus. Its audible gain is volume * duck, or 0 while
// muted, so SetVolume, SetMuted and Duck can be called independently without
// one clobbering another. On a host with a shared mixer the bus is the page
// mixer's bus, and its state lives there.
type Bus struct {
	id     string
	host   Host
	gain   GainNode // local buses only
	shared MixerBus // set when the page mixer owns the bus
	volume float64
	muted  bool
	ducks  map[string]float64
}

// ID returns the bus's manifest ID.
func (b *Bus) ID() string { return b.id }

// Input is the node sources connect to.
func (b *Bus) Input() Node {
	if b.shared != nil {
		return b.shared.Input()
	}
	return b.gain
}

// SetVolume changes the bus's volume in [0,1]. It has no audible effect
// while the bus is muted, but is remembered for when SetMuted(false) runs.
func (b *Bus) SetVolume(volume float64) {
	if b == nil {
		return
	}
	volume = clamp01(volume)
	if b.shared != nil {
		b.shared.SetVolume(volume)
		return
	}
	b.volume = volume
	b.apply(0)
}

// SetMuted mutes or unmutes the bus without discarding its volume.
func (b *Bus) SetMuted(muted bool) {
	if b == nil {
		return
	}
	if b.shared != nil {
		b.shared.SetMuted(muted)
		return
	}
	b.muted = muted
	b.apply(0)
}

// Duck lowers the bus to the linear factor amount in [0,1] over seconds, for
// example to make room for a voice line. key names the caller: the lowest
// active factor wins, and Unduck(key) removes only that caller's duck.
func (b *Bus) Duck(key string, amount, seconds float64) {
	if b == nil {
		return
	}
	amount = clamp01(amount)
	if b.shared != nil {
		b.shared.Duck(key, amount, seconds)
		return
	}
	if b.ducks == nil {
		b.ducks = make(map[string]float64)
	}
	b.ducks[key] = amount
	b.apply(seconds)
}

// Unduck removes key's duck; the bus recovers over seconds.
func (b *Bus) Unduck(key string, seconds float64) {
	if b == nil {
		return
	}
	if b.shared != nil {
		b.shared.Unduck(key, seconds)
		return
	}
	delete(b.ducks, key)
	b.apply(seconds)
}

// Volume returns the bus's currently set volume, regardless of mute state.
func (b *Bus) Volume() float64 {
	if b.shared != nil {
		return b.shared.Volume()
	}
	return b.volume
}

// Muted reports whether the bus is currently muted.
func (b *Bus) Muted() bool {
	if b.shared != nil {
		return b.shared.Muted()
	}
	return b.muted
}

// Gain returns the gain applied to the bus now: volume times the lowest duck
// factor, or 0 while muted. On a shared bus it reports volume and mute only;
// ducks live in the page mixer.
func (b *Bus) Gain() float64 {
	if b.shared != nil {
		if b.shared.Muted() {
			return 0
		}
		return b.shared.Volume()
	}
	return b.effective()
}

func (b *Bus) effective() float64 {
	if b.muted {
		return 0
	}
	duck := 1.0
	for _, d := range b.ducks {
		if d < duck {
			duck = d
		}
	}
	return b.volume * duck
}

// apply writes the effective gain. A positive seconds uses the host's
// click-free gain automation when it has any.
func (b *Bus) apply(seconds float64) {
	value := b.effective()
	if seconds > 0 && b.host != nil {
		// A time constant of seconds/3 reaches 95% of the target in seconds.
		if err := SetGainTarget(b.gain, value, b.host.CurrentTime(), seconds/3); err == nil {
			return
		}
	}
	b.gain.SetGain(value)
}

func clamp01(v float64) float64 {
	if v != v || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// BusGraph is a game's WebAudio bus tree: one master bus feeding a
// compressor feeding the host's destination, with named child buses from a
// game.AudioManifest connected either to master or to another named bus, per
// game.AudioBus.Parent. On a host that implements SharedMixer the graph is the
// page mixer's buses instead, and the manifest's volumes seed them.
type BusGraph struct {
	host       Host
	master     GainNode
	compressor CompressorNode
	buses      map[string]*Bus
}

// NewBusGraph builds the graph described by manifest against host. The master
// bus is available as Bus("master").
func NewBusGraph(host Host, manifest game.AudioManifest) *BusGraph {
	g := &BusGraph{host: host, buses: make(map[string]*Bus, len(manifest.Buses)+1)}
	if shared, ok := host.(SharedMixer); ok {
		if g.buildShared(shared, manifest) {
			return g
		}
	}

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
	g.buses[MasterBus] = &Bus{id: MasterBus, host: host, gain: g.master, volume: orOne(manifest.MasterVolume)}

	for _, spec := range manifest.Buses {
		if spec.ID == "" || spec.ID == MasterBus {
			continue
		}
		gain := host.CreateGain()
		bus := &Bus{id: spec.ID, host: host, gain: gain, volume: orOne(spec.Volume), muted: spec.Muted}
		bus.apply(0)
		g.buses[spec.ID] = bus
	}
	for _, spec := range manifest.Buses {
		bus, ok := g.buses[spec.ID]
		if !ok || spec.ID == MasterBus {
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

// buildShared wires the graph to the page mixer. It reports false when the
// mixer is unavailable, so the caller builds a private graph.
func (g *BusGraph) buildShared(shared SharedMixer, manifest game.AudioManifest) bool {
	master, ok := shared.MixerBus(MasterBus, "")
	if !ok {
		return false
	}
	g.buses[MasterBus] = &Bus{id: MasterBus, host: g.host, shared: master}
	if manifest.MasterVolume != 0 {
		master.SetVolume(manifest.MasterVolume)
	}
	for _, spec := range manifest.Buses {
		if spec.ID == "" || spec.ID == MasterBus {
			continue
		}
		mb, ok := shared.MixerBus(spec.ID, spec.Parent)
		if !ok {
			continue
		}
		if spec.Volume != 0 {
			mb.SetVolume(spec.Volume)
		}
		if spec.Muted {
			mb.SetMuted(true)
		}
		g.buses[spec.ID] = &Bus{id: spec.ID, host: g.host, shared: mb}
	}
	return true
}

// Bus returns the named bus, or nil if the graph does not have it.
func (g *BusGraph) Bus(id string) *Bus {
	if g == nil {
		return nil
	}
	return g.buses[id]
}

// Master returns the master gain node, mainly for inspection in tests. It is
// nil when the page mixer owns the master bus.
func (g *BusGraph) Master() GainNode { return g.master }

// MasterInput is the node that feeds the master bus.
func (g *BusGraph) MasterInput() Node {
	if b := g.buses[MasterBus]; b != nil {
		return b.Input()
	}
	return nil
}

func orOne(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
