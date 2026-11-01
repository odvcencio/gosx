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
<script data-gosx-script="bootstrap" src="/bootstrap.js" data-gosx-scene3d-ocean-fft-url="/gosx/bootstrap-feature-scene3d-ocean-fft.js" data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"></script>
</body></html>`

func TestOceanWaterFrameOnTheGPU(t *testing.T) {
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
			init := `window.__gosx_scene3d_render_truth = true;`
			if backend == "webgl2" {
				init += `window.__gosx_scene3d_force_webgl = true;`
			}
			if err := chromedp.Run(ctx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := page.AddScriptToEvaluateOnNewDocument(init).Do(ctx)
					return err
				}),
				chromedp.Navigate(pageURL),
			); err != nil {
				t.Fatal(err)
			}
			const read = `(async () => {
  const el = document.getElementById("sea");
  const api = window.__gosx && window.__gosx.scene3d;
  if (api) await api.whenReady("sea");
  const w = api && api.oceanWater ? api.oceanWater("sea") : null;
  return JSON.stringify({backend: el.getAttribute("data-gosx-scene3d-backend"), ocean: el.getAttribute("data-gosx-scene3d-ocean"),
    fallback: el.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), now: performance.timeOrigin + performance.now(), w: w ? Array.from(w) : null});
})()`
			type reading struct {
				Backend, Ocean, Fallback string
				Now                      float64
				W                        []float64
			}
			get := func() reading {
				var raw string
				if err := chromedp.Run(ctx, chromedp.Evaluate(read, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
					t.Fatal(err)
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
				if len(first.W) == 9 && first.W[0] >= 30 && first.W[4] == 1 {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			t.Logf("first: %+v", first)
			if len(first.W) != 9 || first.W[4] != 1 {
				t.Fatalf("no spectral reading: %+v", first)
			}
			if backend == "webgl2" && first.Backend != "webgl" {
				t.Fatalf("forced WebGL2 ran on %q", first.Backend)
			}
			time.Sleep(2 * time.Second)
			second := get()
			t.Logf("second: %+v", second)
			dt := (second.W[8] - first.W[8]) / 1000
			frames := second.W[0] - first.W[0]
			if frames < 10 || dt < 1.5 {
				t.Fatalf("frames advanced %v over %vs", frames, dt)
			}
			// The graph integrates the offset from the same clock as the wave time, so the offset moves at the drift
			// per second of wave time. The wave clock is the scene clock, which may run behind the wall clock
			// (the first frames of a backend, a clamped long frame) but never ahead of it.
			dx, dz, wave := second.W[2]-first.W[2], second.W[3]-first.W[3], second.W[1]-first.W[1]
			if wave < 0.5 || wave > dt+0.25 {
				t.Fatalf("wave time moved %.3f over %.3fs of wall time", wave, dt)
			}
			if math.Abs(dx-3*wave) > 1e-3 || math.Abs(dz+2*wave) > 1e-3 {
				t.Fatalf("offset moved (%.4f, %.4f) over %.4fs of wave time; drift (3, -2) expects (%.4f, %.4f)", dx, dz, wave, 3*wave, -2*wave)
			}
			if age := second.Now - second.W[8]; age < 0 || age > 100 {
				t.Fatalf("reading is %vms old when read", age)
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
