//go:build js && wasm

package audiohost

import (
	"encoding/binary"
	"math"
	"syscall/js"
	"testing"
)

// This test runs against the actual WebAudio implementation when the WASM
// suite is hosted in a browser. The Node bridge suite exercises the scripted
// host fixtures instead. It needs no sound assets or autoplay-policy bypass.
func TestHostJSRealBrowserAudioLifecycle(t *testing.T) {
	if js.Global().Get("AudioContext").Type() != js.TypeFunction && js.Global().Get("webkitAudioContext").Type() != js.TypeFunction {
		t.Skip("real WebAudio is unavailable in this WASM host")
	}
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	buffer, err := host.DecodeAudioData(testPCMFile())
	if err != nil || math.Abs(BufferDuration(buffer)-.05) > .001 {
		t.Fatal("real decoder changed buffer duration", err)
	}
	source := host.CreateSource(buffer)
	source.SetLoop(true)
	source.SetPlaybackRate(.5)
	if err := SetLoopRegion(source, .005, .045); err != nil {
		t.Fatal(err)
	}
	gain := host.CreateAutomatedGain()
	pan := host.CreateStereoPanner()
	delay, err := host.CreateDelay(.1)
	if err != nil {
		t.Fatal(err)
	}
	if err := pan.SetPan(.35); err != nil {
		t.Fatal(err)
	}
	if err := delay.SetDelay(.015); err != nil {
		t.Fatal(err)
	}
	source.Connect(gain)
	gain.Connect(pan)
	pan.Connect(delay)
	delay.Connect(host.Destination())
	at := host.CurrentTime() + .01
	for _, err := range []error{gain.Gain().SetAt(0, at), gain.Gain().LinearRamp(.1, at+.01), gain.Gain().SetAt(.1, at+.025), gain.Gain().ExponentialRamp(.0001, at+.035), gain.Gain().LinearRamp(0, at+.04)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	ends := 0
	voice := NewVoice(source, func() { ends++ }, gain, pan)
	if err := StartRegion(source, at, .01, 0); err != nil {
		t.Fatal(err)
	}
	voice.Stop(at + .05)
	// Closing a suspended context must release callbacks without relying on an
	// ended event. Resume is requested synchronously as a gesture handler would.
	if err := host.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	voice.Close()
	delay.Disconnect()
	if host.State() != "closed" || len(host.sources) != 0 || ends != 1 {
		t.Fatal("real context disposal retained voices")
	}
}

func testPCMFile() []byte {
	const frames = 2400
	data := make([]byte, 44+frames*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 96000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], frames*2)
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(1000*math.Sin(2*math.Pi*440*float64(i)/48000))))
	}
	return data
}
