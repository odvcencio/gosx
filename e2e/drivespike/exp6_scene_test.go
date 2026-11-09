//go:build e2e && drivespike

package drivespike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

// scenePage mounts one GoSXScene3D through the bootstrap manifest, as
// e2e/render_upgrades_e2e_test.go does, with a three-rung quality ladder that
// starts on the middle rung, so the governor can move either way.
func scenePage(t *testing.T, root string) []byte {
	yes, no := true, false
	props := scene.Props{
		Width: 480, Height: 270, Background: "#102030", AutoRotate: &no,
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 1.5, 6), FOV: 60, Near: 0.1, Far: 50},
		Graph: scene.NewGraph(
			scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 8, Height: 0.1, Depth: 8}, Position: scene.Vec3(0, -0.6, 0), Material: scene.StandardMaterial{Color: "#c0c0c0", Roughness: 1}},
			scene.Mesh{ID: "box-0", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: scene.Vec3(-1.2, 0, 0), Material: scene.StandardMaterial{Color: "#e07040"}},
			scene.Mesh{ID: "box-1", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: scene.Vec3(1.2, 0, 0), Material: scene.StandardMaterial{Color: "#40a0e0"}},
			scene.DirectionalLight{ID: "sun", Color: "#ffffff", Intensity: 1.5, Direction: scene.Vector3{X: 1, Y: -1, Z: -0.5}},
		),
		QualityLadder:    []scene.QualityRung{{Name: "low"}, {Name: "mid"}, {Name: "high"}},
		QualityStartRung: 1,
	}
	if gpuRunner() {
		props.PreferWebGPU = &yes
	} else {
		props.ForceWebGL, props.RequireWebGL = &yes, &yes
	}
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "arena-engine", "component": "GoSXScene3D", "kind": "surface", "mountId": "arena", "props": props,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "client", "js", "bootstrap-feature-scene3d-*.js"))
	var attrs strings.Builder
	for _, file := range files {
		name := filepath.Base(file)
		kind := strings.TrimSuffix(strings.TrimPrefix(name, "bootstrap-feature-scene3d-"), ".js")
		fmt.Fprintf(&attrs, ` data-gosx-scene3d-%s-url="/gosx/%s"`, kind, name)
	}
	return []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#arena{width:480px;height:270px}</style></head><body><div id="arena"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js" data-gosx-script="bootstrap"%s></script></body></html>`, manifest, attrs.String()))
}

func sceneServer(t *testing.T) string {
	root := repoRoot(t)
	page := scenePage(t, root)
	bundles := filepath.Join(root, "client", "js")
	return serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case r.URL.Path == "/bootstrap.js" || strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-feature-") && strings.HasSuffix(r.URL.Path, ".js"):
			w.Header().Set("Content-Type", "application/javascript")
			http.ServeFile(w, r, filepath.Join(bundles, filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
}

const sceneStepExpr = `__drive.frames(1).then(r => { const m = document.getElementById('arena');
  return { r, hooks: window.hookCalls.length, rung: m.getAttribute('data-gosx-scene3d-quality-rung'), rungRev: m.getAttribute('data-gosx-scene3d-quality-rung-revision') }; })`

type sceneStep struct {
	R       stepResult
	Hooks   int
	Rung    string
	RungRev string
}

type sceneRunResult struct {
	Backend      string
	BootFrames   int
	BootWall     time.Duration
	Hooks        int
	Deltas       []float64
	Steps        []sceneStep
	StepWallMS   []float64
	RungChanges  []string
	Bundle       map[string]float64
	BundleBefore map[string]float64
	AttrHash     string
	Attrs        map[string]string
	HookHash     string
	Err          string
}

// sceneRun boots the Scene3D page under the probe, installs a frame hook,
// steps n frames one by one, and returns what the frame owner saw.
func sceneRun(t *testing.T, b *spikeBrowser, url string, n int) sceneRunResult {
	var out sceneRunResult
	tb := b.newTab(t, 5*time.Minute)
	defer tb.cancel()
	tb.addProbe(map[string]any{"wall": 1.7e12})
	tb.navigate(url + "/")
	start := time.Now()
	ready := false
	for out.BootFrames < 3000 && time.Since(start) < 2*time.Minute {
		tb.waitNetIdle(5 * time.Second)
		tb.frames(1)
		out.BootFrames++
		var ok bool
		tb.eval(`(() => { const m = document.getElementById('arena'); return !!(m && m.__gosxScene3DHandle && m.getAttribute('data-gosx-scene3d-ready') === 'true'); })()`, &ok)
		if ok {
			ready = true
			break
		}
	}
	out.BootWall = time.Since(start)
	if !ready {
		out.Err = "scene never became ready\n" + tb.Console()
		return out
	}
	tb.eval(`document.getElementById('arena').getAttribute('data-gosx-scene3d-backend')`, &out.Backend)
	// Let the mount settle a few frames, then install the frame hook.
	for i := 0; i < 10; i++ {
		tb.frames(1)
	}
	tb.eval(`(() => { window.hookCalls = []; const h = document.getElementById('arena').__gosxScene3DHandle;
	  window.__unhook = h.setFrameHook((now, delta, released) => { if (!released) hookCalls.push([now, delta]); return 0; }); return true; })()`, nil)
	tb.frames(1) // the first hook frame has delta 0
	var hooks0 int
	tb.eval(`hookCalls.length`, &hooks0)
	tb.eval(`document.getElementById('arena').__gosxScene3DHandle.getTelemetry().bundle`, &out.BundleBefore)
	lastRung := ""
	for i := 0; i < n; i++ {
		st := time.Now()
		var s sceneStep
		tb.eval(sceneStepExpr, &s)
		out.StepWallMS = append(out.StepWallMS, float64(time.Since(st).Microseconds())/1000)
		out.Steps = append(out.Steps, s)
		if i == 0 {
			lastRung = s.Rung
		}
		if s.Rung != lastRung {
			out.RungChanges = append(out.RungChanges, fmt.Sprintf("frame %d: %s -> %s", i, lastRung, s.Rung))
			lastRung = s.Rung
		}
	}
	var calls [][2]float64
	tb.eval(`hookCalls`, &calls)
	out.Hooks = len(calls) - hooks0
	for _, c := range calls[hooks0:] {
		out.Deltas = append(out.Deltas, c[1])
	}
	tb.eval(`document.getElementById('arena').__gosxScene3DHandle.getTelemetry().bundle`, &out.Bundle)
	var attrs map[string]string
	tb.eval(`Object.fromEntries(Array.from(document.getElementById('arena').attributes).filter(a => a.name.startsWith('data-gosx-scene3d-')).map(a => [a.name, a.value]))`, &attrs)
	out.Attrs = attrs
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + attrs[k] + ";")
	}
	out.AttrHash = hashString(sb.String())
	out.HookHash = hashString(jsonString(calls))
	return out
}

func pct(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(p * float64(len(s)-1))
	return s[i]
}

// Experiment 6: step N frames on a Scene3D page.
func TestExp6Scene(t *testing.T) {
	res := newResults(t, "6")
	defer res.summary()
	url := sceneServer(t)
	b := startBrowser(t, cpuGLFlags...)
	n := runsFromEnv(600)
	out := sceneRun(t, b, url, n)
	if out.Err != "" {
		res.check("scene boots under the probe", false, "%s", out.Err)
		return
	}
	res.check("scene boots under the probe", true, "backend=%s bootFrames=%d bootWall=%s", out.Backend, out.BootFrames, out.BootWall.Round(time.Millisecond))
	res.check("frame owner ran exactly N frames", out.Hooks == n, "N=%d hookFrames=%d", n, out.Hooks)
	exact := true
	for _, d := range out.Deltas {
		if !near(d, interval, 0.0011) {
			exact = false
		}
	}
	res.check("every hook delta is one frame interval of virtual time", exact, "deltas min..max %v..%v", pct(out.Deltas, 0), pct(out.Deltas, 1))
	waited, timeouts, submits := 0, 0, 0
	var realMS []float64
	for _, s := range out.Steps {
		if s.R.RealFrames == 1 {
			waited++
		}
		timeouts += s.R.PresentTimeouts
		submits += s.R.GPUSubmits
		realMS = append(realMS, s.R.RealMS)
	}
	res.check("each step waited for one real frame", waited == n && timeouts == 0, "stepsWithOneRealFrame=%d/%d presentTimeouts=%d gpuSubmits=%d", waited, n, timeouts, submits)
	t.Logf("RESULT exp=6 browser=%s check=%q INFO step cost in page p50=%.2fms p95=%.2fms max=%.2fms; per CDP round trip p50=%.2fms p95=%.2fms; 600 frames total %.1fs",
		browserLabel(), "real cost per step", pct(realMS, 0.5), pct(realMS, 0.95), pct(realMS, 1), pct(out.StepWallMS, 0.5), pct(out.StepWallMS, 0.95), sum(out.StepWallMS)/1000)
	db := out.Bundle["builds"] + out.Bundle["reuses"] - out.BundleBefore["builds"] - out.BundleBefore["reuses"]
	t.Logf("bundle telemetry: before=%v after=%v (builds+reuses delta %v)", out.BundleBefore, out.Bundle, db)
	res.check("governor held its rung for the stepped frames", len(out.RungChanges) == 0, "changes=%v start=%s end=%s", out.RungChanges, firstRung(out), lastRung(out))
	t.Logf("mount attrs: %s", jsonString(out.Attrs))
}

func firstRung(r sceneRunResult) string {
	if len(r.Steps) == 0 {
		return ""
	}
	return r.Steps[0].Rung
}

func lastRung(r sceneRunResult) string {
	if len(r.Steps) == 0 {
		return ""
	}
	return r.Steps[len(r.Steps)-1].Rung
}

func sum(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s
}

// Experiment 8: repeatability. 20 scripted runs each of the watchdog page
// (both scripts) and the Scene3D page; count distinct final hashes.
func TestExp8Repeat(t *testing.T) {
	res := newResults(t, "8")
	defer res.summary()
	runs := 20
	srv := startWatchdogServer(t)
	b := startBrowser(t, cpuGLFlags...)
	for _, kind := range []string{"ready", "stall"} {
		hashes := map[string]int{}
		for i := 0; i < runs; i++ {
			if kind == "ready" {
				hashes[watchdogReady(t, res, b, srv, 50*time.Millisecond)]++
			} else {
				hashes[watchdogStall(t, res, b, srv, 50*time.Millisecond)]++
			}
		}
		res.check("watchdog "+kind+": one final DOM hash over 20 runs", len(hashes) == 1, "distinct=%d %v", len(hashes), hashes)
	}
	url := sceneServer(t)
	frames := 600
	attrHashes, hookHashes := map[string]int{}, map[string]int{}
	var all []map[string]string
	for i := 0; i < runs; i++ {
		out := sceneRun(t, b, url, frames)
		if out.Err != "" {
			res.check("scene run boots", false, "run %d: %s", i, out.Err)
			continue
		}
		attrHashes[out.AttrHash]++
		hookHashes[out.HookHash]++
		all = append(all, out.Attrs)
	}
	varying := map[string][]string{}
	for k := range firstMap(all) {
		seen := map[string]bool{}
		for _, a := range all {
			seen[a[k]] = true
		}
		if len(seen) > 1 {
			for v := range seen {
				varying[k] = append(varying[k], v)
			}
		}
	}
	res.check("scene: one mount-attribute hash over 20 runs", len(attrHashes) == 1, "distinct=%d varying attrs=%s", len(attrHashes), jsonString(varying))
	res.check("scene: one frame-hook timestamp hash over 20 runs", len(hookHashes) == 1, "distinct=%d", len(hookHashes))
}

func firstMap(v []map[string]string) map[string]string {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}
