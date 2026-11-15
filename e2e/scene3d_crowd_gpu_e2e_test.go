//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
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

// crowdTestBowl is a generic two-tier stadium bowl of about 30,000 fans:
// a 24-section lower tier and a 20-section upper tier with one open end. Half
// the lower tier wears red and white, the rest blue and gold, so the frame
// shows both palettes.
func crowdTestBowl() scene.Crowd {
	red := []scene.CrowdColor{{Color: "#d01818", Weight: 3}, {Color: "#f0f0f0", Weight: 1}}
	blue := []scene.CrowdColor{{Color: "#1838d0", Weight: 3}, {Color: "#f0c020", Weight: 1}}
	lower := make([]scene.CrowdSection, 24)
	for i := range lower {
		lower[i].Colors = red
		if i >= 12 {
			lower[i].Colors = blue
		}
	}
	upper := make([]scene.CrowdSection, 20)
	for i := range upper {
		upper[i].Colors = blue
	}
	return scene.Crowd{
		ID:    "bowl",
		Seed:  7,
		Props: scene.CrowdPropMix{Lights: 0.04, Scarves: 0.06, Towels: 0.04, Banners: 0.02},
		Stands: []scene.CrowdStand{
			{ID: "lower", Shape: scene.CrowdShapeBowl, HalfWidth: 60, HalfDepth: 42, CornerRadius: 26, Rows: 21, Sections: lower},
			{ID: "upper", Shape: scene.CrowdShapeBowl, HalfWidth: 84, HalfDepth: 66, CornerRadius: 40, Rows: 14, FrontHeight: 14, LoopStart: 0.05, LoopEnd: 0.95, Sections: upper},
		},
	}
}

func crowdStadiumProps(backend string, withCrowd bool) scene.Props {
	yes, no := true, false
	props := scene.Props{
		Width: 1280, Height: 720, Background: "#0b1020", AutoRotate: &no,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{X: 0, Y: 46, Z: 118}, Rotation: scene.Euler{X: -0.34}, FOV: 50, Near: 0.5, Far: 600},
		Graph: scene.NewGraph(
			scene.AmbientLight{Color: "#ffffff", Intensity: 0.45},
			scene.DirectionalLight{Color: "#fff4e0", Intensity: 1.6, Direction: scene.Vector3{X: -0.4, Y: -1, Z: -0.3}},
			scene.Mesh{ID: "field", Geometry: scene.BoxGeometry{Width: 110, Height: 0.2, Depth: 74}, Material: scene.StandardMaterial{Color: "#2f7d32", Roughness: 0.9}},
		),
	}
	if withCrowd {
		props.Environment.Crowds = []scene.Crowd{crowdTestBowl()}
	}
	if backend == "webgl" {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	} else {
		props.PreferWebGPU = &yes
	}
	return props
}

type crowdFrameTimes struct {
	Backend   string    `json:"backend"`
	Crowd     string    `json:"crowd"`
	Fans      string    `json:"fans"`
	Tiers     string    `json:"tiers"`
	Draws     string    `json:"draws"`
	Error     string    `json:"error"`
	Frames    int       `json:"frames"`
	Mean      float64   `json:"mean"`
	P50       float64   `json:"p50"`
	P95       float64   `json:"p95"`
	P99       float64   `json:"p99"`
	CrowdCPU  float64   `json:"crowdCPU"`
	SubmitCPU float64   `json:"submitCPU"`
	GPU       []float64 `json:"gpu"`
	GPUSource string    `json:"gpuSource"`
	GPUStatus string    `json:"gpuStatus"`
	Adapter   string    `json:"adapter"`
	WaveMoved bool      `json:"waveMoved"`
}

// crowdMeasureJS records requestAnimationFrame intervals for ms
// milliseconds, the crowd pass's main-thread time, the renderer's submit time
// and, when the device offers timestamp queries, its GPU frame time (sampled
// every tenth frame so the debug snapshot does not distort the intervals).
const crowdMeasureJS = `(async (ms) => {
  const mount = document.getElementById("crowd-root");
  const api = window.__gosx_scene3d_crowd_api;
  const deltas = [], crowdCPU = [], submit = [], gpu = [];
  let last = performance.now(), frame = 0, source = "", status = "";
  await new Promise((resolve) => {
    const end = last + ms;
    function tick(now) {
      deltas.push(now - last); last = now; frame++;
      if (api && api.stats) crowdCPU.push(api.stats.cpuMs);
      if (frame % 10 === 0 && window.__gosx_scene3d_debug) {
        try {
          const t = window.__gosx_scene3d_debug.inspect("crowd-root").frameTiming || {};
          source = t.source || source; status = t.status || status;
          if (typeof t.gpuMS === "number") gpu.push(t.gpuMS);
          if (typeof t.cpuSubmitMS === "number") submit.push(t.cpuSubmitMS);
        } catch (_) {}
      }
      if (now < end) requestAnimationFrame(tick); else resolve();
    }
    requestAnimationFrame(tick);
  });
  deltas.shift();
  const sorted = deltas.slice().sort((a, b) => a - b);
  const pick = (q) => sorted[Math.min(sorted.length - 1, Math.floor(q * sorted.length))];
  const mean = (list) => list.length ? list.reduce((a, b) => a + b, 0) / list.length : 0;
  return {
    backend: mount.getAttribute("data-gosx-scene3d-backend") || "",
    crowd: mount.getAttribute("data-gosx-scene3d-crowd") || "",
    fans: mount.getAttribute("data-gosx-scene3d-crowd-fans") || "",
    tiers: mount.getAttribute("data-gosx-scene3d-crowd-tiers") || "",
    draws: mount.getAttribute("data-gosx-scene3d-crowd-draws") || "",
    error: mount.getAttribute("data-gosx-scene3d-crowd-error") || "",
    frames: deltas.length, mean: mean(deltas), p50: pick(0.5), p95: pick(0.95), p99: pick(0.99),
    crowdCPU: mean(crowdCPU), submitCPU: mean(submit), gpu, gpuSource: source, gpuStatus: status,
    adapter: mount.getAttribute("data-gosx-scene3d-render-gpu") || "",
  };
})`

func crowdServe(t *testing.T, root string, bootstrap []byte, props scene.Props) string {
	t.Helper()
	propsJSON, err := json.Marshal(props)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "crowd", "component": "GoSXScene3D", "kind": "surface", "mountId": "crowd-root", "props": json.RawMessage(propsJSON),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#crowd-root{width:1280px;height:720px}</style></head><body><div id="crowd-root"></div><script id="gosx-manifest" type="application/json">%s</script><script data-gosx-script="bootstrap"%s src="/bootstrap.js"></script></body></html>`, manifest, scene3DChunkAttrs(t, propsJSON))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/bootstrap.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bootstrap)
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(page))
		case strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js"):
			http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	if e2eWindowsRelay() {
		return e2eExposeToWindows(t, server.URL)
	}
	return server.URL
}

// crowdShot captures the canvas as an image.
func crowdShot(t *testing.T, browser *browserPage) image.Image {
	t.Helper()
	var shot []byte
	if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// crowdPixels counts red-dominant and blue-dominant pixels.
func crowdPixels(img image.Image) (red, blue int) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			switch {
			case r > 12000 && r > 2*g && r > 2*bl:
				red++
			case bl > 12000 && bl > 2*g && bl > 2*r:
				blue++
			}
		}
	}
	return red, blue
}

// crowdChangeCentroid returns the mean x of pixels in the top half of the
// frame (the far stand) that differ strongly between a and b, and how many
// there are.
func crowdChangeCentroid(a, b image.Image) (float64, int) {
	bounds := a.Bounds()
	sum, n := 0.0, 0
	for y := bounds.Min.Y; y < bounds.Min.Y+bounds.Dy()/2; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r0, g0, b0, _ := a.At(x, y).RGBA()
			r1, g1, b1, _ := b.At(x, y).RGBA()
			d := absDiff(r0, r1) + absDiff(g0, g1) + absDiff(b0, b1)
			if d > 40000 {
				sum += float64(x)
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// crowdDispatch sends a director frame to the page.
func crowdDispatch(t *testing.T, browser *browserPage, frame scene.CrowdFrame) {
	t.Helper()
	data, err := scene.EncodeCrowdFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	browser.eval(t, `window.__gosx.scene3d.dispatchCrowdFrame("crowd-root", Uint8Array.from(atob("`+base64.StdEncoding.EncodeToString(data)+`"), (c) => c.charCodeAt(0)))`, &ok)
}

type crowdView struct {
	name   string
	camera scene.PerspectiveCamera
}

// The broadcast view looks across the field at the far stand from high on
// the near side; the close view stands on the field 12 m from the near
// stand and faces it, so its front rows draw the full mesh.
var crowdViews = []crowdView{
	{"broadcast", scene.PerspectiveCamera{Position: scene.Vector3{Y: 46, Z: 118}, Rotation: scene.Euler{X: -0.34}, FOV: 50, Near: 0.5, Far: 600}},
	{"close", scene.PerspectiveCamera{Position: scene.Vector3{Y: 5, Z: 30}, Rotation: scene.Euler{X: 0.18, Y: math.Pi}, FOV: 60, Near: 0.3, Far: 600}},
}

// TestCrowdStadiumOnTheGPU renders a 29,760-seat bowl through WebGPU and
// WebGL2 on a real GPU while a CrowdDirector drives it: cheering, a
// synchronized clap, raised props and a travelling wave. It checks that fans
// of both palettes draw at three levels of detail, that the wave moves round
// the bowl, and records frame times with and without the crowd from a
// broadcast view and a close view. GOSX_CROWD_E2E_REPORT names a JSON file
// for the measurements.
//
//	gpu-chrome.sh run -- go test -tags e2e -run CrowdStadium -timeout 20m ./e2e
//	mac-gpu-run <worktree> go test -tags e2e -run CrowdStadium -timeout 20m ./e2e
func TestCrowdStadiumOnTheGPU(t *testing.T) {
	if !gpuRunnerAvailable() {
		t.Skip("needs a real GPU: use gpu-chrome.sh run -- or mac-gpu-run")
	}
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]crowdFrameTimes{}
	for _, backend := range []string{"webgpu", "webgl"} {
		for _, view := range crowdViews {
			for _, withCrowd := range []bool{true, false} {
				label := backend + "/" + view.name + map[bool]string{true: "+crowd", false: "-empty"}[withCrowd]
				t.Run(label, func(t *testing.T) {
					props := crowdStadiumProps(backend, withCrowd)
					props.Camera = view.camera
					url := crowdServe(t, root, bootstrap, props)
					browser := newBrowserPage(t, chrome, map[string]any{"enable-unsafe-webgpu": true}, 1300, 760, ``, 240*time.Second)
					browser.navigate(t, url+"/")
					ready := `(() => { const m = document.getElementById("crowd-root"); return !!m && m.getAttribute("data-gosx-scene3d-backend") === "` + backend + `"` +
						map[bool]string{true: ` && m.getAttribute("data-gosx-scene3d-crowd") === "ready"`, false: ``}[withCrowd] + ` })()`
					if !browser.pollFor(ready, 90*time.Second) {
						var attrs map[string]string
						browser.eval(t, `(() => { const m = document.getElementById("crowd-root"); const o = {}; for (const a of m.getAttributeNames()) if (/crowd|backend|webgpu-last-error|fallback|failure/.test(a)) o[a] = m.getAttribute(a); return o; })()`, &attrs)
						t.Fatalf("%s did not become ready: %v\nConsole:\n%s", label, attrs, browser.Console())
					}
					time.Sleep(1500 * time.Millisecond) // pipelines and first uploads settle
					var wave crowdWaveCheck
					if withCrowd {
						wave = crowdDriveAndWatch(t, browser, view.name == "broadcast")
					}
					var times crowdFrameTimes
					browser.eval(t, crowdMeasureJS+`(5000)`, &times)
					times.WaveMoved = wave.moved
					report[label] = times
					t.Logf("%s: %+v", label, times)
					if errs := browser.PageErrors(); len(errs) != 0 {
						t.Fatalf("page errors: %v", errs)
					}
					if !withCrowd {
						return
					}
					if times.Error != "" || times.Fans != "29760" {
						t.Fatalf("crowd state %q fans %q error %q", times.Crowd, times.Fans, times.Error)
					}
					var full, low, impostor int
					if _, err := fmt.Sscanf(times.Tiers, "%d/%d/%d", &full, &low, &impostor); err != nil {
						t.Fatalf("tiers %q", times.Tiers)
					}
					if view.name == "broadcast" && (impostor == 0 || full+low == 0) {
						t.Fatalf("the broadcast view should mix detail levels, tiers %q", times.Tiers)
					}
					if view.name == "close" && full == 0 {
						t.Fatalf("the close view should draw full-detail fans, tiers %q", times.Tiers)
					}
					red, blue := crowdPixels(crowdShot(t, browser))
					if red < 2000 || blue < 2000 {
						t.Fatalf("expected both palettes on screen: red %d blue %d pixels", red, blue)
					}
					t.Logf("%s pixels: red %d blue %d; wave %+v", label, red, blue, wave)
				})
			}
		}
	}
	if out := os.Getenv("GOSX_CROWD_E2E_REPORT"); out != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(out, data, 0o644)
	}
}

type crowdWaveCheck struct {
	early, late   float64
	earlyN, lateN int
	moved         bool
}

// crowdDriveAndWatch puts a third of the lower tier on its feet cheering,
// a block on a shared 120 bpm clap, raises the props, and sends a wave from
// azimuth 0.45 at 0.1 turns per second. On the broadcast view it checks that
// the wave's changes move left to right across the far stand.
func crowdDriveAndWatch(t *testing.T, browser *browserPage, watch bool) crowdWaveCheck {
	t.Helper()
	director, err := scene.NewCrowdDirector(crowdTestBowl())
	if err != nil {
		t.Fatal(err)
	}
	var check crowdWaveCheck
	var before image.Image
	if watch {
		before = crowdShot(t, browser)
	}
	start := time.Now()
	now := func() float64 { return time.Since(start).Seconds() }
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(director.SetExcitement(now(), 1, 0.5, "lower-0", "lower-1", "lower-2", "lower-3", "lower-4", "lower-5", "lower-6", "lower-7"))
	must(director.SetAction(now(), scene.CrowdCheer, "lower-0", "lower-1", "lower-2", "lower-3"))
	must(director.Sync(now(), scene.CrowdClap, 120, "lower-4", "lower-5", "lower-6", "lower-7"))
	must(director.RaiseProps(now(), true))
	director.StartWave(now()+0.1, scene.CrowdWave{Origin: 0.45, Speed: 0.1, Laps: 3})
	crowdDispatch(t, browser, director.Frame(now()))
	if !watch {
		time.Sleep(1500 * time.Millisecond)
		return check
	}
	time.Sleep(1700 * time.Millisecond)
	early := crowdShot(t, browser)
	time.Sleep(2000 * time.Millisecond)
	late := crowdShot(t, browser)
	check.early, check.earlyN = crowdChangeCentroid(before, early)
	check.late, check.lateN = crowdChangeCentroid(before, late)
	check.moved = check.earlyN > 300 && check.lateN > 300 && check.late > check.early+60
	if !check.moved {
		t.Fatalf("the wave did not travel left to right across the far stand: %+v", check)
	}
	return check
}
