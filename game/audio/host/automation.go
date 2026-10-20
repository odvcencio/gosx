package audiohost

// Parameter is a scalar audio control scheduled on the host's audio clock.
// Times are absolute seconds. Ramps end at the supplied time; HoldAt preserves
// the value of a running ramp while cancelling its future automation.
type Parameter interface {
	SetAt(value, at float64) error
	LinearRamp(value, at float64) error
	ExponentialRamp(value, at float64) error
	HoldAt(at float64) error
}

// AutomatedGainNode adds sample-accurate envelopes to a mixer gain.
type AutomatedGainNode interface {
	GainNode
	Gain() Parameter
}

// ScheduledSource owns one playback and its ended callback. Clearing the
// callback or disconnecting the source releases callback registrations.
type ScheduledSource interface {
	Node
	Start(at float64)
	Stop(at float64)
	OnEnded(func())
}

// Waveform selects a built-in oscillator shape.
type Waveform string

const (
	Sine     Waveform = "sine"
	Square   Waveform = "square"
	Sawtooth Waveform = "sawtooth"
	Triangle Waveform = "triangle"
)

// OscillatorNode is a one-use procedural voice with scheduled frequency.
type OscillatorNode interface {
	ScheduledSource
	Frequency() Parameter
	SetWaveform(Waveform) error
}

// StereoPannerNode positions a signal between its left and right channels.
type StereoPannerNode interface {
	Node
	SetPan(float64) error
}

// DelayNode delays a signal by seconds, including when routed into a feedback
// graph. The maximum delay is chosen when the node is constructed.
type DelayNode interface {
	Node
	SetDelay(float64) error
}
