//go:build js && wasm

package audiohost

import (
	"errors"
	"math"
	"syscall/js"
)

// sharedMixerValue returns window.__gosx.audioMixer, or an undefined value.
func sharedMixerValue() js.Value {
	gosx := js.Global().Get("__gosx")
	if gosx.Type() != js.TypeObject {
		return js.Undefined()
	}
	mixer := gosx.Get("audioMixer")
	if mixer.Type() != js.TypeObject || mixer.Get("input").Type() != js.TypeFunction {
		return js.Undefined()
	}
	return mixer
}

// busInputJS is a shared bus's input gain. It is a plain node for Go: the
// mixer owns its gain.
type busInputJS struct{ value js.Value }

func (n *busInputJS) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *busInputJS) Disconnect()      { n.value.Call("disconnect") }

type mixerBusJS struct {
	host  *HostJS
	id    string
	input *busInputJS
}

func (b *mixerBusJS) Input() Node { return b.input }
func (b *mixerBusJS) Volume() float64 {
	return audioNumber(b.host.mixer.Call("volume", b.id))
}
func (b *mixerBusJS) SetVolume(volume float64) { b.host.mixer.Call("setVolume", b.id, volume) }
func (b *mixerBusJS) Muted() bool              { return b.host.mixer.Call("muted", b.id).Truthy() }
func (b *mixerBusJS) SetMuted(muted bool)      { b.host.mixer.Call("setMuted", b.id, muted) }
func (b *mixerBusJS) Duck(key string, amount, seconds float64) {
	b.host.mixer.Call("duck", b.id, amount, seconds, key)
}
func (b *mixerBusJS) Unduck(key string, seconds float64) {
	b.host.mixer.Call("unduck", b.id, key, seconds)
}

// MixerBus implements SharedMixer. It reports false for a host that owns a
// private context.
func (h *HostJS) MixerBus(id, parent string) (bus MixerBus, ok bool) {
	if !h.shared || h.closed {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			bus, ok = nil, false
		}
	}()
	if id == "" {
		id = "master"
	}
	if parent != "" && id != "master" && h.mixer.Get("registerBus").Type() == js.TypeFunction {
		h.mixer.Call("registerBus", id, parent)
	}
	input := h.mixer.Call("input", id)
	if input.Type() != js.TypeObject {
		return nil, false
	}
	return &mixerBusJS{host: h, id: id, input: &busInputJS{value: input}}, true
}

// Shared reports whether the host uses the page's shared mixer context.
func (h *HostJS) Shared() bool { return h.shared }

// adoptSharedContext returns the page mixer's context for options. It returns
// an undefined mixer when the page has none, or when the shared context
// already runs at another sample rate than options asks for; the caller then
// builds a private context, and Shared reports false.
func adoptSharedContext(options HostOptions) (mixer, ctx js.Value, err error) {
	mixer = sharedMixerValue()
	if mixer.IsUndefined() {
		return mixer, js.Undefined(), nil
	}
	settings := map[string]any{}
	if options.SampleRate > 0 {
		settings["sampleRate"] = options.SampleRate
	}
	if options.LatencyHint != "" {
		settings["latencyHint"] = options.LatencyHint
	}
	ctx = mixer.Call("context", settings)
	if ctx.Type() != js.TypeObject {
		return mixer, js.Undefined(), errors.New("audio: shared mixer has no AudioContext")
	}
	if options.SampleRate > 0 && math.Abs(audioNumber(ctx.Get("sampleRate"))-options.SampleRate) > 0.5 {
		return js.Undefined(), js.Undefined(), nil
	}
	return mixer, ctx, nil
}

type pannerNodeJS3D struct {
	value js.Value
	host  *HostJS
}

func (n *pannerNodeJS3D) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *pannerNodeJS3D) Disconnect()      { n.value.Call("disconnect") }

func setParamAt(param js.Value, value, at float64) {
	if param.Type() == js.TypeObject && param.Get("setValueAtTime").Type() == js.TypeFunction {
		param.Call("setValueAtTime", value, at)
	} else if param.Type() == js.TypeObject {
		param.Set("value", value)
	}
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func (n *pannerNodeJS3D) SetPosition(x, y, z float64) (err error) {
	defer recoverAudio("panner position", &err)
	if n.host.closed {
		return ErrClosed
	}
	if !finite(x, y, z) {
		return errors.New("audio: invalid panner position")
	}
	if n.value.Get("positionX").Type() == js.TypeObject {
		now := n.host.CurrentTime()
		setParamAt(n.value.Get("positionX"), x, now)
		setParamAt(n.value.Get("positionY"), y, now)
		setParamAt(n.value.Get("positionZ"), z, now)
		return nil
	}
	n.value.Call("setPosition", x, y, z)
	return nil
}

func (n *pannerNodeJS3D) Configure(distanceModel string, refDistance, maxDistance, rolloff float64) (err error) {
	defer recoverAudio("panner configure", &err)
	if n.host.closed {
		return ErrClosed
	}
	if !nonnegative(refDistance, maxDistance, rolloff) {
		return errors.New("audio: invalid panner distance settings")
	}
	if distanceModel != "" {
		n.value.Set("distanceModel", distanceModel)
	}
	if refDistance > 0 {
		n.value.Set("refDistance", refDistance)
	}
	if maxDistance > 0 {
		n.value.Set("maxDistance", maxDistance)
	}
	n.value.Set("rolloffFactor", rolloff)
	return nil
}

// CreatePanner implements SpatialHost.
func (h *HostJS) CreatePanner() PannerNode {
	node := h.ctx.Call("createPanner")
	node.Set("panningModel", "HRTF")
	return &pannerNodeJS3D{value: node, host: h}
}

// SetListener implements SpatialHost. A shared host routes through the mixer so
// that clips and the synth see the same listener.
func (h *HostJS) SetListener(position, forward, up [3]float64) (err error) {
	defer recoverAudio("set listener", &err)
	if h.closed {
		return ErrClosed
	}
	for _, v := range [][3]float64{position, forward, up} {
		if !finite(v[0], v[1], v[2]) {
			return errors.New("audio: invalid listener vector")
		}
	}
	toObject := func(v [3]float64) any { return map[string]any{"x": v[0], "y": v[1], "z": v[2]} }
	if h.shared && h.mixer.Get("setListener").Type() == js.TypeFunction {
		h.mixer.Call("setListener", toObject(position), toObject(forward), toObject(up))
		return nil
	}
	listener := h.ctx.Get("listener")
	now := h.CurrentTime()
	if listener.Get("positionX").Type() == js.TypeObject {
		for i, name := range []string{"positionX", "positionY", "positionZ"} {
			setParamAt(listener.Get(name), position[i], now)
		}
		for i, name := range []string{"forwardX", "forwardY", "forwardZ"} {
			setParamAt(listener.Get(name), forward[i], now)
		}
		for i, name := range []string{"upX", "upY", "upZ"} {
			setParamAt(listener.Get(name), up[i], now)
		}
		return nil
	}
	listener.Call("setPosition", position[0], position[1], position[2])
	listener.Call("setOrientation", forward[0], forward[1], forward[2], up[0], up[1], up[2])
	return nil
}
