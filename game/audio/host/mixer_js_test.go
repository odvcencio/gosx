//go:build js && wasm

package audiohost

import (
	"syscall/js"
	"testing"
)

// installFakeMixer publishes a window.__gosx.audioMixer double built in JS. It
// records every call in globalThis.__mixerCalls so tests can check the Go side
// drives the page mixer rather than a private graph.
func installFakeMixer(t *testing.T, sampleRate float64) js.Value {
	t.Helper()
	g := js.Global()
	previous := g.Get("__gosx")
	factory := g.Get("Function").New("rate", `
		const calls = [];
		const node = (name) => ({ name, connect() {}, disconnect() {} });
		const ctx = { sampleRate: rate, currentTime: 1, state: "running", closed: 0, close() { this.closed++; return Promise.resolve(); },
			destination: node("destination"), listener: { setPosition(...a) { calls.push(["lpos", ...a]); }, setOrientation(...a) { calls.push(["lori", ...a]); } },
			createPanner() { return Object.assign(node("panner"), { positionX: undefined, setPosition(...a) { calls.push(["ppos", ...a]); } }); } };
		const inputs = {};
		const state = { music: { v: 1, m: false } };
		const mixer = {
			context(options) { calls.push(["context", options && options.sampleRate]); return ctx; },
			input(id) { return inputs[id] || (inputs[id] = node(id)); },
			registerBus(id, parent) { calls.push(["registerBus", id, parent]); },
			setVolume(id, v) { calls.push(["setVolume", id, v]); (state[id] = state[id] || { v: 1, m: false }).v = v; },
			volume(id) { return (state[id] || { v: 1 }).v; },
			setMuted(id, m) { calls.push(["setMuted", id, m]); (state[id] = state[id] || { v: 1, m: false }).m = m; },
			muted(id) { return (state[id] || { m: false }).m; },
			duck(id, a, s, k) { calls.push(["duck", id, a, s, k]); },
			unduck(id, k, s) { calls.push(["unduck", id, k, s]); },
		};
		globalThis.__gosx = Object.assign(globalThis.__gosx || {}, { audioMixer: mixer });
		globalThis.__mixerCalls = calls;
		globalThis.__mixerCtx = ctx;
		return mixer;`)
	mixer := factory.Invoke(sampleRate)
	t.Cleanup(func() {
		if previous.Type() == js.TypeObject {
			g.Set("__gosx", previous)
		} else {
			g.Delete("__gosx")
		}
	})
	return mixer
}

func mixerCalls() js.Value { return js.Global().Get("__mixerCalls") }

func hasCall(name string, args ...any) bool {
	calls := mixerCalls()
	for i := 0; i < calls.Length(); i++ {
		call := calls.Index(i)
		if call.Index(0).String() != name || call.Length() != len(args)+1 {
			continue
		}
		match := true
		for j, want := range args {
			got := call.Index(j + 1)
			switch w := want.(type) {
			case string:
				match = match && got.Type() == js.TypeString && got.String() == w
			case float64:
				match = match && got.Type() == js.TypeNumber && got.Float() == w
			case bool:
				match = match && got.Type() == js.TypeBoolean && got.Bool() == w
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestHostJSAdoptsSharedMixerContextAndDoesNotCloseIt(t *testing.T) {
	installFakeMixer(t, 48000)
	host, err := NewHostJSWithOptions(HostOptions{SampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	if !host.Shared() {
		t.Fatal("host did not adopt the page mixer")
	}
	if !hasCall("context", 48000.0) {
		t.Fatal("sample rate was not passed to the mixer's context()")
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if js.Global().Get("__mixerCtx").Get("closed").Int() != 0 {
		t.Fatal("closing a shared host closed the page's AudioContext")
	}
}

func TestHostJSRateMismatchFallsBackToPrivateContext(t *testing.T) {
	installFakeMixer(t, 44100)
	f := newBrowserAudioFixture(t)
	host, err := NewHostJSWithOptions(HostOptions{SampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	if host.Shared() {
		t.Fatal("host shared a context at the wrong rate")
	}
	if f.contextOptions.IsUndefined() {
		t.Fatal("no private AudioContext was constructed")
	}
}

func TestHostJSPrivateOptionSkipsSharedMixer(t *testing.T) {
	installFakeMixer(t, 48000)
	f := newBrowserAudioFixture(t)
	host, err := NewHostJSWithOptions(HostOptions{Private: true})
	if err != nil {
		t.Fatal(err)
	}
	if host.Shared() || f.contextOptions.IsUndefined() {
		t.Fatal("Private host must construct its own context")
	}
}

func TestMixerBusStateGoesToPageMixer(t *testing.T) {
	installFakeMixer(t, 48000)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	bus, ok := host.MixerBus("music", "")
	if !ok {
		t.Fatal("no music bus")
	}
	bus.SetVolume(0.25)
	bus.SetMuted(true)
	bus.Duck("voice", 0.5, 0.1)
	bus.Unduck("voice", 0.3)
	for _, c := range []struct {
		name string
		args []any
	}{
		{"setVolume", []any{"music", 0.25}},
		{"setMuted", []any{"music", true}},
		{"duck", []any{"music", 0.5, 0.1, "voice"}},
		{"unduck", []any{"music", "voice", 0.3}},
	} {
		if !hasCall(c.name, c.args...) {
			t.Errorf("mixer call %s%v missing", c.name, c.args)
		}
	}
	if bus.Volume() != 0.25 || !bus.Muted() {
		t.Fatal("bus reads do not come from the page mixer")
	}

	if _, ok := host.MixerBus("ambience", "music"); !ok || !hasCall("registerBus", "ambience", "music") {
		t.Fatal("custom bus was not registered under its parent")
	}
}

func TestSharedHostListenerAndPannerGoThroughPageContext(t *testing.T) {
	installFakeMixer(t, 48000)
	host, err := NewHostJS()
	if err != nil {
		t.Fatal(err)
	}
	// The fake mixer has no setListener, so the host sets the listener itself.
	if err := host.SetListener([3]float64{1, 2, 3}, [3]float64{0, 0, -1}, [3]float64{0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if !hasCall("lpos", 1.0, 2.0, 3.0) || !hasCall("lori", 0.0, 0.0, -1.0, 0.0, 1.0, 0.0) {
		t.Fatal("listener was not placed")
	}
	p := host.CreatePanner()
	if err := p.SetPosition(4, 5, 6); err != nil {
		t.Fatal(err)
	}
	if !hasCall("ppos", 4.0, 5.0, 6.0) {
		t.Fatal("panner position was not set")
	}
	if _, ok := host.Destination().(*busInputJS); !ok {
		t.Fatal("shared host destination must be the master bus input")
	}
}
