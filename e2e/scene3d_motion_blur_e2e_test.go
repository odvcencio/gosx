//go:build e2e

// Motion blur on a real GPU. The scene is a row of hard-edged white boxes on a dark ground. Two checks run on
// WebGPU and on forced WebGL2:
//
//   - Camera sweep: the camera slides sideways at a constant speed. With MotionBlur the horizontal edges of the
//     boxes spread across many pixels while the vertical edges stay sharp, so the ratio of horizontal to
//     vertical gradient energy falls against the same sweep without the effect.
//   - Moving object (WebGPU only, where the renderer draws per-object motion vectors): a bar spins about its
//     center in front of a still camera. Its tips smear into a band of partly covered pixels, so the share of
//     partly covered pixels rises against the same scene without the effect.
//
// Run with a GPU runner and GOSX_WATER_E2E_REQUIRE_WEBGPU=1:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestMotionBlur -timeout 600s ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestMotionBlur -timeout 600s ./e2e/
package e2e

import (
	"bytes"
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
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

// motionBlurScene is a row of white boxes, optionally with one box circling the origin, and an optional
// MotionBlur effect. Ambient light keeps the flat boxes visible on both backends.
func motionBlurScene(t *testing.T, blur bool, orbiter bool) string {
	t.Helper()
	nodes := []scene.Node{}
	for i := 0; i < 70; i++ {
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("b%d", i), Geometry: scene.BoxGeometry{Width: 0.5, Height: 2, Depth: 0.5},
			Material: scene.FlatMaterial{Color: "#ffffff", Wireframe: scene.Bool(false)}, Position: scene.Vec3(-5+1.7*float64(i), 0, -1)})
	}
	if orbiter {
		// A bar spinning about its center: its tips move fast, and it stays a retained mesh with a changing model matrix.
		nodes = []scene.Node{scene.Mesh{ID: "bar", Geometry: scene.BoxGeometry{Width: 5, Height: 0.5, Depth: 0.5}, Material: scene.FlatMaterial{Color: "#ffffff", Wireframe: scene.Bool(false)}, Spin: scene.Rotate(0, 0, 10)}}
	}
	effects := []scene.PostEffect{}
	if blur {
		effects = append(effects, scene.MotionBlur{Quality: scene.MotionBlurMedium, Strength: 1, MaxBlur: 128})
	}
	effects = append(effects, scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1})
	props := scene.Props{
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60, Near: 0.1, Far: 100},
		PostFX: scene.PostFX{Effects: effects},
		Graph:  scene.NewGraph(append(nodes, scene.AmbientLight{ID: "amb", Color: "#ffffff", Intensity: 1})...),
	}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const motionBlurPage = `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000;overflow:hidden}#s{width:800px;height:450px}</style></head><body>
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
// Sweep the camera sideways at a fixed speed (units per frame); wrap before the row of boxes ends.
window.sweep = function(speed) {
 let x = 0;
 (function tick() {
  x += speed; if (x > 90) x = 0;
  window.handle.setCamera({ x: x, y: 0, z: 6, fov: 60, near: 0.1, far: 100 });
  requestAnimationFrame(tick);
 })();
};
</script></body></html>`

func motionBlurServer(t *testing.T, blur, orbiter bool) (string, func()) {
	t.Helper()
	root := e2eRepoRoot(t)
	markup := fmt.Sprintf(motionBlurPage, mustJSON(t, map[string]any{
		"width": decalShotW, "height": decalShotH, "responsive": false, "maxDevicePixelRatio": 1, "controls": "none",
		"shaded": true, "background": "#05070a", "scene": json.RawMessage(motionBlurScene(t, blur, orbiter)),
	}))
	mux := http.NewServeMux()
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(root, "client", "js")))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(markup))
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

// gradientEnergy sums the squared horizontal and vertical luma differences inside roi.
func gradientEnergy(img image.Image, roi image.Rectangle) (horizontal, vertical float64) {
	luma := func(x, y int) float64 {
		r, g, b, _ := img.At(x, y).RGBA()
		return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
	}
	roi = roi.Intersect(img.Bounds())
	for y := roi.Min.Y; y < roi.Max.Y-1; y++ {
		for x := roi.Min.X; x < roi.Max.X-1; x++ {
			l := luma(x, y)
			dx, dy := luma(x+1, y)-l, luma(x, y+1)-l
			horizontal += dx * dx
			vertical += dy * dy
		}
	}
	return
}

func motionBlurShots(t *testing.T, backend string, blur, orbiter bool, n int) []image.Image {
	t.Helper()
	ctx, closeBrowser := decalBrowser(t, backend)
	defer closeBrowser()
	url, stop := motionBlurServer(t, blur, orbiter)
	defer stop()
	decalNavigate(t, ctx, url, backend)
	if !orbiter {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.sweep(0.35)`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1200 * time.Millisecond)
	shots := make([]image.Image, 0, n)
	for i := 0; i < n; i++ {
		var data []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		shots = append(shots, img)
		if dir := os.Getenv("GOSX_DECAL_EVIDENCE"); dir != "" && i == 0 {
			_ = os.MkdirAll(dir, 0o755)
			name := fmt.Sprintf("motion-%s-blur%v-orbit%v.png", backend, blur, orbiter)
			_ = os.WriteFile(filepath.Join(dir, strings.ReplaceAll(name, " ", "")), data, 0o644)
		}
		time.Sleep(180 * time.Millisecond)
	}
	var stats string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__gosx_scene3d_depthfx_api && window.__gosx_scene3d_depthfx_api.stats)`, &stats)); err == nil {
		t.Logf("%s blur=%v orbit=%v: depthfx stats %s", backend, blur, orbiter, stats)
	}
	return shots
}

func meanEnergy(shots []image.Image) (h, v float64) {
	for _, shot := range shots {
		dh, dv := gradientEnergy(shot, image.Rect(100, 60, 700, 390))
		h, v = h+dh, v+dv
	}
	return h / float64(len(shots)), v / float64(len(shots))
}

func TestMotionBlurCameraSweepOnTheGPU(t *testing.T) {
	for _, backend := range decalBackends(t) {
		t.Run(backend, func(t *testing.T) {
			sharpH, sharpV := meanEnergy(motionBlurShots(t, backend, false, false, 6))
			blurH, blurV := meanEnergy(motionBlurShots(t, backend, true, false, 6))
			sharpRatio, blurRatio := sharpH/math.Max(sharpV, 1), blurH/math.Max(blurV, 1)
			t.Logf("%s: gradient energy horizontal/vertical: sharp %.0f/%.0f = %.3f, blurred %.0f/%.0f = %.3f", backend, sharpH, sharpV, sharpRatio, blurH, blurV, blurRatio)
			if blurRatio > sharpRatio*0.6 {
				t.Errorf("camera sweep: horizontal/vertical energy %.3f with blur against %.3f without; blur along the sweep should cut it below 0.6x", blurRatio, sharpRatio)
			}
			if blurV < sharpV*0.7 {
				t.Errorf("vertical edges lost energy (%.0f -> %.0f): the blur must follow the motion, not spread everywhere", sharpV, blurV)
			}
		})
	}
}

// partialShare is the fraction of lit pixels that are partly covered: between the ground (luma about 39) and
// the white box (about 231). A sharp bar has only an antialiased rim; a blurred one smears its tips across a
// band of partly covered pixels.
func partialShare(shots []image.Image) float64 {
	var partial, lit float64
	for _, shot := range shots {
		roi := image.Rect(100, 60, 700, 390).Intersect(shot.Bounds())
		for y := roi.Min.Y; y < roi.Max.Y; y++ {
			for x := roi.Min.X; x < roi.Max.X; x++ {
				r, g, b, _ := shot.At(x, y).RGBA()
				l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
				switch {
				case l > 190:
					lit++
				case l > 80:
					partial++
					lit++
				}
			}
		}
	}
	if lit == 0 {
		return 0
	}
	return partial / lit
}

func TestMotionBlurMovingObjectOnWebGPU(t *testing.T) {
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run TestMotionBlur ./e2e)")
	}
	sharpShots := motionBlurShots(t, "webgpu", false, true, 8)
	blurShots := motionBlurShots(t, "webgpu", true, true, 8)
	sharp, blurred := partialShare(sharpShots), partialShare(blurShots)
	t.Logf("webgpu: spinning bar, share of lit pixels that are partly covered: sharp %.3f, blurred %.3f (%.1fx)", sharp, blurred, blurred/math.Max(sharp, 1e-6))
	if sharp == 0 || sharp > 0.25 {
		t.Fatalf("the sharp bar is not a clean reference (partial share %.3f)", sharp)
	}
	if blurred < sharp*2 || blurred < 0.05 {
		t.Errorf("moving bar: partial share %.3f with blur against %.3f without; blur along the bar's motion should at least double it", blurred, sharp)
	}
}
