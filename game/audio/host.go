package audio

import audiohost "m31labs.dev/gosx/game/audio/host"

// The low-level contracts live in game/audio/host so browser clients can use
// them without importing the manifest player or the server game runtime.
type Buffer = audiohost.Buffer
type Node = audiohost.Node
type GainNode = audiohost.GainNode
type CompressorNode = audiohost.CompressorNode
type SourceNode = audiohost.SourceNode
type ScheduledSource = audiohost.ScheduledSource
type Host = audiohost.Host
type HostOptions = audiohost.HostOptions
type WorkletOptions = audiohost.WorkletOptions
type RegionSourceNode = audiohost.RegionSourceNode
type TargetGainNode = audiohost.TargetGainNode
type Parameter = audiohost.Parameter
type AutomatedGainNode = audiohost.AutomatedGainNode
type Waveform = audiohost.Waveform
type OscillatorNode = audiohost.OscillatorNode
type StereoPannerNode = audiohost.StereoPannerNode
type DelayNode = audiohost.DelayNode
type Voice = audiohost.Voice

func NewVoice(source ScheduledSource, onEnded func(), nodes ...Node) *Voice {
	return audiohost.NewVoice(source, onEnded, nodes...)
}

const (
	Sine     = audiohost.Sine
	Square   = audiohost.Square
	Sawtooth = audiohost.Sawtooth
	Triangle = audiohost.Triangle
)

var ErrClosed = audiohost.ErrClosed
var ErrUnsupported = audiohost.ErrUnsupported

func StartRegion(source SourceNode, at, offset, duration float64) error {
	return audiohost.StartRegion(source, at, offset, duration)
}
func SetLoopRegion(source SourceNode, start, end float64) error {
	return audiohost.SetLoopRegion(source, start, end)
}
func SetGainTarget(gain GainNode, value, at, timeConstant float64) error {
	return audiohost.SetGainTarget(gain, value, at, timeConstant)
}
