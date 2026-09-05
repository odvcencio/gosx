//go:build e2e

package e2e

import (
	"bytes"
	"image/png"
	"math"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// This is the same authored S*Ry(.7)*Rx(pi/2) plane used by the consumed
// native-preview control, with a larger canvas for screenshot/pointer evidence.
func typedAffineBrowserProps(mode string) scene.Props {
	sx := 3.0
	if strings.HasPrefix(mode, "mirrored") {
		sx = -3
	}
	plane := scene.Mesh{ID: "near", Pickable: scene.Bool(true), Geometry: scene.PlaneGeometry{Width: 2, Height: 2}, Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}, Rotation: scene.Euler{X: math.Pi / 2}}
	scale := scene.Vec3(sx, 1, .5)
	if mode == "rotation-only" {
		scale = scene.Vec3(1, 1, 1)
	}
	node := scene.Group{Scale: scale, Children: []scene.Node{scene.Group{Rotation: scene.Euler{Y: .7}, Children: []scene.Node{plane}}}}
	if strings.HasSuffix(mode, "reference") || strings.HasSuffix(mode, "wrong-normal") {
		angle := math.Atan2(math.Sin(.7)/sx, math.Cos(.7)/.5)
		if strings.HasSuffix(mode, "wrong-normal") {
			angle = math.Atan2(math.Sin(.7)*sx, math.Cos(.7)*.5)
		}
		node = scene.Group{Rotation: scene.Euler{Y: angle}, Children: []scene.Node{plane}}
	}
	return scene.Props{Width: 240, Height: 240, Background: "#000000", ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), PickSignalNamespace: "affine", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(-math.Copysign(1, sx), 0, -.2)}, node)}
}

func TestScene3DTypedNestedAffineServedWebGL(t *testing.T) {
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Page("/", func(ctx *server.Context) gosx.Node {
		return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:240px;height:240px")), ctx.Engine(typedAffineBrowserProps(ctx.Request.URL.Query().Get("mode")).EngineConfig(), gosx.Text("loading")))
	})
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__affineEvents=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='pick')__affineEvents.push(e.detail.input)});`, 90*time.Second)
	widths := map[string]int{}
	reds := map[string]int{}
	for _, mode := range []string{"positive", "mirrored", "rotation-only", "positive-reference", "positive-wrong-normal", "mirrored-reference", "mirrored-wrong-normal"} {
		page.navigate(t, srv.URL+"/?mode="+mode)
		page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"][data-gosx-scene3d-ready="true"]')`, 15*time.Second, "typed affine reveal")
		var shot []byte
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(shot))
			if err != nil {
				t.Fatal(err)
			}
			minX, maxX := 248, 8
			for y := 100; y < 156; y++ {
				for x := 8; x < 248; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r > 5000 && g > 5000 && b > 5000 {
						minX = min(minX, x)
						maxX = max(maxX, x)
					}
				}
			}
			if maxX > minX {
				widths[mode] = maxX - minX
				r, _, _, _ := img.At(128, 128).RGBA()
				reds[mode] = int(r >> 8)
				break
			}
			if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
		}
		if widths[mode] == 0 {
			t.Fatalf("%s no visible typed plane", mode)
		}
		if path := os.Getenv("GOSX_AFFINE_SCREENSHOT"); path != "" {
			if err := os.WriteFile(path+"."+mode+".png", shot, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := chromedp.Run(page.ctx, chromedp.MouseClickXY(128, 128)); err != nil {
			t.Fatal(err)
		}
		page.waitFor(t, `__affineEvents.some(e=>e.targetID==='near'&&e.type==='select'&&e.clickCount===1)`, 5*time.Second, "typed plane real pointer pick")
		var hit string
		page.eval(t, `(()=>{const e=__affineEvents.filter(e=>e.type==='select').at(-1);if(Math.abs(e.depth-5)>1e-6||Math.abs(e.worldX)+Math.abs(e.worldY)+Math.abs(e.worldZ)>1e-6||Math.abs(e.localX)+Math.abs(e.localY)+Math.abs(e.localZ)>1e-6)throw Error('typed center metadata mismatch');const gl=document.querySelector('canvas').getContext('webgl2'),ext=gl.getExtension('WEBGL_debug_renderer_info');return JSON.stringify({id:e.targetID,depth:e.depth,triangle:e.targetTriangleIndex,renderer:gl.getParameter(ext?ext.UNMASKED_RENDERER_WEBGL:gl.RENDERER)})})()`, &hit)
		t.Logf("%s visibleWidth=%d centerRed=%d real pick=%s", mode, widths[mode], reds[mode], hit)
	}
	if widths["positive"] <= widths["rotation-only"]*2 || math.Abs(float64(widths["positive"]-widths["mirrored"])) > 2 {
		t.Fatalf("affine placement/removed-scale sensitivity=%v", widths)
	}
	for _, mode := range []string{"positive", "mirrored"} {
		if math.Abs(float64(reds[mode]-reds[mode+"-reference"])) > 3 || math.Abs(float64(reds[mode+"-reference"]-reds[mode+"-wrong-normal"])) < 5 {
			t.Fatalf("%s consumed normal/ordinary-transform sensitivity=%v", mode, reds)
		}
	}
	if errors := page.PageErrors(); len(errors) > 0 {
		t.Fatalf("page errors=%v", errors)
	}
}
