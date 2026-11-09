//go:build e2e

// Real-GPU check for the depth-of-field gather: a far striped wall blurs
// while the in-focus box in front of it stays sharp. Run on the charter GPU
// runners like the other render-upgrade checks, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -timeout 20m -run TestDOFGatherOnGPU ./e2e/
package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

// dofScene is a white box at the focus distance, against black, below a band
// of alternating light and dark bars 30 m behind it (rows 41-77 of 240).
func dofScene(backend string, dof bool) scene.Props {
	yes, no := true, false
	nodes := []scene.Node{
		scene.Mesh{ID: "subject", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 0.2}, Material: scene.StandardMaterial{Color: "#f0f0f0", Roughness: 1}},
		scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.5, Direction: scene.Vector3{Z: -1}},
	}
	for i := -13; i < 13; i++ {
		color := "#202020"
		if i%2 == 0 {
			color = "#e0e0e0"
		}
		nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("bar-%d", i+13), Position: scene.Vector3{X: float64(i) + 0.5, Y: 10, Z: -30},
			Geometry: scene.BoxGeometry{Width: 1, Height: 6, Depth: 0.2}, Material: scene.StandardMaterial{Color: color, Roughness: 1}})
	}
	props := scene.Props{
		Width: 320, Height: 240, Background: "#000000", AutoRotate: &no, AdaptivePostFX: &no, AdaptiveTargetFrameMS: 50,
		Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Z: 4}, FOV: 60, Near: 0.1, Far: 100},
		Environment: scene.Environment{ToneMapping: "linear", AmbientColor: "#ffffff", AmbientIntensity: 0.6},
		Graph:       scene.NewGraph(nodes...),
	}
	if dof {
		props.PostFX = scene.PostFX{Effects: []scene.PostEffect{scene.DOF{FocusDistance: 4, Aperture: 0.05, MaxBlur: 12}}}
	}
	if backend == "webgl" {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	} else {
		props.PreferWebGPU = &yes
	}
	return props
}

func dofShot(t *testing.T, root, backend string, dof bool) image.Image {
	t.Helper()
	url := renderUpgradeServer(t, root, renderUpgradePage(t, "dof-root", 320, 240, dofScene(backend, dof)))
	browser := renderUpgradeBrowser(t, backend, 320, 240, "")
	browser.navigate(t, url+"/")
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		var active string
		browser.eval(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')||''`, &active)
		if active == backend {
			break
		}
	}
	time.Sleep(2 * time.Second)
	var shot []byte
	if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("GOSX_RENDER_OUT"); out != "" {
		_ = os.MkdirAll(out, 0o755)
		_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("dof-%s-%v.png", backend, dof)), shot, 0o644)
	}
	if errors := browser.PageErrors(); len(errors) != 0 {
		t.Fatalf("page errors: %v", errors)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// stripeEnergy is the mean absolute luma step between neighbouring pixels
// along a row through the bars.
func stripeEnergy(img image.Image) float64 {
	b := img.Bounds()
	y := b.Min.Y + b.Dy()*25/100
	sum, n := 0.0, 0
	for x := b.Min.X + b.Dx()/4; x < b.Min.X+b.Dx()*3/4; x++ {
		sum += math.Abs(lumaAt(img, x+1, y) - lumaAt(img, x, y))
		n++
	}
	return sum / float64(n)
}

// edgeStep is the largest luma step on the box's middle row, where only the
// box's left edge meets the black background: a sharp subject keeps a hard
// edge.
func edgeStep(img image.Image) float64 {
	b := img.Bounds()
	y := b.Min.Y + b.Dy()/2
	best := 0.0
	for x := b.Min.X + b.Dx()/4; x < b.Min.X+b.Dx()/2; x++ {
		best = math.Max(best, math.Abs(lumaAt(img, x+1, y)-lumaAt(img, x, y)))
	}
	return best
}

func TestDOFGatherOnGPU(t *testing.T) {
	requireRenderGPU(t)
	root := e2eRepoRoot(t)
	for _, backend := range renderUpgradeBackends() {
		t.Run(backend, func(t *testing.T) {
			sharp, blurred := dofShot(t, root, backend, false), dofShot(t, root, backend, true)
			wall, edge := stripeEnergy(blurred)/math.Max(stripeEnergy(sharp), 0.001), edgeStep(blurred)/math.Max(edgeStep(sharp), 0.001)
			t.Logf("%s: wall detail kept %.2f, subject edge kept %.2f (edge step %.0f -> %.0f)", backend, wall, edge, edgeStep(sharp), edgeStep(blurred))
			if wall > 0.5 {
				t.Errorf("%s: the far wall is not blurred (detail kept %.2f)", backend, wall)
			}
			if edge < 0.6 {
				t.Errorf("%s: the in-focus subject lost its edge (kept %.2f)", backend, edge)
			}
		})
	}
}
