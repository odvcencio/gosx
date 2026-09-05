//go:build e2e

package e2e

import (
	"bytes"
	"image/png"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

// Typed authored controls, real pointer input and rendered preview rollback;
// no backend corpus certification or application persistence is implied.
func TestScene3DGizmoCancelServedWebGL(t *testing.T) {
	app := server.New()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Page("/", func(ctx *server.Context) gosx.Node {
		props := scene.Props{Width: 360, Height: 240, AutoRotate: scene.Bool(false), Controls: "orbit", ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, SelectionInputSignal: "$gizmo.selection", GizmoInputSignal: "$gizmo.mode", GizmoOutputSignal: "$gizmo.result", Graph: scene.NewGraph(scene.Mesh{ID: "cube", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: scene.FlatMaterial{Color: "#ffffff"}}, scene.TransformControls{ID: "handles", Target: "cube", Mode: "translate"})}
		return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:360px;height:240px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
	})
	srv := httptest.NewServer(app.Build())
	defer srv.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 480, `window.__gizmoPhases=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='gizmo-commit')__gizmoPhases.push(e.detail.input)});document.addEventListener('pointerdown',e=>window.__gizmoPointer=e.pointerId,true);`, 90*time.Second)
	page.navigate(t, srv.URL)
	page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"][data-gosx-scene3d-ready="true"]')`, 15*time.Second, "typed gizmo reveal")
	page.eval(t, `window.__gosx_notify_shared_signal('$gizmo.selection',JSON.stringify('cube'));window.__gosx_notify_shared_signal('$gizmo.mode',JSON.stringify('translate'));`, nil)
	var rect map[string]float64
	page.eval(t, `(()=>{const r=document.querySelector('canvas').getBoundingClientRect();return {x:r.left,y:r.top}})()`, &rect)
	centroid := func() float64 {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		count, sum := 0, 0
		for y := int(rect["y"]) + 60; y < int(rect["y"])+180; y++ {
			for x := int(rect["x"]) + 60; x < int(rect["x"])+330; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 50000 && g > 50000 && b > 50000 {
					count++
					sum += x
				}
			}
		}
		if count < 100 {
			t.Fatalf("missing visible typed cube: %d white pixels", count)
		}
		return float64(sum) / float64(count)
	}
	before := centroid()
	x, y := rect["x"]+205, rect["y"]+120
	if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1), input.DispatchMouseEvent(input.MouseMoved, x+30, y).WithButton(input.Left).WithButtons(1)); err != nil {
		t.Fatal(err)
	}
	page.waitFor(t, `__gizmoPhases.some(p=>p.phase==='move')`, 5*time.Second, "real gizmo move")
	preview := centroid()
	if preview-before < 10 {
		t.Fatalf("preview not consumed: %f -> %f", before, preview)
	}
	page.eval(t, `document.querySelector('canvas').dispatchEvent(new PointerEvent('pointercancel',{bubbles:true,pointerId:__gizmoPointer,clientX:999,clientY:999}));`, nil)
	page.waitFor(t, `__gizmoPhases.at(-1)?.phase==='cancel'`, 5*time.Second, "cancel phase")
	if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MouseReleased, x+30, y).WithButton(input.Left)); err != nil {
		t.Fatal(err)
	}
	after := centroid()
	if math.Abs(after-before) > 2 {
		t.Fatalf("cancel pixels did not roll back: %f -> %f", before, after)
	}
	var phases []string
	page.eval(t, `__gizmoPhases.map(p=>p.phase)`, &phases)
	if len(phases) != 3 || phases[0] != "start" || phases[1] != "move" || phases[2] != "cancel" {
		t.Fatalf("unexpected cancellation commits: %v", phases)
	}
	page.eval(t, `__gizmoPhases=[]`, nil)
	if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(input.Left).WithClickCount(1), input.DispatchMouseEvent(input.MouseMoved, x+30, y).WithButton(input.Left).WithButtons(1), input.DispatchMouseEvent(input.MouseReleased, x+30, y).WithButton(input.Left)); err != nil {
		t.Fatal(err)
	}
	page.waitFor(t, `__gizmoPhases.at(-1)?.phase==='end'`, 5*time.Second, "normal end")
	committed := centroid()
	if committed-before < 10 {
		t.Fatalf("normal end not consumed: %f -> %f", before, committed)
	}
	page.eval(t, `__gizmoPhases.map(p=>p.phase)`, &phases)
	ends := 0
	for _, phase := range phases {
		if phase == "end" {
			ends++
		}
		if phase == "cancel" {
			t.Fatal("normal end cancelled")
		}
	}
	if ends != 1 {
		t.Fatalf("end count: %v", phases)
	}
	t.Logf("typed software-WebGL centroid before=%.2f preview=%.2f cancelled=%.2f committed=%.2f", before, preview, after, committed)
}
