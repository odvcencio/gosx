//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/game/audio/worklet"
)

// leb128 appends v as an unsigned LEB128 number.
func leb128(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(dst, b)
		}
		dst = append(dst, b|0x80)
	}
}

func wasmSection(id byte, body []byte) []byte {
	out := []byte{id}
	out = leb128(out, uint32(len(body)))
	return append(out, body...)
}

func wasmName(s string) []byte { return append(leb128(nil, uint32(len(s))), s...) }

// testReactorWASM assembles a minimal gosx_audio reactor with prefix "test_".
// Each render writes the number of committed commands into the first sample of
// channel 0; every other sample of both channels is 0.5 (a data segment). That
// is enough to prove the browser runs the processor, the reactor and the
// command path with no synthesis code.
func testReactorWASM() []byte {
	i32, noParam := byte(0x7f), []byte{}
	functype := func(params []byte, results []byte) []byte {
		b := []byte{0x60}
		b = leb128(b, uint32(len(params)))
		b = append(b, params...)
		b = leb128(b, uint32(len(results)))
		return append(b, results...)
	}
	types := [][]byte{
		functype(noParam, noParam),                   // 0: () -> ()
		functype([]byte{i32, i32, i32}, []byte{i32}), // 1: init
		functype([]byte{i32}, noParam),               // 2: (i32) -> ()
		functype(noParam, []byte{i32}),               // 3: () -> i32
	}
	typeBody := leb128(nil, uint32(len(types)))
	for _, t := range types {
		typeBody = append(typeBody, t...)
	}
	// Functions: _initialize, init, render, out_ptr, cmd_ptr, cmd_cap, cmd_commit, msg_ptr, msg_drain
	funcTypes := []byte{0, 1, 2, 3, 3, 3, 2, 3, 3}
	funcBody := append(leb128(nil, uint32(len(funcTypes))), funcTypes...)

	memBody := []byte{1, 0, 1} // one memory, no max, min 1 page
	globalBody := []byte{1, i32, 1, 0x41, 0, 0x0b}

	names := []string{"_initialize", "test_init", "test_render", "test_out_ptr", "test_cmd_ptr", "test_cmd_cap", "test_cmd_commit", "test_msg_ptr", "test_msg_drain"}
	exportBody := leb128(nil, uint32(len(names)+1))
	exportBody = append(exportBody, wasmName("memory")...)
	exportBody = append(exportBody, 2, 0)
	for i, n := range names {
		exportBody = append(exportBody, wasmName(n)...)
		exportBody = append(exportBody, 0, byte(i))
	}

	constBody := func(v uint32) []byte {
		b := []byte{0x41}
		b = leb128(b, v) // i32.const is signed LEB; every value here is below 8192
		if v >= 64 && v < 8192 && b[len(b)-1]&0x40 != 0 {
			// A positive value whose final 7-bit group has bit 6 set needs one more byte for the sign.
			b[len(b)-1] |= 0x80
			b = append(b, 0)
		}
		return append(b, 0x0b)
	}
	bodies := [][]byte{
		{0x0b},
		{0x41, 0x00, 0x0b},
		append(append(append([]byte{}, constBody(4096)[:len(constBody(4096))-1]...), 0x23, 0x00, 0xb3, 0x38, 0x02, 0x00), 0x0b),
		constBody(4096),
		constBody(1024),
		constBody(8),
		{0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x0b},
		constBody(3072),
		constBody(0),
	}
	codeBody := leb128(nil, uint32(len(bodies)))
	for _, b := range bodies {
		full := append([]byte{0}, b...) // zero local declarations
		codeBody = leb128(codeBody, uint32(len(full)))
		codeBody = append(codeBody, full...)
	}

	half := []byte{0, 0, 0, 0x3f} // float32 0.5, little-endian
	data := make([]byte, 0, 1024)
	for i := 0; i < 256; i++ {
		data = append(data, half...)
	}
	dataBody := []byte{1, 0}
	dataBody = append(dataBody, constBody(4096)...)
	dataBody = leb128(dataBody, uint32(len(data)))
	dataBody = append(dataBody, data...)

	out := []byte{0, 'a', 's', 'm', 1, 0, 0, 0}
	for _, s := range [][]byte{wasmSection(1, typeBody), wasmSection(3, funcBody), wasmSection(5, memBody),
		wasmSection(6, globalBody), wasmSection(7, exportBody), wasmSection(10, codeBody), wasmSection(11, dataBody)} {
		out = append(out, s...)
	}
	return out
}

type offlineResult struct {
	Error    string    `json:"error"`
	Rate     float64   `json:"rate"`
	Channel0 []float64 `json:"ch0"`
	Channel1 []float64 `json:"ch1"`
	Messages []string  `json:"messages"`
}

const offlineWorkletHTML = `<!doctype html><meta charset="utf-8"><title>wasm worklet</title>
<script>
window.runOffline = async function(wasmBase64, commandRecords) {
  const out = { messages: [] };
  try {
    const bytes = Uint8Array.from(atob(wasmBase64), c => c.charCodeAt(0));
    const module = await WebAssembly.compile(bytes);
    const quanta = 16, rate = 48000;
    const ctx = new OfflineAudioContext(2, 128 * quanta, rate);
    await ctx.audioWorklet.addModule('/processor.js');
    const backend = { prefix: 'test_', maxFrames: 128, channels: 2, commandBytes: 16, messageBytes: 4,
      messageCapacity: 8, supportedCapabilities: 0 };
    const node = new AudioWorkletNode(ctx, 'gosx-wasm-audio', { numberOfInputs: 0, numberOfOutputs: 1,
      channelCount: 2, channelCountMode: 'explicit', outputChannelCount: [2],
      processorOptions: { m: module, backend, info: { tempoMilli: 0, requiredCapabilities: 0 }, r: 'offline', l: 0 } });
    const ready = new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('processor never reported ready')), 5000);
      node.port.onmessage = ({ data }) => {
        out.messages.push(data.t);
        if (data.t === 'r') { clearTimeout(timer); resolve(); }
        if (data.t === 'e' || data.t === 'x') { clearTimeout(timer); reject(new Error(data.e)); }
      };
    });
    await ready;
    if (commandRecords) node.port.postMessage({ t: 'c', bytes: new Uint8Array(commandRecords * 16) });
    // A bus-like gain proves the node connects to ordinary graph nodes.
    const bus = ctx.createGain();
    bus.gain.value = 0.5;
    node.connect(bus).connect(ctx.destination);
    const buffer = await ctx.startRendering();
    out.rate = buffer.sampleRate;
    out.ch0 = Array.from(buffer.getChannelData(0));
    out.ch1 = Array.from(buffer.getChannelData(1));
  } catch (error) {
    out.error = String(error && error.message || error);
  }
  return JSON.stringify(out);
};
window.pageReady = true;
</script>`

func TestWASMWorkletRendersInOfflineAudioContext(t *testing.T) {
	chrome := e2eChromePath(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(offlineWorkletHTML))
	})
	mux.HandleFunc("/processor.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(worklet.Source())
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := newChromeContext(t, chrome)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitReady("body")); err != nil {
		if browserUnavailable(err) {
			t.Skipf("chrome could not start in this environment: %v", err)
		}
		t.Fatalf("navigate: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(testReactorWASM())
	run := func(commands int) offlineResult {
		t.Helper()
		var raw string
		call := `window.runOffline(` + jsString(encoded) + `, ` + jsNumber(commands) + `)`
		evalCtx, evalCancel := context.WithTimeout(ctx, 12*time.Second)
		defer evalCancel()
		if err := chromedp.Run(evalCtx, chromedp.Evaluate(call, &raw, awaitPromise)); err != nil {
			t.Fatalf("run offline render: %v", err)
		}
		var result offlineResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatalf("decode result %q: %v", raw, err)
		}
		if result.Error != "" {
			t.Fatalf("browser error: %s (messages %v)", result.Error, result.Messages)
		}
		return result
	}

	// With no commands the kernel counts 0; every other sample is 0.5 * bus gain 0.5.
	idle := run(0)
	if len(idle.Channel0) != 128*16 || len(idle.Channel1) != 128*16 {
		t.Fatalf("rendered %d/%d frames, want %d", len(idle.Channel0), len(idle.Channel1), 128*16)
	}
	for i, v := range idle.Channel0 {
		want := 0.25
		if i%128 == 0 {
			want = 0
		}
		if v != want {
			t.Fatalf("idle ch0[%d] = %v, want %v", i, v, want)
		}
	}
	for i, v := range idle.Channel1 {
		if v != 0.25 {
			t.Fatalf("idle ch1[%d] = %v, want 0.25", i, v)
		}
	}

	// Three posted command records reach the reactor's cmd_commit, so the first
	// sample of every quantum reads 3 * 0.5 after the bus gain.
	driven := run(3)
	for q := 0; q < 16; q++ {
		if got := driven.Channel0[q*128]; got != 1.5 {
			t.Fatalf("quantum %d first sample = %v, want 1.5 (3 commands through a 0.5 gain)", q, got)
		}
		if got := driven.Channel0[q*128+1]; got != 0.25 {
			t.Fatalf("quantum %d second sample = %v, want 0.25", q, got)
		}
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func jsNumber(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func awaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	return p.WithAwaitPromise(true)
}
