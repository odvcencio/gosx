package audio

// Buffer is an opaque decoded audio buffer handle. Only Host implementations
// interpret it; BusGraph and Player pass it through unchanged.
type Buffer any

// Node is anything that can be wired into the graph and torn back out of it.
type Node interface {
	Connect(dst Node)
	Disconnect()
}

// GainNode is a single scalar volume control — a WebAudio GainNode.
type GainNode interface {
	Node
	SetGain(value float64)
}

// CompressorNode is a WebAudio DynamicsCompressorNode with the handful of
// parameters a game bus needs.
type CompressorNode interface {
	Node
	Configure(threshold, ratio, attack, release float64)
}

// SourceNode is one playback of one Buffer — a WebAudio
// AudioBufferSourceNode. A SourceNode is single-use: once started and
// stopped, or once it reaches its natural end, it cannot be restarted.
type SourceNode interface {
	Node
	SetLoop(loop bool)
	SetPlaybackRate(rate float64)
	// Start schedules playback to begin at host audio-clock time at, in the
	// same units as Host.CurrentTime.
	Start(at float64)
	// Stop schedules playback to end at host audio-clock time at.
	Stop(at float64)
	// OnEnded registers fn to run once, at the node's natural or
	// Stop-scheduled end.
	OnEnded(fn func())
}

// Host is the WebAudio surface BusGraph and Player need. HostJS is the real
// browser implementation; tests use a fake.
type Host interface {
	// CurrentTime returns the audio clock, in seconds.
	CurrentTime() float64
	// State returns "suspended", "running", or "closed".
	State() string
	// Resume asks a suspended context to start running. Call it from inside
	// a user gesture handler (Player.Unlock does this).
	Resume() error
	CreateGain() GainNode
	CreateCompressor() CompressorNode
	// CreateSource creates a one-use playback node for buffer.
	CreateSource(buffer Buffer) SourceNode
	// Destination returns the context's final output node.
	Destination() Node
	// DecodeAudioData decodes an encoded audio file's bytes into a Buffer.
	DecodeAudioData(data []byte) (Buffer, error)
}
