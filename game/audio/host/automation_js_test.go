//go:build js && wasm

package audiohost

import (
	"errors"
	"math"
	"reflect"
	"syscall/js"
	"testing"
)

func TestHostJSAutomationRoutingAndVoiceLifetime(t *testing.T) {
	f := newBrowserAudioFixture(t)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	gain := host.CreateAutomatedGain()
	parameter := gain.Gain()
	for _, err := range []error{parameter.SetAt(0, 12.5), parameter.LinearRamp(.7, 12.525), parameter.SetAt(.7, 13.8), parameter.LinearRamp(0, 14.5), parameter.HoldAt(12.6), parameter.LinearRamp(0, 12.68)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := [][]float64{{0, 12.5}, {.7, 12.525}, {.7, 13.8}, {0, 14.5}, {12.6}, {0, 12.68}}
	if !reflect.DeepEqual(f.automation, want) {
		t.Fatalf("envelope/crossfade changed: got %v", f.automation)
	}
	before := len(f.automation)
	for _, err := range []error{parameter.SetAt(math.NaN(), 12.5), parameter.LinearRamp(1, -1), parameter.ExponentialRamp(0, 12.6), parameter.HoldAt(math.Inf(1))} {
		if err == nil {
			t.Fatal("invalid automation accepted")
		}
	}
	if len(f.automation) != before {
		t.Fatal("invalid automation touched audio graph")
	}
	pan := host.CreateStereoPanner()
	if err := pan.SetPan(-.65); err != nil || f.panner.Get("pan").Get("value").Float() != -.65 {
		t.Fatal("stereo pan changed", err)
	}
	if pan.SetPan(2) == nil {
		t.Fatal("invalid pan accepted")
	}
	delay, err := host.CreateDelay(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := delay.SetDelay(.375); err != nil || f.delay.Get("delayTime").Get("value").Float() != .375 || delay.SetDelay(3) == nil {
		t.Fatal("delay bounds changed", err)
	}
	gain.Connect(pan)
	pan.Connect(delay)
	delay.Connect(gain)
	oscillator := host.CreateOscillator()
	if err := oscillator.SetWaveform(Triangle); err != nil || f.oscillator.Get("type").String() != "triangle" || oscillator.SetWaveform("unknown") == nil {
		t.Fatal("oscillator waveform changed", err)
	}
	_ = oscillator.Frequency().SetAt(440, 12.5)
	_ = oscillator.Frequency().ExponentialRamp(220, 12.7)
	_ = gain.Gain().ExponentialRamp(.0001, 12.68)
	ended := 0
	voice := NewVoice(oscillator, func() { ended++ }, gain, pan)
	oscillator.Start(12.5)
	voice.Stop(12.7)
	if len(host.sources) != 1 {
		t.Fatal("oscillator not owned by host")
	}
	f.oscillator.Get("onended").Invoke()
	voice.Close()
	if ended != 1 || len(host.sources) != 0 || !f.oscillator.Get("onended").IsNull() || f.disconnects != 3 {
		t.Fatal("oscillator nodes or callbacks leaked")
	}
	if err := host.SuspendNow(); err != nil || host.State() != "suspended" {
		t.Fatal("callback-safe pause failed", err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(parameter.SetAt(1, 0), ErrClosed) || !errors.Is(oscillator.SetWaveform(Sine), ErrClosed) || !errors.Is(delay.SetDelay(0), ErrClosed) {
		t.Fatal("closed context accepted automation")
	}
}

func TestHostJSDecodeCloseAndLateCompletion(t *testing.T) {
	f := newBrowserAudioFixture(t)
	var resolve js.Value
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { resolve = args[0]; return nil })
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	entered := make(chan struct{}, 1)
	f.method(f.ctx, "decodeAudioData", func(args []js.Value) any {
		bytes := js.Global().Get("Uint8Array").New(args[0])
		if bytes.Get("length").Int() != 3 || bytes.Index(1).Int() != 2 {
			t.Error("encoded bytes changed")
		}
		entered <- struct{}{}
		return promise
	})
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := host.DecodeAudioData([]byte{1, 2, 3}); done <- err }()
	<-entered
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatal("decode caller retained after close", err)
	}
	resolve.Invoke(map[string]any{"duration": 2})
	// Yield through a promise to exercise the retained completion handler.
	_, _ = host.await(js.Global().Get("Promise").Call("resolve", js.Undefined()))
	if _, err := host.DecodeAudioData([]byte{1}); !errors.Is(err, ErrClosed) {
		t.Fatal("closed host decoded audio", err)
	}
}
