//go:build e2e

// Projected decals on a real GPU. A 45 degree slope is lit and viewed head on. A decal box facing the slope
// paints a square from an atlas. The test mounts the page once without the decal and once with it, differences
// the two screenshots, and reads the changed pixels: they must be the atlas cell that was asked for, centered
// where the decal sits, and squashed by the slope the way a square on a 45 degree surface looks from the front.
// It runs on WebGPU and on forced WebGL2, and measures frame time with many pooled decals.
//
// Run with a GPU runner and GOSX_WATER_E2E_REQUIRE_WEBGPU=1 (the same gate as the water tests):
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestDecals -timeout 600s ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestDecals -timeout 600s ./e2e/
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
	"m31labs.dev/gosx/scene"
)

const (
	decalShotW = 800
	decalShotH = 450
)

// decalAtlasPNG is a 2 x 1 atlas: a red cell, then a blue cell.
func decalAtlasPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			if x < 16 {
				img.Set(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.NRGBA{B: 255, A: 255})
			}
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

// decalSlopeSceneJSON builds the scene: a gray slab tilted 45 degrees about X (its top faces up and toward
// the camera), an ambient and a directional light, and a spinning cube so the page keeps drawing frames.
func decalSlopeSceneJSON(t *testing.T, withDecal bool) string {
	t.Helper()
	effects := []scene.PostEffect{}
	n := math.Sqrt2 / 2
	if withDecal {
		effects = append(effects, scene.Decals{
			Materials: []scene.DecalMaterial{{Name: "mark", Albedo: "/atlas.png", Columns: 2, Rows: 1, EdgeFade: 0.001}},
			// The slab is 0.2 thick, so its top surface sits 0.1 along the normal (0, n, n).
			Items: []scene.ProjectedDecal{{Material: "mark", Position: scene.Vec3(0, 0.1*n, 0.1*n), Normal: scene.Vec3(0, n, n), Size: scene.Vec3(2, 2, 1), Frame: 1}},
			Pools: []string{"hits"},
		})
	}
	effects = append(effects, scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1})
	props := scene.Props{
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60, Near: 0.1, Far: 100},
		PostFX: scene.PostFX{Effects: effects},
		Graph: scene.NewGraph(
			scene.Mesh{ID: "slope", Geometry: scene.BoxGeometry{Width: 14, Height: 0.2, Depth: 14}, Material: scene.StandardMaterial{Color: "#808080", Roughness: 0.9}, Rotation: scene.Rotate(math.Pi/4, 0, 0)},
			scene.Group{ID: "spin", Position: scene.Vec3(-3.2, -1.8, 3), Spin: scene.Rotate(0, 2, 0), Children: []scene.Node{
				scene.Mesh{ID: "spinner", Geometry: scene.BoxGeometry{Width: 0.3, Height: 0.3, Depth: 0.3}, Material: scene.StandardMaterial{Color: "#334455"}},
			}},
			scene.AmbientLight{ID: "amb", Color: "#ffffff", Intensity: 0.35},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.6, Direction: scene.Vec3(0, -0.5, -1)},
		),
	}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const decalPage = `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000;overflow:hidden}#s{width:800px;height:450px}</style></head><body>
<div id="s"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d"
 data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js"
 data-gosx-scene3d-webgpu-url="/gosx/bootstrap-feature-scene3d-webgpu.js"
 data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"
 data-gosx-scene3d-depthfx-url="/gosx/bootstrap-feature-scene3d-depthfx.js"
 data-gosx-scene3d-hierarchy-url="/gosx/bootstrap-feature-scene3d-hierarchy.js"
 src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>
window.ready = (async () => {
 window.handle = await window.__gosx_engine_factories.GoSXScene3D({
  mount: document.getElementById('s'), emit() {},
  props: %s
 });
 window.mounted = true;
})().catch(error => { window.mountError = String(error && error.stack || error); });
// Pool of n decals scattered across the slope, published the way DecalPool.Publish does.
window.setPool = function(n) {
 const stride = 20, data = new Float32Array(Math.max(1, n) * stride);
 let seed = 12345; const rand = () => (seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296;
 const k = Math.SQRT1_2;
 for (let i = 0; i < n; i++) {
  const u = (rand() - 0.5) * 9, v = (rand() - 0.5) * 6;
  data.set([u, v * k + 0.1 * k, -v * k + 0.1 * k, 1, -0.3826834, 0, 0, 0.9238795, 1.2, 1.2, 1, i %% 2, 1, 1, 1, 1, 0, i, 0, 0], i * stride);
 }
 window.__gosx_scene3d_decal_pools = window.__gosx_scene3d_decal_pools || {};
 const store = window.__gosx_scene3d_decal_pools.hits || (window.__gosx_scene3d_decal_pools.hits = {});
 store.data = data; store.count = n; store.revision = (store.revision || 0) + 1;
};
</script></body></html>`

type decalRun struct {
	t       *testing.T
	ctx     context.Context
	url     string
	backend string
}

func decalServer(t *testing.T, withDecal bool) (string, func()) {
	t.Helper()
	root := e2eRepoRoot(t)
	markup := fmt.Sprintf(decalPage, mustJSON(t, map[string]any{
		"width": decalShotW, "height": decalShotH, "responsive": false, "maxDevicePixelRatio": 1, "controls": "none",
		"shaded": true, "background": "#101820", "scene": json.RawMessage(decalSlopeSceneJSON(t, withDecal)),
	}))
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/atlas.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(decalAtlasPNG())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(markup))
	})
	server := httptest.NewUnstartedServer(mux)
	// Windows Chrome reaches a WSL server at localhost:<port>, which needs a listener on every interface.
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	return fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port), server.Close
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "%", "%%")
}

func decalBrowser(t *testing.T, backend string) (context.Context, func()) {
	t.Helper()
	browser, err := chrometest.Start(t.Context(), e2eChromePath(t), "--no-sandbox", "--mute-audio", "--window-size=900,520")
	if err != nil {
		t.Skipf("chrome unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(browser.Context, 240*time.Second)
	init := `window.__gosx_scene3d_render_truth = true; window.__gosx_scene3d_webgpu_telemetry = true;`
	if backend == "webgl2" {
		init += `window.__gosx_scene3d_force_webgl = true;`
	}
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(decalShotW, decalShotH, 1, false), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(init).Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	return ctx, func() { cancel(); browser.Close() }
}

func decalNavigate(t *testing.T, ctx context.Context, url, backend string) {
	t.Helper()
	err := chromedp.Run(ctx, chromedp.Navigate(url))
	for i := 0; err != nil && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		err = chromedp.Run(ctx, chromedp.Navigate(url))
	}
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var state string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.mountError ? 'error:' + window.mountError : (window.mounted ? (document.getElementById('s').getAttribute('data-gosx-scene3d-backend') || 'none') : '')`, &state))
		if strings.HasPrefix(state, "error:") {
			t.Fatalf("mount failed: %s", state)
		}
		want := "webgpu"
		if backend == "webgl2" {
			want = "webgl"
		}
		if state == want {
			return
		}
		if state != "" && state != "none" && state != want {
			t.Fatalf("backend %q, want %q", state, want)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("scene did not mount")
}

func decalShot(t *testing.T, ctx context.Context) *image.NRGBA {
	t.Helper()
	time.Sleep(1500 * time.Millisecond) // let the atlas load and a few frames draw
	var data []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewNRGBA(img.Bounds())
	for y := out.Bounds().Min.Y; y < out.Bounds().Max.Y; y++ {
		for x := out.Bounds().Min.X; x < out.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

type decalDiff struct {
	count          int
	minX, minY     int
	maxX, maxY     int
	meanR, meanG   float64
	meanB          float64
	width, height  int
	centerX, ratio float64
	centerY        float64
}

// diffShots compares the two shots inside roi, which leaves out the spinning cube in the corner.
func diffShots(a, b *image.NRGBA, roi image.Rectangle, threshold int) decalDiff {
	d := decalDiff{minX: 1 << 30, minY: 1 << 30, maxX: -1, maxY: -1}
	bounds := a.Bounds().Intersect(roi)
	var sr, sg, sb float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p, q := a.NRGBAAt(x, y), b.NRGBAAt(x, y)
			delta := absInt(int(p.R)-int(q.R)) + absInt(int(p.G)-int(q.G)) + absInt(int(p.B)-int(q.B))
			if delta < threshold {
				continue
			}
			d.count++
			sr, sg, sb = sr+float64(q.R), sg+float64(q.G), sb+float64(q.B)
			d.minX, d.maxX = min(d.minX, x), max(d.maxX, x)
			d.minY, d.maxY = min(d.minY, y), max(d.maxY, y)
		}
	}
	if d.count > 0 {
		d.meanR, d.meanG, d.meanB = sr/float64(d.count), sg/float64(d.count), sb/float64(d.count)
		d.width, d.height = d.maxX-d.minX+1, d.maxY-d.minY+1
		d.centerX, d.centerY = float64(d.minX+d.maxX)/2, float64(d.minY+d.maxY)/2
		d.ratio = float64(d.height) / float64(d.width)
	}
	return d
}

func decalBackends(t *testing.T) []string {
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run TestDecals ./e2e)")
	}
	return []string{"webgpu", "webgl2"}
}

func TestDecalsPaintTheSlopeOnTheGPU(t *testing.T) {
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			ctx, closeBrowser := decalBrowser(t, backend)
			defer closeBrowser()
			controlURL, stopControl := decalServer(t, false)
			defer stopControl()
			decalURL, stopDecal := decalServer(t, true)
			defer stopDecal()

			decalNavigate(t, ctx, controlURL, backend)
			control := decalShot(t, ctx)
			decalNavigate(t, ctx, decalURL, backend)
			withDecal := decalShot(t, ctx)
			var failure string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('s').getAttribute('data-gosx-scene3d-backend-error') || ''`, &failure))

			d := diffShots(control, withDecal, image.Rect(200, 60, 700, 380), 60)
			t.Logf("%s: changed=%d bbox=%dx%d at (%.0f,%.0f) ratio=%.3f mean rgb=(%.0f,%.0f,%.0f)", backend, d.count, d.width, d.height, d.centerX, d.centerY, d.ratio, d.meanR, d.meanG, d.meanB)
			if dir := os.Getenv("GOSX_DECAL_EVIDENCE"); dir != "" {
				_ = os.MkdirAll(dir, 0o755)
				for name, img := range map[string]*image.NRGBA{"control": control, "decal": withDecal} {
					var out bytes.Buffer
					_ = png.Encode(&out, img)
					_ = os.WriteFile(filepath.Join(dir, backend+"-"+name+".png"), out.Bytes(), 0o644)
				}
			}
			total := decalShotW * decalShotH
			if d.count < 800 || d.count > total*4/10 {
				t.Fatalf("decal changed %d of %d pixels, want a bounded patch", d.count, total)
			}
			// Atlas frame 1 is the blue cell.
			if d.meanB < d.meanR+40 || d.meanB < d.meanG+40 {
				t.Errorf("decal color (%.0f,%.0f,%.0f) is not the blue atlas cell", d.meanR, d.meanG, d.meanB)
			}
			// The box sits on the camera axis, so the patch is centered on the canvas.
			if math.Abs(d.centerX-decalShotW/2) > 25 || math.Abs(d.centerY-decalShotH/2) > 30 {
				t.Errorf("decal center (%.0f,%.0f) is far from canvas center", d.centerX, d.centerY)
			}
			// A square painted onto a 45 degree slope seen head on is foreshortened to about cos(45) = 0.71.
			// A decal that floated in a vertical plane would measure 1.
			if d.ratio < 0.55 || d.ratio > 0.85 {
				t.Errorf("patch height/width = %.3f; a decal conforming to the 45 degree slope measures about 0.71", d.ratio)
			}
		})
	}
}

// TestDecalsFrameCost measures frame time with no decals, a few hundred, and a few thousand pooled decals.
// The page draws as fast as the browser lets it; where vsync caps the rate, the numbers sit at the display
// interval and say only that the cost fits inside a frame.
func TestDecalsFrameCost(t *testing.T) {
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			ctx, closeBrowser := decalBrowser(t, backend)
			defer closeBrowser()
			url, stop := decalServer(t, true)
			defer stop()
			decalNavigate(t, ctx, url, backend)
			measure := func(n int) (ms, gpuMS float64) {
				if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`window.setPool(%d)`, n), nil)); err != nil {
					t.Fatal(err)
				}
				time.Sleep(700 * time.Millisecond)
				const sample = `(async () => {
 const frames = []; let last = performance.now();
 await new Promise(resolve => { function tick(now) { frames.push(now - last); last = now; if (frames.length < 150) requestAnimationFrame(tick); else resolve(); } requestAnimationFrame(tick); });
 frames.shift(); frames.sort((a, b) => a - b);
 const gpu = Number(document.getElementById('s').getAttribute('data-gosx-scene3d-webgpu-gpu-ms')) || 0;
 return JSON.stringify({median: frames[frames.length >> 1], gpu});
})()`
				var raw string
				if err := chromedp.Run(ctx, chromedp.Evaluate(sample, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) })); err != nil {
					t.Fatal(err)
				}
				var r struct{ Median, Gpu float64 }
				if err := json.Unmarshal([]byte(raw), &r); err != nil {
					t.Fatalf("%s: %v", raw, err)
				}
				return r.Median, r.Gpu
			}
			results := map[int][2]float64{}
			for _, n := range []int{0, 256, 2048} {
				ms, gpu := measure(n)
				results[n] = [2]float64{ms, gpu}
				t.Logf("%s: %4d pooled decals: median frame %.2f ms, GPU %.3f ms", backend, n, ms, gpu)
			}
			keys := make([]int, 0, len(results))
			for k := range results {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			// 256 decals must not push a frame past a 60 Hz budget on top of the empty scene.
			if base, many := results[0][0], results[256][0]; many > math.Max(base*2, 20) {
				t.Errorf("256 decals: median frame %.2f ms against %.2f ms with none", many, base)
			}
		})
	}
}
