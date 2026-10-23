//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

// The authored vertex shader moves a green mesh two world units when its
// height texture changes from black to white. The standard fallback is red.
// Pixel centroids distinguish real vertex sampling from a parsed descriptor,
// a missing texture, or silent replacement with the built-in material.
const vertexTexturePixelSource = `material TextureShift {
 param heightMap : texture2d
 vertex(geo) -> vec4 {
  let height = sampleLevel(heightMap, vec2f(0.5, 0.5), 0.0).x
  return mvp * vec4f(geo.position + vec3f(height * 2.0, 0.0, 0.0), 1.0)
 }
 surface(geo) -> color { return rgb(0.0, 1.0, 0.0) }
}`

type vertexTexturePixelReading struct {
	Backend     string  `json:"backend"`
	Fallback    string  `json:"fallback"`
	Drawn       string  `json:"drawn"`
	GreenPixels int     `json:"greenPixels"`
	CenterX     float64 `json:"centerX"`
}

// WebGPU is opt-in: some headless software adapters submit draws but cannot
// present canvas pixels. An opted-in backend must render; fallback is a failure.
func TestSelenaVertexTextureDisplacesRenderedPixels(t *testing.T) {
	shaderSource := vertexTexturePixelSource
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	textures := map[string][]byte{}
	for _, value := range []uint8{0, 255} {
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				img.SetNRGBA(x, y, color.NRGBA{value, value, value, 255})
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		textures[fmt.Sprintf("/height-%d.png", value)] = b.Bytes()
	}
	backends := []string{"webgl"}
	if os.Getenv("GOSX_SELENA_VERTEX_TEXTURE_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			yes, no := true, false
			pages := map[string][]byte{}
			for _, value := range []uint8{0, 255} {
				material, _, err := scene.CompileSelenaMaterial([]byte(shaderSource), scene.SelenaMaterialOptions{
					Standard: scene.StandardMaterial{Color: "#ff0000"},
					Uniforms: map[string]any{"heightMap": fmt.Sprintf("/height-%d.png", value)},
				})
				if err != nil {
					t.Fatal(err)
				}
				props := scene.Props{
					Width: 320, Height: 240, Background: "#000000", AutoRotate: &no,
					Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 6}, FOV: 60, Near: 0.1, Far: 30},
					Graph:  scene.NewGraph(scene.Mesh{ID: "shift", Position: scene.Vector3{X: -1}, Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 0.1}, Material: material}),
				}
				if backend == "webgl" {
					props.ForceWebGL = &yes
					props.RequireWebGL = &yes
				} else {
					props.PreferWebGPU = &yes
				}
				manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
					"id": "vertex-texture", "component": "GoSXScene3D", "kind": "surface", "mountId": "vertex-texture-root", "props": props,
				}}})
				if err != nil {
					t.Fatal(err)
				}
				if out := os.Getenv("GOSX_SELENA_VERTEX_TEXTURE_OUT"); out != "" {
					if err := os.MkdirAll(out, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("%s-%d-manifest.json", backend, value)), manifest, 0644); err != nil {
						t.Fatal(err)
					}
				}
				pages[fmt.Sprintf("/%d", value)] = []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#vertex-texture-root{width:320px;height:240px}</style></head><body><div id="vertex-texture-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, manifest))
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if data, ok := textures[r.URL.Path]; ok {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(data)
					return
				}
				if r.URL.Path == "/bootstrap.js" {
					w.Header().Set("Content-Type", "application/javascript")
					_, _ = w.Write(bootstrap)
					return
				}
				if data, ok := pages[r.URL.Path]; ok {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write(data)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js") {
					http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			flags := map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true}
			if backend == "webgl" {
				flags["disable-features"] = "WebGPU"
			} else {
				flags["enable-unsafe-webgpu"] = true
				flags["enable-features"] = "Vulkan"
				flags["use-vulkan"] = "native"
				flags["disable-vulkan-surface"] = true
			}
			initScript := `window.__gosx_scene3d_render_truth=true`
			browser := newBrowserPage(t, chrome, flags, 400, 320, initScript, 150*time.Second)
			readings := map[string]vertexTexturePixelReading{}
			for _, value := range []uint8{0, 255} {
				browser.navigate(t, server.URL+fmt.Sprintf("/%d", value))
				deadline := time.Now().Add(60 * time.Second)
				var reading vertexTexturePixelReading
				var shot []byte
				for time.Now().Before(deadline) {
					browser.eval(t, `(() => {const e=document.querySelector('[data-gosx-scene3d-mounted]');return {backend:e?.getAttribute('data-gosx-scene3d-backend')||'',fallback:e?.getAttribute('data-gosx-scene3d-render-mesh-material-fallback')||'',drawn:e?.getAttribute('data-gosx-scene3d-render-mesh-drawn')||''}})()`, &reading)
					if reading.Backend == backend {
						if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
							t.Fatal(err)
						}
						img, err := png.Decode(bytes.NewReader(shot))
						if err != nil {
							t.Fatal(err)
						}
						count, sum := 0, 0
						for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
							for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
								r, g, b, _ := img.At(x, y).RGBA()
								if g > 40000 && r < 10000 && b < 10000 {
									count++
									sum += x
								}
							}
						}
						reading.GreenPixels = count
						if count > 0 {
							reading.CenterX = float64(sum) / float64(count)
						}
						if count > 100 && reading.Fallback == "0" && ((value == 0 && reading.CenterX < 150) || (value == 255 && reading.CenterX > 170)) {
							break
						}
					}
					time.Sleep(200 * time.Millisecond)
				}
				if reading.Backend != backend || reading.GreenPixels < 100 || reading.Fallback != "0" || (value == 0 && reading.CenterX >= 150) || (value == 255 && reading.CenterX <= 170) {
					var attributes map[string]string
					browser.eval(t, `(() => {const e=document.querySelector('[data-gosx-scene3d-mounted]');return e?Object.fromEntries(e.getAttributeNames().filter(n=>n.startsWith('data-gosx')).map(n=>[n,e.getAttribute(n)])):{}})()`, &attributes)
					if out := os.Getenv("GOSX_SELENA_VERTEX_TEXTURE_OUT"); out != "" {
						_ = os.MkdirAll(out, 0755)
						if len(shot) > 0 {
							_ = os.WriteFile(filepath.Join(out, backend+"-failed.png"), shot, 0644)
						}
						data, _ := json.MarshalIndent(attributes, "", "  ")
						_ = os.WriteFile(filepath.Join(out, backend+"-failed.json"), data, 0644)
					}
					t.Logf("submitted=%s drawn=%s culled=%s undrawable=%s bundle=%s reason=%s", attributes["data-gosx-scene3d-render-mesh-submitted"], attributes["data-gosx-scene3d-render-mesh-drawn"], attributes["data-gosx-scene3d-render-mesh-view-culled"], attributes["data-gosx-scene3d-render-mesh-undrawable"], attributes["data-gosx-scene3d-webgpu-bundle-state"], attributes["data-gosx-scene3d-webgpu-bundle-reason"])
					t.Fatalf("height%d: no expected authored displacement: %+v; pageErrors=%v; console=%s", value, reading, browser.PageErrors(), browser.Console())
				}
				if !browser.anyRequest(func(url string) bool { return url == server.URL+fmt.Sprintf("/height-%d.png", value) }) {
					t.Fatal("height texture was not requested")
				}
				readings[fmt.Sprint(value)] = reading
				if out := os.Getenv("GOSX_SELENA_VERTEX_TEXTURE_OUT"); out != "" {
					if err := os.MkdirAll(out, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("%s-%d.png", backend, value)), shot, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if readings["255"].CenterX-readings["0"].CenterX < 40 {
				t.Fatalf("texture did not move vertices: %+v", readings)
			}
			if errors := browser.PageErrors(); len(errors) != 0 {
				t.Fatalf("page errors: %v", errors)
			}
			t.Logf("actual %s vertex texture displacement: %+v", backend, readings)
			if out := os.Getenv("GOSX_SELENA_VERTEX_TEXTURE_OUT"); out != "" {
				data, _ := json.MarshalIndent(map[string]any{"backend": backend, "readings": readings, "scope": "Actual browser API/pixels with software renderer flags; no physical GPU/frame-rate certification", "source": shaderSource, "pageErrors": browser.PageErrors()}, "", "  ")
				if err := os.WriteFile(filepath.Join(out, backend+".json"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
