//go:build e2e

// Real-GPU check for the per-backend post-effect chunks (TAA, depth of field,
// auto exposure). The page carries the chunk URLs the island renderer writes
// for the scene, as a GoSX page does. A scene without TAA must fetch neither
// post-effect chunk and still draw; a scene with TAA must fetch only its own
// backend's chunk, and TAA must run. Run it on the charter GPU runners:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- env GOSX_WATER_E2E_REQUIRE_WEBGPU=1 go test -tags e2e -timeout 20m -run TestPostFXChunkLoadsOnlyForScenesThatDeclareIt ./e2e/
//	mac-gpu-run <worktree> go test -tags e2e -timeout 20m -run TestPostFXChunkLoadsOnlyForScenesThatDeclareIt ./e2e/
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/scene"
)

func postFXChunkScene(backend string, taa bool) scene.Props {
	positions, normals, uvs := spinningBoxVertices()
	yes, no := true, false
	effects := []scene.PostEffect{scene.Tonemap{}}
	if taa {
		effects = append(effects, scene.TAA{})
	}
	props := scene.Props{
		Width: 320, Height: 240, Background: "#203040", AutoRotate: &no,
		AdaptivePostFX: &no, AdaptiveTargetFrameMS: 50,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 3}, FOV: 60},
		PostFX: scene.PostFX{Effects: effects},
		Graph: scene.NewGraph(
			scene.Mesh{ID: "spinner", Spin: scene.Euler{Y: 1.2}, Material: scene.StandardMaterial{Color: "#ff8040", Roughness: 0.6},
				Geometry: scene.BufferGeometry{Positions: positions, Normals: normals, UVs: uvs, Immutable: true, Revision: 1}},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 2.5, Direction: scene.Vector3{X: -0.4, Y: -1, Z: -0.6}},
		),
	}
	if backend == "webgl" {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	} else {
		props.PreferWebGPU = &yes
	}
	return props
}

var sceneChunkURLAttr = regexp.MustCompile(`data-gosx-scene3d-([a-z0-9-]+)-url="`)

// postFXChunkPage renders the scene's bootstrap tag through the island
// renderer and keeps the chunk kinds it advertises, pointed at the local
// bundles that renderUpgradeServer serves. It returns the page and the kinds.
func postFXChunkPage(t *testing.T, props scene.Props) ([]byte, map[string]bool) {
	t.Helper()
	config := props.EngineConfig()
	r := island.NewRenderer("main")
	r.RenderEngine(config, gosx.Text(""))
	kinds := map[string]bool{}
	var attrs strings.Builder
	for _, match := range sceneChunkURLAttr.FindAllStringSubmatch(gosx.RenderHTML(r.BootstrapScript()), -1) {
		if kind := match[1]; !kinds[kind] {
			kinds[kind] = true
			fmt.Fprintf(&attrs, ` data-gosx-scene3d-%s-url="/gosx/bootstrap-feature-scene3d-%s.js"`, kind, kind)
		}
	}
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "postfx-engine", "component": "GoSXScene3D", "kind": "surface", "mountId": "postfx-root", "props": config.Props,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#postfx-root{width:320px;height:240px}</style></head><body><div id="postfx-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js" data-gosx-script="bootstrap"%s></script></body></html>`, manifest, attrs.String())
	return []byte(page), kinds
}

func TestPostFXChunkLoadsOnlyForScenesThatDeclareIt(t *testing.T) {
	requireRenderGPU(t)
	root := e2eRepoRoot(t)
	for _, backend := range renderUpgradeBackends() {
		other := map[string]string{"webgl": "webgpu", "webgpu": "webgl"}[backend]
		for _, taa := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/taa=%v", backend, taa), func(t *testing.T) {
				page, kinds := postFXChunkPage(t, postFXChunkScene(backend, taa))
				if kinds["postfx-webgpu"] != taa || kinds["postfx-webgl"] != taa {
					t.Fatalf("island advertised postfx-webgpu=%v postfx-webgl=%v for taa=%v", kinds["postfx-webgpu"], kinds["postfx-webgl"], taa)
				}
				url := renderUpgradeServer(t, root, page)
				browser := renderUpgradeBrowser(t, backend, 320, 240, "")
				browser.navigate(t, url+"/")
				var reading struct {
					Backend   string  `json:"backend"`
					AA        string  `json:"aa"`
					TAAActive float64 `json:"taaActive"`
					Frames    float64 `json:"frames"`
				}
				ready := false
				for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
					browser.eval(t, `(() => { const m = document.querySelector('[data-gosx-scene3d-mounted]'); const s = m && m.__gosxScene3DWebGPUStats; return { backend: m?.getAttribute('data-gosx-scene3d-backend') || '', aa: m?.getAttribute('data-gosx-scene3d-antialiasing') || '', taaActive: s ? s.taaActive || 0 : 0, frames: s ? s.frameSeq || 0 : 0 }; })()`, &reading)
					if reading.Backend != backend {
						continue
					}
					// WebGL reports the resolved antialiasing mode on the mount;
					// WebGPU reports whether the temporal resolve ran this frame.
					active := reading.AA == "taa"
					if backend == "webgpu" {
						active = reading.TAAActive > 0
					}
					if taa && active || !taa && (backend == "webgl" && reading.AA != "" || backend == "webgpu" && reading.Frames > 30) {
						ready = true
						break
					}
				}
				fetched := browser.anyRequest(func(u string) bool { return strings.Contains(u, "bootstrap-feature-scene3d-postfx-"+backend+".js") })
				fetchedOther := browser.anyRequest(func(u string) bool { return strings.Contains(u, "bootstrap-feature-scene3d-postfx-"+other+".js") })
				t.Logf("backend %q antialiasing %q taaActive %v frames %v; fetched postfx-%s=%v postfx-%s=%v", reading.Backend, reading.AA, reading.TAAActive, reading.Frames, backend, fetched, other, fetchedOther)
				if !ready {
					t.Fatalf("%s scene (taa=%v) never reached its expected state; console=%s", backend, taa, browser.Console())
				}
				if fetched != taa {
					t.Fatalf("postfx-%s fetched=%v, want %v", backend, fetched, taa)
				}
				if fetchedOther {
					t.Fatalf("a %s page fetched the postfx-%s chunk", backend, other)
				}
				if !taa && reading.AA == "taa" {
					t.Fatalf("TAA ran on a scene that did not declare it")
				}
				var shot []byte
				if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(shot))
				if err != nil {
					t.Fatal(err)
				}
				b := img.Bounds()
				if center := lumaAt(img, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2); center < 30 {
					t.Fatalf("the box is missing from the frame (center luma %.0f)", center)
				}
				if errors := browser.PageErrors(); len(errors) != 0 {
					t.Fatalf("page errors: %v", errors)
				}
			})
		}
	}
}
