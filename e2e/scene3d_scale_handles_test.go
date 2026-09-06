//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
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

func TestScene3DScaleHandlesRemainDistinctAfterSync(t *testing.T) {
	for _, size := range []float64{0, .25, 2} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			app := server.New()
			app.SetRuntimeRoot(e2eRepoRoot(t))
			app.Page("/", func(ctx *server.Context) gosx.Node {
				props := scene.Props{Width: 360, Height: 240, AutoRotate: scene.Bool(false), ForceWebGL: scene.Bool(true), RequireWebGL: scene.Bool(true), Background: "#000000", Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 8), Rotation: scene.Euler{X: .2, Y: .3}, FOV: 60}, SelectionInputSignal: "$offset.selection", GizmoInputSignal: "$offset.mode", Graph: scene.NewGraph(scene.Mesh{ID: "one", Position: scene.Vec3(-1, 0, 0), Visible: scene.Bool(false), Geometry: scene.BoxGeometry{Width: 1}}, scene.Mesh{ID: "two", Position: scene.Vec3(0, 1, 0), Visible: scene.Bool(false), Geometry: scene.BoxGeometry{Width: 1}}, scene.TransformControls{ID: "offset", Size: size, Mode: "translate", Width: 3})}
				return gosx.El("div", gosx.Attrs(gosx.Attr("style", "width:360px;height:240px")), ctx.Engine(props.EngineConfig(), gosx.Text("loading")))
			})
			srv := httptest.NewServer(app.Build())
			defer srv.Close()
			page := newBrowserPage(t, e2eChromePath(t), map[string]any{"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true, "enable-webgl": true, "ignore-gpu-blocklist": true}, 640, 640, "", 25*time.Second)
			page.navigate(t, srv.URL)
			page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-ready="true"][data-gosx-scene3d-renderer="webgl"]')`, 10*time.Second, "scale scene")
			var acceptedShot []byte
			sample := func() [3][2]float64 {
				t.Helper()
				var last [3][2]float64
				var previous [3][2]float64
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					var shot []byte
					if err := chromedp.Run(page.ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
						t.Fatal(err)
					}
					img, err := png.Decode(bytes.NewReader(shot))
					if err != nil {
						t.Fatal(err)
					}
					var rect struct{ X, Y int }
					page.eval(t, `(()=>{const r=document.querySelector('canvas').getBoundingClientRect();return {X:Math.round(r.x),Y:Math.round(r.y)}})()`, &rect)
					if rect.X < 0 || rect.Y < 0 || rect.X+360 > img.Bounds().Max.X || rect.Y+240 > img.Bounds().Max.Y {
						t.Fatal("canvas outside screenshot")
					}
					var counts [3]int
					var sums [3][2]float64
					for y := rect.Y; y < rect.Y+240; y++ {
						for x := rect.X; x < rect.X+360; x++ {
							r, g, b, _ := img.At(x, y).RGBA()
							channel := -1
							if r > 22000 && r > g*2 && r > b*2 {
								channel = 0
							}
							if g > 18000 && g > r*2 && g*5 > b*6 {
								channel = 1
							}
							if b > 22000 && b > r*2 && b*5 > g*6 {
								channel = 2
							}
							if channel >= 0 {
								counts[channel]++
								sums[channel][0] += float64(x)
								sums[channel][1] += float64(y)
							}
						}
					}
					ok := true
					for i := range counts {
						if counts[i] < 2 {
							ok = false
						} else {
							last[i] = [2]float64{sums[i][0] / float64(counts[i]), sums[i][1] / float64(counts[i])}
						}
					}
					if ok {
						for i := 0; i < 3; i++ {
							for j := i + 1; j < 3; j++ {
								if math.Hypot(last[i][0]-last[j][0], last[i][1]-last[j][1]) < 3 {
									ok = false
								}
							}
						}
					}
					if ok && previous == last {
						acceptedShot = shot
						return last
					}
					previous = last
					if err := chromedp.Run(page.ctx, chromedp.Sleep(50*time.Millisecond)); err != nil {
						t.Fatal(err)
					}
				}
				t.Fatalf("colored scale handles collapsed/missing: %v", last)
				return last
			}
			var firstTarget [3][2]float64
			for _, target := range []string{"one", "two"} {
				page.eval(t, fmt.Sprintf(`__gosx_notify_shared_signal('$offset.selection',JSON.stringify(%q));`, target), nil)
				var first [3][2]float64
				for repeat := 0; repeat < 2; repeat++ {
					for step, mode := range []string{"translate", "scale", "rotate", "scale"} {
						page.eval(t, fmt.Sprintf(`__gosx_notify_shared_signal('$offset.mode',JSON.stringify(%q));`, mode), nil)
						if mode == "scale" {
							got := sample()
							if path := os.Getenv("GOSX_SCALE_SCREENSHOT"); path != "" {
								if err := os.WriteFile(fmt.Sprintf("%s.size-%g.%s.%d-%d.png", path, size, target, repeat, step), acceptedShot, 0600); err != nil {
									t.Fatal(err)
								}
							}
							if repeat == 0 {
								first = got
							} else {
								for i := range got {
									if math.Hypot(got[i][0]-first[i][0], got[i][1]-first[i][1]) > 1 {
										t.Fatalf("sync accumulated offset: %v -> %v", first, got)
									}
								}
							}
							t.Logf("size=%g target=%s repeat=%d centers=%v", size, target, repeat, got)
						}
					}
				}
				if target == "one" {
					firstTarget = first
				} else {
					for i := range first {
						if math.Hypot(first[i][0]-firstTarget[i][0], first[i][1]-firstTarget[i][1]) < 10 {
							t.Fatalf("selection did not move handle%d: %v -> %v", i, firstTarget, first)
						}
					}
				}
			}
			if errors := page.PageErrors(); len(errors) > 0 {
				t.Fatalf("page errors: %v", errors)
			}
		})
	}
}
