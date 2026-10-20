//go:build js && wasm

package audiohost

import (
	"errors"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/game/audio/worklet"
)

// WASMWorkletOptions supplies a compiled reactor and an optional opaque image.
// Validate the image with the backend before calling NewWASMWorklet. Module is
// shared across nodes; each node owns a separate reactor instance and buffers.
type WASMWorkletOptions struct {
	Module    js.Value
	Image     []byte
	Backend   worklet.Backend
	Info      worklet.ImageInfo
	ID        string
	LatencyMS float64
}

// CompileWASM compiles a reactor once for reuse by multiple worklet nodes.
// Call it from a goroutine. Closing the host cancels an outstanding compile.
func (h *HostJS) CompileWASM(raw []byte) (module js.Value, err error) {
	defer recoverAudio("compile WASM audio reactor", &err)
	if h.closed {
		return js.Undefined(), ErrClosed
	}
	wasm := js.Global().Get("WebAssembly")
	if !wasm.Truthy() || wasm.Get("compile").Type() != js.TypeFunction {
		return js.Undefined(), ErrUnsupported
	}
	return h.await(wasm.Call("compile", jsutil.NewUint8ArrayFromBytes(raw)))
}

// NewWASMWorklet creates a node using GoSX's external processor asset. First
// load worklet.Path() through AddWorkletModule. The existing WorkletNodeJS API
// owns ports and errors; the processor's close message also stops rendering.
func (h *HostJS) NewWASMWorklet(o WASMWorkletOptions) (node *WorkletNodeJS, err error) {
	defer recoverAudio("create WASM audio worklet", &err)
	if h.closed {
		return nil, ErrClosed
	}
	backend, err := o.Backend.Options()
	if err != nil {
		return nil, err
	}
	if err := o.Backend.ValidateImage(o.Info); err != nil {
		return nil, err
	}
	if !nonnegative(o.LatencyMS) || o.Module.Type() != js.TypeObject || !o.Module.InstanceOf(js.Global().Get("WebAssembly").Get("Module")) {
		return nil, errors.New("audio: invalid WASM worklet options")
	}
	var image any
	if len(o.Image) != 0 {
		image = jsutil.NewUint8ArrayFromBytes(o.Image).Get("buffer")
	}
	node, err = h.NewWorklet(worklet.ProcessorName, WorkletOptions{
		NumberOfOutputs: 1, ChannelCount: o.Backend.Channels,
		ChannelCountMode: "explicit", ChannelInterpretation: "discrete",
		OutputChannelCount: []int{o.Backend.Channels},
		ProcessorOptions: map[string]any{
			"m": o.Module, "i": image, "r": o.ID, "l": o.LatencyMS, "backend": backend,
			"info": map[string]any{"tempoMilli": o.Info.TempoMilli, "requiredCapabilities": float64(o.Info.RequiredCapabilities)},
		},
	})
	if err == nil {
		node.wasmProcessor = true
	}
	return node, err
}
