//go:build e2e

// Port of the retired e2e/water_demo_e2e.test.mjs (playwright) to chromedp.
//
// Water release gate. This deliberately does not treat "a canvas exists" or
// parsed SceneIR state as proof of rendering. The runtime must publish the
// common water-renderer contract, advance real presentation/simulation
// counters, produce a non-trivial composited image, react to authored
// controls, and stop work when paused or offscreen. A second test removes GPU
// contexts and verifies that the mount reports an honest unsupported state
// instead of masquerading as generic WebGL.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"
	"time"

	xdraw "golang.org/x/image/draw"
)

func waterBaseURL(t *testing.T) string {
	if url := os.Getenv("GOSX_WATER_E2E_BASE_URL"); url != "" {
		return url
	}
	return fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t))
}

func waterRequireWebGPU() bool {
	return os.Getenv("GOSX_WATER_E2E_REQUIRE_WEBGPU") == "1"
}

func positiveNumberEnv(name string, fallback float64) float64 {
	parsed, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func waterBrowserFlags() map[string]any {
	if waterRequireWebGPU() {
		return map[string]any{"enable-unsafe-webgpu": true}
	}
	return map[string]any{
		"use-angle":                 "swiftshader",
		"enable-unsafe-swiftshader": true,
		"disable-features":          "WebGPU",
	}
}

type waterSnapshot struct {
	Backend           string  `json:"backend"`
	Renderer          string  `json:"renderer"`
	UnsupportedReason string  `json:"unsupportedReason"`
	Systems           string  `json:"systems"`
	ActiveObject      string  `json:"activeObject"`
	PoolShape         string  `json:"poolShape"`
	FrameSeq          float64 `json:"frameSeq"`
	SimulationSeq     float64 `json:"simulationSeq"`
	Loop              string  `json:"loop"`
	LoopReason        string  `json:"loopReason"`
	Lifecycle         string  `json:"lifecycle"`
}

const waterSnapshotJS = `(() => {
  const el = document.querySelector("[data-gosx-scene3d-mounted]");
  const attr = (name) => el?.getAttribute(name) || "";
  const number = (name) => Number(attr(name) || 0);
  return {
    backend: attr("data-gosx-scene3d-backend"),
    renderer: attr("data-gosx-scene3d-water-renderer"),
    unsupportedReason: attr("data-gosx-scene3d-water-unsupported-reason"),
    systems: attr("data-gosx-scene3d-water-state-systems"),
    activeObject: attr("data-gosx-scene3d-water-state-active-object"),
    poolShape: attr("data-gosx-scene3d-water-state-pool-shape"),
    frameSeq: Number(el?.__gosxScene3DWaterFrameSeq || number("data-gosx-scene3d-water-frame-seq")),
    simulationSeq: Number(el?.__gosxScene3DWaterSimulationSeq || number("data-gosx-scene3d-water-simulation-seq")),
    loop: attr("data-gosx-scene3d-render-loop"),
    loopReason: attr("data-gosx-scene3d-render-loop-reason"),
    lifecycle: attr("data-gosx-scene3d-water-lifecycle"),
  };
})()`

func takeWaterSnapshot(t *testing.T, page *browserPage) waterSnapshot {
	t.Helper()
	var snap waterSnapshot
	page.eval(t, waterSnapshotJS, &snap)
	return snap
}

func waterDiagnostics(page *browserPage, app *docsApp, message string, snap waterSnapshot) string {
	blob, _ := json.Marshal(snap)
	return fmt.Sprintf("%s: %s\n\nConsole:\n%s\n\nLogs:\n%s", message, blob, page.Console(), app.logs.String())
}

func waitForWaterAdvance(t *testing.T, page *browserPage, frameSeq, simulationSeq float64) waterSnapshot {
	t.Helper()
	expr := fmt.Sprintf(`(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    const exactFrame = Number(el?.__gosxScene3DWaterFrameSeq || el?.getAttribute("data-gosx-scene3d-water-frame-seq") || 0);
    const exactSimulation = Number(el?.__gosxScene3DWaterSimulationSeq || el?.getAttribute("data-gosx-scene3d-water-simulation-seq") || 0);
    return exactFrame > %g && exactSimulation > %g;
  })()`, frameSeq, simulationSeq)
	page.waitFor(t, expr, 15*time.Second, "water frame/simulation counters to advance")
	return takeWaterSnapshot(t, page)
}

func setControlValue(t *testing.T, page *browserPage, selector, value string) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	page.eval(t, fmt.Sprintf(`(() => {
    const control = document.querySelector(%s);
    if (!control) throw new Error("control not found: " + %s);
    control.value = %s;
    control.dispatchEvent(new Event("change", { bubbles: true }));
  })()`, sel, sel, val), nil)
}

func setRange(t *testing.T, page *browserPage, selector, value string) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	val, _ := json.Marshal(value)
	page.eval(t, fmt.Sprintf(`(() => {
    const input = document.querySelector(%s);
    if (!input) throw new Error("input not found: " + %s);
    input.value = %s;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  })()`, sel, sel, val), nil)
}

func setChecked(t *testing.T, page *browserPage, selector string, checked bool) {
	t.Helper()
	sel, _ := json.Marshal(selector)
	page.eval(t, fmt.Sprintf(`(() => {
    const control = document.querySelector(%s);
    if (!control) throw new Error("control not found: " + %s);
    control.checked = %v;
    control.dispatchEvent(new Event("change", { bubbles: true }));
  })()`, sel, sel, checked), nil)
}

type pixelStats struct {
	Width           int
	Height          int
	QuantizedColors int
	LuminanceRange  float64
	LuminanceStdDev float64
}

// compositedPixelStats screenshots the canvas element and computes the same
// statistics the retired playwright harness computed in-page: downsample to
// at most 160x100, sample every 4th pixel, count 4-bit quantized colors and
// luminance spread.
func compositedPixelStats(t *testing.T, page *browserPage, selector string) pixelStats {
	t.Helper()
	shot := page.screenshotElement(t, selector)
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatalf("decode canvas screenshot: %v", err)
	}
	bounds := img.Bounds()
	sampleWidth := bounds.Dx()
	if sampleWidth > 160 {
		sampleWidth = 160
	}
	sampleHeight := bounds.Dy()
	if sampleHeight > 100 {
		sampleHeight = 100
	}
	scaled := image.NewRGBA(image.Rect(0, 0, sampleWidth, sampleHeight))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, bounds, xdraw.Src, nil)

	colors := map[string]struct{}{}
	var luminances []float64
	pix := scaled.Pix
	for i := 0; i < len(pix); i += 16 {
		r := float64(pix[i])
		g := float64(pix[i+1])
		b := float64(pix[i+2])
		colors[fmt.Sprintf("%d:%d:%d", pix[i]>>4, pix[i+1]>>4, pix[i+2]>>4)] = struct{}{}
		luminances = append(luminances, 0.2126*r+0.7152*g+0.0722*b)
	}
	mean := 0.0
	minL, maxL := math.Inf(1), math.Inf(-1)
	for _, l := range luminances {
		mean += l
		minL = math.Min(minL, l)
		maxL = math.Max(maxL, l)
	}
	mean /= float64(len(luminances))
	variance := 0.0
	for _, l := range luminances {
		variance += (l - mean) * (l - mean)
	}
	variance /= float64(len(luminances))
	return pixelStats{
		Width:           bounds.Dx(),
		Height:          bounds.Dy(),
		QuantizedColors: len(colors),
		LuminanceRange:  maxL - minL,
		LuminanceStdDev: math.Sqrt(variance),
	}
}

type framePerformance struct {
	Samples int     `json:"samples"`
	FPS     float64 `json:"fps"`
	Mean    float64 `json:"mean"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
}

func sampleWaterFramePerformance(t *testing.T, page *browserPage, sampleCount int) framePerformance {
	t.Helper()
	var intervals []float64
	page.eval(t, fmt.Sprintf(`new Promise((resolve) => {
    const samples = [];
    let previous = 0;
    let warmup = 20;
    function frame(now) {
      if (warmup > 0) {
        warmup--;
      } else if (previous > 0) {
        samples.push(now - previous);
      }
      previous = now;
      if (samples.length >= %d) resolve(samples);
      else requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  })`, sampleCount), &intervals)

	sorted := append([]float64(nil), intervals...)
	sort.Float64s(sorted)
	mean := 0.0
	for _, v := range intervals {
		mean += v
	}
	mean /= float64(len(intervals))
	percentile := func(value float64) float64 {
		idx := int(math.Ceil(float64(len(sorted))*value)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return sorted[idx]
	}
	return framePerformance{
		Samples: len(intervals),
		FPS:     1000 / mean,
		Mean:    mean,
		P95:     percentile(0.95),
		P99:     percentile(0.99),
	}
}

var (
	duckAssetRe    = regexp.MustCompile(`(?i)/water/models/duck/Duck(?:\.gltf|0\.bin|CM\.(?:png|jpe?g))`)
	gltfFeatureRe  = regexp.MustCompile(`(?i)bootstrap-feature-scene3d-gltf(?:\.[^.]+)?\.js(?:\?|$)`)
	duckModelReqRe = regexp.MustCompile(`(?i)/water/models/duck/Duck\.gltf(?:\?|$)`)
)

func TestWaterRendererLifecycle(t *testing.T) {
	chrome := e2eChromePath(t)
	requireWebGPU := waterRequireWebGPU()
	hardwareMinFPS := positiveNumberEnv("GOSX_WATER_E2E_MIN_FPS", 50)
	hardwareP95MaxMS := positiveNumberEnv("GOSX_WATER_E2E_P95_MAX_MS", 25)
	hardwareP99MaxMS := positiveNumberEnv("GOSX_WATER_E2E_P99_MAX_MS", 33)

	app := startDocsApp(t, waterBaseURL(t))
	page := newBrowserPage(t, chrome, waterBrowserFlags(), 1280, 800, "", 180*time.Second)

	if status := page.navigate(t, app.baseURL+"/demos/water"); status < 200 || status > 299 {
		t.Fatalf("/demos/water returned %d\n\nLogs:\n%s", status, app.logs.String())
	}

	page.waitFor(t, `!!document.querySelector("[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer]")`,
		45*time.Second, "[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer]")
	canvasSelector := "canvas[data-gosx-scene3d-canvas]"
	page.waitFor(t, `(() => {
    const canvas = document.querySelector("canvas[data-gosx-scene3d-canvas]");
    if (!canvas) return false;
    const rect = canvas.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  })()`, 30*time.Second, "visible "+canvasSelector)

	initial := takeWaterSnapshot(t, page)
	if initial.Renderer != "active" {
		t.Fatal(waterDiagnostics(page, app, "water renderer did not become active", initial))
	}
	if !containsString([]string{"webgl", "webgpu"}, initial.Backend) {
		t.Fatal(waterDiagnostics(page, app, "water selected no real GPU backend", initial))
	}
	if requireWebGPU {
		if initial.Backend != "webgpu" {
			t.Fatal(waterDiagnostics(page, app, "hardware certification requires WebGPU", initial))
		}
	} else if initial.Backend != "webgl" {
		t.Fatal(waterDiagnostics(page, app, "generic CI must exercise forced software WebGL", initial))
	}
	if initial.Systems != "1" {
		t.Fatalf("expected systems=1, got %q", initial.Systems)
	}
	if initial.ActiveObject != "Sphere" {
		t.Fatalf("expected activeObject=Sphere, got %q", initial.ActiveObject)
	}
	if initial.PoolShape != "Box" {
		t.Fatalf("expected poolShape=Box, got %q", initial.PoolShape)
	}

	advanced := waitForWaterAdvance(t, page, initial.FrameSeq, initial.SimulationSeq)
	if advanced.FrameSeq <= initial.FrameSeq {
		t.Fatal(waterDiagnostics(page, app, "water presentation counter did not advance", advanced))
	}
	if advanced.SimulationSeq <= initial.SimulationSeq {
		t.Fatal(waterDiagnostics(page, app, "water simulation counter did not advance", advanced))
	}

	pixels := compositedPixelStats(t, page, canvasSelector)
	if pixels.QuantizedColors < 24 {
		t.Fatalf("water canvas is visually flat (%+v)", pixels)
	}
	if pixels.LuminanceRange < 35 {
		t.Fatalf("water canvas lacks visible tonal structure (%+v)", pixels)
	}
	if pixels.LuminanceStdDev < 10 {
		t.Fatalf("water canvas resembles a blank/gradient fallback (%+v)", pixels)
	}

	if requireWebGPU {
		perf := sampleWaterFramePerformance(t, page, 150)
		if perf.FPS < hardwareMinFPS {
			t.Fatalf("hardware WebGPU FPS %.2f is below %.0f: %+v", perf.FPS, hardwareMinFPS, perf)
		}
		if perf.P95 > hardwareP95MaxMS {
			t.Fatalf("hardware WebGPU p95 %.2fms exceeds %.0fms: %+v", perf.P95, hardwareP95MaxMS, perf)
		}
		if perf.P99 > hardwareP99MaxMS {
			t.Fatalf("hardware WebGPU p99 %.2fms exceeds %.0fms: %+v", perf.P99, hardwareP99MaxMS, perf)
		}
	}

	var options []string
	page.eval(t, `[...document.querySelectorAll('form[data-gosx-scene3d-controls] select[name="object"] option')].map((option) => option.value)`, &options)
	wantOptions := []string{"None", "Sphere", "Cube", "TorusKnot", "Rubber Duck"}
	if len(options) != len(wantOptions) {
		t.Fatalf("object select options = %v, want %v", options, wantOptions)
	}
	for i := range wantOptions {
		if options[i] != wantOptions[i] {
			t.Fatalf("object select options = %v, want %v", options, wantOptions)
		}
	}

	waitActiveObject := func(name string) {
		page.waitFor(t, fmt.Sprintf(
			`document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-state-active-object") === %q`, name),
			15*time.Second, "active object "+name)
	}

	setControlValue(t, page, `select[name="object"]`, "Cube")
	waitActiveObject("Cube")

	isDuckAsset := func(url string) bool { return duckAssetRe.MatchString(url) }
	isGLTFFeature := func(url string) bool { return gltfFeatureRe.MatchString(url) }
	if page.anyRequest(isDuckAsset) {
		t.Fatal("Duck assets loaded before Duck selection")
	}
	if page.anyRequest(isGLTFFeature) {
		t.Fatal("glTF feature chunk loaded before Duck selection")
	}

	setControlValue(t, page, `select[name="object"]`, "TorusKnot")
	waitActiveObject("TorusKnot")
	if page.anyRequest(isDuckAsset) {
		t.Fatal("TorusKnot selection loaded Duck assets")
	}
	if page.anyRequest(isGLTFFeature) {
		t.Fatal("TorusKnot selection loaded the glTF feature chunk")
	}

	setControlValue(t, page, `select[name="object"]`, "Rubber Duck")
	waitActiveObject("Rubber Duck")
	waitForRequests := func(what string, match func(string) bool) {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if page.anyRequest(match) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("Duck selection did not request %s\n\nConsole:\n%s", what, page.Console())
	}
	waitForRequests("its glTF assets", func(url string) bool { return duckModelReqRe.MatchString(url) })
	waitForRequests("the glTF feature chunk", isGLTFFeature)

	setControlValue(t, page, `select[name="object"]`, "Sphere")
	waitActiveObject("Sphere")

	setControlValue(t, page, `select[name="poolShape"]`, "Rounded Box")
	setRange(t, page, `input[name="poolWidth"]`, "1.5")
	page.waitFor(t, `(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    return el?.getAttribute("data-gosx-scene3d-water-state-pool-shape") === "Rounded Box" &&
      Number(el?.getAttribute("data-gosx-scene3d-water-state-pool-width")) === 1.5;
  })()`, 15*time.Second, "pool shape/width to update")

	setChecked(t, page, `input[name="paused"]`, true)
	page.waitFor(t, `document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-paused") === "true"`,
		15*time.Second, "paused attribute")
	time.Sleep(250 * time.Millisecond)
	pauseStart := takeWaterSnapshot(t, page)
	time.Sleep(750 * time.Millisecond)
	pauseEnd := takeWaterSnapshot(t, page)
	if pauseEnd.SimulationSeq != pauseStart.SimulationSeq {
		t.Fatal(waterDiagnostics(page, app, "paused water continued simulating", pauseEnd))
	}

	setChecked(t, page, `input[name="paused"]`, false)
	resumed := waitForWaterAdvance(t, page, pauseEnd.FrameSeq, pauseEnd.SimulationSeq)

	// Force the observed mount outside layout. The mount-owned scheduler must
	// stop submitting water work and recover after visibility returns.
	page.eval(t, `(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    if (el) el.style.display = "none";
  })()`, nil)
	page.waitFor(t, `document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-lifecycle") === "offscreen"`,
		15*time.Second, "offscreen lifecycle")
	time.Sleep(250 * time.Millisecond)
	hiddenStart := takeWaterSnapshot(t, page)
	time.Sleep(750 * time.Millisecond)
	hiddenEnd := takeWaterSnapshot(t, page)
	if hiddenEnd.FrameSeq != hiddenStart.FrameSeq {
		t.Fatal(waterDiagnostics(page, app, "offscreen water continued presenting", hiddenEnd))
	}
	if hiddenEnd.SimulationSeq != hiddenStart.SimulationSeq {
		t.Fatal(waterDiagnostics(page, app, "offscreen water continued simulating", hiddenEnd))
	}

	page.eval(t, `(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    if (el) el.style.display = "";
  })()`, nil)
	waitForWaterAdvance(t, page, hiddenEnd.FrameSeq, hiddenEnd.SimulationSeq)
	_ = resumed

	if errs := page.PageErrors(); len(errs) > 0 {
		t.Fatalf("page errors:\n%s\n\n%s", joinLines(errs), page.Console())
	}
}

func TestWaterUnsupportedHonesty(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, waterBaseURL(t))
	initScript := `
    const original = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (kind, ...args) {
      if (kind === "webgl" || kind === "experimental-webgl" || kind === "webgl2" || kind === "webgpu") return null;
      return original.call(this, kind, ...args);
    };
    try { Object.defineProperty(navigator, "gpu", { configurable: true, value: undefined }); } catch {}
  `
	page := newBrowserPage(t, chrome, waterBrowserFlags(), 800, 600, initScript, 60*time.Second)

	if status := page.navigate(t, app.baseURL+"/demos/water"); status < 200 || status > 299 {
		t.Fatalf("/demos/water returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t, `!!document.querySelector('[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer="unsupported"]')`,
		30*time.Second, `water renderer "unsupported" state`)

	state := takeWaterSnapshot(t, page)
	if state.Renderer != "unsupported" {
		t.Fatal(waterDiagnostics(page, app, "expected unsupported renderer state", state))
	}
	if state.UnsupportedReason == "" {
		t.Fatal(waterDiagnostics(page, app, "unsupported water state omitted its reason", state))
	}
	if state.Backend == "webgl" {
		t.Fatal(waterDiagnostics(page, app, "generic WebGL masqueraded as a working water renderer", state))
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
