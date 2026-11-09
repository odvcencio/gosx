package animgraph

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

const frameDT = 1.0 / 60

// locomotion is a small but complete graph: idle, a walk/run blend, a
// one-shot jump with an exit time, and an any-state hit reaction.
func locomotion() Graph {
	return Graph{
		Params: []Param{
			{Name: "speed", Kind: Float},
			{Name: "grounded", Kind: Bool, Default: 1},
			{Name: "jump", Kind: Trigger},
			{Name: "hit", Kind: Trigger},
		},
		Clips: []Clip{
			{Name: "Idle", Duration: 2, Loop: true},
			{Name: "Walk", Duration: 1, Loop: true, Events: []Event{{Name: "footstep", Time: 0}, {Name: "footstep", Time: 0.5}},
				Curves: []Curve{{Name: "leftFootIK", Keys: []CurveKey{{0, 1}, {0.5, 0}, {1, 1}}}}},
			{Name: "Run", Duration: 0.6, Loop: true, Events: []Event{{Name: "footstep", Time: 0}, {Name: "footstep", Time: 0.3}},
				Curves: []Curve{{Name: "leftFootIK", Keys: []CurveKey{{0, 1}, {0.3, 0}, {0.6, 1}}}}},
			{Name: "Jump", Duration: 0.8, Events: []Event{{Name: "takeoff", Time: 0.1}, {Name: "land", Time: 0.7}}},
			{Name: "Hit", Duration: 0.5},
		},
		Layers: []Layer{{
			Name: "base",
			Machine: StateMachine{
				Entry: "Idle",
				States: []State{
					{Name: "Idle", Clip: "Idle", Transitions: []Transition{
						{To: "Move", When: []Condition{When("speed", Greater, 0.1)}, Duration: 0.2},
						{To: "Jump", When: []Condition{WhenTriggered("jump")}, Duration: 0.1},
					}},
					{Name: "Move", Blend1D: &BlendSpace1D{Param: "speed", Points: []BlendPoint1D{{Clip: "Walk", At: 1.4}, {Clip: "Run", At: 4}}}, Transitions: []Transition{
						{To: "Idle", When: []Condition{When("speed", Less, 0.1)}, Duration: 0.25},
						{To: "Jump", When: []Condition{WhenTriggered("jump")}, Duration: 0.1},
					}},
					{Name: "Jump", Clip: "Jump", Transitions: []Transition{
						{To: "Idle", When: []Condition{WhenTrue("grounded")}, ExitTime: 0.9, Duration: 0.2},
					}},
					{Name: "Hit", Clip: "Hit", Transitions: []Transition{{To: "Idle", ExitTime: 1, Duration: 0.1}}},
				},
				Any: []Transition{{To: "Hit", When: []Condition{WhenTriggered("hit")}, Duration: 0.05, CanSelf: true}},
			},
		}},
	}
}

func mustProgram(t *testing.T, g Graph) *Program {
	t.Helper()
	p, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func near(t *testing.T, label string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %.9f, want %.9f (tol %g)", label, got, want, tol)
	}
}

type stepRecord struct {
	State      string
	Normalized float64
	Progress   float64
	Events     string
	Samples    string
}

// script drives an instance through a fixed input sequence and records every
// step, so two runs can be compared byte for byte.
func script(p *Program) []stepRecord {
	in := p.New("hero")
	var out []stepRecord
	for step := 0; step < 300; step++ {
		switch step {
		case 10:
			in.SetFloat("speed", 2.7)
		case 80:
			in.SetTrigger("jump")
			in.SetBool("grounded", false)
		case 100:
			in.SetBool("grounded", true)
		case 160:
			in.SetTrigger("hit")
		case 170:
			in.SetTrigger("hit")
		case 220:
			in.SetFloat("speed", 0)
		}
		in.Step(frameDT)
		info := in.State(0)
		var events []string
		for _, e := range in.Events() {
			events = append(events, fmt.Sprintf("%d:%s:%s:%.4f", e.Kind, e.Name, e.Clip, e.Weight))
		}
		var samples []string
		for _, s := range in.Samples(0) {
			samples = append(samples, fmt.Sprintf("%d@%.6f*%.6f", s.Clip, s.Time, s.Weight))
		}
		out = append(out, stepRecord{info.Name, info.Normalized, info.Progress, strings.Join(events, ","), strings.Join(samples, ",")})
	}
	return out
}

func TestTransitionsAreDeterministic(t *testing.T) {
	p := mustProgram(t, locomotion())
	first, second := script(p), script(mustProgram(t, locomotion()))
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two runs of the same input script produced different steps")
	}
	// Step 10 sets speed above 0.1, so the Idle -> Move transition starts on
	// that step and its 0.2 s crossfade takes 12 steps of 1/60 s.
	if first[9].State != "Idle" || first[10].State != "Move" || !strings.Contains(first[10].Events, "1:Move") {
		t.Fatalf("Move did not start on step 10: %+v / %+v", first[9], first[10])
	}
	near(t, "progress 6 steps into the fade", first[16].Progress, 0.5, 1e-9)
	if first[22].Progress != 1 || !strings.Contains(first[22].Events, "2:Idle") {
		t.Fatalf("crossfade did not end on step 22 with an Idle exit: %+v", first[22])
	}
	if first[80].State != "Jump" || first[81].State != "Jump" {
		t.Fatalf("jump trigger did not enter Jump: %+v", first[80])
	}
	if first[160].State != "Hit" || !strings.Contains(first[170].Events, "1:Hit") {
		t.Fatalf("any-state hit did not fire or restart: %+v / %+v", first[160], first[170])
	}
	if first[299].State != "Idle" {
		t.Fatalf("speed 0 did not return to Idle: %+v", first[299])
	}
}

func TestExitTimeOnOneShotClip(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	in.SetTrigger("jump")
	in.Step(frameDT) // Idle -> Jump starts; Jump sits at normalized 0.
	if in.State(0).Name != "Jump" || in.Float("jump") != 0 {
		t.Fatalf("jump trigger not consumed on entry: %+v jump=%v", in.State(0), in.Float("jump"))
	}
	// Exit time 0.9 of a 0.8 s clip is 0.72 s: 43.2 steps, so the gate opens
	// on the 44th advancing step.
	for step := 1; step <= 43; step++ {
		in.Step(frameDT)
		if in.State(0).Name != "Jump" {
			t.Fatalf("left Jump early on advancing step %d (normalized %.4f)", step, in.State(0).Normalized)
		}
	}
	in.Step(frameDT)
	if in.State(0).Name != "Idle" {
		t.Fatalf("exit time did not fire on step 44: %+v", in.State(0))
	}
}

func TestExitTimeOnLoopingStateOpensEveryLoop(t *testing.T) {
	g := Graph{
		Params: []Param{{Name: "go", Kind: Bool}},
		Clips:  []Clip{{Name: "A", Duration: 1, Loop: true}, {Name: "B", Duration: 1, Loop: true}},
		Layers: []Layer{{Machine: StateMachine{States: []State{
			{Name: "A", Clip: "A", Transitions: []Transition{{To: "B", When: []Condition{WhenTrue("go")}, ExitTime: 0.5}}},
			{Name: "B", Clip: "B"},
		}}}},
	}
	in := mustProgram(t, g).New("m")
	for i := 0; i < 70; i++ { // 1.1667 s: second loop, normalized fraction 0.1667
		in.Step(frameDT)
	}
	in.SetBool("go", true)
	for i := 0; i < 19; i++ { // fraction reaches 0.4833, still before 0.5
		in.Step(frameDT)
		if in.State(0).Name != "A" {
			t.Fatalf("gate opened before 0.5 of the loop at step %d", i)
		}
	}
	in.Step(frameDT) // fraction 0.5
	if in.State(0).Name != "B" {
		t.Fatalf("gate did not open at 0.5 of the second loop: %+v", in.State(0))
	}
}

func TestAnyStateRespectsCanSelf(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Any[0].CanSelf = false
	g.Params[3].Kind = Bool // a persistent condition, the case CanSelf guards
	g.Layers[0].Machine.Any[0].When = []Condition{WhenTrue("hit")}
	in := mustProgram(t, g).New("hero")
	in.SetBool("hit", true)
	in.Step(frameDT)
	enters := 0
	for i := 0; i < 20; i++ {
		in.Step(frameDT)
		for _, e := range in.Events() {
			if e.Kind == Enter && e.Name == "Hit" {
				enters++
			}
		}
	}
	if enters != 0 {
		t.Fatalf("any-state transition re-entered Hit %d times without CanSelf", enters)
	}
}

func TestFootstepEventsFireOncePerStep(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	in := mustProgram(t, g).New("hero")
	in.SetFloat("speed", 1.4) // pure Walk: footsteps at 0 and 0.5 s of a 1 s clip
	var times []int
	for step := 0; step < 120; step++ {
		in.Step(frameDT)
		for _, e := range in.Events() {
			if e.Kind == Notify && e.Name == "footstep" {
				times = append(times, step)
			}
		}
	}
	// Step k ends at (k+1)/60 s. Step 0 includes time 0; the 0.5 s footstep
	// is reached by step 29, the next loop's 0 s footstep by step 59, and
	// step 119 ends exactly at 2 s.
	if want := []int{0, 29, 59, 89, 119}; !reflect.DeepEqual(times, want) {
		t.Fatalf("footsteps at steps %v, want %v", times, want)
	}
	// A synced walk/run blend fires from the dominant clip only.
	in.SetFloat("speed", 2.6) // walk 0.538, run 0.462
	count := 0
	for step := 0; step < 120; step++ {
		in.Step(frameDT)
		for _, e := range in.Events() {
			if e.Kind == Notify {
				if e.Clip != "Walk" {
					t.Fatalf("event from non-dominant clip %q", e.Clip)
				}
				count++
			}
		}
	}
	// Effective cycle 0.538*1 + 0.462*0.6 = 0.8154 s, two footsteps per cycle:
	// 2 s is 2.45 cycles, so 4 or 5 footsteps, not the 8 to 10 that firing
	// from both synced clips would give.
	if count < 4 || count > 5 {
		t.Fatalf("got %d footsteps in 2 s of synced blend, want 4 or 5", count)
	}
}

func TestEventsAllModeFiresEveryWeightedClip(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	g.EventMode = EventsAll
	g.EventMinWeight = 0.2
	in := mustProgram(t, g).New("hero")
	in.SetFloat("speed", 2.7)
	in.Step(frameDT)
	clips := map[string]bool{}
	for _, e := range in.Events() {
		clips[e.Clip] = true
	}
	if !clips["Walk"] || !clips["Run"] {
		t.Fatalf("EventsAll at time 0 fired %v, want both Walk and Run", clips)
	}
}

func TestBlend1DSyncsByNormalizedTime(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	in := mustProgram(t, g).New("hero")
	in.SetFloat("speed", 2.7) // (4-2.7)/(4-1.4) = 0.5 walk, 0.5 run
	steps := 30
	for i := 0; i < steps; i++ {
		in.Step(frameDT)
	}
	samples := in.Samples(0)
	if len(samples) != 2 {
		t.Fatalf("samples = %+v", samples)
	}
	// One cycle lasts 0.5*1 + 0.5*0.6 = 0.8 s, so after 0.5 s the shared phase
	// is 0.625: walk at 0.625 s, run at 0.375 s.
	near(t, "walk weight", samples[0].Weight, 0.5, 1e-12)
	near(t, "run weight", samples[1].Weight, 0.5, 1e-12)
	near(t, "walk time", samples[0].Time, 0.625, 1e-9)
	near(t, "run time", samples[1].Time, 0.375, 1e-9)
	near(t, "walk/run phase", samples[0].Time/1, samples[1].Time/0.6, 1e-9)
}

func TestBlend1DScaleSpeed(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	g.Layers[0].Machine.States[1].Blend1D.ScaleSpeed = true
	in := mustProgram(t, g).New("hero")
	in.SetFloat("speed", 8) // twice the run point
	for i := 0; i < 9; i++ {
		in.Step(frameDT)
	}
	// Run alone, at double rate: 0.15 s of wall time is 0.3 s of clip time.
	near(t, "run time", in.Samples(0)[0].Time, 0.3, 1e-9)
}

func TestGradientBandWeights(t *testing.T) {
	px := []float64{0, 0, 0, -1, 1, 0}
	py := []float64{0, 1, -1, 0, 0, 3}
	w := make([]float64, len(px))
	for i := range px {
		gradientBand(px, py, px[i], py[i], w)
		for j := range w {
			want := 0.0
			if i == j {
				want = 1
			}
			near(t, fmt.Sprintf("w[%d] at point %d", j, i), w[j], want, 1e-12)
		}
	}
	gradientBand(px, py, 0.5, 0.5, w)
	sum := 0.0
	for _, v := range w {
		if v < 0 {
			t.Fatalf("negative weight %v", w)
		}
		sum += v
	}
	near(t, "weight sum", sum, 1, 1e-12)
	if !(w[1] > 0.2 && w[4] > 0.2 && w[2] == 0 && w[3] == 0) {
		t.Fatalf("(0.5,0.5) should blend forward and right only: %v", w)
	}
	// Halfway between forward walk (0,1) and forward run (0,3) on the axis.
	gradientBand(px, py, 0, 2, w)
	near(t, "walk at (0,2)", w[1], 0.5, 1e-12)
	near(t, "run at (0,2)", w[5], 0.5, 1e-12)
}

func TestCurvesBlendAndDriveIKWeight(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	ik, _ := Humanoid(true)
	g.IK = ik
	g.Clips[1].Curves = append(g.Clips[1].Curves, Curve{Name: "rightFootIK", Keys: []CurveKey{{0, 0}, {1, 0}}})
	p := mustProgram(t, g)
	in := p.New("hero")
	in.SetFloat("speed", 1.4)
	for i := 0; i < 15; i++ { // walk at 0.25 s: leftFootIK = 0.5
		in.Step(frameDT)
	}
	near(t, "leftFootIK", in.Curve("leftFootIK"), 0.5, 1e-9)
	left := p.IKID("leftFoot")
	in.SetIKWeight(left, 0.8)
	near(t, "weighted IK", in.IKWeight(left), 0.4, 1e-9)
	if p.IKID("missing") != -1 {
		t.Fatal("unknown chain has an ID")
	}
}

func TestEmptyStateFadesLayerOut(t *testing.T) {
	g := Graph{
		Params: []Param{{Name: "aim", Kind: Bool}},
		Clips:  []Clip{{Name: "Idle", Duration: 1, Loop: true}, {Name: "Aim", Duration: 1, Loop: true}},
		Masks:  []Mask{{Name: "upper", Include: []string{"spine"}}},
		Layers: []Layer{
			{Name: "base", Machine: StateMachine{States: []State{{Name: "Idle", Clip: "Idle"}}}},
			{Name: "upper", Mask: "upper", Machine: StateMachine{States: []State{
				{Name: "Off", Transitions: []Transition{{To: "Aim", When: []Condition{WhenTrue("aim")}, Duration: 0.5}}},
				{Name: "Aim", Clip: "Aim", Transitions: []Transition{{To: "Off", When: []Condition{WhenFalse("aim")}, Duration: 0.5}}},
			}}},
		},
	}
	in := mustProgram(t, g).New("m")
	in.Step(frameDT)
	if in.LayerWeight(1) != 0 || len(in.Samples(1)) != 0 {
		t.Fatalf("empty state should weigh 0: %v %v", in.LayerWeight(1), in.Samples(1))
	}
	in.SetBool("aim", true)
	in.Step(frameDT) // transition starts, aim at 0
	for i := 0; i < 15; i++ {
		in.Step(frameDT)
	}
	near(t, "layer weight halfway", in.LayerWeight(1), 0.5, 1e-9)
	in.SetLayerWeight(1, 0.5)
	in.Step(0)
	near(t, "runtime-scaled layer weight", in.LayerWeight(1), 0.25, 1e-9)
}

func TestCrossfadeStackHandlesInterruptions(t *testing.T) {
	g := Graph{
		Params: []Param{{Name: "s", Kind: Float}},
		Clips:  []Clip{{Name: "A", Duration: 1, Loop: true}, {Name: "B", Duration: 1, Loop: true}, {Name: "C", Duration: 1, Loop: true}},
		Layers: []Layer{{Machine: StateMachine{
			States: []State{{Name: "A", Clip: "A"}, {Name: "B", Clip: "B"}, {Name: "C", Clip: "C"}},
			Any: []Transition{
				{To: "A", When: []Condition{When("s", Equal, 0)}, Duration: 0.5},
				{To: "B", When: []Condition{When("s", Equal, 1)}, Duration: 0.5},
				{To: "C", When: []Condition{When("s", Equal, 2)}, Duration: 0.5},
			},
		}}},
	}
	in := mustProgram(t, g).New("m")
	in.Step(frameDT)
	in.SetFloat("s", 1)
	in.Step(frameDT)
	for i := 0; i < 15; i++ { // B halfway in
		in.Step(frameDT)
	}
	in.SetFloat("s", 2)
	// This step first advances the B fade to 16/30, then starts C, so the
	// snapshot is A = 14/30 and B = 16/30.
	in.Step(frameDT)
	for i := 0; i < 15; i++ { // C halfway in: A and B keep half their snapshot
		in.Step(frameDT)
	}
	weights := map[int]float64{}
	sum := 0.0
	for _, s := range in.Samples(0) {
		weights[s.Clip] += s.Weight
		sum += s.Weight
	}
	near(t, "weights sum to one", sum, 1, 1e-9)
	near(t, "C", weights[2], 0.5, 1e-9)
	near(t, "A", weights[0], 7.0/30, 1e-9)
	near(t, "B", weights[1], 8.0/30, 1e-9)
}

func TestCompileRejectsBadGraphs(t *testing.T) {
	cases := map[string]func(*Graph){
		"no layers":         func(g *Graph) { g.Layers = nil },
		"unknown clip":      func(g *Graph) { g.Layers[0].Machine.States[0].Clip = "Nope" },
		"unknown state":     func(g *Graph) { g.Layers[0].Machine.States[0].Transitions[0].To = "Nope" },
		"unknown param":     func(g *Graph) { g.Layers[0].Machine.States[0].Transitions[0].When[0].Param = "nope" },
		"trigger kind":      func(g *Graph) { g.Layers[0].Machine.Any[0].When[0].Param = "speed" },
		"zero duration":     func(g *Graph) { g.Clips[0].Duration = 0 },
		"duplicate clip":    func(g *Graph) { g.Clips[1].Name = "Idle" },
		"event out of clip": func(g *Graph) { g.Clips[1].Events[0].Time = 3 },
		"two motions":       func(g *Graph) { g.Layers[0].Machine.States[0].Blend1D = &BlendSpace1D{Param: "speed"} },
		"unknown mask":      func(g *Graph) { g.Layers[0].Mask = "upper" },
		"negative fade":     func(g *Graph) { g.Layers[0].Machine.States[0].Transitions[0].Duration = -1 },
		"nan exit":          func(g *Graph) { g.Layers[0].Machine.States[0].Transitions[0].ExitTime = math.NaN() },
		"unknown scheme":    func(g *Graph) { g.Rig.Humanoid = "maya" },
		"ik curve":          func(g *Graph) { g.IK = []TwoBoneIK{{Name: "l", Root: "a", Mid: "b", End: "c", WeightCurve: "nope"}} },
		"duplicate blend point": func(g *Graph) {
			g.Layers[0].Machine.States[1].Blend1D.Points[1].At = 1.4
		},
	}
	for name, mutate := range cases {
		g := locomotion()
		mutate(&g)
		if _, err := g.Compile(); err == nil {
			t.Errorf("%s: Compile accepted an invalid graph", name)
		}
	}
}

func TestRigJSONIsStableAndValid(t *testing.T) {
	g := locomotion()
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	g.Masks = []Mask{{Name: "upper", Include: []string{"spine"}, Exclude: []string{"leftUpperLeg"}, Weights: map[string]float64{"spine": 0.3, "chest": 0.6}}}
	g.Layers = append(g.Layers, Layer{Name: "aim \"quoted\"", Mask: "upper", Additive: true, Machine: StateMachine{States: []State{{Name: "Off"}}}})
	g.Rig = Rig{Humanoid: "mixamo", Bones: map[string]string{"hips": "mixamorig:Hips"}, Sources: []ClipSource{{Src: "/a.glb", Prefix: "m_"}}}
	g.Pelvis = Pelvis{MaxDrop: 0.4}
	a, b := mustProgram(t, g), mustProgram(t, g)
	if string(a.RigJSON()) != string(b.RigJSON()) || a.RigHash() != b.RigHash() || a.RigHash() == 0 {
		t.Fatal("rig JSON or hash is not stable")
	}
	var rig struct {
		Humanoid string            `json:"humanoid"`
		Bones    map[string]string `json:"bones"`
		Masks    []struct {
			Include []string           `json:"include"`
			Weights map[string]float64 `json:"weights"`
		} `json:"masks"`
		Layers []struct {
			Name     string `json:"name"`
			Mask     int    `json:"mask"`
			Additive bool   `json:"additive"`
		} `json:"layers"`
		IK []struct {
			End   string `json:"end"`
			Foot  bool   `json:"foot"`
			Align bool   `json:"align"`
		} `json:"ik"`
		LookAt []struct {
			Axis     []float64 `json:"axis"`
			MaxAngle float64   `json:"maxAngle"`
		} `json:"lookAt"`
		Pelvis struct {
			Bone    string  `json:"bone"`
			MaxDrop float64 `json:"maxDrop"`
		} `json:"pelvis"`
		Sources []struct {
			Humanoid string `json:"humanoid"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(a.RigJSON(), &rig); err != nil {
		t.Fatalf("rig JSON does not parse: %v\n%s", err, a.RigJSON())
	}
	if rig.Humanoid != "mixamo" || rig.Bones["hips"] != "mixamorig:Hips" || rig.Masks[0].Weights["chest"] != 0.6 {
		t.Fatalf("rig fields lost: %+v", rig)
	}
	if rig.Layers[0].Mask != -1 || rig.Layers[1].Mask != 0 || !rig.Layers[1].Additive || rig.Layers[1].Name != `aim "quoted"` {
		t.Fatalf("layer fields lost: %+v", rig.Layers)
	}
	if !rig.IK[0].Foot || !rig.IK[0].Align || rig.IK[2].Foot || rig.IK[2].End != "leftHand" {
		t.Fatalf("IK fields lost: %+v", rig.IK)
	}
	if !reflect.DeepEqual(rig.LookAt[0].Axis, []float64{0, 0, 1}) || rig.LookAt[0].MaxAngle != 1.2 || rig.Pelvis.Bone != "hips" || rig.Pelvis.MaxDrop != 0.4 {
		t.Fatalf("defaults lost: %+v %+v", rig.LookAt, rig.Pelvis)
	}
	if rig.Sources[0].Humanoid != "auto" {
		t.Fatalf("source scheme default lost: %+v", rig.Sources)
	}
}

// decodedFrame mirrors the TypeScript decoder for wire tests.
type decodedFrame struct {
	Models []decodedModel
}

type decodedModel struct {
	ID      string
	RigHash uint32
	DT      float32
	Layers  []decodedLayer
	IK      []decodedIK
	Look    []decodedLook
}

type decodedLayer struct {
	Weight  float32
	Samples []decodedSample
}

type decodedSample struct {
	Clip         string
	Time, Weight float32
	Loop         bool
}

type decodedIK struct {
	Chain  int
	Flags  byte
	Weight float32
	Values []float32
}

type decodedLook struct {
	Chain  int
	Weight float32
	Target [3]float32
}

func decodeFrame(t *testing.T, b []byte) decodedFrame {
	t.Helper()
	at := 0
	u8 := func() byte { v := b[at]; at++; return v }
	u16 := func() int { v := int(binary.LittleEndian.Uint16(b[at:])); at += 2; return v }
	u32 := func() uint32 { v := binary.LittleEndian.Uint32(b[at:]); at += 4; return v }
	f32 := func() float32 { return math.Float32frombits(u32()) }
	str := func() string { n := u16(); s := string(b[at : at+n]); at += n; return s }
	if string(b[:4]) != FrameMagic {
		t.Fatalf("magic %q", b[:4])
	}
	at = 4
	names := make([]string, u16())
	for i := range names {
		names[i] = str()
	}
	var f decodedFrame
	for m := u16(); m > 0; m-- {
		model := decodedModel{ID: str(), RigHash: u32(), DT: f32()}
		for l := u8(); l > 0; l-- {
			layer := decodedLayer{Weight: f32()}
			for s := u8(); s > 0; s-- {
				sample := decodedSample{Clip: names[u16()], Time: f32(), Weight: f32(), Loop: u8() == 1}
				layer.Samples = append(layer.Samples, sample)
			}
			model.Layers = append(model.Layers, layer)
		}
		for k := u8(); k > 0; k-- {
			ik := decodedIK{Chain: int(u8()), Flags: u8(), Weight: f32()}
			n := 0
			if ik.Flags&1 != 0 {
				n += 3
			}
			if ik.Flags&2 != 0 {
				n += 3
			}
			if ik.Flags&4 != 0 {
				n += 4
			}
			for ; n > 0; n-- {
				ik.Values = append(ik.Values, f32())
			}
			model.IK = append(model.IK, ik)
		}
		for k := u8(); k > 0; k-- {
			model.Look = append(model.Look, decodedLook{Chain: int(u8()), Weight: f32(), Target: [3]float32{f32(), f32(), f32()}})
		}
		f.Models = append(f.Models, model)
	}
	if at != len(b) {
		t.Fatalf("decoded %d of %d bytes", at, len(b))
	}
	return f
}

func TestFrameEncodingLayout(t *testing.T) {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	p := mustProgram(t, g)
	hero, other := p.New("hero"), p.New("npc-2")
	hero.SetFloat("speed", 2.7)
	hero.Step(0.25)
	other.Step(0.1)
	left := p.IKID("leftFoot")
	hero.SetIKWeight(left, 1)
	hero.SetGround(left, 0.2, [3]float64{0, 2, 0})
	hand := p.IKID("rightHand")
	hero.SetIKWeight(hand, 0.5)
	hero.SetIKTarget(hand, [3]float64{1, 1.5, 0.3})
	hero.SetIKPole(hand, [3]float64{1, 1, -1})
	hero.SetIKWeight(p.IKID("rightFoot"), 1) // no target or ground: not sent
	hero.SetLookAt(p.LookAtID("head"), [3]float64{0, 1.7, 5}, 0.75)
	enc := new(FrameEncoder)
	data, err := enc.Encode(hero, other)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, data)
	if len(f.Models) != 2 || f.Models[0].ID != "hero" || f.Models[1].ID != "npc-2" || f.Models[0].RigHash != p.RigHash() {
		t.Fatalf("models = %+v", f.Models)
	}
	h := f.Models[0]
	if h.DT != 0.25 || h.Layers[0].Weight != 1 || len(h.Layers[0].Samples) != 2 || h.Layers[0].Samples[0].Clip != "Walk" || !h.Layers[0].Samples[0].Loop {
		t.Fatalf("hero layer = %+v", h)
	}
	if len(h.IK) != 2 || h.IK[0].Chain != left || h.IK[0].Flags != 4 || !reflect.DeepEqual(h.IK[0].Values, []float32{0.2, 0, 1, 0}) {
		t.Fatalf("foot IK = %+v", h.IK)
	}
	if h.IK[1].Chain != hand || h.IK[1].Flags != 3 || h.IK[1].Weight != 0.5 || len(h.IK[1].Values) != 6 {
		t.Fatalf("hand IK = %+v", h.IK[1])
	}
	if len(h.Look) != 1 || h.Look[0].Weight != 0.75 || h.Look[0].Target != [3]float32{0, 1.7, 5} {
		t.Fatalf("look = %+v", h.Look)
	}
	if _, err := enc.Encode(hero, hero); err == nil {
		t.Fatal("duplicate model IDs encoded")
	}
	if _, err := enc.Encode(p.New("")); err == nil {
		t.Fatal("empty model ID encoded")
	}
}

func TestStepAndEncodeDoNotAllocate(t *testing.T) {
	g := locomotion()
	ik, look := Humanoid(true)
	g.IK, g.LookAt = ik, look
	g.Clips[1].Curves = append(g.Clips[1].Curves, Curve{Name: "rightFootIK", Keys: []CurveKey{{0, 0}, {1, 1}}})
	p := mustProgram(t, g)
	in := p.New("hero")
	enc := new(FrameEncoder)
	speed, jump := p.ParamID("speed"), p.ParamID("jump")
	step := 0
	run := func() {
		step++
		in.SetFloatID(speed, 2+math.Sin(float64(step)*0.05)*2)
		if step%90 == 0 {
			in.SetBoolID(jump, true)
		}
		in.SetGround(0, 0.1, [3]float64{0, 1, 0})
		in.Step(frameDT)
		if _, err := enc.Encode(in); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 600; i++ { // grow the event buffer and the encoder to peak size
		run()
	}
	if allocs := testing.AllocsPerRun(600, run); allocs != 0 {
		t.Fatalf("Step+Encode allocates %.2f times per frame", allocs)
	}
}

func TestFeedbackDecoding(t *testing.T) {
	vals := []float64{7, 1, 1, 1, 2, 3, 4, 5, 6, 0, 1.6, 0, 0, 0, 1, 9, 9, 9, 42}
	var f Feedback
	if !f.DecodeFeedback(vals) {
		t.Fatal("valid feedback rejected")
	}
	if f.RigHash != 7 || f.Animated[0] != [3]float64{1, 2, 3} || f.Solved[0] != [3]float64{4, 5, 6} ||
		f.LookForward[0] != [3]float64{0, 0, 1} || f.Root != [3]float64{9, 9, 9} || f.EvalMicros != 42 {
		t.Fatalf("feedback = %+v", f)
	}
	if pos, ok := f.AnimatedEnd(0); !ok || pos != [3]float64{1, 2, 3} {
		t.Fatal("AnimatedEnd lost the position")
	}
	if f.DecodeFeedback(vals[:10]) || f.Valid {
		t.Fatal("short feedback accepted")
	}
}

func BenchmarkStep(b *testing.B) {
	g := locomotion()
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	p := g.MustCompile()
	in := p.New("hero")
	speed := p.ParamID("speed")
	enc := new(FrameEncoder)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		in.SetFloatID(speed, 2+math.Sin(float64(i)*0.05)*2)
		in.Step(frameDT)
		_, _ = enc.Encode(in)
	}
}
