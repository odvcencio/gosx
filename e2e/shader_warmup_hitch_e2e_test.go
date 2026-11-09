//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

// A lit Selena material in the shape games use: every scene light, both
// shadow slots and fog. The first verb names it; the second takes a per-run constant so the driver's shader
// cache cannot answer for a program this run has not compiled.
const warmupHitchMaterial = `material Hitch%d {
    context {
        cameraPos       : vec3
        cameraForward   : vec3
        ambient         : vec3
        fog             : vec4
        lightCount      : float
        lights          : array<vec4, 32>
        shadow0         : vec4
        shadow1         : vec4
        shadowSplits0   : vec4
        shadowSplits1   : vec4
        shadowMatrices0 : array<vec4, 16>
        shadowMatrices1 : array<vec4, 16>
        shadowMap0      : textureDepth2dArray
        shadowMap1      : textureDepth2dArray
        time            : float
    }
    param albedo : color = rgb(0.7, 0.5, 0.3)
    surface(geo) -> color {
        let n = normalize(geo.worldNormal)
        let v = normalize(cameraPos - geo.worldPos)
        let s0 = mlShadow0(geo.worldPos)
        let s1 = mlShadow1(geo.worldPos)
        let wobble = sin(geo.worldPos.x * %s + time) * 0.5 + 0.5
        var lit = ambient * albedo
        for (var i = 0i; i < 8i; i = i + 1i) {
            let shadow = mlLightShadow(i, s0, s1)
            lit = lit + (albedo * wobble * mlLightDiffuse(i, n, geo.worldPos) + 0.25 * mlLightSpecular(i, n, v, geo.worldPos, 32.0)) * shadow
        }
        return mlFog(lit, geo.worldPos)
    }
}`

type hitchStep struct {
	Label       string  `json:"label"`
	FrameGapMS  float64 `json:"frameGapMS"`
	RAFGapMS    float64 `json:"rafGapMS"`
	FramesAfter int     `json:"framesAfter"`
}

type hitchRun struct {
	Backend       string      `json:"backend"`
	Warmup        bool        `json:"warmup"`
	ToneMapped    bool        `json:"toneMapped"`
	FirstFrameMS  float64     `json:"firstFrameMS"`
	ShadersReady  string      `json:"shadersReady"`
	ShadersTotal  string      `json:"shadersTotal"`
	Steps         []hitchStep `json:"steps"`
	MaxFrameGapMS float64     `json:"maxFrameGapMS"`
}

// TestShaderWarmupRemovesFirstUseHitches mounts a scene with one object, then
// adds six objects one at a time, each with a material no earlier frame used:
// four lit Selena materials and two standard materials with transmission and
// clearcoat. For each addition it records the longest interval between two
// drawn frames (a frame waits while its programs link) and the longest
// requestAnimationFrame interval (a synchronous compile blocks the page).
// It runs each backend three times: without Props.ShaderWarmup, with every
// added material listed in it, and with the warm-up plus a tone-mapping post
// effect from the start, which keeps WebGPU's render target format fixed when
// the first transmission material appears. The numbers are logged and, with
// GOSX_WARMUP_HITCH_OUT, written as JSON. Run on the GPU runners:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestShaderWarmupRemovesFirstUseHitches -timeout 20m ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestShaderWarmupRemovesFirstUseHitches -timeout 20m ./e2e/
func TestShaderWarmupRemovesFirstUseHitches(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	backends := []string{"webgl"}
	// The GPU runners set GPU_CHROME_ENDPOINT (mac-gpu-run) or GPU_CHROME_STATE (gpu-chrome.sh).
	if os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GPU_CHROME_STATE") != "" || os.Getenv("GOSX_WARMUP_HITCH_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	var runs []hitchRun
	for _, backend := range backends {
		for _, mode := range []string{"cold", "warm", "warm-tonemapped"} {
			name := backend + "/" + mode
			warm, post := mode != "cold", mode == "warm-tonemapped"
			t.Run(name, func(t *testing.T) {
				run := runWarmupHitch(t, chrome, bootstrap, root, backend, warm, post)
				runs = append(runs, run)
				for _, s := range run.Steps {
					t.Logf("%s %-12s frame gap %7.1f ms  rAF gap %7.1f ms  (%d frames after)", name, s.Label, s.FrameGapMS, s.RAFGapMS, s.FramesAfter)
				}
				t.Logf("%s first frame %.0f ms after navigation; shaders %s (%s programs); worst frame gap %.1f ms", name, run.FirstFrameMS, run.ShadersReady, run.ShadersTotal, run.MaxFrameGapMS)
			})
		}
	}
	if out := os.Getenv("GOSX_WARMUP_HITCH_OUT"); out != "" {
		data, _ := json.MarshalIndent(runs, "", "  ")
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		_ = os.WriteFile(out, data, 0o644)
	}
}

func runWarmupHitch(t *testing.T, chrome string, bootstrap []byte, root, backend string, warm, post bool) hitchRun {
	t.Helper()
	nonce := fmt.Sprintf("%d.%d", time.Now().UnixNano()%1000, time.Now().Nanosecond()%997+1)
	var added []scene.Material
	for i := 0; i < 4; i++ {
		src := fmt.Sprintf(warmupHitchMaterial, i, nonce+fmt.Sprint(i))
		material, _, err := scene.CompileSelenaMaterial([]byte(src), scene.SelenaMaterialOptions{Modules: []string{"lighting"}})
		if err != nil {
			t.Fatal(err)
		}
		added = append(added, material)
	}
	added = append(added,
		scene.StandardMaterial{Color: "#a0d8ff", Roughness: 0.05, Transmission: 1, Thickness: 0.4, IOR: scene.Float(1.45)},
		scene.StandardMaterial{Color: "#c03030", Roughness: 0.3, Metalness: 0.2, Clearcoat: 1, Sheen: 0.5},
	)
	labels := []string{"selena-0", "selena-1", "selena-2", "selena-3", "transmission", "clearcoat"}

	yes := true
	base := func(count int) scene.Props {
		nodes := []scene.Node{
			scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 12, Height: 0.2, Depth: 12}, Position: scene.Vector3{Y: -1}, Material: scene.StandardMaterial{Color: "#808080"}, ReceiveShadow: true},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.2, Direction: scene.Vector3{X: 0.4, Y: -1, Z: 0.3}, CastShadow: true, ShadowSize: 1024},
			scene.PointLight{ID: "lamp", Color: "#ffd0a0", Intensity: 2, Position: scene.Vector3{X: 2, Y: 2, Z: 1}},
		}
		for i := 0; i < count; i++ {
			nodes = append(nodes, scene.Mesh{ID: fmt.Sprintf("added-%d", i), Geometry: scene.SphereGeometry{Radius: 0.5, Segments: 32},
				Position: scene.Vector3{X: float64(i%3)*1.6 - 1.6, Z: float64(i/3)*1.6 - 0.8}, Material: added[i], CastShadow: true, ReceiveShadow: true})
		}
		props := scene.Props{
			Width: 480, Height: 320, Background: "#101418", AutoRotate: &yes,
			Camera:      scene.PerspectiveCamera{Position: scene.Vector3{Y: 3, Z: 6}, Rotation: scene.Euler{X: -0.45}, FOV: 55, Near: 0.1, Far: 60},
			Environment: scene.Environment{FogColor: "#101418", FogDensity: 0.02},
			Graph:       scene.NewGraph(nodes...),
		}
		if backend == "webgl" {
			props.ForceWebGL, props.RequireWebGL = &yes, &yes
		} else {
			props.PreferWebGPU = &yes
		}
		if post {
			props.PostFX = scene.PostFX{Effects: []scene.PostEffect{scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1}}}
		}
		return props
	}
	initial := base(0)
	if warm {
		warmup := &scene.ShaderWarmup{}
		for _, m := range added {
			warmup.Materials = append(warmup.Materials, scene.WarmupMaterial{Material: m, CastShadow: true})
		}
		initial.ShaderWarmup = warmup
	}
	var steps [][]scene.Command
	for i := range added {
		steps = append(steps, scene.DiffPropsCommands(base(i), base(i+1)))
	}
	stepJSON, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "warmup-hitch", "component": "GoSXScene3D", "kind": "surface", "mountId": "warmup-root", "props": initial,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#warmup-root{width:480px;height:320px}</style></head><body><div id="warmup-root"></div><script id="hitch-steps" type="application/json">%s</script><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, stepJSON, manifest))
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
	// Frame completions come from the renderer's own perf marks; every rAF
	// tick is recorded to find main-thread stalls.
	probe := `window.__gosx_scene3d_perf = true;
window.__hitch = { frames: [], raf: [] };
new PerformanceObserver(list => { for (const e of list.getEntries()) if (e.name === "scene3d-render") window.__hitch.frames.push(e.startTime + e.duration); }).observe({ entryTypes: ["measure"] });
(function loop(t) { window.__hitch.raf.push(t); requestAnimationFrame(loop); })(0);`
	browser := newBrowserPage(t, chrome, flags, 560, 400, probe, 240*time.Second)
	browser.navigate(t, url+"/")

	var run hitchRun
	run.Backend, run.Warmup, run.ToneMapped = backend, warm, post
	var state struct {
		Backend string  `json:"backend"`
		Ready   string  `json:"ready"`
		Shaders string  `json:"shaders"`
		Total   string  `json:"total"`
		First   float64 `json:"first"`
	}
	stateJS := `(() => { const e = document.getElementById('warmup-root'); return { backend: e.getAttribute('data-gosx-scene3d-backend') || '', ready: e.getAttribute('data-gosx-scene3d-ready') || '', shaders: e.getAttribute('data-gosx-scene3d-shaders') || '', total: e.getAttribute('data-gosx-scene3d-shaders-total') || '', first: window.__hitch.frames[0] || 0 }; })()`
	deadline := time.Now().Add(120 * time.Second)
	for ; time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		browser.eval(t, stateJS, &state)
		if state.Backend == backend && state.Ready == "true" && state.First > 0 && (!warm || state.Shaders == "ready") {
			break
		}
	}
	if state.Backend != backend || state.First == 0 {
		t.Fatalf("scene did not draw on %s: %+v; errors=%v console=%s", backend, state, browser.PageErrors(), browser.Console())
	}
	if warm && state.Shaders != "ready" {
		t.Fatalf("warm-up did not finish: %+v", state)
	}
	run.FirstFrameMS, run.ShadersReady, run.ShadersTotal = state.First, state.Shaders, state.Total

	// Let the boot settle, then add one object per step and watch 1.5 s.
	stepJS := `(async (index) => {
  const steps = JSON.parse(document.getElementById('hitch-steps').textContent);
  const mount = document.getElementById('warmup-root');
  const h = window.__hitch;
  const t0 = performance.now();
  await window.__gosx.scene3d.dispatchCommands(mount, steps[index]);
  await new Promise(resolve => setTimeout(resolve, 1500));
  const t1 = performance.now();
  const gap = (list) => { let last = list.filter(x => x <= t0).pop() || t0, worst = 0, n = 0; for (const x of list) { if (x <= t0 || x > t1) continue; worst = Math.max(worst, x - last); last = x; n++; } return { worst: Math.max(worst, n ? 0 : t1 - t0), n }; };
  const frames = gap(h.frames), raf = gap(h.raf);
  h.frames = h.frames.filter(x => x > t1 - 100); h.raf = h.raf.filter(x => x > t1 - 100);
  return { frameGapMS: frames.worst, rafGapMS: raf.worst, framesAfter: frames.n };
})(%d)`
	time.Sleep(1500 * time.Millisecond)
	for i, label := range labels {
		var s hitchStep
		browser.eval(t, fmt.Sprintf(stepJS, i), &s)
		s.Label = label
		run.Steps = append(run.Steps, s)
		run.MaxFrameGapMS = max(run.MaxFrameGapMS, s.FrameGapMS)
		if s.FramesAfter == 0 {
			t.Errorf("%s: no frame drew in 1.5 s after adding %s", backend, label)
		}
	}
	if errs := browser.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v", errs)
	}
	return run
}
