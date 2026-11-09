//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/game/audio/worklet"
	"m31labs.dev/gosx/internal/chrometest"
)

// cicadaPageHTML drives the real mixer (the bootstrap's window.__gosx.audioMixer),
// a clip played on the sfx bus, and a Cicada kernel running in GoSX's
// gosx-wasm-audio processor. It does what game/audio/kernel and
// cicada/sdk/gosxhost do from Go, so the browser behaviour is checked without
// a Go wasm build.
const cicadaPageHTML = `<!doctype html><meta charset="utf-8"><script src="/bootstrap.js"></script>
<script>
const b64 = s => Uint8Array.from(atob(s), c => c.charCodeAt(0));
window.runCicada = async function() {
  const out = { kinds: [], landed: [], steps: [] };
  try {
    const mixer = window.__gosx.audioMixer;
    const fx = await (await fetch('/fixture.json')).json();
    const ctx = mixer.context({ sampleRate: fx.rate });
    await ctx.resume();
    out.sampleRate = ctx.sampleRate; out.state = ctx.state;
    await ctx.audioWorklet.addModule('/processor.js');
    const module = await WebAssembly.compile(await (await fetch('/kernel.wasm')).arrayBuffer());
    const image = b64(fx.image);
    const node = new AudioWorkletNode(ctx, 'gosx-wasm-audio', { numberOfInputs: 0, numberOfOutputs: 1,
      channelCount: 2, channelCountMode: 'explicit', outputChannelCount: [2],
      processorOptions: { m: module, i: image.buffer, r: 'k', l: 0, backend: fx.backend,
        info: { tempoMilli: fx.tempoMilli, requiredCapabilities: fx.requiredCapabilities } } });
    const events = [];
    const ready = new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('kernel never became ready')), 10000);
      node.port.onmessage = ({ data }) => {
        if (data.t === 'r') { clearTimeout(timer); resolve(); }
        if (data.t === 'e' || data.t === 'f' || data.t === 'x') out.steps.push('processor ' + JSON.stringify(data.e || data.a || data.r));
        if (data.t === 'm') {
          const bytes = new Uint8Array(data.bytes, 0, data.n);
          for (let at = 0; at < data.n; at += 16) {
            const v = new DataView(bytes.buffer, bytes.byteOffset + at, 16);
            events.push({ kind: bytes[at], a: v.getUint16(2, true), b: v.getUint32(4, true) });
          }
          out.frames = data.f;
          node.port.postMessage({ t: 'b', bytes: data.bytes }, [data.bytes]);
        }
      };
    });
    await ready;
    out.steps.push('ready');
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 2048;
    node.connect(analyser);
    node.connect(mixer.input('music'));
    mixer.setVolume('music', 0.5);
    const send = name => node.port.postMessage({ t: 'c', bytes: b64(fx.batches[name]) });
    const until = (pred, what) => new Promise((resolve, reject) => {
      const t0 = performance.now();
      const tick = () => {
        const hit = events.find(pred);
        if (hit) return resolve(hit);
        if (performance.now() - t0 > 15000) return reject(new Error('timed out waiting for ' + what + '; saw kinds ' + events.map(e => e.kind).join(',')));
        setTimeout(tick, 20);
      };
      tick();
    });
    send('start');
    await until(e => e.kind === 13 && e.b === 0, 'explore to land');
    out.steps.push('explore landed');
    const mark = events.length;
    send('combat'); send('intensity');
    const combat = await new Promise((resolve, reject) => {
      const t0 = performance.now();
      const tick = () => {
        const hit = events.slice(mark).find(e => e.kind === 13 && e.b === 1);
        if (hit) return resolve(hit);
        if (performance.now() - t0 > 15000) return reject(new Error('combat never landed'));
        setTimeout(tick, 20);
      };
      tick();
    });
    out.steps.push('combat landed');
    send('stinger');
    await until(e => e.kind === 14, 'stinger start');
    out.steps.push('stinger started');
    // The mixer's sfx bus: a generated clip, played through window.__gosx.audio.
    const rate = 8000, n = 800, wav = new DataView(new ArrayBuffer(44 + n * 2));
    const w = (o, s) => { for (let i = 0; i < s.length; i++) wav.setUint8(o + i, s.charCodeAt(i)); };
    w(0, 'RIFF'); wav.setUint32(4, 36 + n * 2, true); w(8, 'WAVEfmt '); wav.setUint32(16, 16, true);
    wav.setUint16(20, 1, true); wav.setUint16(22, 1, true); wav.setUint32(24, rate, true); wav.setUint32(28, rate * 2, true);
    wav.setUint16(32, 2, true); wav.setUint16(34, 16, true); w(36, 'data'); wav.setUint32(40, n * 2, true);
    for (let i = 0; i < n; i++) wav.setInt16(44 + i * 2, 8000 * Math.sin(i / 6), true);
    const url = URL.createObjectURL(new Blob([wav.buffer], { type: 'audio/wav' }));
    window.__gosx.audio.load('beep', url, { bus: 'sfx' });
    mixer.setVolume('sfx', 0.7);
    out.sfxHandle = await window.__gosx.audio.play('beep', {});
    out.steps.push('sfx played');
    await new Promise(r => setTimeout(r, 600));
    const samples = new Float32Array(analyser.fftSize);
    let peak = 0;
    for (let i = 0; i < 20; i++) {
      analyser.getFloatTimeDomainData(samples);
      for (const s of samples) peak = Math.max(peak, Math.abs(s));
      await new Promise(r => setTimeout(r, 30));
    }
    out.peak = peak;
    out.kinds = [...new Set(events.map(e => e.kind))].sort((a, b) => a - b);
    out.landed = events.filter(e => e.kind === 13).map(e => e.b);
    out.snapshot = mixer.snapshot();
    out.currentTime = ctx.currentTime;
    out.contextCount = 1;
    node.port.postMessage({ t: 'close' });
  } catch (error) {
    out.error = String(error && error.message || error);
  }
  return JSON.stringify(out);
};
</script>`

type cicadaBrowserResult struct {
	Error       string   `json:"error"`
	Steps       []string `json:"steps"`
	SampleRate  float64  `json:"sampleRate"`
	State       string   `json:"state"`
	Kinds       []int    `json:"kinds"`
	Landed      []int    `json:"landed"`
	Peak        float64  `json:"peak"`
	SfxHandle   string   `json:"sfxHandle"`
	CurrentTime float64  `json:"currentTime"`
	Frames      float64  `json:"frames"`
	Snapshot    map[string]struct {
		Volume float64 `json:"volume"`
	} `json:"snapshot"`
}

// TestCicadaKernelAndMixerInRealBrowser starts the Cicada kernel in a real
// AudioWorklet next to the GoSX mixer, switches scene and intensity, plays a
// stinger and a clip, and reads back the kernel's state messages. Generate its
// inputs with Cicada's TestWriteBrowserFixture and put kernel.wasm beside
// fixture.json in GOSX_CICADA_FIXTURE (default e2e/cicada-fixture).
func TestCicadaKernelAndMixerInRealBrowser(t *testing.T) {
	dir := os.Getenv("GOSX_CICADA_FIXTURE")
	if dir == "" {
		dir = filepath.Join("cicada-fixture")
	}
	for _, name := range []string{"fixture.json", "kernel.wasm"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Skipf("Cicada fixture missing (%s): %v", name, err)
		}
	}
	bootstrap, err := os.ReadFile(filepath.Join("..", "client", "js", "bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(cicadaPageHTML))
	})
	js := func(path string, data []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write(data)
		})
	}
	js("/bootstrap.js", bootstrap)
	js("/processor.js", worklet.Source())
	for _, name := range []string{"fixture.json", "kernel.wasm"} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		mime := "application/json"
		if filepath.Ext(name) == ".wasm" {
			mime = "application/wasm"
		}
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", mime)
			_, _ = w.Write(data)
		})
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	url := server.URL
	if e2eWindowsRelay() {
		url = e2eExposeToWindows(t, server.URL)
	}

	browser, err := chrometest.Start(t.Context(), e2eChromePath(t), "--no-sandbox", "--autoplay-policy=no-user-gesture-required")
	if err != nil {
		if browserUnavailable(err) {
			t.Skipf("chrome could not start: %v", err)
		}
		t.Fatalf("start Chrome: %v", err)
	}
	defer browser.Close()
	ctx, cancel := context.WithTimeout(browser.Context, 90*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(url), chromedp.WaitReady("body")); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.runCicada()`, &raw, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true).WithUserGesture(true)
	})); err != nil {
		t.Fatalf("run: %v", err)
	}
	var r cicadaBrowserResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	t.Logf("steps %v; context %s at %.0f Hz, currentTime %.2fs, kernel position %.0f frames; message kinds %v; scenes landed %v; analyser peak %.4f; sfx handle %q",
		r.Steps, r.State, r.SampleRate, r.CurrentTime, r.Frames, r.Kinds, r.Landed, r.Peak, r.SfxHandle)
	if r.Error != "" {
		t.Fatalf("browser error: %s (steps %v)", r.Error, r.Steps)
	}
	if r.State != "running" || r.SampleRate != 48000 {
		t.Fatalf("context = %s at %v Hz", r.State, r.SampleRate)
	}
	if len(r.Landed) < 2 || r.Landed[0] != 0 || r.Landed[len(r.Landed)-1] != 1 {
		t.Fatalf("scenes landed = %v, want explore (0) then combat (1)", r.Landed)
	}
	has := func(kind int) bool {
		for _, k := range r.Kinds {
			if k == kind {
				return true
			}
		}
		return false
	}
	if !has(9) || !has(14) {
		t.Fatalf("message kinds %v lack Bar (9) or StingerStarted (14)", r.Kinds)
	}
	if r.Peak < 0.001 {
		t.Fatalf("kernel output is silent (peak %v)", r.Peak)
	}
	if r.SfxHandle == "" {
		t.Fatal("the clip did not play through the mixer")
	}
	if r.Snapshot["music"].Volume != 0.5 || r.Snapshot["sfx"].Volume != 0.7 {
		t.Fatalf("mixer snapshot = %+v", r.Snapshot)
	}
	if r.Frames <= 0 {
		t.Fatalf("kernel position = %v", r.Frames)
	}
}
