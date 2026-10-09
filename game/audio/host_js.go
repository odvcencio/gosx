//go:build js && wasm

package audio

import (
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// HostJS is the real browser Host, backed by a WebAudio AudioContext.
type HostJS struct {
	ctx      js.Value
	closed   bool
	closing  chan struct{}
	worklets map[*WorkletNodeJS]struct{}
	sources  map[*sourceNodeJS]struct{}
}

// NewHostJS creates an AudioContext-backed Host. It fails if neither
// AudioContext nor the legacy webkitAudioContext constructor is available.
// The context starts suspended, per browser autoplay policy — call
// Player.Unlock from a user gesture handler to start it running.
func NewHostJS() (*HostJS, error) {
	return NewHostJSWithOptions(HostOptions{})
}

// NewHostJSWithOptions creates a context with the requested device settings.
// Unsupported rates and browser construction errors are returned to the caller.
func NewHostJSWithOptions(options HostOptions) (host *HostJS, err error) {
	defer recoverAudio("create context", &err)
	if !nonnegative(options.SampleRate) {
		return nil, errors.New("audio: invalid sample rate")
	}
	ctor := js.Global().Get("AudioContext")
	if ctor.Type() != js.TypeFunction {
		ctor = js.Global().Get("webkitAudioContext")
	}
	if ctor.Type() != js.TypeFunction {
		return nil, errors.New("audio: AudioContext is not available")
	}
	settings := map[string]any{}
	if options.SampleRate > 0 {
		settings["sampleRate"] = options.SampleRate
	}
	if options.LatencyHint != "" {
		settings["latencyHint"] = options.LatencyHint
	}
	return &HostJS{ctx: ctor.New(settings), closing: make(chan struct{}), worklets: make(map[*WorkletNodeJS]struct{}), sources: make(map[*sourceNodeJS]struct{})}, nil
}

func (h *HostJS) CurrentTime() float64 { return h.ctx.Get("currentTime").Float() }
func (h *HostJS) State() string        { return h.ctx.Get("state").String() }

func (h *HostJS) Resume() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("audio: resume: %v", r)
		}
	}()
	if h.closed {
		return ErrClosed
	}
	h.ctx.Call("resume")
	return nil
}

func (h *HostJS) CreateGain() GainNode {
	return &gainNodeJS{value: h.ctx.Call("createGain")}
}

func (h *HostJS) CreateCompressor() CompressorNode {
	return &compressorNodeJS{value: h.ctx.Call("createDynamicsCompressor")}
}

func (h *HostJS) CreateSource(buffer Buffer) SourceNode {
	node := h.ctx.Call("createBufferSource")
	if jsBuffer, ok := buffer.(js.Value); ok {
		node.Set("buffer", jsBuffer)
	}
	source := &sourceNodeJS{value: node, host: h}
	h.sources[source] = struct{}{}
	return source
}

func (h *HostJS) Destination() Node {
	return &rawNodeJS{value: h.ctx.Get("destination")}
}

func (h *HostJS) DecodeAudioData(data []byte) (buffer Buffer, err error) {
	defer recoverAudio("decode", &err)
	if h.closed {
		return nil, ErrClosed
	}
	array := jsutil.NewUint8ArrayFromBytes(data)
	value, err := h.await(h.ctx.Call("decodeAudioData", array.Get("buffer")))
	if err != nil {
		return nil, err
	}
	if h.closed {
		return nil, ErrClosed
	}
	return value, nil
}

func jsValueOf(n Node) js.Value {
	switch v := n.(type) {
	case *rawNodeJS:
		return v.value
	case *gainNodeJS:
		return v.value
	case *compressorNodeJS:
		return v.value
	case *sourceNodeJS:
		return v.value
	case *WorkletNodeJS:
		return v.value
	default:
		return js.Undefined()
	}
}

type rawNodeJS struct{ value js.Value }

func (n *rawNodeJS) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *rawNodeJS) Disconnect()      { n.value.Call("disconnect") }

type gainNodeJS struct{ value js.Value }

func (n *gainNodeJS) Connect(dst Node)      { n.value.Call("connect", jsValueOf(dst)) }
func (n *gainNodeJS) Disconnect()           { n.value.Call("disconnect") }
func (n *gainNodeJS) SetGain(value float64) { n.value.Get("gain").Set("value", value) }

type compressorNodeJS struct{ value js.Value }

func (n *compressorNodeJS) Connect(dst Node) { n.value.Call("connect", jsValueOf(dst)) }
func (n *compressorNodeJS) Disconnect()      { n.value.Call("disconnect") }
func (n *compressorNodeJS) Configure(threshold, ratio, attack, release float64) {
	n.value.Get("threshold").Set("value", threshold)
	n.value.Get("ratio").Set("value", ratio)
	n.value.Get("attack").Set("value", attack)
	n.value.Get("release").Set("value", release)
}

type sourceNodeJS struct {
	value    js.Value
	host     *HostJS
	ended    js.Func
	hasEnded bool
}

func (n *sourceNodeJS) Connect(dst Node)             { n.value.Call("connect", jsValueOf(dst)) }
func (n *sourceNodeJS) Disconnect()                  { n.value.Call("disconnect") }
func (n *sourceNodeJS) SetLoop(loop bool)            { n.value.Set("loop", loop) }
func (n *sourceNodeJS) SetPlaybackRate(rate float64) { n.value.Get("playbackRate").Set("value", rate) }
func (n *sourceNodeJS) Start(at float64)             { n.value.Call("start", at) }
func (n *sourceNodeJS) Stop(at float64)              { n.value.Call("stop", at) }
func (n *sourceNodeJS) OnEnded(fn func()) {
	n.clearEnded()
	if fn == nil || n.host.closed {
		return
	}
	n.ended = js.FuncOf(func(js.Value, []js.Value) any {
		n.clearEnded()
		delete(n.host.sources, n)
		fn()
		return nil
	})
	n.hasEnded = true
	n.value.Set("onended", n.ended)
}

func (n *sourceNodeJS) clearEnded() {
	n.value.Set("onended", js.Null())
	if n.hasEnded {
		n.ended.Release()
		n.hasEnded = false
	}
}
