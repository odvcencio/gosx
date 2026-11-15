//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/humanoidtest"
	"m31labs.dev/gosx/motion/animgraph"
	"m31labs.dev/gosx/motion/animgraph/gltfclip"
	"m31labs.dev/gosx/scene"
)

// This scene puts 22 skinned characters on screen: two in a paired move
// (one lunges in and reaches both hands to the other's shoulders), a mascot
// with short legs, a long neck and an enlarged head walking with an
// additive waddle synced to its walk, and 19 players walking at distances
// from 4 m to 60 m under the level-of-detail scheduler.
//
// The pair stands at the placement the simulation decided for contact; the
// root warp that would bring them there is plain Go math covered by
// motion/animgraph tests.

const (
	pairX      = -1.2
	mascotX    = 1.2
	numPlayers = 19
)

func playerZ(i int) float64 { return -4 - float64(i)*3 }

func lodGraph(t *testing.T, glb []byte, extra func(*animgraph.Graph)) *animgraph.Program {
	t.Helper()
	infos, err := gltfclip.Read(glb)
	if err != nil {
		t.Fatal(err)
	}
	clips := gltfclip.Clips(infos, func(name string) bool {
		return name == "Idle" || name == "Walk" || name == "Run" || name == "Waddle" || name == "Bounce"
	})
	for i := range clips {
		if clips[i].Name == "PairA" {
			clips[i].Events = append(clips[i].Events, animgraph.Event{Name: "contact", Time: humanoidtest.PairContact})
		}
	}
	ik, look := animgraph.Humanoid(false)
	g := animgraph.Graph{
		Params: []animgraph.Param{{Name: "speed", Kind: animgraph.Float}},
		Clips:  clips,
		Layers: []animgraph.Layer{{Name: "locomotion", Machine: animgraph.StateMachine{States: []animgraph.State{
			{Name: "Idle", Clip: "Idle", Transitions: []animgraph.Transition{{To: "Walk", When: []animgraph.Condition{animgraph.When("speed", animgraph.Greater, 0.1)}, Duration: 0.2}}},
			{Name: "Walk", Clip: "Walk"},
		}}}},
		IK: ik, LookAt: look,
	}
	if extra != nil {
		extra(&g)
	}
	p, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnimationPairedLODMascotOnTheGPU(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	hero, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo})
	if err != nil {
		t.Fatal(err)
	}
	mascot, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Rigify, NoClips: true, LegScale: 0.75, NeckScale: 1.6, HeadScale: 1.5, Color: [4]float64{0.45, 0.1, 0.75, 1}})
	if err != nil {
		t.Fatal(err)
	}
	clipSource, err := humanoidtest.Build(humanoidtest.Options{Scheme: humanoidtest.Mixamo, NoMesh: true, ArmatureScale: 0.01})
	if err != nil {
		t.Fatal(err)
	}
	playerProg := lodGraph(t, hero, nil)
	mascotProg := lodGraph(t, hero, func(g *animgraph.Graph) {
		g.Layers = append(g.Layers, animgraph.Layer{Name: "style", Additive: true, SyncLayer: "locomotion", Machine: animgraph.StateMachine{States: []animgraph.State{{Name: "Waddle", Clip: "Waddle"}}}})
		g.Rig = animgraph.Rig{Humanoid: "rigify", Sources: []animgraph.ClipSource{{Src: "/mixamo-clips.glb"}}, BoneScale: map[string]float64{"head": 1.4}, KeepProportions: true}
	})

	// The pair: roles placed as authored (attacker 1.3 m behind the
	// alignment root facing +Z, defender 0.4 m ahead facing -Z), alignment
	// root at (pairX, 0, -1).
	pathA, err := gltfclip.RootPath(hero, "PairA", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := gltfclip.RootPath(hero, "PairB", "", 30)
	if err != nil {
		t.Fatal(err)
	}
	pair := animgraph.PairedClip{
		Name: "lunge", Outcome: "stop", Contact: humanoidtest.PairContact, BlendIn: 0.1, BlendOut: 0.2,
		Roles: [2]animgraph.PairRole{
			animgraph.PairRole{Name: "attacker", Clip: "PairA", Path: pathA}.Placed(animgraph.Pose{Z: -1.3}),
			animgraph.PairRole{Name: "defender", Clip: "PairB", Path: pathB}.Placed(animgraph.Pose{Z: 0.4, Yaw: math.Pi}),
		},
		IK: []animgraph.ContactIK{
			{Role: 0, Chain: "leftHand", Bone: "rightShoulder", In: 0.3, Full: 0.45, Out: 0.7, End: 0.9},
			{Role: 0, Chain: "rightHand", Bone: "leftShoulder", In: 0.3, Full: 0.45, Out: 0.7, End: 0.9},
		},
	}
	// Turned a quarter circle so the camera sees the pair side-on.
	align := animgraph.Pose{X: pairX, Z: -1, Yaw: math.Pi / 2}
	// The models stand where Root puts them at contact.
	contactPose := func(role int) animgraph.Pose {
		r := pair.Roles[role]
		var x, z float64
		for i := range r.Path {
			if r.Path[i].T <= pair.Contact+1e-9 {
				x, z = r.Path[i].X, r.Path[i].Z
			}
		}
		s, c := math.Sincos(align.Yaw)
		return animgraph.Pose{X: align.X + c*x + s*z, Z: align.Z - s*x + c*z, Yaw: align.Yaw + r.Facing}
	}
	attackerPose, defenderPose := contactPose(0), contactPose(1)

	nodes := []scene.Node{
		scene.AmbientLight{Intensity: 0.6},
		scene.DirectionalLight{Intensity: 1.4, Direction: scene.Vector3{X: -0.3, Y: -1, Z: -0.5}},
		scene.Mesh{ID: "ground", Geometry: scene.BoxGeometry{Width: 30, Height: 0.1, Depth: 80}, Position: scene.Vector3{Y: -0.05, Z: -30}, Material: scene.StandardMaterial{Color: "#3a4048", Roughness: 0.9}},
		scene.Model{ID: "attacker", Src: "/hero.glb", Position: scene.Vector3{X: attackerPose.X, Z: attackerPose.Z}, Rotation: scene.Euler{Y: attackerPose.Yaw}, ClipSampled: true},
		scene.Model{ID: "defender", Src: "/hero.glb", Position: scene.Vector3{X: defenderPose.X, Z: defenderPose.Z}, Rotation: scene.Euler{Y: defenderPose.Yaw}, ClipSampled: true},
		scene.Model{ID: "mascot", Src: "/mascot.glb", Position: scene.Vector3{X: mascotX, Z: -1}, ClipSampled: true},
	}
	for i := 0; i < numPlayers; i++ {
		x := float64(i%5-2) * 1.5
		nodes = append(nodes, scene.Model{ID: fmt.Sprintf("p%d", i), Src: "/hero.glb", Position: scene.Vector3{X: x, Z: playerZ(i)}, ClipSampled: true})
	}
	no, yes := false, true
	props := scene.Props{
		Width: 640, Height: 400, Background: "#0b0f14", AutoRotate: &no,
		Camera: scene.PerspectiveCamera{Position: scene.Vector3{Y: 1.5, Z: 5}, FOV: 50, Near: 0.1, Far: 120},
		Graph:  scene.NewGraph(nodes...),
	}
	backends := []string{"webgl"}
	if animGPURunner() {
		backends = []string{"webgpu", "webgl"}
	}
	assets := map[string][]byte{"/hero.glb": hero, "/mascot.glb": mascot, "/mixamo-clips.glb": clipSource}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			p := props
			if backend == "webgl" {
				p.ForceWebGL, p.RequireWebGL = &yes, &yes
			} else {
				p.PreferWebGPU = &yes
			}
			manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{"id": "anim", "component": "GoSXScene3D", "kind": "surface", "mountId": "anim-root", "props": p}}})
			if err != nil {
				t.Fatal(err)
			}
			ctx := animOpenPage(t, chrome, root, backend, manifest, assets)
			runPairedLODScript(t, ctx, backend, playerProg, mascotProg, &pair, align)
		})
	}
}

// animOpenPage serves the scene and its assets and opens it in Chrome.
func animOpenPage(t *testing.T, chrome, root, backend string, manifest []byte, assets map[string][]byte) context.Context {
	t.Helper()
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
	t.Cleanup(server.Close)
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
	return ctx
}

func runPairedLODScript(t *testing.T, ctx context.Context, backend string, playerProg, mascotProg *animgraph.Program, pair *animgraph.PairedClip, align animgraph.Pose) {
	attacker, defender, mascot := playerProg.New("attacker"), playerProg.New("defender"), mascotProg.New("mascot")
	players := make([]*animgraph.Instance, numPlayers)
	for i := range players {
		players[i] = playerProg.New(fmt.Sprintf("p%d", i))
		players[i].SetFloat("speed", 1.4)
	}
	mascot.SetFloat("speed", 1.4)
	all := append([]*animgraph.Instance{attacker, defender, mascot}, players...)
	targets := make([]animgraph.LODTarget, len(all))
	for i, in := range all {
		pos := [3]float64{pairX, 0, -1}
		if in == mascot {
			pos = [3]float64{mascotX, 0, -1}
		} else if i >= 3 {
			pos = [3]float64{float64((i-3)%5-2) * 1.5, 0, playerZ(i - 3)}
		}
		targets[i] = animgraph.LODTarget{Inst: in, Position: pos, Height: 1.8, Pinned: in == attacker || in == defender}
	}
	cam := animgraph.LODCamera{Position: [3]float64{0, 1.5, 5}, FOVY: 50 * math.Pi / 180, ViewportHeight: 400}
	sched := &animgraph.LODScheduler{}
	rigs := fmt.Sprintf(`[[%d, %q], [%d, %q]]`, playerProg.RigHash(), playerProg.RigJSON(), mascotProg.RigHash(), mascotProg.RigJSON())
	enc := new(animgraph.FrameEncoder)
	feedback := func(in *animgraph.Instance, vals []float64) {
		if len(vals) > 0 {
			in.Feedback().DecodeFeedback(vals)
		}
	}
	type result struct {
		Ready    bool                 `json:"ready"`
		Result   map[string]any       `json:"result"`
		Feedback map[string][]float64 `json:"feedback"`
		Backend  string               `json:"backend"`
	}
	dispatch := func(models []*animgraph.Instance) result {
		data, err := enc.Encode(models...)
		if err != nil {
			t.Fatal(err)
		}
		expr := fmt.Sprintf(`(() => {
  const api = window.__gosx && window.__gosx.scene3d;
  const el = document.getElementById("anim-root");
  const backend = el ? (el.getAttribute("data-gosx-scene3d-backend") || "") : "";
  if (!api || typeof api.dispatchAnimationFrame !== "function") return JSON.stringify({ready: false, backend});
  if (!window.__animRigs) { window.__animRigs = true; for (const [h, j] of %s) api.defineAnimationRig(h, j); }
  const result = api.dispatchAnimationFrame("anim-root", Uint8Array.from(atob(%q), c => c.charCodeAt(0)));
  const out = {}, buf = new Float64Array(120);
  for (const id of ["attacker", "defender", "mascot"]) { const n = api.animationFeedback("anim-root", id, buf); out[id] = Array.from(buf.subarray(0, n)); }
  return JSON.stringify({ready: true, result, feedback: out, backend});
})()`, rigs, base64.StdEncoding.EncodeToString(data))
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &raw)); err != nil {
			t.Fatal(err)
		}
		var r result
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		feedback(attacker, r.Feedback["attacker"])
		feedback(defender, r.Feedback["defender"])
		feedback(mascot, r.Feedback["mascot"])
		return r
	}
	// Wait for every model to pose (and the mascot's retargeted clips).
	deadline := time.Now().Add(120 * time.Second)
	var last result
	for time.Now().Before(deadline) {
		last = dispatch(all)
		if last.Ready && last.Result["applied"] == true && last.Result["pending"] != true && attacker.Feedback().Valid && mascot.Feedback().RigHash == mascotProg.RigHash() {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !attacker.Feedback().Valid || !mascot.Feedback().Valid {
		t.Fatalf("models never posed: %+v", last)
	}
	if last.Backend != backend {
		t.Fatalf("scene ran on %q, want %q", last.Backend, backend)
	}
	// Let the mascot's clip source load and its walk settle.
	for i := 0; i < 60; i++ {
		for _, in := range all {
			in.Step(frameStep)
		}
		dispatch(all)
	}

	move, err := animgraph.StartPaired(pair, attacker, defender, animgraph.PairStart{
		Poses: [2]animgraph.Pose{{X: pairX - 1.3, Z: -1, Yaw: math.Pi / 2}, {X: pairX + 0.4, Z: -1, Yaw: -math.Pi / 2}},
		Align: align, In: 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	leftHand := playerProg.IKID("leftHand")
	dueCount := map[*animgraph.Instance]int{}
	contactEvents, contactFrames := 0, 0
	worstReach := 0.0
	mascotMinFoot := math.Inf(1)
	frames := 0
	var midShot []byte
	for !move.Done() && frames < 240 {
		move.Step(frameStep)
		for _, in := range all {
			in.Step(frameStep)
		}
		for _, e := range attacker.Events() {
			if e.Kind == animgraph.Notify && e.Name == "contact" {
				contactEvents++
			}
		}
		due := sched.Update(frameStep, cam, targets)
		for _, in := range due {
			dueCount[in]++
		}
		r := dispatch(due)
		if r.Result["applied"] != true {
			t.Fatalf("frame %d rejected: %+v", frames, r.Result)
		}
		frames++
		af := attacker.Feedback()
		if attacker.IKWeight(leftHand) >= 1 && af.Valid {
			reach := math.Sqrt(sq(af.Solved[leftHand][0]-af.Target[leftHand][0]) + sq(af.Solved[leftHand][1]-af.Target[leftHand][1]) + sq(af.Solved[leftHand][2]-af.Target[leftHand][2]))
			worstReach = math.Max(worstReach, reach)
			contactFrames++
			if midShot == nil {
				midShot = animScreenshot(t, ctx)
			}
		}
		if mf := mascot.Feedback(); mf.Valid && mf.Chains > 0 {
			mascotMinFoot = math.Min(mascotMinFoot, mf.Animated[0][1])
		}
	}
	t.Logf("paired move: %d frames, %d at full contact IK, worst hand-to-shoulder gap %.4f m, contact events %d", frames, contactFrames, worstReach, contactEvents)
	if !move.Done() || contactEvents != 1 || contactFrames < 8 {
		t.Fatalf("paired move: done %v contact events %d contact frames %d", move.Done(), contactEvents, contactFrames)
	}
	if worstReach > 0.03 {
		t.Fatalf("the attacker's hand missed the defender's shoulder by %.4f m", worstReach)
	}
	// The mascot's legs are 0.75 as long; the retargeted walk is scaled by
	// hip height, so its planted foot still reaches the ankle height (0.08).
	t.Logf("mascot lowest animated foot height %.3f m", mascotMinFoot)
	if mascotMinFoot < 0.03 || mascotMinFoot > 0.15 {
		t.Fatalf("mascot foot height %.3f: proportions were not retargeted", mascotMinFoot)
	}
	levels := map[int]int{}
	for _, in := range all {
		level, _ := in.LOD()
		levels[level]++
	}
	t.Logf("LOD levels in use: %v; updates over %d frames: attacker %d, nearest player %d, farthest %d", levels, frames, dueCount[attacker], dueCount[players[0]], dueCount[players[numPlayers-1]])
	if len(levels) < 3 || dueCount[attacker] != frames || dueCount[players[numPlayers-1]] > frames/4 {
		t.Fatalf("LOD did not spread characters over levels and rates: %v", levels)
	}
	shot := animScreenshot(t, ctx)
	purple := 0
	if img, err := animDecode(shot); err == nil {
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				if bl > 0x5000 && r > 0x3000 && g < r && g < bl/2 {
					purple++
				}
			}
		}
	}
	orange := characterPixels(t, shot).count
	t.Logf("pixels: orange %d (players), purple %d (mascot)", orange, purple)
	if orange < 300 || purple < 200 {
		t.Fatalf("characters did not render: orange %d purple %d", orange, purple)
	}
	// Frame budget from in-page timings: each character's full evaluation
	// cost at its current level, times the share of frames it updated, plus
	// the re-skin a moving character pays on the frames it skips.
	var cost map[string]float64
	expr := `(() => { const api = window.__gosx_scene3d_animation_graph_api; const out = {};
  for (const id of %s) { out[id] = api.measure("anim-root", id, 400); out[id + ":reskin"] = api.measure("anim-root", id, 2000, "reskin"); }
  return out; })()`
	ids := make([]string, len(all))
	for i, in := range all {
		ids[i] = in.ID()
	}
	idJSON, _ := json.Marshal(ids)
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(expr, idJSON), &cost)); err != nil {
		t.Fatal(err)
	}
	budget, fullAll := 0.0, 0.0
	for _, in := range all {
		full, reskin := cost[in.ID()], cost[in.ID()+":reskin"]
		share := float64(dueCount[in]) / float64(frames)
		budget += full*share + reskin*(1-share)
		fullAll += full
	}
	t.Logf("%s: 22 skinned characters, animation CPU per frame: %.0f us with LOD (%.0f us if every character re-evaluated every frame at its level); attacker %.1f us, far player %.1f us full, %.1f us re-skin",
		backend, budget, fullAll, cost["attacker"], cost[players[numPlayers-1].ID()], cost[players[numPlayers-1].ID()+":reskin"])
	if budget <= 0 || budget > 1000 {
		t.Fatalf("22-character animation budget %.0f us is missing or above 1 ms", budget)
	}
	if dir := os.Getenv("GOSX_ANIM_E2E_OUT"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, backend+"-team.png"), shot, 0o644)
		if midShot != nil {
			_ = os.WriteFile(filepath.Join(dir, backend+"-contact.png"), midShot, 0o644)
		}
	}
}

func sq(v float64) float64 { return v * v }
