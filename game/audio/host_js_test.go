//go:build js && wasm

package audio

import (
	"errors"
	"strings"
	"syscall/js"
	"testing"
)

type browserAudioFixture struct {
	ctx, contextOptions, workletOptions, worklet, port, source, gain js.Value
	modulePromise, moduleURL, posted                                 js.Value
	functions                                                        []js.Func
	starts, automation                                               [][]float64
	resumeReject                                                     bool
	closes, portCloses, disconnects                                  int
}

func newBrowserAudioFixture(t *testing.T) *browserAudioFixture {
	f := &browserAudioFixture{}
	object := func() js.Value { return js.Global().Get("Object").New() }
	f.ctx, f.worklet, f.port, f.source, f.gain = object(), object(), object(), object(), object()
	f.ctx.Set("state", "suspended")
	f.ctx.Set("currentTime", 12.5)
	f.ctx.Set("sampleRate", 48000)
	f.ctx.Set("baseLatency", .01)
	f.ctx.Set("outputLatency", .02)
	f.ctx.Set("destination", object())
	resolved := func() js.Value { return js.Global().Get("Promise").Call("resolve", js.Undefined()) }
	f.method(f.ctx, "resume", func([]js.Value) any {
		if f.resumeReject {
			return js.Global().Get("Promise").Call("reject", "device denied")
		}
		f.ctx.Set("state", "running")
		return resolved()
	})
	f.method(f.ctx, "suspend", func([]js.Value) any { f.ctx.Set("state", "suspended"); return resolved() })
	f.method(f.ctx, "close", func([]js.Value) any { f.closes++; f.ctx.Set("state", "closed"); return resolved() })
	audioWorklet := object()
	f.ctx.Set("audioWorklet", audioWorklet)
	f.method(audioWorklet, "addModule", func(args []js.Value) any {
		f.moduleURL = args[0]
		if !f.modulePromise.IsUndefined() {
			return f.modulePromise
		}
		return resolved()
	})
	f.worklet.Set("port", f.port)
	f.method(f.worklet, "connect", func(args []js.Value) any { return args[0] })
	f.method(f.worklet, "disconnect", func([]js.Value) any { f.disconnects++; return nil })
	f.method(f.port, "close", func([]js.Value) any { f.portCloses++; return nil })
	f.method(f.port, "postMessage", func(args []js.Value) any {
		if len(args) == 2 {
			f.posted = js.Global().Call("structuredClone", args[0], map[string]any{"transfer": args[1]})
		} else {
			f.posted = js.Global().Call("structuredClone", args[0])
		}
		return nil
	})
	f.method(f.ctx, "createBufferSource", func([]js.Value) any { return f.source })
	f.method(f.source, "start", func(args []js.Value) any {
		x := make([]float64, len(args))
		for i, arg := range args {
			x[i] = arg.Float()
		}
		f.starts = append(f.starts, x)
		return nil
	})
	f.method(f.source, "disconnect", func([]js.Value) any { return nil })
	f.method(f.ctx, "createGain", func([]js.Value) any { return f.gain })
	param := object()
	f.gain.Set("gain", param)
	for _, method := range []string{"cancelScheduledValues", "setValueAtTime", "setTargetAtTime"} {
		f.method(param, method, func(args []js.Value) any {
			values := make([]float64, len(args))
			for i, arg := range args {
				values[i] = arg.Float()
			}
			f.automation = append(f.automation, values)
			return nil
		})
	}
	global := js.Global()
	context, worklet := global.Get("AudioContext"), global.Get("AudioWorkletNode")
	f.method(global, "AudioContext", func(args []js.Value) any { f.contextOptions = args[0]; return f.ctx })
	f.method(global, "AudioWorkletNode", func(args []js.Value) any {
		f.workletOptions = args[2]
		return f.worklet
	})
	t.Cleanup(func() {
		global.Set("AudioContext", context)
		global.Set("AudioWorkletNode", worklet)
		for _, fn := range f.functions {
			fn.Release()
		}
	})
	return f
}

func (f *browserAudioFixture) method(object js.Value, name string, fn func([]js.Value) any) {
	wrapped := js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
	f.functions = append(f.functions, wrapped)
	object.Set(name, wrapped)
}

func TestHostJSWorkletTransferAndLifetime(t *testing.T) {
	f := newBrowserAudioFixture(t)
	host, err := NewHostJSWithOptions(HostOptions{SampleRate: 48000, LatencyHint: "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	if f.contextOptions.Get("sampleRate").Int() != 48000 || f.contextOptions.Get("latencyHint").String() != "interactive" || host.SampleRate() != 48000 || host.BaseLatency() != .01 || host.OutputLatency() != .02 {
		t.Fatal("context options or clock metadata lost")
	}
	if err := host.AddWorkletModule("/.proxy/vendor/processor.js"); err != nil || f.moduleURL.String() != "/.proxy/vendor/processor.js" {
		t.Fatal("external processor URL changed", err)
	}
	module := js.Global().Get("Object").New()
	node, err := host.NewWorklet("synth", WorkletOptions{NumberOfInputs: 1, NumberOfOutputs: 1, ChannelCount: 2, OutputChannelCount: []int{2}, ProcessorOptions: map[string]any{"module": module}})
	if err != nil {
		t.Fatal(err)
	}
	if !f.workletOptions.Get("processorOptions").Get("module").Equal(module) || f.workletOptions.Get("outputChannelCount").Index(0).Int() != 2 {
		t.Fatal("processor handles or channel layout copied incorrectly")
	}
	messages, failures := 0, 0
	node.OnMessage(func(data js.Value) { messages += data.Get("value").Int() })
	f.port.Get("onmessage").Invoke(map[string]any{"data": map[string]any{"value": 3}})
	node.OnMessage(func(data js.Value) { messages += data.Get("value").Int() * 2 })
	f.port.Get("onmessage").Invoke(map[string]any{"data": map[string]any{"value": 4}})
	node.OnError(func(error) { failures++ })
	f.port.Get("onmessageerror").Invoke()
	f.worklet.Get("onprocessorerror").Invoke()
	buffer := js.Global().Get("ArrayBuffer").New(16)
	if err := node.PostMessage(map[string]any{"bytes": buffer}, buffer); err != nil {
		t.Fatal(err)
	}
	if buffer.Get("byteLength").Int() != 0 || f.posted.Get("bytes").Get("byteLength").Int() != 16 || messages != 11 || failures != 2 {
		t.Fatal("port callbacks or transferable ownership failed")
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := node.Close(); err != nil || host.Close() != nil || f.portCloses != 1 || f.disconnects != 1 || f.closes != 1 || !f.port.Get("onmessage").IsNull() || !f.worklet.Get("onprocessorerror").IsNull() {
		t.Fatal("host shutdown did not release the node exactly once", err)
	}
	if !errors.Is(node.PostMessage(nil), ErrClosed) || !errors.Is(host.AddWorkletModule("later.js"), ErrClosed) {
		t.Fatal("closed host accepted worklet traffic")
	}
}

func TestHostJSPromiseFailuresAndCloseDuringLoad(t *testing.T) {
	f := newBrowserAudioFixture(t)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	f.resumeReject = true
	if err := host.ResumeAndWait(); err == nil || !strings.Contains(err.Error(), "device denied") {
		t.Fatal("resume rejection disappeared", err)
	}
	f.resumeReject = false
	if err := host.ResumeAndWait(); err != nil || host.State() != "running" {
		t.Fatal(err)
	}
	if err := host.Suspend(); err != nil || host.State() != "suspended" {
		t.Fatal(err)
	}
	var resolve js.Value
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { resolve = args[0]; return nil })
	f.modulePromise = js.Global().Get("Promise").New(executor)
	executor.Release()
	done := make(chan error, 1)
	go func() { done <- host.AddWorkletModule("pending.js") }()
	// Yield through a resolved promise so the loader has installed its handlers.
	if err := host.ResumeAndWait(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatal("pending load resurrected a closed host", err)
	}
	resolve.Invoke()
	if _, err := host.NewWorklet("late", WorkletOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatal("closed host created a node", err)
	}
}

func TestHostJSSpritesGainAndSourceCallbackCleanup(t *testing.T) {
	f := newBrowserAudioFixture(t)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	buffer := js.ValueOf(map[string]any{"duration": 8.5})
	source := host.CreateSource(buffer)
	if BufferDuration(buffer) != 8.5 || BufferDuration(nil) != 0 {
		t.Fatal("decoded duration lost")
	}
	if err := StartRegion(source, 12.5, 3, .05); err != nil || len(f.starts) != 1 || len(f.starts[0]) != 3 || f.starts[0][1] != 3 || f.starts[0][2] != .05 {
		t.Fatal("wrong sprite scheduled", err)
	}
	if err := SetLoopRegion(source, .2, 8); err != nil || f.source.Get("loopStart").Float() != .2 || f.source.Get("loopEnd").Float() != 8 {
		t.Fatal("wrong loop range", err)
	}
	if err := SetGainTarget(host.CreateGain(), 0, 12.5, .05); err != nil || len(f.automation) != 2 || len(f.automation[1]) != 3 || f.automation[1][0] != 0 {
		t.Fatal("mute target lost", err)
	}
	ended := 0
	source.OnEnded(func() { ended += 10 })
	source.OnEnded(func() { ended++ })
	f.source.Get("onended").Invoke()
	if ended != 1 || !f.source.Get("onended").IsNull() || len(host.sources) != 0 {
		t.Fatal("ended callback was duplicated or retained")
	}
	source = host.CreateSource(buffer)
	source.OnEnded(func() { ended++ })
	if err := host.Close(); err != nil || !f.source.Get("onended").IsNull() || len(host.sources) != 0 || ended != 1 {
		t.Fatal("close retained or fired a source callback", err)
	}
}
