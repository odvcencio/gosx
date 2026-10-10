//go:build js && wasm

package audiohost

import (
	"errors"
	"fmt"
	"syscall/js"
)

// AddWorkletModule loads a processor from an external asset URL. No source is
// generated or evaluated by this package. Call it from a goroutine.
func (h *HostJS) AddWorkletModule(url string) (err error) {
	defer recoverAudio("load worklet module", &err)
	if h.closed {
		return ErrClosed
	}
	worklet := h.ctx.Get("audioWorklet")
	if !worklet.Truthy() || worklet.Get("addModule").Type() != js.TypeFunction {
		return ErrUnsupported
	}
	_, err = h.await(worklet.Call("addModule", url))
	if err == nil && h.closed {
		return ErrClosed
	}
	return err
}

// WorkletNodeJS owns one AudioWorkletNode and its message-port handlers. Close
// disconnects it and closes the port; it does not send a processor-specific
// shutdown message. Applications own that protocol and the processor assets.
type WorkletNodeJS struct {
	host                  *HostJS
	value, port           js.Value
	closed                bool
	message, messageError js.Func
	processorError        js.Func
	hasMessage, hasError  bool
}

func (h *HostJS) NewWorklet(name string, options WorkletOptions) (node *WorkletNodeJS, err error) {
	defer recoverAudio("create worklet", &err)
	if h.closed {
		return nil, ErrClosed
	}
	constructor := js.Global().Get("AudioWorkletNode")
	if constructor.Type() != js.TypeFunction {
		return nil, ErrUnsupported
	}
	outputs := options.NumberOfOutputs
	if outputs == 0 {
		outputs = 1
	}
	settings := map[string]any{"numberOfInputs": options.NumberOfInputs, "numberOfOutputs": outputs}
	if options.ChannelCount != 0 {
		settings["channelCount"] = options.ChannelCount
	}
	if options.ChannelCountMode != "" {
		settings["channelCountMode"] = options.ChannelCountMode
	}
	if options.ChannelInterpretation != "" {
		settings["channelInterpretation"] = options.ChannelInterpretation
	}
	if options.OutputChannelCount != nil {
		counts := make([]any, len(options.OutputChannelCount))
		for i, count := range options.OutputChannelCount {
			counts[i] = count
		}
		settings["outputChannelCount"] = counts
	}
	if options.ProcessorOptions != nil {
		settings["processorOptions"] = options.ProcessorOptions
	}
	if options.ParameterData != nil {
		parameters := make(map[string]any, len(options.ParameterData))
		for name, value := range options.ParameterData {
			parameters[name] = value
		}
		settings["parameterData"] = parameters
	}
	value := constructor.New(h.ctx, name, settings)
	node = &WorkletNodeJS{host: h, value: value, port: value.Get("port")}
	h.worklets[node] = struct{}{}
	return node, nil
}

func (n *WorkletNodeJS) Connect(dst Node) {
	if !n.closed {
		n.value.Call("connect", jsValueOf(dst))
	}
}

func (n *WorkletNodeJS) Disconnect() {
	if !n.closed {
		n.value.Call("disconnect")
	}
}

// OnMessage replaces the handler for structured-cloned processor messages.
// It runs inside a JS callback; start a goroutine before awaiting any promise.
// Passing nil detaches the handler. Close releases it automatically.
func (n *WorkletNodeJS) OnMessage(fn func(js.Value)) {
	n.port.Set("onmessage", js.Null())
	if n.hasMessage {
		n.message.Release()
		n.hasMessage = false
	}
	if fn == nil || n.closed {
		return
	}
	n.message = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !n.closed && len(args) > 0 {
			fn(args[0].Get("data"))
		}
		return nil
	})
	n.hasMessage = true
	n.port.Set("onmessage", n.message)
}

// OnError handles message deserialization and processor errors. Processor
// protocol messages remain the responsibility of OnMessage.
func (n *WorkletNodeJS) OnError(fn func(error)) {
	n.port.Set("onmessageerror", js.Null())
	n.value.Set("onprocessorerror", js.Null())
	if n.hasError {
		n.messageError.Release()
		n.processorError.Release()
		n.hasError = false
	}
	if fn == nil || n.closed {
		return
	}
	wrap := func(message string) js.Func {
		return js.FuncOf(func(_ js.Value, _ []js.Value) any {
			if !n.closed {
				fn(errors.New(message))
			}
			return nil
		})
	}
	n.messageError = wrap("audio: worklet message could not be deserialized")
	n.processorError = wrap("audio: worklet processor failed")
	n.hasError = true
	n.port.Set("onmessageerror", n.messageError)
	n.value.Set("onprocessorerror", n.processorError)
}

// PostMessage passes structured-clone data and an optional transfer list.
// Transfer ArrayBuffers, not their typed-array views; ownership then passes to
// the processor. Browser handles in message are preserved without JSON copying.
func (n *WorkletNodeJS) PostMessage(message any, transferables ...js.Value) (err error) {
	defer recoverAudio("post worklet message", &err)
	if n.closed || n.host.closed {
		return ErrClosed
	}
	if len(transferables) == 0 {
		n.port.Call("postMessage", message)
	} else {
		list := make([]any, len(transferables))
		for i, value := range transferables {
			list[i] = value
		}
		n.port.Call("postMessage", message, list)
	}
	return nil
}

func (n *WorkletNodeJS) Close() (err error) {
	defer recoverAudio("close worklet", &err)
	if n.closed {
		return nil
	}
	n.closed = true
	n.OnMessage(nil)
	n.OnError(nil)
	delete(n.host.worklets, n)
	// Release callbacks even if a device has already disconnected the node.
	var disconnectErr error
	func() {
		defer recoverAudio("disconnect worklet", &disconnectErr)
		n.value.Call("disconnect")
	}()
	n.port.Call("close")
	if disconnectErr != nil {
		return fmt.Errorf("audio: worklet closed: %w", disconnectErr)
	}
	return nil
}
