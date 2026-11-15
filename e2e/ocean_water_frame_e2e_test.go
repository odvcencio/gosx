//go:build e2e

// The drawn water's frame on a real GPU (Scene3D.OceanWater; scene.OceanWaterFrame). A spectral sea with a
// drift is mounted on WebGPU and on forced WebGL2. The test reads the page's record twice, a known time
// apart, and checks that it is a spectral reading, that the frame count and the water offset advance with
// the drift, and that the reading is the one the renderer used. It also writes a screenshot of each backend
// when OCEAN_WATER_EVIDENCE names a folder.
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

const oceanWaterPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>ocean water frame</title>
<style>html,body{margin:0;background:#000}#sea{width:800px;height:450px}</style>
</head><body>
<div id="sea"></div>
<script id="gosx-manifest" type="application/json">%s</script>
<script data-gosx-script="bootstrap" src="/bootstrap.js" data-gosx-scene3d-ocean-fft-url="/gosx/bootstrap-feature-scene3d-ocean-fft.js" data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js" data-gosx-scene3d-ocean-query-url="/gosx/bootstrap-feature-scene3d-ocean-query.js"></script>
</body></html>`

func TestWaterFrameOnTheGPU(t *testing.T) {
	// Hosted runners have no GPU. Like the water certification (ci.yml water-webgpu-certification), run only
	// where GOSX_WATER_E2E_REQUIRE_WEBGPU=1 names a GPU runner; there a missing GPU fails the test.
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run 'TestWater' ./e2e)")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	spectrum := scene.OceanPreset("rough")
	props := scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{
		Spectrum:     &spectrum,
		Presentation: &scene.OceanPresentation{TimeScale: 1, WaterDriftX: 3, WaterDriftZ: -2},
	}}}
	env, err := json.Marshal(props.SceneIR().Environment)
	if err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"engines":[{"id":"gosx-engine-sea","component":"GoSXScene3D","kind":"surface","mountId":"sea","props":{"width":800,"height":450,"background":"#000000","camera":{"x":0,"y":14,"z":40,"fov":60},"scene":{"environment":%s,"objects":[]}}}]}`, env)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		name = strings.TrimPrefix(name, "gosx/")
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
	// Windows Chrome reaches a WSL server at localhost:<port>, which needs a listener on every interface.
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
			ctx, cancel := context.WithTimeout(browser.Context, 150*time.Second)
			defer cancel()
			// Record how the WebGPU device ends: a Metal command buffer that fails ("Insufficient Memory") loses the device and the
			// renderer falls back to WebGL, which a test that only reads the water frame would not notice.
			init := `window.__gosx_scene3d_render_truth = true; window.__gpuLost = null; window.__gpuTimestamps = false;
if (navigator.gpu) { const requestAdapter = navigator.gpu.requestAdapter.bind(navigator.gpu); navigator.gpu.requestAdapter = async function (...a) {
  const adapter = await requestAdapter(...a);
  if (adapter) { const requestDevice = adapter.requestDevice.bind(adapter); adapter.requestDevice = async function (...b) {
    const device = await requestDevice(...b); window.__gpuTimestamps = device.features.has("timestamp-query");
    device.lost.then((info) => { window.__gpuLost = info.reason + ": " + info.message; }); return device; }; }
  return adapter; }; }`
			if backend == "webgl2" {
				init += `window.__gosx_scene3d_force_webgl = true;`
			}
			if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				_, err := page.AddScriptToEvaluateOnNewDocument(init).Do(ctx)
				return err
			})); err != nil {
				t.Fatal(err)
			}
			// A new WSL port can take a moment to appear on Windows localhost: retry a refused load.
			err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			for i := 0; err != nil && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			}
			if err != nil {
				t.Fatal(err)
			}
			const read = `(async () => {
  const el = document.getElementById("sea");
  const api = window.__gosx && window.__gosx.scene3d;
  if (api) await api.whenReady("sea");
  const w = api && api.oceanWater ? api.oceanWater("sea") : null;
  return JSON.stringify({backend: el.getAttribute("data-gosx-scene3d-backend"), ocean: el.getAttribute("data-gosx-scene3d-ocean"),
    fallback: el.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), clock: Number(el.getAttribute("data-gosx-scene3d-animation-clock")), now: performance.timeOrigin + performance.now(), w: w ? Array.from(w) : null});
})()`
			type reading struct {
				Backend, Ocean, Fallback string
				Now, Clock               float64
				W                        []float64
			}
			// A wedged page must fail the subtest with what the page knew, not hold the GPU for the whole 150 s.
			lostNote := func() string {
				var note string
				c, stop := context.WithTimeout(ctx, 5*time.Second)
				defer stop()
				if chromedp.Run(c, chromedp.Evaluate(`String(window.__gpuLost)`, &note)) != nil {
					return "the page does not answer"
				}
				return "device lost: " + note
			}
			get := func() reading {
				var raw string
				c, stop := context.WithTimeout(ctx, 20*time.Second)
				defer stop()
				if err := chromedp.Run(c, chromedp.Evaluate(read, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
					t.Fatalf("reading the page's water frame: %v (%s)", err, lostNote())
				}
				var r reading
				if err := json.Unmarshal([]byte(raw), &r); err != nil {
					t.Fatalf("%s: %v", raw, err)
				}
				return r
			}
			var first reading
			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) {
				first = get()
				if len(first.W) >= 10 && first.W[0] >= 30 && first.W[4] == 1 && first.Ocean == "surface+spectrum" {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			t.Logf("first: %+v", first)
			if len(first.W) < 10 || first.W[4] != 1 || first.Ocean != "surface+spectrum" {
				t.Fatalf("no spectral reading: %+v", first)
			}
			if backend == "webgl2" && first.Backend != "webgl" {
				t.Fatalf("forced WebGL2 ran on %q", first.Backend)
			}
			if backend == "webgpu" && first.Backend != "webgpu" {
				t.Fatalf("WebGPU run drew on %q (%s)", first.Backend, lostNote())
			}
			time.Sleep(2 * time.Second)
			second := get()
			t.Logf("second: %+v", second)
			dt := (second.W[8] - first.W[8]) / 1000
			frames := second.W[0] - first.W[0]
			if frames < 10 || dt < 1.5 {
				t.Fatalf("frames advanced %v over %vs", frames, dt)
			}
			// The wave clock is the scene clock: over the same readings the two move together (a frame of slack for
			// reading them between frames), and the offset moves at the drift per second of wave time.
			dx, dz, wave := second.W[2]-first.W[2], second.W[3]-first.W[3], second.W[1]-first.W[1]
			clock := second.Clock - first.Clock
			if math.Abs(wave-clock) > 0.05 {
				t.Fatalf("wave time moved %.4fs but the scene clock moved %.4fs", wave, clock)
			}
			if wave < 0.5 || wave > dt+0.05 {
				t.Fatalf("wave time moved %.3f over %.3fs of wall time", wave, dt)
			}
			if math.Abs(dx-3*wave) > 1e-3 || math.Abs(dz+2*wave) > 1e-3 {
				t.Fatalf("offset moved (%.4f, %.4f) over %.4fs of wave time; drift (3, -2) expects (%.4f, %.4f)", dx, dz, wave, 3*wave, -2*wave)
			}
			if age := second.Now - second.W[8]; age < 0 || age > 100 {
				t.Fatalf("reading is %vms old when read", age)
			}
			if backend == "webgpu" {
				// The WebGPU device must still be alive after the frames above. Stamping the graph's ~400 compute passes in one
				// Metal command buffer once lost it. (Chrome 157 on Metal reports no GPU time for the renderer's own stamps
				// either, so the graph's ms is logged and not asserted.)
				var end struct {
					Lost       *string
					Timestamps bool
					Ms         *float64
					Backend    string
				}
				var raw string
				const finish = `(async () => {
  const api = window.__gosx_scene3d_ocean_fft_api;
  for (let i = 0; i < 100; i++) { const t = api && api.timing(); if (t && t.ms > 0) break; await new Promise((r) => setTimeout(r, 50)); }
  const t = api && api.timing();
  return JSON.stringify({lost: window.__gpuLost, timestamps: window.__gpuTimestamps, ms: t ? t.ms : null, backend: document.getElementById("sea").getAttribute("data-gosx-scene3d-backend")});
})()`
				c, stop := context.WithTimeout(ctx, 20*time.Second)
				defer stop()
				if err := chromedp.Run(c, chromedp.Evaluate(finish, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
					t.Fatalf("reading the device state: %v", err)
				}
				if err := json.Unmarshal([]byte(raw), &end); err != nil {
					t.Fatalf("%s: %v", raw, err)
				}
				t.Logf("device: %s", raw)
				if end.Lost != nil || end.Backend != "webgpu" {
					t.Fatalf("the WebGPU device did not survive the water frames: lost=%v backend=%q", end.Lost, end.Backend)
				}
			}
			if dir := os.Getenv("OCEAN_WATER_EVIDENCE"); dir != "" {
				var shot []byte
				if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err == nil {
					_ = os.MkdirAll(dir, 0o755)
					_ = os.WriteFile(filepath.Join(dir, backend+".png"), shot, 0o644)
					blob, _ := json.Marshal(map[string]any{"first": first, "second": second, "dt": dt, "frames": frames, "dx": dx, "dz": dz})
					_ = os.WriteFile(filepath.Join(dir, backend+".json"), blob, 0o644)
				}
			}
		})
	}
}
