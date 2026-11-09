//go:build e2e

// Real-GPU check for WebGL2 TAA motion vectors: a spinning retained box draws
// the velocity pass and the frame keeps drawing the box. Run on the charter GPU
// runners like the other render-upgrade checks, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -timeout 20m -run TestWebGLTAAMotionVectorsOnGPU ./e2e/
package e2e

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

// spinningBoxVertices is a unit cube as 36 unindexed vertices.
func spinningBoxVertices() (positions, normals, uvs []float64) {
	faces := [][3]float64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for _, n := range faces {
		// Two tangent axes perpendicular to the face normal.
		u := [3]float64{n[1], n[2], n[0]}
		v := [3]float64{n[1]*u[2] - n[2]*u[1], n[2]*u[0] - n[0]*u[2], n[0]*u[1] - n[1]*u[0]}
		corner := func(a, b float64) {
			for k := 0; k < 3; k++ {
				positions = append(positions, 0.5*(n[k]+a*u[k]+b*v[k]))
				normals = append(normals, n[k])
			}
			uvs = append(uvs, (a+1)/2, (b+1)/2)
		}
		corner(-1, -1)
		corner(1, -1)
		corner(1, 1)
		corner(-1, -1)
		corner(1, 1)
		corner(-1, 1)
	}
	return positions, normals, uvs
}

func TestWebGLTAAMotionVectorsOnGPU(t *testing.T) {
	requireRenderGPU(t)
	root := e2eRepoRoot(t)
	positions, normals, uvs := spinningBoxVertices()
	yes, no := true, false
	props := scene.Props{
		Width: 320, Height: 240, Background: "#203040", AutoRotate: &no, ForceWebGL: &yes, RequireWebGL: &yes,
		AdaptivePostFX: &no, AdaptiveTargetFrameMS: 50,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 3}, FOV: 60},
		PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.Tonemap{}, scene.TAA{}}},
		Graph: scene.NewGraph(
			// Immutable, revisioned geometry keeps the box retained, so it moves by
			// its model matrix and draws motion vectors.
			scene.Mesh{ID: "spinner", Spin: scene.Euler{Y: 1.2}, Material: scene.StandardMaterial{Color: "#ff8040", Roughness: 0.6},
				Geometry: scene.BufferGeometry{Positions: positions, Normals: normals, UVs: uvs, Immutable: true, Revision: 1}},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 2.5, Direction: scene.Vector3{X: -0.4, Y: -1, Z: -0.6}},
		),
	}
	url := renderUpgradeServer(t, root, renderUpgradePage(t, "motion-root", 320, 240, props))
	browser := renderUpgradeBrowser(t, "webgl", 320, 240, `window.__gosx_scene3d_render_truth=true`)
	browser.navigate(t, url+"/")
	var movers int
	var antialiasing string
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		var reading struct {
			Movers string `json:"movers"`
			AA     string `json:"aa"`
		}
		browser.eval(t, `(() => { const m = document.querySelector('[data-gosx-scene3d-mounted]'); return { movers: m?.getAttribute('data-gosx-scene3d-render-taa-movers') || '', aa: m?.getAttribute('data-gosx-scene3d-antialiasing') || '' }; })()`, &reading)
		antialiasing = reading.AA
		if n, err := strconv.Atoi(reading.Movers); err == nil && n > movers {
			movers = n
		}
		if movers > 0 && antialiasing == "taa" {
			break
		}
	}
	var shot []byte
	if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("GOSX_RENDER_OUT"); out != "" {
		_ = os.MkdirAll(out, 0o755)
		_ = os.WriteFile(filepath.Join(out, "taa-motion-webgl.png"), shot, 0o644)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	center := lumaAt(img, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
	t.Logf("antialiasing %s, movers drawn %d, box center luma %.0f", antialiasing, movers, center)
	if antialiasing != "taa" || movers < 1 {
		t.Fatalf("WebGL TAA drew no motion vectors (antialiasing %q, movers %d); console=%s", antialiasing, movers, browser.Console())
	}
	if center < 30 {
		t.Fatalf("the spinning box is missing from the frame (center luma %.0f)", center)
	}
	if errors := browser.PageErrors(); len(errors) != 0 {
		t.Fatalf("page errors: %v", errors)
	}
}
