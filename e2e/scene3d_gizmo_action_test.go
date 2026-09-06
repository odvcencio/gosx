//go:build e2e

package e2e

import (
	"bytes"
	"image/png"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/session"
)

func TestScene3DGizmoActionServed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "page.gsx"), []byte("package app\nfunc Page() Node { return <main /> }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var calls []map[string]string
	modules := route.NewFileModuleRegistry()
	err := modules.Register(route.FileModuleFor("page.gsx", route.FileModuleOptions{
		Render: func(ctx *route.RouteContext, _ route.FilePage, _ any) (gosx.Node, error) {
			props := scene.Props{Width: 360, Height: 240, AutoRotate: scene.Bool(false), ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, GizmoCommitAction: ctx.ActionPath("transform"), GizmoLocalScalePreview: scene.Bool(true), SelectionInputSignal: "$action.selection", GizmoInputSignal: "$action.mode", Graph: scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: .6, Depth: .4}, Scale: scene.Vec3(1.2, .8, 1), Material: scene.FlatMaterial{Color: "#ffffff"}}, scene.TransformControls{ID: "action", Target: "box", Mode: "scale"})}
			return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:360px;height:240px")), gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "csrf_token"), gosx.Attr("value", session.Token(ctx.Request)))), ctx.Engine(props.EngineConfig(), gosx.Text("loading"))), nil
		},
		Actions: route.FileActions{"transform": func(ctx *action.Context) error {
			fields := map[string]string{}
			for k, v := range ctx.FormData {
				fields[k] = v
			}
			fields["method"] = ctx.Request.Method
			fields["xhr"] = ctx.Request.Header.Get("X-Requested-With")
			mu.Lock()
			calls = append(calls, fields)
			mu.Unlock()
			return ctx.Success("saved", map[string]any{"$action.selection": "other", "$action.mode": "translate", "scaleX": 999})
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Gizmo action", body))
	})
	if err = router.AddDir(root, route.FileRoutesOptions{Modules: modules}); err != nil {
		t.Fatal(err)
	}
	app := server.New()
	app.EnableNavigation()
	app.SetRuntimeRoot(e2eRepoRoot(t))
	app.Mount("/", router.Build())
	sessions := session.MustNew("gizmo-action-served-test-secret", session.Options{})
	srv := httptest.NewServer(sessions.Middleware(sessions.Protect(app.Build())))
	defer srv.Close()
	page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__phases=[];window.__results=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='gizmo-commit')__phases.push(e.detail.input)});document.addEventListener('gosx:scene3d:gizmo-action-result',e=>__results.push(e.detail));document.addEventListener('pointerdown',e=>window.__pointer=e.pointerId,true);`, 30*time.Second)
	page.navigate(t, srv.URL)
	page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-ready="true"]')`, 10*time.Second, "scene ready")
	page.eval(t, `window.__doc=document;window.__canvas=document.querySelector('canvas');window.__href=location.href;window.__nav=performance.getEntriesByType('navigation').length;__gosx_notify_shared_signal('$action.selection',JSON.stringify('box'));__gosx_notify_shared_signal('$action.mode',JSON.stringify('scale'));`, nil)
	var rect struct{ X, Y float64 }
	page.eval(t, `(()=>{const r=__canvas.getBoundingClientRect();return {X:r.x,Y:r.y}})()`, &rect)
	width := func() int {
		t.Helper()
		var shot []byte
		if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		lo, hi := 9999, -1
		for y := int(rect.Y); y < int(rect.Y)+240; y++ {
			for x := int(rect.X); x < int(rect.X)+360; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 50000 && g > 50000 && b > 50000 {
					lo = min(lo, x)
					hi = max(hi, x)
				}
			}
		}
		if hi < lo {
			t.Fatal("no visible white object")
		}
		return hi - lo + 1
	}
	baseline := width()
	// Install a test-only applicable managed-data sink even without an island
	// WASM input setter. A mistaken applyManagedFormData call must be visible.
	page.eval(t, `window.__dataApplied=0;window.__originalBatch=window.__gosx_set_input_batch;window.__gosx_set_input_batch=function(value){if(JSON.parse(value).scaleX===999){__dataApplied++;__canvas.setAttribute('data-response-sentinel','applied');return}if(typeof __originalBatch==='function')return __originalBatch.apply(this,arguments)};__gosx_set_input_batch(JSON.stringify({scaleX:999}));`, nil)
	var observed bool
	page.eval(t, `typeof __gosx_set_input_batch==='function'&&__dataApplied===1&&__canvas.getAttribute('data-response-sentinel')==='applied'`, &observed)
	if !observed {
		t.Fatal("managed-data sentinel is not observable")
	}
	page.eval(t, `__dataApplied=0;__canvas.removeAttribute('data-response-sentinel');`, nil)
	fire := func(kind input.MouseType, x float64) {
		t.Helper()
		if err := chromedp.Run(page.ctx, input.DispatchMouseEvent(kind, rect.X+x, rect.Y+120).WithButton(input.Left).WithClickCount(1)); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(calls) }
	fire(input.MousePressed, 223)
	fire(input.MouseMoved, 249)
	if count() != 0 {
		t.Fatal("request before end")
	}
	page.eval(t, `__canvas.dispatchEvent(new PointerEvent('pointercancel',{pointerId:__pointer,bubbles:true}));`, nil)
	time.Sleep(100 * time.Millisecond)
	if count() != 0 {
		t.Fatal("cancel submitted")
	}
	fire(input.MouseReleased, 249)
	fire(input.MousePressed, 223)
	fire(input.MouseMoved, 249)
	time.Sleep(100 * time.Millisecond)
	preview := width()
	if preview <= baseline {
		t.Fatalf("preview did not grow: %d -> %d", baseline, preview)
	}
	fire(input.MouseReleased, 249)
	var debug string
	page.eval(t, `JSON.stringify({phases:__phases,api:!!(window.__gosx&&__gosx.navigation&&__gosx.navigation.submitBackgroundAction),results:__results})`, &debug)
	t.Log(debug)
	page.waitFor(t, `__results.length===1`, 5*time.Second, "action result")
	time.Sleep(100 * time.Millisecond)
	retained := width()
	if retained != preview {
		t.Fatalf("response changed local preview: %d -> %d", preview, retained)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("calls %v", got)
	}
	fields := got[0]
	if len(fields) != 8 || fields["target"] != "box" || fields["mode"] != "scale" || fields["axis"] != "x" || fields["phase"] != "end" || fields["csrf_token"] == "" || fields["method"] != "POST" || fields["xhr"] != "XMLHttpRequest" {
		t.Fatalf("fields %v", fields)
	}
	factor, err := strconv.ParseFloat(fields["scale_factor"], 64)
	if err != nil || factor <= 1 {
		t.Fatalf("factor %v", fields)
	}
	var phases []struct {
		Phase, Target, Mode, Axis string
		ScaleFactor               float64
	}
	page.eval(t, `__phases`, &phases)
	want := []string{"start", "move", "cancel", "start", "move", "end"}
	if len(phases) != len(want) {
		t.Fatalf("phase count: %+v", phases)
	}
	for i, p := range phases {
		if p.Phase != want[i] {
			t.Fatalf("phase sequence: %+v", phases)
		}
	}
	end := phases[len(phases)-1]
	if end.Target != fields["target"] || end.Mode != fields["mode"] || end.Axis != fields["axis"] || math.IsNaN(end.ScaleFactor) || math.IsInf(end.ScaleFactor, 0) || math.Abs(factor-end.ScaleFactor) > 1e-12*math.Max(1, math.Abs(end.ScaleFactor)) {
		t.Fatalf("submitted factor %g does not match end %+v", factor, end)
	}
	var stable bool
	page.eval(t, `__dataApplied===0&&!__canvas.hasAttribute('data-response-sentinel')&&__results[0].ok===true&&__results[0].status===200&&document===__doc&&document.querySelector('canvas')===__canvas&&location.href===__href&&performance.getEntriesByType('navigation').length===__nav&&__phases.filter(p=>p.phase==='end').length===1`, &stable)
	if !stable {
		t.Fatal("navigation/identity/result changed")
	}
	var statuses []int
	page.eval(t, `Promise.all(['','wrong'].map(token=>fetch('/__actions/transform',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded','Accept':'application/json'},body:new URLSearchParams(token?{csrf_token:token}:{}),credentials:'same-origin'}).then(r=>r.status)))`, &statuses)
	if len(statuses) != 2 || statuses[0] != 403 || statuses[1] != 403 || count() != 1 {
		t.Fatalf("CSRF %v calls %d", statuses, count())
	}
	t.Logf("exact fields verified; factor %.6f; CSRF %v; one end result; document/canvas/navigation unchanged; screenshot widths %d -> %d -> %d", factor, statuses, baseline, preview, retained)
}
