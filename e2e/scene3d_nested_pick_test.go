//go:build e2e

package e2e

import (
	"bytes"
	"github.com/chromedp/chromedp"
	"image/png"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
	"math"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScene3DNestedOffCenterPickServed(t *testing.T) {
	for _, variant := range []struct {
		sx      float64
		reverse bool
	}{{2, false}, {-2, false}, {2, true}, {-2, true}} {
		sx := variant.sx
		name := map[bool]string{true: "positive", false: "mirrored"}[sx > 0]
		if variant.reverse {
			name += "-far-first"
		}
		t.Run(name, func(t *testing.T) {
			plane := func(id string, z float64) scene.Node {
				return scene.Group{Position: scene.Vec3(.3, .2, z), Rotation: scene.Euler{Z: .4}, Children: []scene.Node{scene.Mesh{ID: id, Pickable: scene.Bool(true), Geometry: scene.PlaneGeometry{Width: 2, Height: 2}, Rotation: scene.Euler{X: math.Pi / 2}, Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}}}}
			}
			children := []scene.Node{plane("near", .6), plane("far", -.6)}
			if variant.reverse {
				children[0], children[1] = children[1], children[0]
			}
			props := scene.Props{Width: 320, Height: 240, AutoRotate: scene.Bool(false), Background: "#000000", ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), PickSignalNamespace: "nested", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 6), FOV: 60}, Graph: scene.NewGraph(scene.AmbientLight{Color: "#ffffff", Intensity: 1}, scene.DirectionalLight{Color: "#ffffff", Intensity: 1, Direction: scene.Vec3(0, 0, -1)}, scene.Group{Position: scene.Vec3(.4, -.2, 0), Scale: scene.Vec3(sx, 1.3, .5), Children: children})}
			app := server.New()
			app.SetRuntimeRoot(e2eRepoRoot(t))
			app.Page("/", func(ctx *server.Context) gosx.Node {
				return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:320px;height:240px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
			})
			srv := httptest.NewServer(app.Build())
			defer srv.Close()
			page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, `window.__picks=[];document.addEventListener('gosx:scene3d:input',e=>{if(e.detail.kind==='pick'&&e.detail.input.type==='select')__picks.push(e.detail.input)});`, 30*time.Second)
			page.navigate(t, srv.URL)
			page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-renderer="webgl"][data-gosx-scene3d-ready="true"]')`, 10*time.Second, "nested scene")
			var rect struct{ X, Y float64 }
			page.eval(t, `(()=>{const r=document.querySelector('canvas').getBoundingClientRect();return {X:r.x,Y:r.y}})()`, &rect)
			// Authored plane point (.25,0,-.2): Rx(pi/2), then child Rz,
			// child translation, nonuniform parent scale, parent translation.
			x0, y0 := .25, .2
			cx, cy := math.Cos(.4)*x0-math.Sin(.4)*y0, math.Sin(.4)*x0+math.Cos(.4)*y0
			wx, wy, wz := .4+sx*(cx+.3), -.2+1.3*(cy+.2), .3
			px, py := 160+wx/(6-wz)*120/math.Tan(math.Pi/6), 120-wy/(6-wz)*120/math.Tan(math.Pi/6)
			if math.Abs(px-160) < 10 || math.Abs(py-120) < 5 {
				t.Fatal("fixture is not off-center")
			}
			var shot []byte
			var r, g, b uint32
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(shot))
				if err != nil {
					t.Fatal(err)
				}
				r, g, b, _ = img.At(int(rect.X+px), int(rect.Y+py)).RGBA()
				if r > 40000 && g > 40000 && b > 40000 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if r < 40000 || g < 40000 || b < 40000 {
				t.Fatalf("pick pixel not rendered white: %d/%d/%d", r, g, b)
			}
			if err := chromedp.Run(page.ctx, chromedp.MouseClickXY(rect.X+px, rect.Y+py)); err != nil {
				t.Fatal(err)
			}
			page.waitFor(t, `__picks.length===1`, 5*time.Second, "off-center pick")
			var hit struct {
				TargetID                                              string
				WorldX, WorldY, WorldZ, LocalX, LocalY, LocalZ, Depth float64
				TargetTriangleIndex                                   int
			}
			page.eval(t, `__picks[0]`, &hit)
			t.Logf("sx%g pixel(%.6f,%.6f) rgb%d/%d/%d hit%+v", sx, px, py, r>>8, g>>8, b>>8, hit)
			if hit.TargetID != "near" || hit.TargetTriangleIndex != 0 {
				t.Fatalf("reverse-declared near/far order: %+v", hit)
			}
			close := func(got, want float64) {
				t.Helper()
				if math.Abs(got-want) > 1e-5 {
					t.Fatalf("got %.9f want %.9f", got, want)
				}
			}
			close(hit.WorldX, wx)
			close(hit.WorldY, wy)
			close(hit.WorldZ, wz)
			close(hit.LocalX, .25)
			close(hit.LocalY, 0)
			close(hit.LocalZ, -.2)
			close(hit.Depth, math.Sqrt(wx*wx+wy*wy+(6-wz)*(6-wz)))
			// Counterfactual omitted-parent mapping is distinguishable on this point.
			wrongX, wrongY := cx+.3, cy+.2
			if math.Hypot(hit.WorldX-wrongX, hit.WorldY-wrongY) < .1 || math.Hypot(hit.LocalX-hit.WorldX, hit.LocalZ-hit.WorldZ) < .1 {
				t.Fatal("fixture insensitive to omitted parent/inverse")
			}
		})
	}
}
