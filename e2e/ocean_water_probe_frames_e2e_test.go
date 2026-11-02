//go:build e2e

// The host probe against the frame it was issued in, on WebGPU and WebGL2, with a moving sea: TimeScale 1, a drift, a non-zero
// ocean level, and a quality-tier switch in the middle of the run. The page captures the displacement and slope textures the shader
// samples in every frame that issues a probe, keyed by the Seq the reading will carry, and the test evaluates each reading's
// points on the CPU, in double precision, from the textures of its own frame. A reading must match frame N within a stated
// tolerance and be nearer to frame N than to N-1 and N+1, so a reading tagged with the wrong frame is caught. It runs only where
// GOSX_WATER_E2E_REQUIRE_WEBGPU=1 names a GPU runner.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
	"m31labs.dev/gosx/scene"
)

const probeFramesInit = `window.__gosx_scene3d_render_truth = true; window.__frames = {}; window.__capOn = false; window.__capErr = "";
const keyOf = () => window.__capW[0] + 1;
const frameOf = (water) => ({ x: water.x, z: water.z, time: water.time, gen: water.gen, cas: [] });
if (window.WebGL2RenderingContext) {
  const fence = WebGL2RenderingContext.prototype.fenceSync;
  WebGL2RenderingContext.prototype.fenceSync = function (...a) {
    if (window.__capOn && window.__capW) {
      try {
        const gl = this, water = window.__gosx_scene3d_ocean_fft_api.water(), key = keyOf();
        if (water && !window.__frames[key]) {
          const frame = frameOf(water), sizes = window.__tier(water.gen), prevFB = gl.getParameter(gl.READ_FRAMEBUFFER_BINDING), prevUnit = gl.getParameter(gl.ACTIVE_TEXTURE);
          const fb = window.__capFB || (window.__capFB = gl.createFramebuffer());
          const read = (unit, n) => { gl.activeTexture(gl.TEXTURE0 + unit); const tex = gl.getParameter(gl.TEXTURE_BINDING_2D); gl.bindFramebuffer(gl.READ_FRAMEBUFFER, fb); gl.framebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0); const d = new Float32Array(n * n * 4); gl.readPixels(0, 0, n, n, gl.RGBA, gl.FLOAT, d); return d; };
          sizes.forEach((n, i) => frame.cas.push({ n: n, d: read(4 + i, n), s: read(8 + i, n) }));
          gl.bindFramebuffer(gl.READ_FRAMEBUFFER, prevFB); gl.activeTexture(prevUnit);
          window.__frames[key] = frame;
        }
      } catch (e) { window.__capErr = "gl: " + e; }
    }
    return fence.apply(this, a);
  };
}
if (window.GPUAdapter) {
  const request = GPUAdapter.prototype.requestDevice;
  GPUAdapter.prototype.requestDevice = async function (...a) {
    const d = await request.apply(this, a); window.__gpuTex = {};
    const make = d.createTexture.bind(d);
    d.createTexture = (desc) => { desc = Object.assign({}, desc, { usage: desc.usage | GPUTextureUsage.COPY_SRC }); const t = make(desc); if (desc.label) window.__gpuTex[desc.label] = { t, n: desc.size[0] || desc.size.width }; return t; };
    const copy = GPUCommandEncoder.prototype.copyBufferToBuffer;
    GPUCommandEncoder.prototype.copyBufferToBuffer = function (src, so, dst, doff, size) {
      if (window.__capOn && window.__capW && size === 1024) {
        try {
          const water = window.__gosx_scene3d_ocean_fft_api.water(), key = keyOf();
          if (water && !window.__frames[key]) {
            const frame = frameOf(water), pending = [];
            for (let i = 0; i < 4; i++) {
              const dt = window.__gpuTex["gosx-elio-ocean-dispTex" + i], st = window.__gpuTex["gosx-elio-ocean-slopeTex" + i];
              if (!dt || !st || (window.__tier(water.gen).length <= i)) continue;
              for (const [kind, e] of [["d", dt], ["s", st]]) {
                const buf = d.createBuffer({ size: e.n * e.n * 8, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
                this.copyTextureToBuffer({ texture: e.t }, { buffer: buf, bytesPerRow: e.n * 8 }, [e.n, e.n]); pending.push({ i, kind, n: e.n, buf });
              }
            }
            frame.pending = pending; window.__frames[key] = frame;
          }
        } catch (e) { window.__capErr = "gpu: " + e; }
      }
      return copy.call(this, src, so, dst, doff, size);
    };
    return d;
  };
}`

// The page script: ask for a 7 x 5 grid, collect readings while the sea moves, switch tier part-way, then compare each reading
// with a CPU evaluation of the textures captured in its own frame.
const probeFramesScript = `(async () => {
  const el = document.getElementById("sea"), api = window.__gosx.scene3d, wait = (ms) => new Promise((r) => setTimeout(r, ms));
  await api.whenReady("sea");
  const w = api.oceanWater("sea"); window.__capW = w;
  const out = { backend: el.getAttribute("data-gosx-scene3d-backend"), error: "", readings: [], gens: [], capErr: "" };
  const G = window.__gosx_elio_graph, M = window.__gosx_scene3d_elio_ocean, webgl = out.backend === "webgl", tag = webgl ? "webgl2" : "webgpu";
  const info = { high: window.__gosx_scene3d_ocean_fft_api.describe(G.normalize(M.graphs[tag + "/high"], M)).cascades, low: window.__gosx_scene3d_ocean_fft_api.describe(G.normalize(M.graphs[tag + "/low"], M)).cascades };
  let firstGen = 0, switched = false;
  window.__tier = (gen) => (switched && gen !== firstGen ? info.low : info.high).map((c) => c.n);
  for (let i = 0; i < 400 && !(w[0] >= 30 && w[4] === 1); i++) await wait(50);
  if (w[4] !== 1) { out.error = "no spectral frame"; return JSON.stringify(out); }
  await wait(1500);
  firstGen = w[9];
  const pts = ` + probePoints + `;
  window.__capOn = true; w[10] = pts.length; w[338] = 1; pts.forEach((p, i) => { w[18 + 2 * i] = p[0]; w[19 + 2 * i] = p[1]; });
  let last = w[17], n = 0;
  const take = () => { out.readings.push({ seq: w[11], gen: w[12], req: w[339], lat: w[15], flags: w[13], heights: Array.from(w.slice(146, 146 + 105)).filter((_, i) => i % 3 === 0), normals: Array.from(w.slice(146, 146 + 105)).map((v, i) => [v, i]).filter(([, i]) => i % 3 !== 0).map(([v]) => v), water: Array.from(w.slice(0, 10)) }); };
  for (let i = 0; i < 3000 && n < 90; i++) {
    await new Promise((r) => requestAnimationFrame(r));
    if (n === 40 && !switched) { switched = true; Object.defineProperty(navigator, "deviceMemory", { get: () => 4, configurable: true }); Object.defineProperty(navigator, "hardwareConcurrency", { get: () => 4, configurable: true }); }
    if (w[17] !== last) { last = w[17]; n++; take(); }
  }
  window.__capOn = false; w[10] = 0;
  await wait(500);
  // WebGPU frames are staged in buffers; read them now.
  const half = (h) => { const e = (h >> 10) & 31, f = h & 1023, sgn = h & 0x8000 ? -1 : 1; return e === 0 ? sgn * f * Math.pow(2, -24) : e === 31 ? (f ? NaN : sgn * Infinity) : sgn * (1 + f / 1024) * Math.pow(2, e - 15); };
  for (const key of Object.keys(window.__frames)) {
    const fr = window.__frames[key];
    if (!fr.pending) continue;
    for (const p of fr.pending) { await p.buf.mapAsync(GPUMapMode.READ); const raw = new Uint16Array(p.buf.getMappedRange().slice(0)); p.buf.unmap(); p.buf.destroy(); (fr.cas[p.i] = fr.cas[p.i] || { n: p.n })[p.kind] = Float32Array.from(raw, half); }
    delete fr.pending;
  }
  out.capErr = window.__capErr;
  const level = 2;
` + probeRefJS + `
  const frameFor = (key, tiles) => { const f = window.__frames[key]; if (!f || !f.cas.length) return null; const cas = f.cas.map((c, i) => ({ tile: tiles[i].tile, n: c.n, d: c.d, s: c.s, om: [f.x - Math.floor(f.x / tiles[i].tile) * tiles[i].tile, f.z - Math.floor(f.z / tiles[i].tile) * tiles[i].tile] })); return { cas, gen: f.gen }; };
  const tilesOf = (cas) => (cas.length === 3 ? info.low : info.high);
  const errAgainst = (reading, key) => {
    const f = window.__frames[key]; if (!f || !f.cas.length) return null;
    const cas = frameFor(key, tilesOf(f.cas)).cas; let eh = 0, en = 0;
    for (let i = 0; i < pts.length; i++) { const r = refAt(cas, pts[i], level); eh = Math.max(eh, Math.abs(r.h - reading.heights[i])); en = Math.max(en, Math.abs(r.n[0] - reading.normals[2 * i]), Math.abs(r.n[1] - reading.normals[2 * i + 1])); }
    return [eh, en];
  };
  out.compare = out.readings.map((r) => ({ seq: r.seq, gen: r.gen, lat: r.lat, req: r.req, flags: r.flags, own: errAgainst(r, r.seq), prev: errAgainst(r, r.seq - 1), next: errAgainst(r, r.seq + 1), have: !!window.__frames[r.seq], cascades: window.__frames[r.seq] ? window.__frames[r.seq].cas.length : 0, waterGen: window.__frames[r.seq] ? window.__frames[r.seq].gen : 0 }));
  return JSON.stringify(out);
})()`

type frameCompare struct {
	Seq, Gen, Lat, Req, Flags float64
	Own, Prev, Next           []float64
	Have                      bool
	Cascades                  int
	WaterGen                  float64
}

func TestWaterProbeFramesOnTheGPU(t *testing.T) {
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run 'TestWater' ./e2e)")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	spectrum := scene.OceanPreset("rough")
	props := scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{
		Level: 2, Spectrum: &spectrum,
		Presentation: &scene.OceanPresentation{TimeScale: 1, WaterDriftX: 3, WaterDriftZ: -2, TransitionSeconds: 0.1},
	}}}
	env, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"engines":[{"id":"gosx-engine-sea","component":"GoSXScene3D","kind":"surface","mountId":"sea","props":{"width":800,"height":450,"background":"#000000","camera":{"x":0,"y":14,"z":40,"fov":60},"scene":{"environment":%s,"objects":[]}}}]}`, env)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), "gosx/")
		if name == "bootstrap.js" || strings.HasPrefix(name, "bootstrap-feature-") {
			data, err := os.ReadFile(filepath.Join(root, "client", "js", filepath.Base(name)))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, oceanWaterPage, manifest)
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()
	pageURL := fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port)

	for _, backend := range []string{"webgpu", "webgl2"} {
		t.Run(backend, func(t *testing.T) {
			browser, err := chrometest.Start(t.Context(), chrome, "--no-sandbox", "--mute-audio", "--window-size=900,520")
			if err != nil {
				t.Skipf("chrome unavailable: %v", err)
			}
			defer browser.Close()
			ctx, cancel := context.WithTimeout(browser.Context, 240*time.Second)
			defer cancel()
			init := probeFramesInit
			if backend == "webgl2" {
				init += `window.__gosx_scene3d_force_webgl = true;`
			}
			if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(init).Do(ctx)
				return err
			})); err != nil {
				t.Fatal(err)
			}
			err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			for i := 0; err != nil && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			}
			if err != nil {
				t.Fatal(err)
			}
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(probeFramesScript, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
				t.Fatal(err)
			}
			var r struct {
				Backend, Error, CapErr string
				Compare                []frameCompare
			}
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				t.Fatalf("%s: %v", raw[:min(len(raw), 400)], err)
			}
			if r.Error != "" || r.CapErr != "" {
				t.Fatalf("%s: error %q capture %q", backend, r.Error, r.CapErr)
			}
			if dir := os.Getenv("OCEAN_PROBE_EVIDENCE"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				blob, _ := json.MarshalIndent(r.Compare, "", " ")
				_ = os.WriteFile(filepath.Join(dir, "frames-"+backend+".json"), blob, 0o644)
			}
			var worstH, worstN, worstRatio float64
			gens := map[float64]bool{}
			var matched, distinct, lats int
			var latSum float64
			for _, c := range r.Compare {
				gens[c.Gen] = true
				if !c.Have || c.Own == nil {
					continue
				}
				matched++
				worstH, worstN = math.Max(worstH, c.Own[0]), math.Max(worstN, c.Own[1])
				near := math.Inf(1)
				for _, o := range [][]float64{c.Prev, c.Next} {
					if o != nil {
						near = math.Min(near, o[0])
					}
				}
				if !math.IsInf(near, 1) {
					worstRatio = math.Max(worstRatio, c.Own[0]/near)
					if c.Own[0] < near {
						distinct++
					}
				}
				latSum += c.Lat
				lats++
				if c.Flags != 1 {
					t.Fatalf("seq %v: flags %v", c.Seq, c.Flags)
				}
			}
			t.Logf("%s: %d readings, %d matched to a captured frame, %d generations; vs own frame: max height diff %.5f m, max normal diff %.5f; nearer to own frame than to N-1 and N+1 in %d; worst own/neighbour error ratio %.3f; mean latency %.2f frames",
				backend, len(r.Compare), matched, len(gens), worstH, worstN, distinct, worstRatio, latSum/math.Max(1, float64(lats)))
			if matched < 40 {
				t.Fatalf("only %d readings could be matched to a captured frame", matched)
			}
			if len(gens) < 2 {
				t.Fatalf("the tier switch did not change the generation: %v", gens)
			}
			if worstH > 0.015 || worstN > 0.02 {
				t.Fatalf("a reading differs from its own frame: height %.5f m, normal %.5f", worstH, worstN)
			}
			if distinct < matched-matched/10 {
				t.Fatalf("readings are not nearer to their own frame than to its neighbours: %d of %d", distinct, matched)
			}
		})
	}
}
