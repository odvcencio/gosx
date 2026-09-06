//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"image/png"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestScene3DOptInScalePreviewServedWebGL(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			app := server.New()
			app.SetRuntimeRoot(e2eRepoRoot(t))
			app.Page("/", func(ctx *server.Context) gosx.Node {
				props := scene.Props{Width: 360, Height: 240, AutoRotate: scene.Bool(false), ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, GizmoLocalScalePreview: scene.Bool(enabled), SelectionInputSignal: "$preview.selection", GizmoInputSignal: "$preview.mode", Graph: scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: .6, Depth: .4}, Scale: scene.Vec3(1.2, .8, 1), Material: scene.FlatMaterial{Color: "#ffffff"}}, scene.TransformControls{ID: "preview", Target: "box", Mode: "scale"})}
				return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:360px;height:240px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
			})
			srv := httptest.NewServer(app.Build())
			defer srv.Close()
			page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__scalePhases=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='gizmo-commit')__scalePhases.push(e.detail.input)});document.addEventListener('pointerdown',e=>window.__scalePointer=e.pointerId,true);`, 30*time.Second)
			page.navigate(t, srv.URL)
			page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-ready="true"][data-gosx-scene3d-renderer="webgl"]')`, 10*time.Second, "scale preview ready")
			page.eval(t, `__gosx_notify_shared_signal('$preview.selection',JSON.stringify('box'));__gosx_notify_shared_signal('$preview.mode',JSON.stringify('scale'));`, nil)
			var rect struct{ X, Y float64 }
			page.eval(t, `(()=>{const r=document.querySelector('canvas').getBoundingClientRect();return {X:r.x,Y:r.y}})()`, &rect)
			measure := func(label string, accept func([2]int) bool) [2]int {
				t.Helper()
				deadline := time.Now().Add(4 * time.Second)
				var previous [2]int
				for time.Now().Before(deadline) {
					var shot []byte
					if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
						t.Fatal(err)
					}
					img, err := png.Decode(bytes.NewReader(shot))
					if err != nil {
						t.Fatal(err)
					}
					if rect.X < 0 || rect.Y < 0 || int(rect.X)+360 > img.Bounds().Max.X || int(rect.Y)+240 > img.Bounds().Max.Y {
						t.Fatal("canvas outside screenshot")
					}
					minX, minY, maxX, maxY := 9999, 9999, -1, -1
					count := 0
					for y := int(rect.Y); y < int(rect.Y)+240; y++ {
						for x := int(rect.X); x < int(rect.X)+360; x++ {
							r, g, b, _ := img.At(x, y).RGBA()
							if r > 50000 && g > 50000 && b > 50000 {
								count++
								minX = min(minX, x)
								maxX = max(maxX, x)
								minY = min(minY, y)
								maxY = max(maxY, y)
							}
						}
					}
					extent := [2]int{maxX - minX + 1, maxY - minY + 1}
					if count > 100 && accept(extent) && extent == previous {
						if path := os.Getenv("GOSX_SCALE_PREVIEW_SCREENSHOT"); path != "" {
							if err := os.WriteFile(fmt.Sprintf("%s.%t.%s.png", path, enabled, label), shot, 0600); err != nil {
								t.Fatal(err)
							}
						}
						return extent
					}
					previous = extent
					if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
						t.Fatal(err)
					}
				}
				t.Fatalf("%s extent did not converge: %v", label, previous)
				return previous
			}
			before := measure("before", func(v [2]int) bool { return v[0] > 10 })
			x, y := rect.X+205, rect.Y+120
			begin := func() {
				page.eval(t, `__scalePhases=[]`, nil)
				if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1), input.DispatchMouseEvent(input.MouseMoved, x+30, y).WithButton(input.Left).WithButtons(1)); err != nil {
					t.Fatal(err)
				}
				page.waitFor(t, `__scalePhases.some(p=>p.phase==='move')`, 3*time.Second, "scale move")
			}
			begin()
			var factor float64
			page.eval(t, `__scalePhases.at(-1).scaleFactor`, &factor)
			if factor < 1.5 {
				t.Fatalf("factor%g", factor)
			}
			want := func(v [2]int) bool {
				ratio := 1.
				if enabled {
					ratio = factor
				}
				return math.Abs(float64(v[0])/float64(before[0])-ratio) < .1 && math.Abs(float64(v[1]-before[1])) <= 1
			}
			preview := measure("preview", want)
			page.eval(t, `document.querySelector('canvas').dispatchEvent(new PointerEvent('pointercancel',{bubbles:true,pointerId:__scalePointer,clientX:999,clientY:999}));`, nil)
			if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MouseReleased, x+30, y).WithButton(input.Left)); err != nil {
				t.Fatal(err)
			}
			restored := measure("cancelled", func(v [2]int) bool { return v == before })
			var phases []string
			page.eval(t, `__scalePhases.map(p=>p.phase)`, &phases)
			if len(phases) != 3 || phases[2] != "cancel" {
				t.Fatalf("cancel phases%v", phases)
			}
			begin()
			if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MouseReleased, x+30, y).WithButton(input.Left)); err != nil {
				t.Fatal(err)
			}
			retained := measure("ended", want)
			page.eval(t, `__scalePhases.map(p=>p.phase)`, &phases)
			ends := 0
			for _, p := range phases {
				if p == "end" {
					ends++
				}
				if p == "cancel" {
					t.Fatal("end cancelled")
				}
			}
			if ends != 1 {
				t.Fatalf("end phases%v", phases)
			}
			t.Logf("enabled=%t factor=%g before=%v preview=%v cancelled=%v ended=%v", enabled, factor, before, preview, restored, retained)
			if errors := page.PageErrors(); len(errors) > 0 {
				t.Fatalf("page errors%v", errors)
			}
		})
	}
}
