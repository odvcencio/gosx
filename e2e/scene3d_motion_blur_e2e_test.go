//go:build e2e

// Motion blur on a real GPU. The scene is a row of hard-edged white boxes on a dark ground. Two checks run on
// WebGPU and on forced WebGL2:
//
//   - Camera sweep: the camera slides sideways at a constant speed. With MotionBlur the horizontal edges of the
//     boxes spread across many pixels while the vertical edges stay sharp, so the ratio of horizontal to
//     vertical gradient energy falls against the same sweep without the effect.
//   - Moving object (WebGPU only, where the renderer draws per-object motion vectors): a box circles a still
//     camera. The frame's total gradient energy falls against the same scene without the effect.
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
// MotionBlur effect.
func motionBlurScene(t *testing.T, blur bool, orbiter bool) string {
	t.Helper()
	nodes := []scene.Node{}
	for i := 0; i < 70; i++ {
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("b%d", i), Geometry: scene.BoxGeometry{Width: 0.5, Height: 2, Depth: 0.5},
			Material: scene.FlatMaterial{Color: "#ffffff"}, Position: scene.Vec3(-5+1.7*float64(i), 0, -1)})
	}
	if orbiter {
		nodes = []scene.Node{scene.Group{ID: "spin", Spin: scene.Rotate(0, 0, 7), Children: []scene.Node{
			scene.Mesh{ID: "orbiter", Geometry: scene.BoxGeometry{Width: 0.7, Height: 0.7, Depth: 0.7}, Material: scene.FlatMaterial{Color: "#ffffff"}, Position: scene.Vec3(2.2, 0, 0)},
		}}}
	}
	effects := []scene.PostEffect{}
	if blur {
		effects = append(effects, scene.MotionBlur{Quality: scene.MotionBlurHigh, Strength: 0.9, MaxBlur: 16})
	}
	effects = append(effects, scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1})
	props := scene.Props{
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60, Near: 0.1, Far: 100},
		PostFX: scene.PostFX{Effects: effects},
		Graph:  scene.NewGraph(nodes...),
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

func TestMotionBlurMovingObjectOnWebGPU(t *testing.T) {
	if !waterRequireWebGPU() {
		t.Skip("needs a real GPU: set GOSX_WATER_E2E_REQUIRE_WEBGPU=1 on a GPU runner (go test -tags e2e -run TestMotionBlur ./e2e)")
	}
	sharpH, sharpV := meanEnergy(motionBlurShots(t, "webgpu", false, true, 8))
	blurH, blurV := meanEnergy(motionBlurShots(t, "webgpu", true, true, 8))
	sharp, blurred := sharpH+sharpV, blurH+blurV
	t.Logf("webgpu: orbiting box gradient energy: sharp %.0f, blurred %.0f (%.2fx)", sharp, blurred, blurred/math.Max(sharp, 1))
	if sharp < 1000 {
		t.Fatalf("the orbiting box is not visible (energy %.0f)", sharp)
	}
	if blurred > sharp*0.75 {
		t.Errorf("moving box: gradient energy %.0f with blur against %.0f without; the blur should cut it below 0.75x", blurred, sharp)
	}
}
