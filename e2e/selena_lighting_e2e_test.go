//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
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

// The probe paints the Selena scene lighting block as colors, so one screenshot
// shows whether each renderer bound it: red is mlShadow0 (1 lit, 0 in the
// directional shadow), green is lightCount / 8 (two lights give 0.25), blue is
// mlFogFactor (the share of color that survives the scene fog).
const selenaLightingProbeSource = `material LightingProbe {
    context {
        cameraPos       : vec3
        cameraForward   : vec3
        fog             : vec4
        lightCount      : float
        lights          : array<vec4, 32>
        shadow0         : vec4
        shadowSplits0   : vec4
        shadowMatrices0 : array<vec4, 16>
        shadowMap0      : textureDepth2dArray
    }
    surface(geo) -> color {
        let s = mlShadow0(geo.worldPos)
        return vec3f(s, clamp(lightCount / 8.0, 0.0, 1.0), mlFogFactor(geo.worldPos))
    }
}`

type selenaLightingReading struct {
	Backend  string `json:"backend"`
	Fallback string `json:"fallback"`
	Drawn    string `json:"drawn"`
	Shadow   [3]int `json:"shadowPixel"`
	Lit      [3]int `json:"litPixel"`
	Corner   [3]int `json:"cornerPixel"`
}

// The camera looks straight down at a ground slab from 6 m. A shadow-casting
// bar (x -2.5..-1.5, 3 m up) sits at the left edge of the view, and the
// directional light travels down at 45 degrees toward +x, so the bar's shadow
// falls on the ground at x 0.5..1.5 (about pixel 195 of 320). The ground at
// x = +4 (pixel 299) is lit. The fog density lets 69% of the color through at
// the shadow sample (6.1 m away) and about 50% at the top-right corner (8.3 m).
//
// Run on real GPUs with the charter runners, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestSelenaSceneLightingReachesShaders ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestSelenaSceneLightingReachesShaders ./e2e/
func TestSelenaSceneLightingReachesShaders(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	backends := []string{"webgl"}
	if os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GOSX_SELENA_LIGHTING_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	material, _, err := scene.CompileSelenaMaterial([]byte(selenaLightingProbeSource), scene.SelenaMaterialOptions{
		Standard: scene.StandardMaterial{Color: "#ff00ff"},
		Modules:  []string{"lighting"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			yes, no := true, false
			props := scene.Props{
				Width: 320, Height: 240, Background: "#000000", AutoRotate: &no,
				// Z is 0.001, not 0: the camera normalizer treats z = 0 as unset and moves the camera to z = 6.
				Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 6, Z: 0.001}, Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 60, Near: 0.1, Far: 30},
				Environment: scene.Environment{FogColor: "#000000", FogDensity: 0.1, ToneMapping: "linear"},
				Graph: scene.NewGraph(
					scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 20, Height: 0.1, Depth: 20}, Position: scene.Vector3{Y: -0.05}, Material: material, ReceiveShadow: true},
					scene.Mesh{ID: "occluder", Geometry: scene.BoxGeometry{Width: 1, Height: 0.2, Depth: 6}, Position: scene.Vector3{X: -2, Y: 3}, Material: scene.StandardMaterial{Color: "#808080"}, CastShadow: true},
					scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1, Direction: scene.Vector3{X: 1, Y: -1}, CastShadow: true, ShadowSize: 1024, ShadowBias: 0.002, ShadowCascades: 2},
					scene.PointLight{ID: "lamp", Color: "#ffffff", Intensity: 1, Position: scene.Vector3{X: 3, Y: 2}},
				),
			}
			if backend == "webgl" {
				props.ForceWebGL = &yes
				props.RequireWebGL = &yes
			} else {
				props.PreferWebGPU = &yes
			}
			manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
				"id": "selena-lighting", "component": "GoSXScene3D", "kind": "surface", "mountId": "selena-lighting-root", "props": props,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#selena-lighting-root{width:320px;height:240px}</style></head><body><div id="selena-lighting-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, manifest))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write(page)
				case r.URL.Path == "/bootstrap.js":
					w.Header().Set("Content-Type", "application/javascript")
					_, _ = w.Write(bootstrap)
				case strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js"):
					http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			url := server.URL
			if e2eWindowsRelay() {
				url = e2eExposeToWindows(t, server.URL)
			}
			flags := map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true}
			if backend == "webgl" {
				flags["disable-features"] = "WebGPU"
			} else {
				flags["enable-unsafe-webgpu"] = true
			}
			browser := newBrowserPage(t, chrome, flags, 400, 320, `window.__gosx_scene3d_render_truth=true`, 150*time.Second)
			browser.navigate(t, url+"/")

			var reading selenaLightingReading
			var shot []byte
			ok := false
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
				browser.eval(t, `(() => {const e=document.querySelector('[data-gosx-scene3d-mounted]');return {backend:e?.getAttribute('data-gosx-scene3d-backend')||'',fallback:e?.getAttribute('data-gosx-scene3d-render-mesh-material-fallback')||'',drawn:e?.getAttribute('data-gosx-scene3d-render-mesh-drawn')||''}})()`, &reading)
				if reading.Backend != backend {
					continue
				}
				if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(shot))
				if err != nil {
					t.Fatal(err)
				}
				b := img.Bounds()
				reading.Shadow = pixel8(img, b.Min.X+b.Dx()*195/320, b.Min.Y+b.Dy()/2)
				reading.Lit = pixel8(img, b.Min.X+b.Dx()*299/320, b.Min.Y+b.Dy()/2)
				reading.Corner = pixel8(img, b.Max.X-3, b.Min.Y+2)
				if selenaLightingReadingOK(reading, backend == "webgl") {
					ok = true
					break
				}
			}
			if out := os.Getenv("GOSX_SELENA_LIGHTING_OUT"); out != "" && len(shot) > 0 {
				_ = os.MkdirAll(out, 0o755)
				_ = os.WriteFile(filepath.Join(out, backend+".png"), shot, 0o644)
			}
			if !ok {
				t.Fatalf("%s did not render the lighting probe: %+v; pageErrors=%v; console=%s", backend, reading, browser.PageErrors(), browser.Console())
			}
			if errors := browser.PageErrors(); len(errors) != 0 {
				t.Fatalf("page errors: %v", errors)
			}
			t.Logf("%s lighting probe: %+v", backend, reading)
		})
	}
}

// selenaLightingReadingOK checks the probe colors with wide margins, so tone
// mapping or the sRGB encoding of the canvas cannot flip a verdict (0.25 reads
// as 64 linear or 137 sRGB):
// the shadowed center is dark red and the lit side is bright red; green shows
// two lights; blue shows fog thinning toward the far corner.
//
// requireShadow is false on WebGPU: on 2026-11-08 the WebGPU renderer drew no
// directional shadow in this scene for its own standard material either (the
// same scene with a standard-material ground showed none on an RTX 5070 Ti),
// so the probe can only confirm that WebGPU binds the shadow inputs without
// error. WebGL's standard-material shadow and the probe's shadow matched pixel
// for pixel. Require the WebGPU shadow once the renderer draws it.
func selenaLightingReadingOK(r selenaLightingReading, requireShadow bool) bool {
	if r.Fallback != "0" {
		return false
	}
	shadowed := r.Shadow[0] < 60 || !requireShadow
	lit := r.Lit[0] > 150
	lights := r.Lit[1] > 40 && r.Lit[1] < 175 && r.Shadow[1] > 40 && r.Shadow[1] < 175
	fog := r.Shadow[2] > 110 && r.Shadow[2] < 240 && r.Corner[2] < r.Shadow[2]-12
	return shadowed && lit && lights && fog
}

func pixel8(img image.Image, x, y int) [3]int {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}
