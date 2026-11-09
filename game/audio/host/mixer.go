package audiohost

// MixerBus is one bus of a mixer that the page owns, such as the browser's
// window.__gosx.audioMixer. Sources connect to Input and never set its gain:
// the bus computes the gain from its volume, mute flag and active ducks.
type MixerBus interface {
	// Input is the node sources connect to.
	Input() Node
	Volume() float64
	SetVolume(volume float64)
	Muted() bool
	SetMuted(muted bool)
	// Duck lowers the bus to the linear factor amount over seconds. The key
	// names the caller; the lowest active factor wins, so two callers cannot
	// undo each other's duck.
	Duck(key string, amount, seconds float64)
	// Unduck removes key's duck and lets the bus recover over seconds.
	Unduck(key string, seconds float64)
}

// SharedMixer is an optional Host capability. A host that implements it
// shares one AudioContext and one set of buses with every other audio path in
// the page (clips, the synth, streamed media), so a volume set through any
// path reaches all of them.
type SharedMixer interface {
	// MixerBus returns the shared bus id, creating a custom bus under parent
	// (empty means master) on first use. The standard buses already exist.
	MixerBus(id, parent string) (MixerBus, bool)
}

// PannerNode is a 3D positional source node, a WebAudio PannerNode.
type PannerNode interface {
	Node
	// SetPosition moves the source in world units.
	SetPosition(x, y, z float64) error
	// Configure sets the distance model ("inverse", "linear" or
	// "exponential"), the reference and maximum distances, and the rolloff.
	Configure(distanceModel string, refDistance, maxDistance, rolloff float64) error
}

// SpatialHost is an optional Host capability for 3D positional audio.
type SpatialHost interface {
	CreatePanner() PannerNode
	// SetListener places the shared listener. Call it each frame with the
	// camera's position, forward direction and up vector.
	SetListener(position, forward, up [3]float64) error
}
