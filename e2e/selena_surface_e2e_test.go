//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/scene"
)

// These tests check the standard lighting path that StandardMaterial and
// Selena surface materials share, on real GPUs. Run them with the charter
// runners, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -timeout 20m -run 'TestSelenaSurface|TestStandardLightingWhiteFurnace' ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -timeout 20m -run 'TestSelenaSurface|TestStandardLightingWhiteFurnace' ./e2e/
//
// GOSX_SELENA_SURFACE_OUT=<dir> keeps the screenshots.

// surfaceTwinSource is a Selena surface material configured like the
// StandardMaterial it is compared with: the same texture times the same
// linear tint, roughness, metalness and clear coat.
const surfaceTwinSource = `material SurfaceTwin {
    param albedo : texture2d
    param tint : color = rgb(1, 1, 1)
    surface(geo) -> Surface {
        let c = sample(albedo, geo.uv).rgb * tint
        return withClearcoat(pbr(c, 0.45, 0.3), 0.6, 0.04)
    }
}`

const furnaceSurfaceSource = `material FurnaceSurface {
    surface(geo) -> Surface { return pbr(rgb(1, 1, 1), 1.0, 1.0) }
}`

type scene3DReading struct {
	Backend  string `json:"backend"`
	Fallback string `json:"fallback"`
	Drawn    string `json:"drawn"`
	Error    string `json:"error"`
}

// e2eSceneFiles builds the uniform or sky IBL products plus a periodic test
// texture, keyed by URL path.
func e2eSceneFiles(t *testing.T, radiance func(d ibl.Vec3) (float64, float64, float64)) (map[string][]byte, scene.EnvironmentIBL) {
	t.Helper()
	source := ibl.CubeFromRadiance(32, radiance)
	chain := ibl.Prefilter(source, ibl.PrefilterOptions{Samples: 128, MipSelect: true})
	irradiance := ibl.IrradianceCube(ibl.ProjectSH(source), 16)
	lut := ibl.GenerateBRDFLUT(64, 256)
	radianceKTX, err := ibl.EncodeCubeKTX2(chain, map[string]string{"GoSXiblModel": ibl.BRDFModel, "GoSXiblRole": string(ibl.ProductRoleRadiance), "GoSXColorSpace": "linear"})
	if err != nil {
		t.Fatal(err)
	}
	irradianceKTX, err := ibl.EncodeCubeKTX2(ibl.Chain{irradiance}, map[string]string{"GoSXiblModel": "lambert-sh9", "GoSXiblRole": string(ibl.ProductRoleIrradiance), "GoSXColorSpace": "linear"})
	if err != nil {
		t.Fatal(err)
	}
	lutKTX, err := ibl.EncodeBRDFLUTKTX2(lut, map[string]string{"GoSXiblModel": ibl.BRDFModel, "GoSXiblRole": string(ibl.ProductRoleBRDFLUT), "GoSXColorSpace": "linear"})
	if err != nil {
		t.Fatal(err)
	}
	// The texture is periodic, so clamp and repeat wrap read the same texels
	// at the UV seam.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			u, v := 2*math.Pi*float64(x)/64, 2*math.Pi*float64(y)/64
			img.Set(x, y, color.RGBA{uint8(150 + 90*math.Cos(u)), uint8(140 + 80*math.Cos(2*v)), uint8(120 + 70*math.Cos(u+v)), 255})
		}
	}
	var texture bytes.Buffer
	if err := png.Encode(&texture, img); err != nil {
		t.Fatal(err)
	}
	cube := func(uri string, levels, size int, role scene.TextureRole) scene.TextureDescriptor {
		return scene.TextureDescriptor{URI: uri, Role: role, ColorSpace: scene.TextureColorSpaceLinear, Channels: "rgba", View: "cube", Format: "rgba16f", MipLevels: levels, Width: size, Height: size, Faces: 6}
	}
	files := map[string][]byte{"/radiance.ktx2": radianceKTX, "/irradiance.ktx2": irradianceKTX, "/brdf.ktx2": lutKTX, "/texture.png": texture.Bytes()}
	env := scene.EnvironmentIBL{
		SchemaVersion: ibl.DescriptorSchemaVersion,
		Radiance:      cube("/radiance.ktx2", len(chain), 32, scene.TextureRoleEnvironmentRadiance),
		Irradiance:    cube("/irradiance.ktx2", 1, 16, scene.TextureRoleEnvironmentIrradiance),
		BRDFLUT:       scene.TextureDescriptor{URI: "/brdf.ktx2", Role: scene.TextureRoleBRDFLUT, ColorSpace: scene.TextureColorSpaceLinear, Channels: "rg", View: "2d", Format: "rg16f", MipLevels: 1, Width: 64, Height: 64, Faces: 1},
		BRDFModel:     ibl.BRDFModel,
	}
	for level := range chain {
		env.RoughnessPerLevel = append(env.RoughnessPerLevel, ibl.RoughnessForLevel(level, len(chain)))
	}
	return files, env
}

func e2eSceneBackends() []string {
	backends := []string{"webgl"}
	if os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GOSX_SELENA_SURFACE_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	return backends
}

// renderScene3DStable mounts props on backend the way a production page does
// (bootstrap-runtime.js, then the scene3d feature chunk, which lazy-loads the
// packed renderer chunk), waits until ready accepts a screenshot and three
// consecutive screenshots match, and returns the last one. The scene must
// draw with no material fallback.
func renderScene3DStable(t *testing.T, backend string, props scene.Props, files map[string][]byte, ready func(image.Image) bool) image.Image {
	t.Helper()
	wantIBL := !props.Environment.IBL.IsZero()
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	yes, no := true, false
	props.AutoRotate = &no
	props.Responsive = &no
	props.MaxDevicePixelRatio = 1
	if backend == "webgl" {
		props.ForceWebGL = &yes
		props.RequireWebGL = &yes
	} else {
		props.PreferWebGPU = &yes
	}
	// The production lazy-chunk page: the runtime bootstrap and the scene3d
	// feature, which loads the packed renderer chunk for the backend.
	payload, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	runtime := `<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" src="/gosx/bootstrap-feature-scene3d.js"></script>`
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#scene{width:%dpx;height:%dpx}</style></head><body><div id="scene"></div>
%s
<script>window.ready=(async()=>{window.handle=await window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('scene'),emit(){},props:%s});window.mounted=true;})().catch(error=>{window.mountError=String(error);});</script></body></html>`, int(props.Width), int(props.Height), runtime, payload))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case strings.HasPrefix(r.URL.Path, "/gosx/") && strings.HasSuffix(r.URL.Path, ".js"):
			http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
		case files[r.URL.Path] != nil:
			if strings.HasSuffix(r.URL.Path, ".png") {
				w.Header().Set("Content-Type", "image/png")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			_, _ = w.Write(files[r.URL.Path])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
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
	browser := newBrowserPage(t, chrome, flags, int(props.Width)+80, int(props.Height)+80, surfaceDiagnosticsScript, 180*time.Second)
	browser.navigate(t, url+"/")
	var reading scene3DReading
	var last []byte
	same := 0
	start := time.Now()
	for deadline := start.Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		browser.eval(t, `(() => {const e=document.getElementById('scene');return {backend:e?.getAttribute('data-gosx-scene3d-renderer')||'',fallback:e?.getAttribute('data-gosx-scene3d-render-mesh-material-fallback')||'',drawn:e?.getAttribute('data-gosx-scene3d-render-mesh-drawn')||'',error:window.mountError||''}})()`, &reading)
		if reading.Error != "" {
			t.Fatalf("%s mount failed: %s\n%s", backend, reading.Error, browser.Console())
		}
		if console := browser.Console(); strings.Contains(console, "pipeline failed") || strings.Contains(console, "Selena surface anchor") {
			t.Fatalf("%s renderer failed: %s", backend, console)
		}
		if reading.Backend != backend || (reading.Fallback != "" && reading.Fallback != "0") {
			continue
		}
		// Both renderers warn once when image light fails or is unsupported:
		// "[gosx] IBL failed: ..." (WebGL2), "[gosx] WebGPU IBL failed: ...".
		if console := browser.Console(); wantIBL && (strings.Contains(console, "IBL failed") || strings.Contains(console, "IBL unsupported")) {
			t.Fatalf("%s image light did not load: %s", backend, console)
		}
		var shot []byte
		if err := chromedp.Run(browser.ctx, chromedp.Screenshot("#scene canvas", &shot, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		if !ready(img) {
			same = 0
			last = nil
			continue
		}
		if last != nil && bytes.Equal(last, shot) {
			same++
		} else {
			same = 0
		}
		last = shot
		if same >= 2 && time.Since(start) > 3*time.Second {
			if errs := browser.PageErrors(); len(errs) != 0 {
				t.Fatalf("page errors: %v", errs)
			}
			return img
		}
	}
	var diag []string
	browser.eval(t, `window.__gosxShaderDiag || []`, &diag)
	t.Fatalf("%s scene never settled: %+v; pageErrors=%v; shader logs=%q; console=%s", backend, reading, browser.PageErrors(), diag, browser.Console())
	return nil
}

// surfaceDiagnosticsScript turns on render truth and records WebGL shader
// compile and link failures, which the renderer reports only as a material
// fallback when the parallel-compile extension defers them.
const surfaceDiagnosticsScript = `window.__gosx_scene3d_render_truth=true;
(() => {
  if (typeof WebGL2RenderingContext === "undefined") return;
  const p = WebGL2RenderingContext.prototype, link = p.linkProgram, source = p.shaderSource;
  p.shaderSource = function(shader, text) {
    if (String(text).includes("SelenaSurface gosxS")) (window.__gosxShaderDiag = window.__gosxShaderDiag || []).push("surface fragment compiled");
    return source.call(this, shader, text);
  };
  p.linkProgram = function(program) {
    link.call(this, program);
    if (this.getProgramParameter(program, this.LINK_STATUS)) return;
    const diag = window.__gosxShaderDiag = window.__gosxShaderDiag || [];
    diag.push("link: " + this.getProgramInfoLog(program));
    for (const shader of this.getAttachedShaders(program) || []) {
      if (!this.getShaderParameter(shader, this.COMPILE_STATUS)) diag.push("compile: " + this.getShaderInfoLog(shader));
    }
  };
})();`

func saveSurfaceShot(t *testing.T, name string, img image.Image) {
	out := os.Getenv("GOSX_SELENA_SURFACE_OUT")
	if out == "" || img == nil {
		return
	}
	_ = os.MkdirAll(out, 0o755)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err == nil {
		_ = os.WriteFile(filepath.Join(out, name+".png"), buf.Bytes(), 0o644)
	}
}

type imageDiff struct {
	Max, P99   int
	Mean       float64
	OverTwoPct float64
}

func diffImages(t *testing.T, a, b image.Image) imageDiff {
	t.Helper()
	if a.Bounds().Size() != b.Bounds().Size() {
		t.Fatalf("image sizes differ: %v vs %v", a.Bounds().Size(), b.Bounds().Size())
	}
	var diffs []int
	total, over := 0, 0
	ab, bb := a.Bounds(), b.Bounds()
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			pa, pb := pixel8(a, ab.Min.X+x, ab.Min.Y+y), pixel8(b, bb.Min.X+x, bb.Min.Y+y)
			d := 0
			for c := 0; c < 3; c++ {
				if v := pa[c] - pb[c]; v > d {
					d = v
				} else if -v > d {
					d = -v
				}
			}
			diffs = append(diffs, d)
			total += d
			if d > 2 {
				over++
			}
		}
	}
	sort.Ints(diffs)
	return imageDiff{Max: diffs[len(diffs)-1], P99: diffs[len(diffs)*99/100], Mean: float64(total) / float64(len(diffs)), OverTwoPct: 100 * float64(over) / float64(len(diffs))}
}

func srgbToLinear(c uint8) float64 {
	v := float64(c) / 255
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// surfaceParityProps is one scene with every standard lighting input: image
// light from a sky, a shadowing directional light, a point and a spot light,
// fog, a texture and a clear coat. Only the ball's material changes between
// runs; an occluder shadows part of it. The ball is the only object with the
// material under test, so its shaders stay inline rather than hoisted into the
// scene's shader library, which only a manifest-loaded page inflates.
func surfaceParityProps(env scene.EnvironmentIBL, material scene.Material) scene.Props {
	ground := scene.StandardMaterial{Color: "#7a8a70", Roughness: 0.8}
	return scene.Props{
		Width: 320, Height: 240, Background: "#000000",
		Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 1.6, Z: 4.2}, Rotation: scene.Euler{X: -0.33}, FOV: 50, Near: 0.1, Far: 30},
		Environment: scene.Environment{IBL: env, EnvIntensity: 0.6, FogColor: "#8899aa", FogDensity: 0.04, ToneMapping: "linear"},
		Graph: scene.NewGraph(
			scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 10, Height: 0.1, Depth: 10}, Position: scene.Vector3{Y: -1.05}, Material: ground, ReceiveShadow: true},
			scene.Mesh{ID: "ball", Geometry: scene.SphereGeometry{Radius: 1, Segments: 48}, Material: material, CastShadow: true, ReceiveShadow: true},
			scene.Mesh{ID: "occluder", Geometry: scene.BoxGeometry{Width: 0.7, Height: 0.1, Depth: 0.7}, Position: scene.Vector3{X: 0.75, Y: 1.9, Z: 0.6}, Material: ground, CastShadow: true},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.5, Direction: scene.Vector3{X: -0.5, Y: -1, Z: -0.4}, CastShadow: true, ShadowSize: 1024, ShadowBias: 0.002},
			scene.PointLight{ID: "lamp", Color: "#ffcc88", Intensity: 3, Position: scene.Vector3{X: 2, Y: 1.5, Z: 2}},
			scene.SpotLight{ID: "spot", Color: "#88aaff", Intensity: 4, Position: scene.Vector3{X: -2, Y: 3, Z: 1}, Direction: scene.Vector3{X: 0.5, Y: -1, Z: -0.2}, Angle: 0.6, Penumbra: 0.3},
		),
	}
}

func skyRadiance(d ibl.Vec3) (float64, float64, float64) {
	up := math.Max(0, d.Y)
	down := math.Max(0, -d.Y)
	return 0.5 + 0.9*up, 0.6 + 0.8*up - 0.3*down, 0.8 + 0.7*up - 0.5*down
}

// A Selena surface material configured like a StandardMaterial must render
// the same pixels as that StandardMaterial on each backend: both run the same
// standard lighting code and only the material block differs.
func TestSelenaSurfaceMatchesStandardMaterial(t *testing.T) {
	files, env := e2eSceneFiles(t, skyRadiance)
	tint := [3]float64{srgbToLinear(0xc0), srgbToLinear(0x88), srgbToLinear(0x50)}
	standard := scene.StandardMaterial{Color: "#c08850", Texture: "/texture.png", Roughness: 0.45, Metalness: 0.3, Clearcoat: 0.6}
	selena, _, err := scene.CompileSelenaMaterial([]byte(surfaceTwinSource), scene.SelenaMaterialOptions{Uniforms: map[string]any{
		"albedo": scene.SelenaTexture{URL: "/texture.png", ColorSpace: scene.TextureColorSpaceSRGB},
		"tint":   tint[:],
	}})
	if err != nil {
		t.Fatal(err)
	}
	lit := func(img image.Image) bool {
		b := img.Bounds()
		p := pixel8(img, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)
		return p[0]+p[1]+p[2] > 120
	}
	shots := map[string]image.Image{}
	for _, backend := range e2eSceneBackends() {
		t.Run(backend, func(t *testing.T) {
			std := renderScene3DStable(t, backend, surfaceParityProps(env, standard), files, lit)
			sel := renderScene3DStable(t, backend, surfaceParityProps(env, selena), files, lit)
			saveSurfaceShot(t, backend+"-standard", std)
			saveSurfaceShot(t, backend+"-selena-surface", sel)
			shots[backend] = sel
			d := diffImages(t, std, sel)
			t.Logf("%s Selena surface vs StandardMaterial: max %d, p99 %d, mean %.3f, %.3f%% of pixels over 2/255", backend, d.Max, d.P99, d.Mean, d.OverTwoPct)
			if d.P99 > 1 || d.Mean > 0.25 || d.Max > 8 {
				t.Fatalf("%s Selena surface differs from the StandardMaterial: %+v", backend, d)
			}
		})
	}
	if shots["webgl"] != nil && shots["webgpu"] != nil {
		d := diffImages(t, shots["webgl"], shots["webgpu"])
		// Recorded, not asserted: the backends' shadow filters differ (WebGL
		// PCSS with up to 4 cascades, WebGPU 4-tap PCF on one map) and their
		// edge antialiasing differs.
		t.Logf("Selena surface WebGL2 vs WebGPU: max %d, p99 %d, mean %.3f, %.3f%% of pixels over 2/255", d.Max, d.P99, d.Mean, d.OverTwoPct)
	}
}

// White furnace: a white, fully rough, fully metallic sphere under uniform
// white image light reflects all of it, so every sphere pixel equals the
// environment. EnvIntensity 0.5 keeps the expected value at 0.5 linear, so a
// gain above one is as visible as a loss in the 8-bit readback; with no tone
// curve the renderers encode it with pow(x, 1/2.2), which gives 186. Without
// multiple-scattering compensation the rough lobe reflects only its
// single-scatter albedo, 1 - ln 2 = 0.31 at the center, which reads 109.
func TestStandardLightingWhiteFurnace(t *testing.T) {
	files, env := e2eSceneFiles(t, func(ibl.Vec3) (float64, float64, float64) { return 1, 1, 1 })
	surface, _, err := scene.CompileSelenaMaterial([]byte(furnaceSurfaceSource), scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	materials := map[string]scene.Material{
		"standard":       scene.StandardMaterial{Color: "#ffffff", Roughness: 1, Metalness: 1},
		"selena-surface": surface,
	}
	want := int(math.Round(255 * math.Pow(0.5, 1/2.2)))
	for _, backend := range e2eSceneBackends() {
		for _, name := range []string{"standard", "selena-surface"} {
			t.Run(backend+"/"+name, func(t *testing.T) {
				props := scene.Props{
					Width: 240, Height: 240, Background: "#000000",
					Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Z: 3.2}, FOV: 45, Near: 0.1, Far: 10},
					Environment: scene.Environment{IBL: env, EnvIntensity: 0.5, ToneMapping: "linear"},
					Graph:       scene.NewGraph(scene.Mesh{ID: "furnace", Geometry: scene.SphereGeometry{Radius: 1, Segments: 64}, Material: materials[name]}),
				}
				img := renderScene3DStable(t, backend, props, files, func(img image.Image) bool {
					b := img.Bounds()
					return pixel8(img, b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)[0] > 100
				})
				saveSurfaceShot(t, "furnace-"+backend+"-"+name, img)
				b := img.Bounds()
				cx, cy := b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2
				// The sphere spans about 0.75 of the half-height; sample every
				// pixel inside 0.6 of it, which covers view angles out to about
				// 60 degrees from the normal.
				radius := 0.6 * 0.75 * float64(b.Dy()) / 2
				lo, hi, n := 255, 0, 0
				for y := cy - int(radius); y <= cy+int(radius); y++ {
					for x := cx - int(radius); x <= cx+int(radius); x++ {
						if math.Hypot(float64(x-cx), float64(y-cy)) > radius {
							continue
						}
						for _, v := range pixel8(img, x, y) {
							lo, hi = min(lo, v), max(hi, v)
						}
						n++
					}
				}
				t.Logf("%s %s furnace: %d pixels in [%d, %d], want %d", backend, name, n, lo, hi, want)
				if lo < want-3 || hi > want+3 {
					t.Fatalf("%s %s white furnace out of tolerance: [%d, %d], want %d ± 3", backend, name, lo, hi, want)
				}
			})
		}
	}
}

const shadowProbeSurfaceSource = `material ShadowProbe {
    param tint : color = rgb(1, 1, 1)
    surface(geo) -> Surface { return pbr(tint, 0.7, 0.0) }
}`

// The directional shadow reaches a surface material exactly as it reaches a
// StandardMaterial. The camera looks straight down at the ground from 6 m; a
// bar 3 m up at x -2.5..-1.5 casts its shadow, along a light travelling down
// at 45 degrees toward +x, onto x 0.5..1.5 (pixel 195 of 320); x = +4 (pixel
// 299) stays lit (hazel's lighting-probe geometry). WebGL2 must draw the
// shadow. WebGPU's value is recorded, not required: on 2026-11-08 the WebGPU
// renderer drew no directional shadow in this scene for StandardMaterial
// either, and a separate lane is adding WebGPU cascaded shadows.
func TestSelenaSurfaceReceivesDirectionalShadow(t *testing.T) {
	tint := [3]float64{srgbToLinear(0xd0), srgbToLinear(0xd0), srgbToLinear(0xd0)}
	surface, _, err := scene.CompileSelenaMaterial([]byte(shadowProbeSurfaceSource), scene.SelenaMaterialOptions{Uniforms: map[string]any{"tint": tint[:]}})
	if err != nil {
		t.Fatal(err)
	}
	materials := map[string]scene.Material{"standard": scene.StandardMaterial{Color: "#d0d0d0", Roughness: 0.7}, "selena-surface": surface}
	for _, backend := range e2eSceneBackends() {
		readings := map[string][2]int{}
		for _, name := range []string{"standard", "selena-surface"} {
			props := scene.Props{
				Width: 320, Height: 240, Background: "#000000",
				// Z is 0.001, not 0: the camera normalizer treats z = 0 as unset.
				Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 6, Z: 0.001}, Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 60, Near: 0.1, Far: 30},
				Environment: scene.Environment{ToneMapping: "linear"},
				Graph: scene.NewGraph(
					scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 20, Height: 0.1, Depth: 20}, Position: scene.Vector3{Y: -0.05}, Material: materials[name], ReceiveShadow: true},
					scene.Mesh{ID: "occluder", Geometry: scene.BoxGeometry{Width: 1, Height: 0.2, Depth: 6}, Position: scene.Vector3{X: -2, Y: 3}, Material: scene.StandardMaterial{Color: "#808080"}, CastShadow: true},
					scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1, Direction: scene.Vector3{X: 1, Y: -1}, CastShadow: true, ShadowSize: 1024, ShadowBias: 0.002, ShadowCascades: 2},
					scene.AmbientLight{ID: "fill", Color: "#ffffff", Intensity: 0.15},
				),
			}
			img := renderScene3DStable(t, backend, props, nil, func(img image.Image) bool {
				b := img.Bounds()
				return pixel8(img, b.Min.X+b.Dx()*299/320, b.Min.Y+b.Dy()/2)[0] > 60
			})
			saveSurfaceShot(t, "shadow-"+backend+"-"+name, img)
			b := img.Bounds()
			shadow := pixel8(img, b.Min.X+b.Dx()*195/320, b.Min.Y+b.Dy()/2)[0]
			lit := pixel8(img, b.Min.X+b.Dx()*299/320, b.Min.Y+b.Dy()/2)[0]
			readings[name] = [2]int{shadow, lit}
			t.Logf("%s %s: shadowed pixel %d, lit pixel %d", backend, name, shadow, lit)
		}
		std, srf := readings["standard"], readings["selena-surface"]
		if abs(std[0]-srf[0]) > 2 || abs(std[1]-srf[1]) > 2 {
			t.Fatalf("%s: surface material shadow %v differs from StandardMaterial %v", backend, srf, std)
		}
		// The ambient fill keeps the shadow above black: about 87 shadowed
		// against 132 lit on the reference GPUs.
		if backend == "webgl" && std[1]-std[0] < 30 {
			t.Fatalf("webgl drew no directional shadow: shadowed %d, lit %d", std[0], std[1])
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
