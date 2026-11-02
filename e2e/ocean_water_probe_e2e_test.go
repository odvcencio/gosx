//go:build e2e

// The drawn water's heights at host-given points, read back from the GPU (scene.OceanProbeReading), on WebGPU and
// on WebGL2. The sea is made nearly static (TimeScale 0.0001) so two backends draw the same surface. For each
// backend the test asks for a 7 x 5 grid of points, waits for readings, and checks:
//   - the reading is tagged with a drawn frame and its generation, and its age in frames is small;
//   - on WebGL2, the heights equal a CPU evaluation (double precision) of the very displacement and slope textures the
//     ocean shader samples, read back from the GL context, within a stated tolerance;
//   - the two backends' heights agree within a stated tolerance.
//
// It is gated like the water certification: it needs a real GPU, so it runs only where GOSX_WATER_E2E_REQUIRE_WEBGPU=1.
// OCEAN_PROBE_EVIDENCE names a folder for screenshots and the readings as JSON.
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

type probeReading struct {
	Backend string      `json:"backend"`
	Ocean   string      `json:"ocean"`
	W       []float64   `json:"w"`
	Heights []float64   `json:"heights"`
	Normals [][]float64 `json:"normals"`
	Ref     []float64   `json:"ref"`
	RefN    [][]float64 `json:"refN"`
	Lat     []float64   `json:"lat"`
	Seq     []float64   `json:"seq"`
	Ticks   float64     `json:"ticks"`
	Labels  []string    `json:"labels"`
	Err     string      `json:"error"`
}

const probePoints = `(() => { const p = []; for (let i = 0; i < 7; i++) for (let j = 0; j < 5; j++) p.push([-12 + i * 4 + j * 0.37, -9 + j * 4.5 + i * 0.21]); return p; })()`

// probeRefJS is a double-precision CPU evaluation of the cascades' displacement and slope textures, as the ocean shaders sample them
// (bilinear at the texel centres, repeating), with the same Newton inversion of the horizontal map. cas[i] = { tile, n, d, s, om: [x, z] }.
const probeRefJS = `
  const bil = (d, n, u, v) => {
    const x = u * n - 0.5, y = v * n - 0.5, x0 = Math.floor(x), y0 = Math.floor(y), fx = x - x0, fy = y - y0, r = [0, 0, 0, 0];
    for (const [i, j, k] of [[0, 0, (1 - fx) * (1 - fy)], [1, 0, fx * (1 - fy)], [0, 1, (1 - fx) * fy], [1, 1, fx * fy]]) {
      const o = ((((y0 + j) % n) + n) % n * n + (((x0 + i) % n) + n) % n) * 4; for (let c = 0; c < 4; c++) r[c] += k * d[o + c];
    }
    return r;
  };
  const evalCas = (cas, q) => {
    let s = [0, 0, 0], dx = [1, 0, 0], dz = [0, 0, 1];
    for (const c of cas) {
      const u = (q[0] - c.om[0]) / c.tile, v = (q[1] - c.om[1]) / c.tile, a = bil(c.d, c.n, u - Math.floor(u), v - Math.floor(v)), b = bil(c.s, c.n, u - Math.floor(u), v - Math.floor(v));
      s[0] += a[0]; s[1] += a[1]; s[2] += a[2]; dx[0] += b[2]; dx[1] += b[0]; dx[2] += a[3]; dz[0] += a[3]; dz[1] += b[1]; dz[2] += b[3];
    }
    return { s, dx, dz };
  };
  const refAt = (cas, t, level) => {
    let q = [t[0], t[1]], e;
    for (let k = 0; k < 8; k++) {
      e = evalCas(cas, q); const ex = q[0] + e.s[0] - t[0], ez = q[1] + e.s[2] - t[1], det = e.dx[0] * e.dz[2] - e.dz[0] * e.dx[2];
      if (Math.abs(det) > 1e-4) { q = [q[0] - (e.dz[2] * ex - e.dz[0] * ez) / det, q[1] - (e.dx[0] * ez - e.dx[2] * ex) / det]; } else { q = [q[0] - ex * 0.5, q[1] - ez * 0.5]; }
    }
    e = evalCas(cas, q);
    const n = [e.dz[1] * e.dx[2] - e.dz[2] * e.dx[1], e.dz[2] * e.dx[0] - e.dz[0] * e.dx[2], e.dz[0] * e.dx[1] - e.dz[1] * e.dx[0]], l = Math.hypot(n[0], n[1], n[2]);
    return { h: level + e.s[1], n: [n[0] / l, n[2] / l] };
  };
`

func TestWaterProbeOnTheGPU(t *testing.T) {
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run 'TestWater' ./e2e)")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	spectrum := scene.OceanPreset("rough")
	props := scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{
		Level: 2, Spectrum: &spectrum,
		Presentation: &scene.OceanPresentation{TimeScale: 0.0001, TransitionSeconds: 0.1}, // a nearly static sea, whose sea-state blend has finished
	}}}
	env, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"engines":[{"id":"gosx-engine-sea","component":"GoSXScene3D","kind":"surface","mountId":"sea","props":{"width":800,"height":450,"background":"#000000","camera":{"x":0,"y":14,"z":40,"fov":60},"scene":{"environment":%s,"objects":[]}}}]}`, env)
	gerstner, _ := json.Marshal(scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{}}}.SceneIR().Environment)
	gerstnerManifest := fmt.Sprintf(`{"engines":[{"id":"gosx-engine-sea","component":"GoSXScene3D","kind":"surface","mountId":"sea","props":{"width":800,"height":450,"background":"#000000","camera":{"x":0,"y":14,"z":40,"fov":60},"scene":{"environment":%s,"objects":[]}}}]}`, gerstner)
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
		if r.URL.Query().Get("gerstner") != "" {
			_, _ = fmt.Fprintf(w, oceanWaterPage, gerstnerManifest)
			return
		}
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

	// The page script: ask for the points, collect readings for a while (age and tags), and, on WebGL2, evaluate the same
	// textures on the CPU in double precision.
	script := `(async () => {
  const el = document.getElementById("sea"), api = window.__gosx.scene3d;
  await api.whenReady("sea");
  const w = api.oceanWater("sea");
  const pts = ` + probePoints + `;
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const out = { backend: el.getAttribute("data-gosx-scene3d-backend"), ocean: el.getAttribute("data-gosx-scene3d-ocean"), lat: [], seq: [], ticks: 0, error: "" };
  for (let i = 0; i < 400 && !(w[0] >= 30 && w[4] === 1); i++) await wait(50);
  if (!(w[4] === 1)) { out.error = "no spectral frame"; out.w = Array.from(w.slice(0, 18)); return JSON.stringify(out); }
  await wait(1500); // the sea-state blend (0.1 s) is long over
  w[10] = pts.length; pts.forEach((p, i) => { w[18 + 2 * i] = p[0]; w[19 + 2 * i] = p[1]; });
  let last = w[17], first = last;
  for (let i = 0; i < 1500 && out.lat.length < 60; i++) {
    await wait(10);
    if (w[17] !== last) { last = w[17]; if (w[13] & 4) { out.error = "unavailable"; break; } out.lat.push(w[15]); out.seq.push(w[0] - w[11]); }
  }
  out.ticks = w[17] - first;
  out.w = Array.from(w.slice(0, 18));
  out.heights = []; out.normals = [];
  for (let i = 0; i < pts.length; i++) { out.heights.push(w[146 + 3 * i]); out.normals.push([w[147 + 3 * i], w[148 + 3 * i]]); }
  // Stop asking, so the textures the CPU reads are the last frame's.
  w[10] = 0;
  await wait(300);
  const canvas = el.querySelector("canvas"), webgl = el.getAttribute("data-gosx-scene3d-backend") === "webgl", gl = webgl ? canvas.getContext("webgl2") : null;
  out.labels = (window.__gpuTex || []).map((x) => [x.desc.label, JSON.stringify(x.desc.size), x.desc.format].join(":"));
  {
    try {
      const G = window.__gosx_elio_graph, M = window.__gosx_scene3d_elio_ocean, info = window.__gosx_scene3d_ocean_fft_api.describe(G.normalize(M.graphs[webgl ? "webgl2/high" : "webgpu/high"], M));
      const half = (h) => { const e = (h >> 10) & 31, f = h & 1023, sgn = h & 0x8000 ? -1 : 1; return e === 0 ? sgn * f * Math.pow(2, -24) : e === 31 ? (f ? NaN : sgn * Infinity) : sgn * (1 + f / 1024) * Math.pow(2, e - 15); };
      const fb = webgl ? gl.createFramebuffer() : null;
      const fetchGL = async (unit, n) => {
        gl.activeTexture(gl.TEXTURE0 + unit);
        const tex = gl.getParameter(gl.TEXTURE_BINDING_2D);
        gl.bindFramebuffer(gl.READ_FRAMEBUFFER, fb); gl.framebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
        if (gl.checkFramebufferStatus(gl.READ_FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) throw new Error("texture " + unit + " is not readable");
        const d = new Float32Array(n * n * 4); gl.readPixels(0, 0, n, n, gl.RGBA, gl.FLOAT, d); gl.bindFramebuffer(gl.READ_FRAMEBUFFER, null);
        return d;
      };
      // WebGPU: the graph's displacement and slope textures are the rgba16float ones of each cascade's size, created in order disp0, slope0, ... (labels are in out.labels).
      const fetchGPU = async (entry, n) => {
        const dev = window.__gpuDevice, buf = dev.createBuffer({ size: n * n * 8, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
        const enc = dev.createCommandEncoder(); enc.copyTextureToBuffer({ texture: entry.t }, { buffer: buf, bytesPerRow: n * 8 }, [n, n]); dev.queue.submit([enc.finish()]);
        await buf.mapAsync(GPUMapMode.READ); const raw = new Uint16Array(buf.getMappedRange().slice(0)); buf.unmap(); buf.destroy();
        return Float32Array.from(raw, half);
      };
      const cas = [];
      for (let i = 0; i < info.cascades.length; i++) {
        const c = info.cascades[i];
        let d, sl;
        if (webgl) { d = await fetchGL(4 + i, c.n); sl = await fetchGL(8 + i, c.n); }
        else {
          const want = (kind) => (window.__gpuTex || []).filter((x) => x.desc.label === "gosx-elio-ocean-" + kind + "Tex" + i);
          const a = want("disp"), b = want("slope");
          if (!a.length || !b.length) throw new Error("no displacement texture labelled dispTex" + i + " (see labels)");
          d = await fetchGPU(a[a.length - 1], c.n); sl = await fetchGPU(b[b.length - 1], c.n);
        }
        cas.push({ tile: c.tile, n: c.n, d: d, s: sl, om: [w[2] - Math.floor(w[2] / c.tile) * c.tile, w[3] - Math.floor(w[3] / c.tile) * c.tile] });
      }
      const bil = (d, n, u, v) => {
        const x = u * n - 0.5, y = v * n - 0.5, x0 = Math.floor(x), y0 = Math.floor(y), fx = x - x0, fy = y - y0, r = [0, 0, 0, 0];
        for (const [i, j, k] of [[0, 0, (1 - fx) * (1 - fy)], [1, 0, fx * (1 - fy)], [0, 1, (1 - fx) * fy], [1, 1, fx * fy]]) {
          const o = ((((y0 + j) % n) + n) % n * n + (((x0 + i) % n) + n) % n) * 4; for (let c = 0; c < 4; c++) r[c] += k * d[o + c];
        }
        return r;
      };
      const ev = (q) => {
        let s = [0, 0, 0], dx = [1, 0, 0], dz = [0, 0, 1];
        for (const c of cas) {
          const u = (q[0] - c.om[0]) / c.tile, v = (q[1] - c.om[1]) / c.tile, a = bil(c.d, c.n, u - Math.floor(u), v - Math.floor(v)), b = bil(c.s, c.n, u - Math.floor(u), v - Math.floor(v));
          s[0] += a[0]; s[1] += a[1]; s[2] += a[2]; dx[0] += b[2]; dx[1] += b[0]; dx[2] += a[3]; dz[0] += a[3]; dz[1] += b[1]; dz[2] += b[3];
        }
        return { s, dx, dz };
      };
      out.ref = []; out.refN = [];
      for (const t of pts) {
        let q = [t[0], t[1]], e;
        for (let k = 0; k < 8; k++) {
          e = ev(q); const ex = q[0] + e.s[0] - t[0], ez = q[1] + e.s[2] - t[1], det = e.dx[0] * e.dz[2] - e.dz[0] * e.dx[2];
          if (Math.abs(det) > 1e-4) { q = [q[0] - (e.dz[2] * ex - e.dz[0] * ez) / det, q[1] - (e.dx[0] * ez - e.dx[2] * ex) / det]; } else { q = [q[0] - ex * 0.5, q[1] - ez * 0.5]; }
        }
        e = ev(q);
        const n = [e.dz[1] * e.dx[2] - e.dz[2] * e.dx[1], e.dz[2] * e.dx[0] - e.dz[0] * e.dx[2], e.dz[0] * e.dx[1] - e.dz[1] * e.dx[0]], l = Math.hypot(n[0], n[1], n[2]);
        out.ref.push(2 + e.s[1]); out.refN.push([n[0] / l, n[2] / l]);
      }
    } catch (error) { out.error = "cpu reference: " + error; }
  }
  return JSON.stringify(out);
})()`

	results := map[string]probeReading{}
	for _, backend := range []string{"webgpu", "webgl2"} {
		t.Run(backend, func(t *testing.T) {
			browser, err := chrometest.Start(t.Context(), chrome, "--no-sandbox", "--mute-audio", "--window-size=900,520")
			if err != nil {
				t.Skipf("chrome unavailable: %v", err)
			}
			defer browser.Close()
			ctx, cancel := context.WithTimeout(browser.Context, 180*time.Second)
			defer cancel()
			init := `window.__gosx_scene3d_render_truth = true;
if (window.GPUAdapter) { const request = GPUAdapter.prototype.requestDevice; GPUAdapter.prototype.requestDevice = async function (...a) {
  const d = await request.apply(this, a); window.__gpuDevice = d; window.__gpuTex = [];
  const create = d.createTexture.bind(d); d.createTexture = (desc) => { desc = Object.assign({}, desc, { usage: desc.usage | GPUTextureUsage.COPY_SRC }); const t = create(desc); window.__gpuTex.push({ t, desc }); return t; }; return d; }; }`
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
			if err := chromedp.Run(ctx, chromedp.Evaluate(script, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
				t.Fatal(err)
			}
			var r probeReading
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			if r.Err != "" {
				t.Fatalf("%s: %s\nGPU textures: %v", backend, r.Err, r.Labels)
			}
			results[backend] = r
			if dir := os.Getenv("OCEAN_PROBE_EVIDENCE"); dir != "" {
				var shot []byte
				if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err == nil {
					_ = os.MkdirAll(dir, 0o755)
					_ = os.WriteFile(filepath.Join(dir, backend+".png"), shot, 0o644)
					blob, _ := json.MarshalIndent(r, "", " ")
					_ = os.WriteFile(filepath.Join(dir, backend+".json"), blob, 0o644)
				}
			}
			var maxLat, sumLat float64
			for _, l := range r.Lat {
				maxLat, sumLat = math.Max(maxLat, l), sumLat+l
			}
			meanLat := sumLat / math.Max(1, float64(len(r.Lat)))
			t.Logf("%s: %d points, %d readings sampled (%v new in the window), latency frames min/mean/max = %.0f/%.2f/%.0f, tags %v", backend, len(r.Heights), len(r.Lat), r.Ticks, minOf(r.Lat), meanLat, maxLat, r.W[11:14])
			if len(r.Heights) != 35 || len(r.Lat) < 30 {
				t.Fatalf("too few readings: %d points, %d readings", len(r.Heights), len(r.Lat))
			}
			if maxLat > 6 {
				t.Fatalf("a reading was %.0f frames old", maxLat)
			}
			if r.W[11] <= 0 || r.W[12] <= 0 {
				t.Fatalf("a reading carries no frame tag: seq %v generation %v", r.W[11], r.W[12])
			}
			{
				var worst, worstN float64
				for i := range r.Heights {
					worst = math.Max(worst, math.Abs(r.Heights[i]-r.Ref[i]))
					worstN = math.Max(worstN, math.Max(math.Abs(r.Normals[i][0]-r.RefN[i][0]), math.Abs(r.Normals[i][1]-r.RefN[i][1])))
				}
				t.Logf("%s readback vs CPU evaluation of the drawn textures: max height diff %.5f m, max normal component diff %.5f", backend, worst, worstN)
				if worst > 0.01 || worstN > 0.01 {
					t.Fatalf("readback differs from the CPU evaluation: height %.5f m, normal %.5f", worst, worstN)
				}
			}
		})
	}
	t.Run("gerstner", func(t *testing.T) {
		browser, err := chrometest.Start(t.Context(), chrome, "--no-sandbox", "--mute-audio", "--window-size=900,520")
		if err != nil {
			t.Skipf("chrome unavailable: %v", err)
		}
		defer browser.Close()
		ctx, cancel := context.WithTimeout(browser.Context, 120*time.Second)
		defer cancel()
		err = chromedp.Run(ctx, chromedp.Navigate(pageURL+"/?gerstner=1"))
		for i := 0; err != nil && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			err = chromedp.Run(ctx, chromedp.Navigate(pageURL+"/?gerstner=1"))
		}
		if err != nil {
			t.Fatal(err)
		}
		const gscript = `(async () => {
  const api = window.__gosx.scene3d; await api.whenReady("sea");
  const w = api.oceanWater("sea"), wait = (ms) => new Promise((r) => setTimeout(r, ms));
  for (let i = 0; i < 400 && !(w[0] >= 10); i++) await wait(50);
  const pts = ` + probePoints + `; w[10] = pts.length; pts.forEach((p, i) => { w[18 + 2 * i] = p[0]; w[19 + 2 * i] = p[1]; });
  for (let i = 0; i < 400 && !(w[17] > 0); i++) await wait(50);
  return JSON.stringify({ w: Array.from(w.slice(0, 18)), h: [w[146], w[149], w[146 + 3 * 34]], spectral: w[4] });
})()`
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(gscript, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
			t.Fatal(err)
		}
		var g struct {
			W        []float64 `json:"w"`
			H        []float64 `json:"h"`
			Spectral float64   `json:"spectral"`
		}
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatal(err)
		}
		t.Logf("gerstner: flags %v n %v latency %v heights %v", g.W[13], g.W[14], g.W[15], g.H)
		if g.Spectral != 0 || int(g.W[13]) != 3 || g.W[14] != 35 || g.W[15] != 0 {
			t.Fatalf("not a Gerstner reading: %v", g.W)
		}
		for _, h := range g.H {
			if math.Abs(h) > 3 || h == 0 {
				t.Fatalf("implausible Gerstner height %v", g.H)
			}
		}
	})
	gpu, gl := results["webgpu"], results["webgl2"]
	if len(gpu.Heights) == 35 && len(gl.Heights) == 35 {
		var worst, worstN float64
		for i := range gpu.Heights {
			worst = math.Max(worst, math.Abs(gpu.Heights[i]-gl.Heights[i]))
			worstN = math.Max(worstN, math.Max(math.Abs(gpu.Normals[i][0]-gl.Normals[i][0]), math.Abs(gpu.Normals[i][1]-gl.Normals[i][1])))
		}
		t.Logf("WebGPU vs WebGL2 readback of the same sea: max height diff %.5f m, max normal component diff %.5f", worst, worstN)
		if worst > 0.02 || worstN > 0.02 {
			t.Fatalf("the backends disagree: height %.5f m, normal %.5f", worst, worstN)
		}
	}
}

func minOf(v []float64) float64 {
	m := math.Inf(1)
	for _, x := range v {
		m = math.Min(m, x)
	}
	return m
}
