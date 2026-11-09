//go:build e2e

// Real-GPU checks for the WebGPU render upgrades: a plain StandardMaterial
// casts and receives a directional shadow on both backends (one map and four
// cascades), and the Blackglass Coast beacon view reports its frame cost with
// and without the new effects.
//
// Run on the charter GPU runners, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -timeout 20m -run 'TestStandardMaterialShadowOnGPU|TestBeaconFrameCost' ./e2e/
//	GPU_CHROME_EXCLUSIVE=1 /home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -timeout 20m -run 'TestStandardMaterialShadowOnGPU|TestBeaconFrameCost' ./e2e/
//
// GOSX_RENDER_BUNDLES names a directory of bootstrap bundles to serve instead
// of client/js (for a before/after comparison). GOSX_RENDER_OUT names a folder
// for screenshots and the frame-cost JSON.
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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	beacon "m31labs.dev/gosx/examples/gosx-docs/app/demos/beacon"
	"m31labs.dev/gosx/scene"
)

func renderUpgradeBackends() []string {
	backends := []string{"webgl"}
	if e2eWindowsRelay() || os.Getenv("GOSX_WATER_E2E_REQUIRE_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	return backends
}

func renderUpgradeBundles(root string) string {
	if dir := os.Getenv("GOSX_RENDER_BUNDLES"); dir != "" {
		return dir
	}
	return filepath.Join(root, "client", "js")
}

// renderUpgradeServer serves one page, the bootstrap bundles, and the docs
// site's public assets (models, environment maps) at their site URLs.
func renderUpgradeServer(t *testing.T, root string, page []byte) string {
	bundles := renderUpgradeBundles(root)
	public := filepath.Join(root, "examples", "gosx-docs", "public")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case r.URL.Path == "/bootstrap.js" || strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "application/javascript")
			http.ServeFile(w, r, filepath.Join(bundles, filepath.Base(r.URL.Path)))
		default:
			http.ServeFile(w, r, filepath.Join(public, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/"))))
		}
	}))
	t.Cleanup(server.Close)
	if e2eWindowsRelay() {
		return e2eExposeToWindows(t, server.URL)
	}
	return server.URL
}

func renderUpgradePage(t *testing.T, mountID string, width, height int, props any) []byte {
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": mountID + "-engine", "component": "GoSXScene3D", "kind": "surface", "mountId": mountID, "props": props,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#%s{width:%dpx;height:%dpx}</style></head><body><div id="%s"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js" data-gosx-script="bootstrap"%s></script></body></html>`, mountID, width, height, mountID, manifest, renderUpgradeChunkAttrs(t)))
}

// renderUpgradeChunkAttrs advertises every Scene3D feature chunk on the
// bootstrap tag (data-gosx-scene3d-<kind>-url), as the docs site does, so
// opt-in chunks such as zoom load on demand.
func renderUpgradeChunkAttrs(t *testing.T) string {
	files, err := filepath.Glob(filepath.Join(renderUpgradeBundles(e2eRepoRoot(t)), "bootstrap-feature-scene3d-*.js"))
	if err != nil {
		t.Fatal(err)
	}
	var attrs strings.Builder
	for _, file := range files {
		name := filepath.Base(file)
		kind := strings.TrimSuffix(strings.TrimPrefix(name, "bootstrap-feature-scene3d-"), ".js")
		fmt.Fprintf(&attrs, ` data-gosx-scene3d-%s-url="/gosx/%s"`, kind, name)
	}
	return attrs.String()
}

func renderUpgradeBrowser(t *testing.T, backend string, width, height int, init string) *browserPage {
	flags := map[string]any{}
	if backend == "webgl" {
		flags["disable-features"] = "WebGPU"
	} else {
		flags["enable-unsafe-webgpu"] = true
	}
	return newBrowserPage(t, e2eChromePath(t), flags, width+80, height+80, init, 180*time.Second)
}

func lumaAt(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
}

// The camera looks down at a white ground slab from 6 m. A grey bar 3 m up at
// x = -2 casts the shadow of a light travelling down at 45 degrees toward +x
// onto the ground near x = +1 (pixel 195 of 320); x = +4 (pixel 299) is lit.
func TestStandardMaterialShadowOnGPU(t *testing.T) {
	root := e2eRepoRoot(t)
	for _, backend := range renderUpgradeBackends() {
		for _, cascades := range []int{0, 4} {
			t.Run(fmt.Sprintf("%s/cascades-%d", backend, cascades), func(t *testing.T) {
				yes, no := true, false
				props := scene.Props{
					Width: 320, Height: 240, Background: "#000000", AutoRotate: &no,
					Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 6, Z: 0.001}, Rotation: scene.Euler{X: -math.Pi / 2}, FOV: 60, Near: 0.1, Far: 30},
					Environment: scene.Environment{ToneMapping: "linear", AmbientColor: "#ffffff", AmbientIntensity: 0.05},
					Graph: scene.NewGraph(
						scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 20, Height: 0.1, Depth: 20}, Position: scene.Vector3{Y: -0.05}, Material: scene.StandardMaterial{Color: "#ffffff", Roughness: 1}, ReceiveShadow: true},
						scene.Mesh{ID: "occluder", Geometry: scene.BoxGeometry{Width: 1, Height: 0.2, Depth: 6}, Position: scene.Vector3{X: -2, Y: 3}, Material: scene.StandardMaterial{Color: "#808080"}, CastShadow: true},
						scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.5, Direction: scene.Vector3{X: 1, Y: -1}, CastShadow: true, ShadowSize: 1024, ShadowBias: 0.002, ShadowCascades: cascades},
					),
				}
				if backend == "webgl" {
					props.ForceWebGL, props.RequireWebGL = &yes, &yes
				} else {
					props.PreferWebGPU = &yes
				}
				url := renderUpgradeServer(t, root, renderUpgradePage(t, "shadow-root", 320, 240, props))
				browser := renderUpgradeBrowser(t, backend, 320, 240, `window.__gosx_scene3d_render_truth=true`)
				browser.navigate(t, url+"/")
				var shadow, lit float64
				var shot []byte
				ok := false
				for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
					var active string
					browser.eval(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')||''`, &active)
					if active != backend {
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
					shadow = lumaAt(img, b.Min.X+b.Dx()*195/320, b.Min.Y+b.Dy()/2)
					lit = lumaAt(img, b.Min.X+b.Dx()*299/320, b.Min.Y+b.Dy()/2)
					if lit > 80 && shadow < lit*0.5 {
						ok = true
						break
					}
				}
				if out := os.Getenv("GOSX_RENDER_OUT"); out != "" && len(shot) > 0 {
					_ = os.MkdirAll(out, 0o755)
					_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("shadow-%s-%d.png", backend, cascades)), shot, 0o644)
				}
				t.Logf("%s cascades=%d shadow luma %.0f, lit luma %.0f", backend, cascades, shadow, lit)
				if !ok {
					t.Fatalf("%s drew no directional shadow (shadow %.0f, lit %.0f); pageErrors=%v console=%s", backend, shadow, lit, browser.PageErrors(), browser.Console())
				}
			})
		}
	}
}

type frameCost struct {
	Backend    string  `json:"backend"`
	Variant    string  `json:"variant"`
	Frames     int     `json:"frames"`
	IntervalMS float64 `json:"intervalMeanMS"`
	CPUP50     float64 `json:"renderCPUp50MS"`
	CPUP95     float64 `json:"renderCPUp95MS"`
	GPUP50     float64 `json:"gpuP50MS"`
	GPUP95     float64 `json:"gpuP95MS"`
	GPUSource  string  `json:"gpuSource"`
	TAA        float64 `json:"taaHistoryValid"`
	TAAActive  float64 `json:"taaActive"`
	Exposure   float64 `json:"autoExposure"`
	Tier       string  `json:"qualityTier"`
	PostFX     string  `json:"postFX"`
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[int(math.Min(float64(len(sorted)-1), math.Floor(p*float64(len(sorted)))))]
}

// beaconProps returns the beacon view at noon. The "features" variant adds the
// upgrades: four shadow cascades, TAA, and auto exposure.
func beaconProps(t *testing.T, backend string, features bool) map[string]any {
	props := beacon.BlackglassBeachProgram("beacon", "noon")
	yes := true
	if backend == "webgl" {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	} else {
		props.PreferWebGPU = &yes
	}
	data, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	// Pin quality: the adaptive governor drops TAA below the full tier and
	// swaps effects for FXAA under load, which would change what is measured.
	// A 50 ms target (the maximum) keeps the governor measuring (it owns the
	// WebGL GPU timer) without ever leaving the full tier on these GPUs.
	record["adaptiveTargetFrameMS"] = 50
	record["adaptivePostFX"] = false
	// The experience descriptor (camera tour, bookmarks) is served by the docs
	// site and changes no rendering work; this page draws the authored camera.
	delete(record, "experienceRef")
	if !features {
		return record
	}
	sceneRecord, _ := record["scene"].(map[string]any)
	if sceneRecord == nil {
		sceneRecord = record
	}
	if lights, ok := sceneRecord["lights"].([]any); ok {
		for _, raw := range lights {
			if light, ok := raw.(map[string]any); ok && light["kind"] == "directional" && light["castShadow"] == true {
				light["shadowCascades"] = 4
				light["shadowDistance"] = 80
			}
		}
	}
	// Post effects live in the scene record (SceneIR.PostEffects).
	target := record
	if _, ok := sceneRecord["postEffects"]; ok {
		target = sceneRecord
	}
	effects, _ := target["postEffects"].([]any)
	target["postEffects"] = append(effects, map[string]any{"kind": "taa"})
	env, _ := sceneRecord["environment"].(map[string]any)
	if env == nil {
		env = map[string]any{}
		sceneRecord["environment"] = env
	}
	env["autoExposure"] = map[string]any{"minExposure": 0.5, "maxExposure": 2}
	return record
}

const frameCostProbe = `(() => {
  const out = { intervals: [], cpu: [], gpu: [], gpuSource: "", taa: 0, taaActive: 0, exposure: 0, tier: "", postfx: "" };
  window.__gosxFrameCost = out;
  document.addEventListener("gosx:scene3d:perf", (e) => { if (e.detail && e.detail.scope === "frame" && e.detail.gpuMS > 0) { out.gpu.push(e.detail.gpuMS); out.gpuSource = e.detail.source; } });
  let last = 0;
  const tick = (now) => {
    if (last) out.intervals.push(now - last);
    last = now;
    const mount = document.querySelector('[data-gosx-scene3d-mounted]');
    const quality = mount && mount.__gosxScene3DQualityState;
    const sample = quality && quality.lastMeasurement;
    if (sample && String(sample.source).indexOf("webgl-timer") >= 0 && sample.rendererDurationMS > 0 && sample.atMS !== out.lastAt) { out.lastAt = sample.atMS; out.gpu.push(sample.rendererDurationMS); out.gpuSource = "webgl-timer"; }
    const stats = mount && mount.__gosxScene3DWebGPUStats;
    if (stats) { out.taa = stats.taaHistoryValid || 0; out.taaActive = stats.taaActive || 0; out.exposure = stats.autoExposure || 0; }
    if (quality) out.tier = String(quality.activeTier || "");
    if (mount) out.postfx = mount.getAttribute("data-gosx-scene3d-postfx") || "";
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  new PerformanceObserver((list) => { for (const entry of list.getEntries()) if (entry.name.indexOf("scene3d-render") === 0) out.cpu.push(entry.duration); }).observe({ entryTypes: ["measure"] });
  return true;
})()`

// TestBeaconFrameCost measures the Blackglass Coast beacon view for 8 s per
// backend and variant, with adaptive quality pinned. It records numbers; it
// fails on page errors, a renderer that never mounts, or (WebGPU "features")
// TAA or auto exposure that never runs.
func TestBeaconFrameCost(t *testing.T) {
	if os.Getenv("GOSX_RENDER_FRAMES") != "1" {
		t.Skip("set GOSX_RENDER_FRAMES=1 on a GPU runner")
	}
	root := e2eRepoRoot(t)
	variants := []string{"baseline", "features"}
	if os.Getenv("GOSX_RENDER_BUNDLES") != "" {
		variants = []string{"baseline"}
	}
	var results []frameCost
	for _, backend := range renderUpgradeBackends() {
		for _, variant := range variants {
			t.Run(backend+"/"+variant, func(t *testing.T) {
				url := renderUpgradeServer(t, root, renderUpgradePage(t, "beacon-root", 1280, 720, beaconProps(t, backend, variant == "features")))
				browser := renderUpgradeBrowser(t, backend, 1280, 720, `window.__gosx_scene3d_perf=true`)
				browser.navigate(t, url+"/")
				mounted := false
				for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
					var active string
					browser.eval(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')||''`, &active)
					if active == backend {
						mounted = true
						break
					}
				}
				if !mounted {
					t.Fatalf("%s never mounted; pageErrors=%v console=%s", backend, browser.PageErrors(), browser.Console())
				}
				time.Sleep(4 * time.Second) // asset loads and pipeline compiles settle
				var started bool
				browser.eval(t, frameCostProbe, &started)
				time.Sleep(8 * time.Second)
				var raw struct {
					Intervals []float64 `json:"intervals"`
					CPU       []float64 `json:"cpu"`
					GPU       []float64 `json:"gpu"`
					GPUSource string    `json:"gpuSource"`
					TAA       float64   `json:"taa"`
					TAAActive float64   `json:"taaActive"`
					Exposure  float64   `json:"exposure"`
					Tier      string    `json:"tier"`
					PostFX    string    `json:"postfx"`
				}
				browser.eval(t, `window.__gosxFrameCost`, &raw)
				mean := 0.0
				for _, v := range raw.Intervals {
					mean += v / float64(len(raw.Intervals))
				}
				cost := frameCost{Backend: backend, Variant: variant, Frames: len(raw.Intervals), IntervalMS: mean,
					CPUP50: percentile(raw.CPU, 0.5), CPUP95: percentile(raw.CPU, 0.95), GPUP50: percentile(raw.GPU, 0.5), GPUP95: percentile(raw.GPU, 0.95),
					GPUSource: raw.GPUSource, TAA: raw.TAA, TAAActive: raw.TAAActive, Exposure: raw.Exposure, Tier: raw.Tier, PostFX: raw.PostFX}
				results = append(results, cost)
				t.Logf("%+v", cost)
				if out := os.Getenv("GOSX_RENDER_OUT"); out != "" {
					var shot []byte
					if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err == nil {
						_ = os.MkdirAll(out, 0o755)
						_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("beacon-%s-%s.png", backend, variant)), shot, 0o644)
					}
				}
				if errors := browser.PageErrors(); len(errors) != 0 {
					t.Fatalf("page errors: %v", errors)
				}
				if backend == "webgpu" && variant == "features" && (raw.TAAActive != 1 || raw.Exposure <= 0) {
					t.Fatalf("WebGPU TAA or auto exposure did not run: %+v console=%s", cost, browser.Console())
				}
			})
		}
	}
	if out := os.Getenv("GOSX_RENDER_OUT"); out != "" {
		data, _ := json.MarshalIndent(results, "", "  ")
		_ = os.MkdirAll(out, 0o755)
		_ = os.WriteFile(filepath.Join(out, "frame-cost.json"), data, 0o644)
	}
}

// TestWebGPUDeviceLossFallsBackWithoutReload destroys the WebGPU device and
// expects the mount to rebuild on WebGL without a page reload and report the
// reset (lost, then restored on WebGL). A page's own device.destroy() stays on
// WebGL by design; the return to WebGPU after a genuine loss is covered by the
// Node mount tests, because no page API produces a genuine GPU loss.
func TestWebGPUDeviceLossFallsBackWithoutReload(t *testing.T) {
	if !e2eWindowsRelay() && os.Getenv("GOSX_WATER_E2E_REQUIRE_WEBGPU") != "1" {
		t.Skip("needs a real WebGPU adapter")
	}
	root := e2eRepoRoot(t)
	yes, no := true, false
	props := scene.Props{
		Width: 320, Height: 240, Background: "#102030", AutoRotate: &no, PreferWebGPU: &yes,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Z: 5}, FOV: 60},
		Graph: scene.NewGraph(
			scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: scene.StandardMaterial{Color: "#ff8040"}},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1, Direction: scene.Vector3{X: -1, Y: -1, Z: -1}},
		),
	}
	url := renderUpgradeServer(t, root, renderUpgradePage(t, "loss-root", 320, 240, props))
	browser := renderUpgradeBrowser(t, "webgpu", 320, 240, `window.__gosxResets=[];document.addEventListener("gosx:scene3d:graphics-reset",e=>window.__gosxResets.push(e.detail),true)`)
	browser.navigate(t, url+"/")
	waitBackend := func(want string, within time.Duration) bool {
		for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
			var active string
			browser.eval(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')||''`, &active)
			if active == want {
				return true
			}
		}
		return false
	}
	if !waitBackend("webgpu", 60*time.Second) {
		t.Fatalf("never mounted on WebGPU; console=%s", browser.Console())
	}
	started := time.Now()
	var destroyed bool
	browser.eval(t, `(() => { const p = window.__gosx_scene3d_webgpu_probe && window.__gosx_scene3d_webgpu_probe(); if (!p || !p.device) return false; p.device.destroy(); return true; })()`, &destroyed)
	if !destroyed {
		t.Fatal("no probed device to destroy")
	}
	fellBack := waitBackend("webgl", 20*time.Second)
	var resets []map[string]any
	browser.eval(t, `window.__gosxResets`, &resets)
	t.Logf("fell back to WebGL: %v after %s; resets=%v", fellBack, time.Since(started).Round(time.Millisecond), resets)
	if !fellBack {
		t.Fatalf("mount did not rebuild on WebGL; resets=%v console=%s", resets, browser.Console())
	}
	// The init script's capture listener sees each mount event exactly once.
	if len(resets) != 2 || resets[0]["phase"] != "lost" || resets[0]["backend"] != "webgpu" || resets[1]["phase"] != "restored" || resets[1]["backend"] != "webgl" {
		t.Fatalf("want one WebGPU loss then one WebGL restore, got %v", resets)
	}
	if errors := browser.PageErrors(); len(errors) != 0 {
		t.Fatalf("page errors: %v", errors)
	}
}
