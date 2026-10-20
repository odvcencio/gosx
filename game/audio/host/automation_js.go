//go:build js && wasm

package audiohost

import (
	"errors"
	"math"
	"syscall/js"
)

type parameterJS struct {
	value js.Value
	host  *HostJS
}

func (p *parameterJS) schedule(method string, value, at float64) (err error) {
	defer recoverAudio("parameter automation", &err)
	if p.host.closed {
		return ErrClosed
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || !nonnegative(at) || (method == "exponentialRampToValueAtTime" && value <= 0) {
		return errors.New("audio: invalid parameter automation")
	}
	p.value.Call(method, value, at)
	return nil
}
func (p *parameterJS) SetAt(value, at float64) error { return p.schedule("setValueAtTime", value, at) }
func (p *parameterJS) LinearRamp(value, at float64) error {
	return p.schedule("linearRampToValueAtTime", value, at)
}
func (p *parameterJS) ExponentialRamp(value, at float64) error {
	return p.schedule("exponentialRampToValueAtTime", value, at)
}
func (p *parameterJS) HoldAt(at float64) (err error) {
	defer recoverAudio("hold parameter", &err)
	if p.host.closed {
		return ErrClosed
	}
	if !nonnegative(at) {
		return errors.New("audio: invalid hold time")
	}
	p.value.Call("cancelAndHoldAtTime", at)
	return nil
}

func (n *gainNodeJS) Gain() Parameter { return &parameterJS{value: n.value.Get("gain"), host: n.host} }

// CreateAutomatedGain creates a gain that supports sample-accurate envelopes.
func (h *HostJS) CreateAutomatedGain() AutomatedGainNode { return h.CreateGain().(*gainNodeJS) }

// CreateOscillator creates a one-use oscillator whose lifetime is owned by
// this host just like decoded buffer sources.
func (h *HostJS) CreateOscillator() OscillatorNode {
	source := &sourceNodeJS{value: h.ctx.Call("createOscillator"), host: h}
	h.sources[source] = struct{}{}
	return &oscillatorNodeJS{sourceNodeJS: source}
}

type oscillatorNodeJS struct{ *sourceNodeJS }

func (n *oscillatorNodeJS) Frequency() Parameter {
	return &parameterJS{value: n.value.Get("frequency"), host: n.host}
}
func (n *oscillatorNodeJS) SetWaveform(wave Waveform) (err error) {
	defer recoverAudio("oscillator waveform", &err)
	if n.host.closed {
		return ErrClosed
	}
	switch wave {
	case Sine, Square, Sawtooth, Triangle:
	default:
		return errors.New("audio: invalid waveform")
	}
	n.value.Set("type", string(wave))
	return nil
}

// CreateStereoPanner creates a stereo panner, initially centered.
func (h *HostJS) CreateStereoPanner() StereoPannerNode {
	return &pannerNodeJS{value: h.ctx.Call("createStereoPanner"), host: h}
}

type pannerNodeJS struct {
	value js.Value
	host  *HostJS
}

func (n *pannerNodeJS) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *pannerNodeJS) Disconnect()      { n.value.Call("disconnect") }
func (n *pannerNodeJS) SetPan(pan float64) (err error) {
	defer recoverAudio("stereo pan", &err)
	if n.host.closed {
		return ErrClosed
	}
	if math.IsNaN(pan) || math.IsInf(pan, 0) || pan < -1 || pan > 1 {
		return errors.New("audio: invalid pan")
	}
	n.value.Get("pan").Set("value", pan)
	return nil
}

// CreateDelay creates a delay with a maximum delay time in seconds.
func (h *HostJS) CreateDelay(maximum float64) (node DelayNode, err error) {
	defer recoverAudio("create delay", &err)
	if h.closed {
		return nil, ErrClosed
	}
	if !nonnegative(maximum) || maximum == 0 {
		return nil, errors.New("audio: invalid maximum delay")
	}
	return &delayNodeJS{value: h.ctx.Call("createDelay", maximum), host: h, maximum: maximum}, nil
}

type delayNodeJS struct {
	value   js.Value
	host    *HostJS
	maximum float64
}

func (n *delayNodeJS) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *delayNodeJS) Disconnect()      { n.value.Call("disconnect") }
func (n *delayNodeJS) SetDelay(seconds float64) (err error) {
	defer recoverAudio("delay time", &err)
	if n.host.closed {
		return ErrClosed
	}
	if !nonnegative(seconds) || seconds > n.maximum {
		return errors.New("audio: invalid delay time")
	}
	n.value.Get("delayTime").Set("value", seconds)
	return nil
}

// SuspendNow requests suspension synchronously, so it can be used directly
// from a browser callback. Suspend reports asynchronous device failures.
func (h *HostJS) SuspendNow() (err error) {
	defer recoverAudio("suspend", &err)
	if h.closed {
		return ErrClosed
	}
	h.ctx.Call("suspend")
	return nil
}
