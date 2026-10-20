//go:build js && wasm

package audiohost

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/game/audio/worklet"
)

func TestHostJSWASMProcessorOptionsAndShutdown(t *testing.T) {
	f := newBrowserAudioFixture(t)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	module, err := host.CompileWASM([]byte{0, 97, 115, 109, 1, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.CompileWASM([]byte("invalid")); err == nil {
		t.Fatal("accepted an invalid WASM module")
	}
	backend := worklet.Backend{MaxFrames: 64, Channels: 1, CommandBytes: 4, MessageBytes: 4, MessageCapacity: 8}
	node, err := host.NewWASMWorklet(WASMWorkletOptions{Module: module, Backend: backend, Image: []byte{1, 2}, ID: "synth"})
	if err != nil {
		t.Fatal(err)
	}
	o := f.workletOptions
	p := o.Get("processorOptions")
	if !p.Get("m").Equal(module) || p.Get("backend").Get("channels").Int() != 1 || p.Get("i").Get("byteLength").Int() != 2 || o.Get("numberOfInputs").Int() != 0 {
		t.Fatal("WASM backend options lost")
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	if f.posted.Get("t").String() != "close" || f.portCloses != 1 {
		t.Fatal("processor did not receive shutdown before closing its port")
	}
	if _, err := host.NewWASMWorklet(WASMWorkletOptions{Module: js.Undefined(), Backend: backend}); err == nil {
		t.Fatal("accepted a missing module")
	}
	_ = host.Close()
	if _, err := host.CompileWASM(nil); err != ErrClosed {
		t.Fatalf("closed compile: %v", err)
	}
}
