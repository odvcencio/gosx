//go:build e2e

// Pixel gate for silent MATERIAL SUBSTITUTION in the WebGL2 renderer.
//
// The incident this guards: a Selena-material mesh drew with its companion
// StandardMaterial instead of its compiled shader whenever a
// LinesGeometry/GlowMaterial mesh shared the frame. The renderer reported
// success. There was no console warning, no shader diagnostic and no dropped
// object, so every attribute a person could read said the scene was healthy
// while the framebuffer showed a flat opaque quad. It shipped on a production
// homepage for months and survived two investigations.
//
// The mechanism was an over-draw, not a program-selection mistake:
// drawPBRObjectList bound the Selena program and drew correctly, and then the
// legacy immediate-mode world-mesh path (renderSceneWebGLMeshWorldBundle)
// re-drew the SAME triangles with the flat world program and the material's
// baked base color. That path only runs when the frame also carries world line
// segments, which is why neither the mesh alone nor the lines alone reproduced
// it.
//
// Source-level coverage lives in client/js/runtime.test.js. This test answers
// the question no unit test can: which material owns the PIXELS. The Selena
// shader paints pure green; the companion StandardMaterial is pure red. One
// screenshot decides it.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
)

const selenaOverdrawPageTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><title>selena material overdraw</title>
<style>html,body{margin:0;background:#000}#selena-overdraw-root{width:320px;height:240px}</style>
</head><body>
<div id="selena-overdraw-root"></div>
<script id="gosx-manifest" type="application/json">%s</script>
<script src="/bootstrap.js"></script>
</body></html>`

// The authored surface: unconditional pure green, ignoring lighting entirely so
// the expected pixel is exact rather than approximate.
const selenaOverdrawVertexGLSL = `attribute vec3 position;
attribute vec3 normal;
uniform mat4 mvp;
uniform mat3 normalMatrix;
uniform vec3 padTint;
void main() { gl_Position = (mvp * vec4(position, 1.0)); }`

const selenaOverdrawFragmentGLSL = `precision mediump float;
uniform mat4 mvp;
uniform mat3 normalMatrix;
uniform vec3 padTint;
void main() { gl_FragColor = vec4(padTint, 1.0); }`

type selenaOverdrawReading struct {
	Backend  string `json:"backend"`
	Renderer string `json:"renderer"`
	Fallback string `json:"fallback"`
	Detail   string `json:"detail"`
	MeshDraw string `json:"meshDrawn"`
	Err      string `json:"error"`
}

func TestSelenaMaterialOwnsItsPixelsBesideALinesMesh(t *testing.T) {
	chrome := e2eChromePath(t)

	layout := map[string]any{
		"schemaVersion":   "selena.descriptor.v1",
		"languageVersion": "selena.lang.v1",
		"material":        "OverdrawProbe",
		"kind":            "mesh",
		"entryPoints":     map[string]any{"vertex": "vertexMain", "fragment": "fragmentMain"},
		"attributes": []any{
			map[string]any{"location": 0, "name": "position", "type": "vec3"},
			map[string]any{"location": 1, "name": "normal", "type": "vec3"},
		},
		"textures": []any{},
		"uniformBlock": map[string]any{
			"size": 128,
			"fields": []any{
				map[string]any{"name": "mvp", "type": "mat4", "offset": 0, "size": 64},
				map[string]any{"name": "normalMatrix", "type": "mat3", "offset": 64, "size": 48},
				map[string]any{"name": "padTint", "type": "vec3", "offset": 112, "size": 12},
			},
			"defaults": []any{
				map[string]any{"name": "padTint", "type": "vec3", "values": []any{0, 1, 0}},
			},
		},
		"wgsl":  map[string]any{"group": 0, "binding": 0},
		"metal": map[string]any{"buffer": 0},
	}

	manifest := map[string]any{
		"engines": []any{
			map[string]any{
				"id":        "gosx-engine-selena-overdraw",
				"component": "GoSXScene3D",
				"kind":      "surface",
				"mountId":   "selena-overdraw-root",
				"props": map[string]any{
					"width":        320,
					"height":       240,
					"background":   "#000000",
					"requireWebGL": true,
					"camera":       map[string]any{"x": 0, "y": 0, "z": 6, "fov": 72},
					"scene": map[string]any{
						"materials": []any{
							map[string]any{
								"name": "overdraw-pad",
								"kind": "custom",
								// The companion StandardMaterial color. This is
								// what reaches the framebuffer when the renderer
								// substitutes the built-in material.
								"color":          "#ff0000",
								"wireframe":      false,
								"shaderBackend":  "selena",
								"customVertex":   selenaOverdrawVertexGLSL,
								"customFragment": selenaOverdrawFragmentGLSL,
								"shaderLayout":   layout,
								"customUniforms": map[string]any{"padTint": []any{0, 1, 0}},
							},
							map[string]any{"name": "overdraw-glow", "kind": "glow", "color": "#8de1ff"},
						},
						"objects": []any{
							map[string]any{
								"id": "overdraw-pad", "kind": "box", "material": "overdraw-pad",
								"size": 3, "x": 0, "y": 0, "z": 0,
								"wireframe": false, "doubleSided": true,
							},
							// The trigger. A LinesGeometry/GlowMaterial mesh puts
							// world line segments in the bundle, which is the only
							// gate on the legacy world-mesh path.
							map[string]any{
								"id": "overdraw-wire", "kind": "lines", "material": "overdraw-glow",
								"points": []any{
									map[string]any{"x": -2.5, "y": 2.6, "z": 0},
									map[string]any{"x": 2.5, "y": 2.6, "z": 0},
								},
								"lineSegments": []any{[]any{0, 1}},
								"lineWidth":    2,
							},
						},
					},
				},
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}

	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client", "js", "bootstrap.js"))
	if err != nil {
		t.Fatalf("read bootstrap bundle: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootstrap.js" {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = w.Write(bootstrap)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, selenaOverdrawPageTemplate, manifestJSON)
	}))
	defer server.Close()

	// SwiftShader gives headless Chrome a real WebGL implementation. Without it
	// the runtime downgrades to Canvas2D, runs no shaders, and every pixel
	// assertion below would be meaningless -- the backend check guards that.
	browser, err := chrometest.Start(t.Context(), chrome,
		"--no-sandbox", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--window-size=640,480")
	if err != nil {
		t.Fatalf("start Chrome for material overdraw: %v", err)
	}
	defer browser.Close()
	ctx, cancel := context.WithTimeout(browser.Context, 120*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// The fixture renders on demand. Installing the diagnostics flag
			// after Navigate races the first (and possibly only) healthy frame:
			// pixels are correct, but no telemetry attributes are published.
			// Seed the flag in every new document before any application script
			// can schedule that frame.
			_, err := page.AddScriptToEvaluateOnNewDocument(
				`window.__gosx_scene3d_render_truth = true`,
			).Do(ctx)
			return err
		}),
		chromedp.Navigate(server.URL),
	); err != nil {
		t.Skipf("chrome could not start in this environment: %v", err)
	}

	const readScript = `(() => {
  try {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    if (!el) return JSON.stringify({error: "no mount"});
    return JSON.stringify({
      backend: el.getAttribute("data-gosx-scene3d-backend") || "",
      renderer: el.getAttribute("data-gosx-scene3d-renderer") || "",
      fallback: el.getAttribute("data-gosx-scene3d-render-mesh-material-fallback") || "",
      detail: el.getAttribute("data-gosx-scene3d-render-mesh-material-fallback-detail") || "",
      meshDrawn: el.getAttribute("data-gosx-scene3d-render-mesh-drawn") || "",
    });
  } catch (e) { return JSON.stringify({error: String(e)}); }
})()`

	var reading selenaOverdrawReading
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(readScript, &raw)); err != nil {
			t.Fatalf("read scene attributes: %v", err)
		}
		if err := json.Unmarshal([]byte(raw), &reading); err != nil {
			t.Fatalf("decode scene attributes %q: %v", raw, err)
		}
		if reading.Backend != "" && reading.MeshDraw != "" && reading.MeshDraw != "0" {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}

	blob, _ := json.Marshal(reading)
	t.Logf("[selena-overdraw] %s", blob)
	if reading.Backend != "webgl" {
		t.Skipf("scene did not reach the WebGL backend (%q); a Canvas2D fallback runs no shaders", reading.Backend)
	}

	// The canvas drawing buffer is undefined after compositing, so sample the
	// COMPOSITED frame rather than calling gl.readPixels on a spent buffer.
	var shot []byte
	if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err != nil {
		t.Fatalf("capture screenshot: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}
	bounds := img.Bounds()
	green, red := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			r, g, b := int(r16>>8), int(g16>>8), int(b16>>8)
			if g > r+40 && g > b+40 {
				green++
			}
			if r > g+40 && r > b+40 {
				red++
			}
		}
	}
	t.Logf("[selena-overdraw] green=%d red=%d", green, red)

	if green == 0 && red == 0 {
		t.Fatalf("neither material reached the framebuffer; the fixture drew nothing: %s", blob)
	}
	if red > 0 {
		t.Fatalf("the companion StandardMaterial repainted the Selena mesh: red=%d green=%d. "+
			"A mesh must keep its authored material regardless of what else shares the frame: %s",
			red, green, blob)
	}
	if green == 0 {
		t.Fatalf("the Selena surface never reached the framebuffer: %s", blob)
	}

	// The substitution counter must read a clean zero on a healthy frame, and
	// must be PRESENT -- an absent attribute is the old silence.
	if reading.Fallback != "0" {
		t.Fatalf("expected data-gosx-scene3d-render-mesh-material-fallback=%q on a healthy frame, got %q: %s",
			"0", reading.Fallback, blob)
	}
	if reading.Detail != "" {
		t.Fatalf("expected an empty material-fallback detail on a healthy frame, got %q", reading.Detail)
	}
}
