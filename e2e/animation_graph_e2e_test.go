//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
	"m31labs.dev/gosx/internal/humanoidtest"
	"m31labs.dev/gosx/motion/animgraph"
	"m31labs.dev/gosx/motion/animgraph/gltfclip"
	"m31labs.dev/gosx/scene"
)

// animGPURunner reports a real-GPU runner: the NVIDIA gate (gpu-chrome.sh
// points GOSX_E2E_CHROME at fake-chrome.sh and sets GPU_CHROME_STATE) or the
// Apple runner (mac-gpu-run sets GPU_CHROME_ENDPOINT and
// GOSX_WATER_E2E_REQUIRE_WEBGPU=1). Elsewhere the test runs WebGL2 on
// SwiftShader only.
func animGPURunner() bool {
	return e2eWindowsRelay() || os.Getenv("GPU_CHROME_STATE") != "" || os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GOSX_WATER_E2E_REQUIRE_WEBGPU") == "1"
}

const animPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>animation graph</title>
<style>html,body{margin:0;background:#0b0f14}#anim-root{width:640px;height:400px}</style>
</head><body>
<div id="anim-root"></div>
<script id="gosx-manifest" type="application/json">%s</script>
<script data-gosx-script="bootstrap" src="/bootstrap.js" data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js"></script>
<script src="/gosx/bootstrap-feature-scene3d-animation-graph.js"></script>
</body></html>`

// Hero stands at x=-0.7 with its own Mixamo-named clips. Twin stands at
// x=+0.7 with Rigify-named bones and no clips: it plays the same clips
// retargeted from an animation-only Mixamo GLB with a 0.01 armature scale.
// A 0.2 m step sits under the hero's left foot only.
const (
	heroX     = -0.7
	twinX     = 0.7
	stepTop   = 0.2
	stepMinX  = heroX + 0.02
	stepMaxX  = heroX + 0.16
	frameStep = 1.0 / 60
)

func animGraph(t *testing.T, glb []byte, sources []animgraph.ClipSource, humanoid string) *animgraph.Program {
	t.Helper()
	infos, err := gltfclip.Read(glb)
	if err != nil {
		t.Fatal(err)
	}
	clips := gltfclip.Clips(infos, func(name string) bool { return name != "Jump" })
	for i := range clips {
		for _, at := range humanoidtest.Footsteps[clips[i].Name] {
			clips[i].Events = append(clips[i].Events, animgraph.Event{Name: "footstep", Time: at})
		}
	}
	ik, look := animgraph.Humanoid(false)
	g := animgraph.Graph{
		Params: []animgraph.Param{{Name: "speed", Kind: animgraph.Float}, {Name: "aim", Kind: animgraph.Bool}},
		Clips:  clips,
		Masks:  []animgraph.Mask{{Name: "upper", Include: []string{"upperChest"}, Exclude: []string{"neck"}}},
		Layers: []animgraph.Layer{
			{Name: "locomotion", Machine: animgraph.StateMachine{States: []animgraph.State{
				{Name: "Idle", Clip: "Idle", Transitions: []animgraph.Transition{{To: "Move", When: []animgraph.Condition{animgraph.When("speed", animgraph.Greater, 0.1)}, Duration: 0.2}}},
				{Name: "Move", Blend1D: &animgraph.BlendSpace1D{Param: "speed", ScaleSpeed: true, Points: []animgraph.BlendPoint1D{{Clip: "Walk", At: 1.4}, {Clip: "Run", At: 4}}},
					Transitions: []animgraph.Transition{{To: "Idle", When: []animgraph.Condition{animgraph.When("speed", animgraph.Less, 0.1)}, Duration: 0.25}}},
			}}},
			{Name: "aim", Mask: "upper", Additive: true, Machine: animgraph.StateMachine{States: []animgraph.State{
				{Name: "Off", Transitions: []animgraph.Transition{{To: "Aim", When: []animgraph.Condition{animgraph.WhenTrue("aim")}, Duration: 0.2}}},
				{Name: "Aim", Clip: "Aim", Transitions: []animgraph.Transition{{To: "Off", When: []animgraph.Condition{animgraph.WhenFalse("aim")}, Duration: 0.2}}},
			}}},
		},
		IK: ik, LookAt: look,
		Pelvis: animgraph.Pelvis{MaxDrop: 0.3},
		Rig:    animgraph.Rig{Humanoid: humanoid, Sources: sources},
	}
	p, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type animFrameResult struct {
	Ready    bool                 `json:"ready"`
	Result   map[string]any       `json:"result"`
	Feedback map[string][]float64 `json:"feedback"`
	Backend  string               `json:"backend"`
}

func TestAnimationGraphCharacterOnTheGPU(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	hero, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo})
	if err != nil {
		t.Fatal(err)
	}
	twin, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Rigify, NoClips: true, Color: [4]float64{0.1, 0.6, 0.55, 1}})
	if err != nil {
		t.Fatal(err)
	}
	clipSource, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo, NoMesh: true, ArmatureScale: 0.01})
	if err != nil {
		t.Fatal(err)
	}
	heroProg := animGraph(t, hero, nil, "auto")
	twinProg := animGraph(t, hero, []animgraph.ClipSource{{Src: "/mixamo-clips.glb"}}, "rigify")

	no := false
	props := scene.Props{
		Width: 640, Height: 400, Background: "#0b0f14", AutoRotate: &no,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Y: 1.1, Z: 3.4}, FOV: 50, Near: 0.1, Far: 40},
		Graph: scene.NewGraph(
			scene.AmbientLight{Intensity: 0.6},
			scene.DirectionalLight{Intensity: 1.4, Direction: scene.Vector3{X: -0.3, Y: -1, Z: -0.5}},
			scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 8, Height: 0.1, Depth: 8}, Position: scene.Vector3{Y: -0.05}, Material: scene.StandardMaterial{Color: "#3a4048", Roughness: 0.9}},
			scene.Mesh{ID: "step", Geometry: scene.BoxGeometry{Width: stepMaxX - stepMinX, Height: stepTop, Depth: 0.5}, Position: scene.Vector3{X: (stepMinX + stepMaxX) / 2, Y: stepTop / 2}, Material: scene.StandardMaterial{Color: "#6f7a86", Roughness: 0.8}},
			scene.Model{ID: "hero", Src: "/hero.glb", Position: scene.Vector3{X: heroX}, ClipSampled: true},
			scene.Model{ID: "twin", Src: "/twin.glb", Position: scene.Vector3{X: twinX}, ClipSampled: true},
		),
	}
	backends := []string{"webgl"}
	if animGPURunner() {
		backends = []string{"webgpu", "webgl"}
	}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			p := props
			yes := true
			if backend == "webgl" {
				p.ForceWebGL, p.RequireWebGL = &yes, &yes
			} else {
				p.PreferWebGPU = &yes
			}
			manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{"id": "anim", "component": "GoSXScene3D", "kind": "surface", "mountId": "anim-root", "props": p}}})
			if err != nil {
				t.Fatal(err)
			}
			assets := map[string][]byte{"/hero.glb": hero, "/twin.glb": twin, "/mixamo-clips.glb": clipSource}
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if data, ok := assets[r.URL.Path]; ok {
					w.Header().Set("Content-Type", "model/gltf-binary")
					_, _ = w.Write(data)
					return
				}
				name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), "gosx/")
				if name == "bootstrap.js" || strings.HasPrefix(name, "bootstrap-feature-") {
					data, err := os.ReadFile(filepath.Join(root, "client", "js", filepath.Base(name)))
					if err != nil {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
					_, _ = w.Write(data)
					return
				}
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprintf(w, animPage, manifest)
			}))
			listener, err := net.Listen("tcp", "0.0.0.0:0")
			if err != nil {
				t.Fatal(err)
			}
			server.Listener = listener
			server.Start()
			defer server.Close()
			pageURL := fmt.Sprintf("http://localhost:%d/", listener.Addr().(*net.TCPAddr).Port)

			ctx := animBrowser(t, chrome, backend)
			err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			for i := 0; err != nil && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				err = chromedp.Run(ctx, chromedp.Navigate(pageURL))
			}
			if err != nil {
				t.Fatal(err)
			}
			runAnimationScript(t, ctx, backend, heroProg, twinProg)
		})
	}
}

// animBrowser starts Chrome for one backend: GPU flags come from the runner;
// elsewhere WebGL2 runs on SwiftShader. The browser closes with the test.
func animBrowser(t *testing.T, chrome, backend string) context.Context {
	t.Helper()
	args := []string{"--no-sandbox", "--mute-audio", "--window-size=700,480"}
	if !animGPURunner() {
		args = append(args, "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--disable-features=WebGPU")
	}
	browser, err := chrometest.Start(t.Context(), chrome, args...)
	if err != nil {
		t.Skipf("chrome unavailable: %v", err)
	}
	t.Cleanup(browser.Close)
	ctx, cancel := context.WithTimeout(browser.Context, 240*time.Second)
	t.Cleanup(cancel)
	init := `window.__gosx_scene3d_render_truth = true;`
	if backend == "webgl" {
		init += `window.__gosx_scene3d_force_webgl = true;`
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(init).Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func animDecode(shot []byte) (image.Image, error) { return png.Decode(bytes.NewReader(shot)) }

// groundAt is the test's stand-in for a game raycast: the step top inside
// its X range, the floor elsewhere.
func groundAt(x float64) float64 {
	if x >= stepMinX && x <= stepMaxX {
		return stepTop
	}
	return 0
}

func runAnimationScript(t *testing.T, ctx context.Context, backend string, heroProg, twinProg *animgraph.Program) {
	heroIn, twinIn := heroProg.New("hero"), twinProg.New("twin")
	rigs := fmt.Sprintf(`[[%d, %q], [%d, %q]]`, heroProg.RigHash(), heroProg.RigJSON(), twinProg.RigHash(), twinProg.RigJSON())
	enc := new(animgraph.FrameEncoder)
	feet := []int{heroProg.IKID("leftFoot"), heroProg.IKID("rightFoot")}
	head := heroProg.LookAtID("head")
	dispatch := func() animFrameResult {
		data, err := enc.Encode(heroIn, twinIn)
		if err != nil {
			t.Fatal(err)
		}
		expr := fmt.Sprintf(`(() => {
  const api = window.__gosx && window.__gosx.scene3d;
  const el = document.getElementById("anim-root");
  const backend = el ? (el.getAttribute("data-gosx-scene3d-backend") || "") : "";
  if (!api || typeof api.dispatchAnimationFrame !== "function") return JSON.stringify({ready: false, backend});
  if (!window.__animRigs) { window.__animRigs = true; for (const [h, j] of %s) api.defineAnimationRig(h, j); }
  const bytes = Uint8Array.from(atob(%q), c => c.charCodeAt(0));
  const result = api.dispatchAnimationFrame("anim-root", bytes);
  const out = {}, buf = new Float64Array(80);
  for (const id of ["hero", "twin"]) { const n = api.animationFeedback("anim-root", id, buf); out[id] = Array.from(buf.subarray(0, n)); }
  return JSON.stringify({ready: true, result, feedback: out, backend});
})()`, rigs, base64.StdEncoding.EncodeToString(data))
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw)); err != nil {
			t.Fatal(err)
		}
		var r animFrameResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if r.Ready {
			heroIn.Feedback().DecodeFeedback(r.Feedback["hero"])
			twinIn.Feedback().DecodeFeedback(r.Feedback["twin"])
		}
		return r
	}
	// Wait until both models pose (hydrated, clips bound, twin's retargeted
	// clip source loaded). The graph sits in Idle meanwhile.
	deadline := time.Now().Add(120 * time.Second)
	var last animFrameResult
	for time.Now().Before(deadline) {
		heroIn.Step(0)
		twinIn.Step(0)
		last = dispatch()
		hf, tf := heroIn.Feedback(), twinIn.Feedback()
		if last.Ready && last.Result["applied"] == true && last.Result["pending"] != true && hf.Valid && tf.Valid && hf.RigHash == heroProg.RigHash() && tf.RigHash == twinProg.RigHash() {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !heroIn.Feedback().Valid || !twinIn.Feedback().Valid {
		t.Fatalf("models never posed: %+v", last)
	}
	if backend == "webgpu" && last.Backend != "webgpu" || backend == "webgl" && last.Backend != "webgl" {
		t.Fatalf("scene ran on %q, want %q", last.Backend, backend)
	}
	// The twin's clips arrive asynchronously (retargeted source GLB): wait
	// until its pose matches the hero's at the same graph time.
	matched := false
	for i := 0; i < 200 && !matched; i++ {
		heroIn.Step(frameStep)
		twinIn.Step(frameStep)
		dispatch()
		matched = sameLocalPose(heroIn.Feedback(), twinIn.Feedback(), 0.01)
		if !matched {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !matched {
		t.Fatalf("twin never matched the hero pose: hero %+v twin %+v", heroIn.Feedback(), twinIn.Feedback())
	}

	var evalHero, evalTwin []float64
	footsteps := map[string]int{}
	type sample struct{ leftZ, leftY float64 }
	var walkLeft []sample
	step := func(phase string) {
		heroIn.Step(frameStep)
		twinIn.Step(frameStep)
		for _, e := range heroIn.Events() {
			if e.Kind == animgraph.Notify && e.Name == "footstep" {
				footsteps[phase]++
			}
		}
		// Raycast below last frame's animated feet, as a game would.
		for _, in := range []*animgraph.Instance{heroIn, twinIn} {
			for _, chain := range feet {
				if pos, ok := in.Feedback().AnimatedEnd(chain); ok {
					in.SetGround(chain, groundAt(pos[0]), [3]float64{0, 1, 0})
				}
			}
		}
		r := dispatch()
		if r.Result["applied"] != true {
			t.Fatalf("%s frame rejected: %+v", phase, r.Result)
		}
		evalHero = append(evalHero, heroIn.Feedback().EvalMicros)
		evalTwin = append(evalTwin, twinIn.Feedback().EvalMicros)
	}

	// Phase 1: idle with foot IK on and a look-at target to the front right.
	for _, in := range []*animgraph.Instance{heroIn, twinIn} {
		for _, chain := range feet {
			in.SetIKWeight(chain, 1)
		}
		in.SetLookAt(head, [3]float64{heroX + 2, 1.55, 2}, 1)
	}
	for i := 0; i < 45; i++ {
		step("idle")
	}
	hf := heroIn.Feedback()
	left, right := hf.Solved[feet[0]], hf.Solved[feet[1]]
	leftAnim, rightAnim := hf.Animated[feet[0]], hf.Animated[feet[1]]
	t.Logf("idle hero left foot animated %.3f solved %.3f; right animated %.3f solved %.3f", leftAnim[1], left[1], rightAnim[1], right[1])
	if math.Abs(left[1]-leftAnim[1]-stepTop) > 0.02 {
		t.Fatalf("left foot on the step: solved y %.3f, animated %.3f, want +%.2f", left[1], leftAnim[1], stepTop)
	}
	if math.Abs(right[1]-rightAnim[1]) > 0.01 {
		t.Fatalf("right foot off the step moved: %.3f -> %.3f", rightAnim[1], right[1])
	}
	tf := twinIn.Feedback()
	if math.Abs(tf.Solved[feet[0]][1]-tf.Animated[feet[0]][1]) > 0.01 {
		t.Fatalf("twin has no step but its left foot moved: %+v", tf)
	}
	fwd := hf.LookForward[head]
	toTarget := [3]float64{heroX + 2 - hf.LookPivot[head][0], 1.55 - hf.LookPivot[head][1], 2 - hf.LookPivot[head][2]}
	l := math.Sqrt(toTarget[0]*toTarget[0] + toTarget[1]*toTarget[1] + toTarget[2]*toTarget[2])
	if dot := (fwd[0]*toTarget[0] + fwd[1]*toTarget[1] + fwd[2]*toTarget[2]) / l; dot < 0.97 {
		t.Fatalf("head does not face the look-at target: forward %v, target direction dot %.3f", fwd, dot)
	}
	idleShot := animScreenshot(t, ctx)

	// Phase 2: walk for 2 s with the upper-body aim layer on; feet follow
	// the clip (no IK weight while moving).
	for _, in := range []*animgraph.Instance{heroIn, twinIn} {
		for _, chain := range feet {
			in.SetIKWeight(chain, 0)
		}
		in.SetLookAt(head, [3]float64{}, 0)
		in.SetFloat("speed", 1.4)
		in.SetBool("aim", true)
	}
	maxDiff := 0.0
	for i := 0; i < 120; i++ {
		step("walk")
		walkLeft = append(walkLeft, sample{heroIn.Feedback().Animated[feet[0]][2], heroIn.Feedback().Animated[feet[0]][1]})
		if i > 20 {
			if d := poseDiff(heroIn.Feedback(), twinIn.Feedback()); d > maxDiff {
				maxDiff = d
			}
		}
	}
	walkShot := animScreenshot(t, ctx)
	// Phase 3: run for 2 s.
	for _, in := range []*animgraph.Instance{heroIn, twinIn} {
		in.SetFloat("speed", 4)
	}
	for i := 0; i < 120; i++ {
		step("run")
		if d := poseDiff(heroIn.Feedback(), twinIn.Feedback()); d > maxDiff {
			maxDiff = d
		}
	}
	runShot := animScreenshot(t, ctx)

	minZ, maxZ := math.Inf(1), math.Inf(-1)
	for _, s := range walkLeft[30:] {
		minZ, maxZ = math.Min(minZ, s.leftZ), math.Max(maxZ, s.leftZ)
	}
	t.Logf("walk: left foot z range %.3f m; footsteps walk=%d run=%d; hero/twin max foot gap %.4f m", maxZ-minZ, footsteps["walk"], footsteps["run"], maxDiff)
	if maxZ-minZ < 0.2 {
		t.Fatalf("walk did not swing the left foot: z range %.3f", maxZ-minZ)
	}
	// Walk: 2 s at a 1 s cycle with two steps per cycle; the fade from Idle
	// delays the first. Run: 2 s at 0.6 s cycles is 6.7 steps.
	if footsteps["walk"] < 3 || footsteps["walk"] > 5 || footsteps["run"] < 6 || footsteps["run"] > 7 {
		t.Fatalf("footstep events walk=%d run=%d, want 3-5 and 6-7", footsteps["walk"], footsteps["run"])
	}
	if maxDiff > 0.01 {
		t.Fatalf("retargeted twin drifted %.4f m from the hero", maxDiff)
	}
	idleHero, walkHero, runHero := characterPixels(t, idleShot), characterPixels(t, walkShot), characterPixels(t, runShot)
	t.Logf("hero pixels idle %d walk %d run %d; changed idle->walk %d walk->run %d", idleHero.count, walkHero.count, runHero.count, animPixelChanges(idleShot, walkShot), animPixelChanges(walkShot, runShot))
	if idleHero.count < 300 || walkHero.count < 300 || runHero.count < 300 {
		t.Fatalf("the skinned hero did not render: %d/%d/%d orange pixels", idleHero.count, walkHero.count, runHero.count)
	}
	if animPixelChanges(idleShot, walkShot) < 200 || animPixelChanges(walkShot, runShot) < 200 {
		t.Fatal("the rendered pose did not change between idle, walk and run")
	}
	// Per-frame EvalMicros readings are coarsened to 100 us by Chrome on a
	// page without cross-origin isolation, so report their mean only, and
	// take the cost from an in-page loop of 4000 evaluations of the posed
	// model (last run frame: walk/run blend, aim layer, IK, look-at inputs).
	var cost map[string]float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const api = window.__gosx_scene3d_animation_graph_api;
  return { hero: api.measure("anim-root", "hero", 4000), twin: api.measure("anim-root", "twin", 4000) }; })()`, &cost)); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: browser CPU evaluation per character per frame (60 joints): hero %.1f us, twin %.1f us (in-page loop of 4000); per-frame feedback mean hero %.1f us, twin %.1f us over %d frames",
		backend, cost["hero"], cost["twin"], animMean(evalHero), animMean(evalTwin), len(evalHero))
	if cost["hero"] <= 0 || cost["twin"] <= 0 || cost["hero"] > 100 || cost["twin"] > 100 {
		t.Fatalf("evaluation cost hero %.1f / twin %.1f us is missing or exceeds 0.1 ms", cost["hero"], cost["twin"])
	}
	if dir := os.Getenv("GOSX_ANIM_E2E_OUT"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		for name, shot := range map[string][]byte{"idle": idleShot, "walk": walkShot, "run": runShot} {
			_ = os.WriteFile(filepath.Join(dir, backend+"-"+name+".png"), shot, 0o644)
		}
	}
}

// sameLocalPose compares feet relative to each model's root bone.
func sameLocalPose(a, b *animgraph.Feedback, tol float64) bool {
	return a.Valid && b.Valid && poseDiff(a, b) <= tol
}

func poseDiff(a, b *animgraph.Feedback) float64 {
	worst := 0.0
	for chain := 0; chain < 2 && chain < a.Chains && chain < b.Chains; chain++ {
		for axis := 0; axis < 3; axis++ {
			da := a.Animated[chain][axis] - a.Root[axis]
			db := b.Animated[chain][axis] - b.Root[axis]
			worst = math.Max(worst, math.Abs(da-db))
		}
	}
	return worst
}

// animScreenshot captures the scene after two animation frames, so the
// capture shows a presented frame of the current pose. A capture with no
// character pixels is retried a few times without sending new frames: on
// the Windows GPU relay an occasional capture comes back blank while the
// page keeps rendering. A scene that never renders still fails afterwards.
func animScreenshot(t *testing.T, ctx context.Context) []byte {
	t.Helper()
	var shot []byte
	for attempt := 0; attempt < 5; attempt++ {
		var ok bool
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))`, &ok, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }),
			chromedp.Screenshot("#anim-root", &shot, chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
		if characterPixels(t, shot).count >= 300 {
			return shot
		}
		if attempt < 4 {
			t.Logf("capture %d showed no character; retrying", attempt+1)
			time.Sleep(200 * time.Millisecond)
		}
	}
	return shot
}

type animPixelStats struct{ count int }

// characterPixels counts the hero's orange (red well above green and blue).
func characterPixels(t *testing.T, shot []byte) animPixelStats {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	var s animPixelStats
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0x6000 && r > g+0x2000 && r > bl+0x3000 {
				s.count++
			}
		}
	}
	return s
}

func animPixelChanges(a, b []byte) int {
	ia, err1 := png.Decode(bytes.NewReader(a))
	ib, err2 := png.Decode(bytes.NewReader(b))
	if err1 != nil || err2 != nil || ia.Bounds() != ib.Bounds() {
		return 0
	}
	changed := 0
	bounds := ia.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if animColorDistance(ia, ib, x, y) > 0x1800 {
				changed++
			}
		}
	}
	return changed
}

func animColorDistance(a, b image.Image, x, y int) uint32 {
	r1, g1, b1, _ := a.At(x, y).RGBA()
	r2, g2, b2, _ := b.At(x, y).RGBA()
	d := func(p, q uint32) uint32 {
		if p > q {
			return p - q
		}
		return q - p
	}
	return d(r1, r2) + d(g1, g2) + d(b1, b2)
}

func animMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
