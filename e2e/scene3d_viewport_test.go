//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

func TestScene3DTypedViewportCentersServedWebGL(t *testing.T) {
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Page("/", func(ctx *server.Context) gosx.Node {
		q := ctx.Request.URL.Query()
		w, _ := strconv.Atoi(q.Get("w"))
		h, _ := strconv.Atoi(q.Get("h"))
		plane := scene.Mesh{ID: "viewport", Pickable: scene.Bool(true), Geometry: scene.PlaneGeometry{Width: 20, Height: 20}, Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}, Rotation: scene.Euler{X: math.Pi / 2}}
		props := scene.Props{Width: w, Height: h, Background: "#000000", ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), PickSignalNamespace: "viewport", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5), FOV: 60, Near: .1, Far: 100}, Graph: scene.NewGraph(scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(-1, 0, -.2)}, scene.Group{Scale: scene.Vec3(3, 1, .5), Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{plane}}}})}
		if q.Get("mode") == "orthographic" {
			props.OrthographicCamera = &scene.OrthographicCamera{Position: scene.Vec3(0, 0, 5), Left: 0, Right: 4, Bottom: -1, Top: 3, Zoom: 2, Near: .1, Far: 100}
		}
		return gosx.El("div", gosx.Attrs(gosx.Attr("style", fmt.Sprintf("width:%dpx;height:%dpx", w, h))), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
	})
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__viewportEvents=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='pick')__viewportEvents.push(e.detail.input)});`, 90*time.Second)
	for _, mode := range []string{"perspective", "orthographic"} {
		// The browser's authored base viewport has a 240x180 minimum. Native
		// 1x1/tiny targets are covered separately; use real odd/even CSS sizes here.
		for _, size := range [][2]int{{240, 180}, {241, 181}, {320, 180}, {321, 181}} {
			page.navigate(t, fmt.Sprintf("%s/?mode=%s&w=%d&h=%d", srv.URL, mode, size[0], size[1]))
			page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"][data-gosx-scene3d-ready="true"]')`, 15*time.Second, "viewport reveal")
			var rect struct {
				Left, Top, Width, Height float64
				W, H                     int
			}
			page.eval(t, `(()=>{const c=document.querySelector('canvas'),r=c.getBoundingClientRect();return {Left:r.left,Top:r.top,Width:r.width,Height:r.height,W:c.width,H:c.height}})()`, &rect)
			if rect.W != size[0] || rect.H != size[1] {
				t.Fatalf("requested=%v canvas=%+v", size, rect)
			}
			var shot []byte
			samples := [][2]int{{0, 0}, {size[0] - 1, 0}, {0, size[1] - 1}, {size[0] - 1, size[1] - 1}, {size[0] / 2, size[1] / 2}}
			deadline := time.Now().Add(5 * time.Second)
			visible := false
			for time.Now().Before(deadline) {
				if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(shot))
				if err != nil {
					t.Fatal(err)
				}
				visible = true
				for _, p := range samples {
					r, g, b, _ := img.At(int(rect.Left+(float64(p[0])+.5)*rect.Width/float64(rect.W)), int(rect.Top+(float64(p[1])+.5)*rect.Height/float64(rect.H))).RGBA()
					if r <= 5000 || g <= 5000 || b <= 5000 {
						visible = false
					}
				}
				if visible {
					break
				}
				if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
					t.Fatal(err)
				}
			}
			if !visible {
				t.Fatalf("%s/%v blank screenshot", mode, size)
			}
			if path := os.Getenv("GOSX_VIEWPORT_SCREENSHOT"); path != "" {
				if err := os.WriteFile(fmt.Sprintf("%s.%s-%dx%d.png", path, mode, size[0], size[1]), shot, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, p := range samples {
				page.eval(t, `__viewportEvents.length=0`, nil)
				if err := chromedp.Run(page.ctx, chromedp.MouseClickXY(rect.Left+(float64(p[0])+.5)*rect.Width/float64(rect.W), rect.Top+(float64(p[1])+.5)*rect.Height/float64(rect.H))); err != nil {
					t.Fatal(err)
				}
				page.waitFor(t, `__viewportEvents.some(e=>e.type==='select'&&e.targetID==='viewport')`, 3*time.Second, "pixel-center pick")
				var hit struct{ WorldX, WorldY, WorldZ, Depth float64 }
				page.eval(t, `(()=>{const e=__viewportEvents.filter(e=>e.type==='select').at(-1);return {WorldX:e.worldX,WorldY:e.worldY,WorldZ:e.worldZ,Depth:e.depth}})()`, &hit)
				nx := 2*(float64(p[0])+.5)/float64(size[0]) - 1
				ny := 1 - 2*(float64(p[1])+.5)/float64(size[1])
				k := math.Tan(.7) / 6
				dx := nx * float64(size[0]) / float64(size[1]) * math.Tan(math.Pi/6)
				dy := ny * math.Tan(math.Pi/6)
				depth := 5 / (1 - k*dx)
				x, y := dx*depth, dy*depth
				rayDistance := depth * math.Sqrt(1+dx*dx+dy*dy)
				if mode == "orthographic" {
					x, y = 2+nx, 1+ny
					depth = 5 + k*x
					rayDistance = depth - .1
				}
				if math.Abs(hit.WorldX-x) > 1e-4 || math.Abs(hit.WorldY-y) > 1e-4 || math.Abs(hit.WorldZ-(5-depth)) > 1e-4 || math.Abs(hit.Depth-rayDistance) > 1e-4 {
					t.Fatalf("%s size=%v pixel=%v hit=%+v analytic=(%g,%g,%g) rayDistance=%g", mode, size, p, hit, x, y, 5-depth, rayDistance)
				}
			}
			t.Logf("%s size=%v visible screenshot and five sample-center analytic picks; native orthographic distance differs by near=.1", mode, size)
		}
	}
	if errs := page.PageErrors(); len(errs) > 0 {
		t.Fatalf("page errors=%v", errs)
	}
}
