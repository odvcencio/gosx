package audiohost

import (
	"errors"
	"math"
)

var ErrClosed = errors.New("audio: host or node is closed")
var ErrUnsupported = errors.New("audio: host does not support this operation")

// HostOptions configures a browser AudioContext. Zero SampleRate uses the
// device default; LatencyHint accepts the browser's named latency categories.
type HostOptions struct {
	SampleRate  float64
	LatencyHint string
}

// WorkletOptions describes an AudioWorkletNode without embedding processor
// code. Inputs default to zero and outputs to one. ProcessorOptions is cloned
// by the browser and may contain browser handles such as WebAssembly.Module.
type WorkletOptions struct {
	NumberOfInputs        int
	NumberOfOutputs       int
	ChannelCount          int
	ChannelCountMode      string
	ChannelInterpretation string
	OutputChannelCount    []int
	ProcessorOptions      any
	ParameterData         map[string]float64
}

// RegionSourceNode is an optional SourceNode capability for audio sprites and
// bounded loop regions. Existing Host and SourceNode implementations need not
// implement it; helpers return ErrUnsupported instead of changing playback.
type RegionSourceNode interface {
	SourceNode
	StartRegion(at, offset, duration float64) error
	SetLoopRegion(start, end float64) error
}

// TargetGainNode supports a scheduled, click-free gain approach. A zero time
// constant sets the value at the specified audio-clock time immediately.
type TargetGainNode interface {
	GainNode
	SetGainTarget(value, at, timeConstant float64) error
}

func nonnegative(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	return true
}

// StartRegion schedules one sprite. A zero duration plays to the buffer's end.
func StartRegion(source SourceNode, at, offset, duration float64) error {
	if !nonnegative(at, offset, duration) {
		return errors.New("audio: invalid source region")
	}
	if source, ok := source.(RegionSourceNode); ok {
		return source.StartRegion(at, offset, duration)
	}
	return ErrUnsupported
}

// SetLoopRegion selects loop bounds in buffer seconds; it does not enable
// looping. SetLoop controls that independently.
func SetLoopRegion(source SourceNode, start, end float64) error {
	if !nonnegative(start, end) || end <= start {
		return errors.New("audio: invalid loop region")
	}
	if source, ok := source.(RegionSourceNode); ok {
		return source.SetLoopRegion(start, end)
	}
	return ErrUnsupported
}

// SetGainTarget replaces future gain automation with a new target.
func SetGainTarget(gain GainNode, value, at, timeConstant float64) error {
	if !nonnegative(value, at, timeConstant) {
		return errors.New("audio: invalid gain automation")
	}
	if gain, ok := gain.(TargetGainNode); ok {
		return gain.SetGainTarget(value, at, timeConstant)
	}
	return ErrUnsupported
}
