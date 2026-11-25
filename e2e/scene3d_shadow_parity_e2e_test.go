//go:build e2e

// A cast shadow looks the same on both renderers. One wall casts a shadow on a
// floor from a directional light that sets no softness (the light, floor, camera
// and map settings are those of shadowStage in scene3d_shadow_live_update_e2e_test.go,
// which draws a shadow today), and both renderers draw the same scene at 480 by
// 300. On the shadow's right edge, row by row:
//
//   - the 10-to-90 per cent width of the edge, in pixels, agrees within 15 per cent;
//   - the position of the 50 per cent crossing agrees within 1 pixel;
//   - the shadowed interior's mean agrees within 2 codes.
//
// The shadow map is small (256), so one map texel is a few pixels: a
// hard edge shows as a stair, a softened one as a ramp, and half a texel of
// offset shows as more than a pixel. GOSX_SHADOW_EVIDENCE=<dir> keeps both
// stills and a crop of the edge as PNG. Run on a GPU runner:
//
//	mac-gpu-run <checkout> env GOSX_WATER_E2E_REQUIRE_WEBGPU=1 go test -tags e2e -run TestShadowParity -timeout 600s ./e2e/
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
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
	"m31labs.dev/gosx/scene"
)

const shadowParityW, shadowParityH = 480, 300

// shadowParityScene is a wall, 0.2 wide, 3 high and 12 long, on a floor seen from
// straight above. The light travels toward +x, -y and -z, so the wall's shadow is a
// band to its right whose far edge is a straight line the light's map grid crosses
// at an angle.
func shadowParityScene(t *testing.T) json.RawMessage {
	t.Helper()
	return shadowParityProps(t, shadowParityW, shadowParityH, 256, 0, false)
}

// shadowParityProps is the scene at w by h with a shadow map of the given size and
// cascade count; extra adds soft particles beside the wall.
func shadowParityProps(t *testing.T, w, h, size, cascades int, extra bool) json.RawMessage {
	t.Helper()
	no := false
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "key", Color: "#ffffff", Intensity: 3, Direction: scene.Vec3(0.6, -1, -0.5), CastShadow: true, ShadowSize: size, ShadowCascades: cascades, ShadowBias: 0.0012},
		scene.Mesh{ID: "floor", Geometry: scene.PlaneGeometry{Width: 40, Height: 40}, Material: scene.StandardMaterial{Color: "#d8d4c8", Roughness: 1}, ReceiveShadow: true},
		scene.Mesh{ID: "wall", Geometry: scene.BoxGeometry{Width: 0.2, Height: 3, Depth: 12}, Position: scene.Vec3(0, 1.5, 0), Material: scene.StandardMaterial{Color: "#b0552f", Roughness: 0.8}, CastShadow: true},
	}
	if extra {
		nodes = append(nodes, scene.ComputeParticles{ID: "mist", Count: 256, Emitter: scene.ParticleEmitter{Kind: "disc", Position: scene.Vec3(-5, 0.2, 0), Radius: 1.5, Lifetime: 2},
			Material: scene.ParticleMaterial{Color: "#e8f4ff", Size: 0.4, Opacity: 0.6, BlendMode: scene.BlendAlpha, SoftDistance: 0.75}})
	}
	props := scene.Props{
		Width: w, Height: h, Background: "#f0ede6", AutoRotate: &no, Responsive: &no, DragToRotate: &no, AdaptiveQuality: &no,
		MaxDevicePixelRatio: 1,
		Environment:         scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.35, ToneMapping: "linear"},
		// Z is 0.001, not 0: the camera normalizer treats z = 0 as unset.
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 15, 0.001), Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 50, Near: 0.1, Far: 100},
		Graph:  scene.NewGraph(nodes...),
	}
	data, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func shadowParityServer(t *testing.T) (string, func()) {
	t.Helper()
	return shadowParityServerFor(t, shadowParityW, shadowParityH, shadowParityScene(t))
}

func shadowParityServerFor(t *testing.T, w, h int, sceneJSON json.RawMessage) (string, func()) {
	t.Helper()
	markup := fmt.Sprintf(smaaPage, w, h, "", strings.ReplaceAll(string(sceneJSON), "%", "%%"))
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(e2eRepoRoot(t), "client", "js")))))
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write([]byte(markup))
	})
	server := httptest.NewUnstartedServer(mux)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	return fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port), server.Close
}

func shadowParityShot(t *testing.T, backend string) (image.Image, []byte) {
	t.Helper()
	ctx, closeBrowser := decalBrowser(t, backend)
	defer closeBrowser()
	url, stop := shadowParityServer(t)
	defer stop()
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(shadowParityW, shadowParityH, 1, false)); err != nil {
		t.Fatal(err)
	}
	decalNavigate(t, ctx, url, backend)
	time.Sleep(2 * time.Second)
	var data []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img, data
}

// luma is the 8-bit display luminance of a pixel.
func shadowLuma(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (0.2126*float64(r>>8) + 0.7152*float64(g>>8) + 0.0722*float64(b>>8))
}

// shadowEdge is what one backend's still says about the shadow's far edge.
type shadowEdge struct {
	width, position, interior, lit float64
}

// measureShadowEdge reads the right edge of the wall's shadow on the rows 80 to 220.
// The lit floor is on the right (x 330 to 440) and the band's interior at x 252 to
// 272. It first checks that the floor shows a shadow at all.
func measureShadowEdge(t *testing.T, img image.Image) shadowEdge {
	t.Helper()
	var litMean, darkest float64 = 0, 255
	n := 0
	for y := 80; y < 220; y++ {
		for x := 330; x < 440; x++ {
			litMean += shadowLuma(img, x, y)
			n++
		}
		for x := 252; x < 272; x++ {
			darkest = math.Min(darkest, shadowLuma(img, x, y))
		}
	}
	litMean /= float64(n)
	if litMean-darkest < 25 {
		t.Fatalf("the floor shows no shadow: the lit floor averages %.1f and its darkest pixel in the shadow band is %.1f (the scene, not the renderer, is wrong)", litMean, darkest)
	}
	var widths, positions, interiors, lits []float64
	for y := 80; y < 220; y++ {
		var lit, dark float64
		for x := 330; x < 440; x++ {
			lit += shadowLuma(img, x, y) / 110
		}
		for x := 252; x < 272; x++ {
			dark += shadowLuma(img, x, y) / 20
		}
		if lit-dark < 25 {
			t.Fatalf("row %d: lit %.1f and shadow %.1f are too close to measure an edge", y, lit, dark)
		}
		// f is 1 on the lit floor and 0 in the shadow. Scan right to left for where it crosses 0.9, 0.5, 0.1.
		cross := func(level float64) float64 {
			prev := 1.0
			for x := 320; x > 262; x-- {
				f := (shadowLuma(img, x, y) - dark) / (lit - dark)
				if f <= level && prev > level {
					return float64(x+1) - (prev-level)/(prev-f)
				}
				prev = f
			}
			return math.NaN()
		}
		x90, x50, x10 := cross(0.9), cross(0.5), cross(0.1)
		if math.IsNaN(x90) || math.IsNaN(x50) || math.IsNaN(x10) {
			t.Fatalf("row %d: no edge found between x 262 and 320", y)
		}
		widths = append(widths, x90-x10)
		positions = append(positions, x50)
		interiors = append(interiors, dark)
		lits = append(lits, lit)
	}
	median := func(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
	return shadowEdge{width: median(widths), position: median(positions), interior: median(interiors), lit: median(lits)}
}

// shadowCrop cuts the edge's neighbourhood and doubles it, for the evidence.
func shadowCrop(img image.Image) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, 160*4, 60*4))
	for y := 0; y < 60*4; y++ {
		for x := 0; x < 160*4; x++ {
			out.Set(x, y, img.At(220+x/4, 130+y/4))
		}
	}
	return out
}

func TestShadowParityOnTheGPU(t *testing.T) {
	edges := map[string]shadowEdge{}
	for _, backend := range []string{"webgl2", "webgpu"} {
		img, data := shadowParityShot(t, backend)
		if dir := os.Getenv("GOSX_SHADOW_EVIDENCE"); dir != "" {
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(filepath.Join(dir, "shadow-"+backend+".png"), data, 0o644)
			var crop bytes.Buffer
			_ = png.Encode(&crop, shadowCrop(img))
			_ = os.WriteFile(filepath.Join(dir, "shadow-"+backend+"-edge-crop.png"), crop.Bytes(), 0o644)
		}
		edges[backend] = measureShadowEdge(t, img)
		e := edges[backend]
		t.Logf("%s: edge width %.2f px, position %.2f px, shadow interior %.1f, lit floor %.1f", backend, e.width, e.position, e.interior, e.lit)
	}
	gl, gpu := edges["webgl2"], edges["webgpu"]
	if d := math.Abs(gl.width-gpu.width) / math.Max(gl.width, gpu.width); d > 0.15 {
		t.Errorf("edge width: WebGL %.2f px, WebGPU %.2f px: they differ by %.0f%%, want at most 15%%", gl.width, gpu.width, 100*d)
	}
	if d := math.Abs(gl.position - gpu.position); d > 1 {
		t.Errorf("edge position: WebGL %.2f px, WebGPU %.2f px: %.2f px apart, want at most 1", gl.position, gpu.position, d)
	}
	if d := math.Abs(gl.interior - gpu.interior); d > 2 {
		t.Errorf("shadow interior: WebGL %.1f, WebGPU %.1f: %.1f codes apart, want at most 2", gl.interior, gpu.interior, d)
	}
}

// TestShadowFrameCost reports the whole-frame GPU time of a shadowed 1920 by 1080 scene
// (a floor that receives the light on every pixel, two cascades of 1536, the camera
// moving each frame) on each backend, from the renderer's own timer (WebGL's timer query; WebGPU's perf events). It asserts nothing:
// run it three times on the tree before and the tree after a change to the shadow
// path and compare the medians.
func TestShadowFrameCost(t *testing.T) {
	const width, height = 1920, 1080
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			ctx, closeBrowser := decalBrowser(t, backend)
			defer closeBrowser()
			url, stop := shadowParityServerFor(t, width, height, shadowParityProps(t, width, height, 1536, 2, false))
			defer stop()
			if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(width, height, 1, false)); err != nil {
				t.Fatal(err)
			}
			decalNavigate(t, ctx, url, backend)
			time.Sleep(1500 * time.Millisecond)
			var raw string
			err := chromedp.Run(ctx, chromedp.Evaluate(`(async () => {
 window.gpuFrames = []; const gpu = [], frames = []; let last = performance.now(), n = 0, seq = -1; const end = last + 4000;
 await new Promise(resolve => { function tick(now) { frames.push(now - last); last = now; n++;
   const snap = window.__gosx_scene3d_debug && window.__gosx_scene3d_debug.inspect("s"), ft = snap && snap.frameTiming;
   if (ft && ft.status === "measured" && ft.gpuMS > 0 && ft.frameSeq !== seq) { seq = ft.frameSeq; gpu.push(ft.gpuMS); }
   window.handle.setCamera({ x: (n % 40) * 0.01, y: 15, z: 0.001, fov: 50, near: 0.1, far: 100 });
   if (now < end) requestAnimationFrame(tick); else resolve(); } requestAnimationFrame(tick); });
 return JSON.stringify({ gpu: gpu.length ? gpu : window.gpuFrames.slice(5), frames: frames.slice(10) });
})()`, &raw, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }))
			if err != nil {
				t.Fatal(err)
			}
			var got struct{ GPU, Frames []float64 }
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.GPU) < 20 {
				t.Fatalf("%s reported %d timed frames in four seconds (%d frames drawn)", backend, len(got.GPU), len(got.Frames))
			}
			sort.Float64s(got.GPU)
			t.Logf("%s @%dx%d: whole-frame GPU p50 %.3f ms, p95 %.3f ms over %d timed frames", backend, width, height, got.GPU[len(got.GPU)/2], got.GPU[len(got.GPU)*95/100], len(got.GPU))
		})
	}
}

// TestShadowUnitBudgetOnWebGL draws, on WebGL, a scene that holds a shadow, the
// image-based lighting samplers the renderer always declares, and soft particles
// (which take the highest texture unit), and counts the errors the browser
// raises after every draw call. A program with more than 16 samplers fails to
// link on a 16-unit GPU and the renderer silently falls back and draws no shadow,
// so the renderer must not report a fallback, no draw may raise an error, and the
// shadow must still show.
func TestShadowUnitBudgetOnWebGL(t *testing.T) {
	for _, backend := range decalBackends(t) {
		if backend != "webgl2" {
			continue
		}
		ctx, closeBrowser := decalBrowser(t, backend)
		defer closeBrowser()
		url, stop := shadowParityServerFor(t, shadowParityW, shadowParityH, shadowParityProps(t, shadowParityW, shadowParityH, 256, 0, true))
		defer stop()
		guard := `(() => { window.__glErrors = []; const p = WebGL2RenderingContext.prototype;
 const getContext = HTMLCanvasElement.prototype.getContext; HTMLCanvasElement.prototype.getContext = function (...a) { const c = getContext.apply(this, a); if (c && a[0] === "webgl2") window.__gl = c; return c; };
 for (const name of ["drawArrays", "drawElements", "drawArraysInstanced", "drawElementsInstanced"]) { const f = p[name];
  p[name] = function (...a) { f.apply(this, a); const e = this.getError(); if (e) window.__glErrors.push(name + ":" + e); }; } })()`
		if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(shadowParityW, shadowParityH, 1, false), chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(guard).Do(ctx)
			return err
		})); err != nil {
			t.Fatal(err)
		}
		decalNavigate(t, ctx, url, backend)
		time.Sleep(4 * time.Second)
		var fallback string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('s').getAttribute('data-gosx-scene3d-renderer-fallback') || ''`, &fallback)); err != nil {
			t.Fatal(err)
		}
		if fallback != "" {
			t.Fatalf("the renderer fell back (%s): a program that fails to link draws no shadow, and this scene must never end there", fallback)
		}
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({errors: window.__glErrors, units: window.__gl ? window.__gl.getParameter(window.__gl.MAX_TEXTURE_IMAGE_UNITS) : 0})`, &raw)); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Errors []string
			Units  int
		}
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatal(err)
		}
		t.Logf("MAX_TEXTURE_IMAGE_UNITS %d, no fallback, GL errors after draws: %v", got.Units, got.Errors)
		if len(got.Errors) > 0 {
			t.Errorf("%d draw calls raised a GL error, first %s", len(got.Errors), got.Errors[0])
		}
		var data []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		measureShadowEdge(t, img)
	}
}
