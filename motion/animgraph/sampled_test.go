package animgraph

import (
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A sampled layer sends exactly the samples it was given, and Step does not
// advance them, fire clip events or run the layer's state machine.
func TestSampledLayerSendsTheGivenSamples(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	walk, run := p.ClipID("Walk"), p.ClipID("Run")
	want := []Sample{
		{Clip: walk, Time: 0.25, Weight: 0.75, Loop: true},
		{Clip: run, Time: 0.15, Weight: 0.25, Loop: true},
	}
	if !in.SetLayerSamples(0, want...) {
		t.Fatal("valid samples rejected")
	}
	if !in.LayerSampled(0) {
		t.Fatal("layer is not reported as sampled")
	}
	// A speed that would move the state machine from Idle to Move, and a step
	// long enough to cross both Walk footsteps.
	in.SetFloat("speed", 3)
	in.Step(1)
	got := in.Samples(0)
	if len(got) != len(want) {
		t.Fatalf("samples = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if events := in.Events(); len(events) != 0 {
		t.Fatalf("a sampled layer fired %d events: %+v", len(events), events)
	}
	if w := in.LayerWeight(0); w != 1 {
		t.Fatalf("layer weight = %v, want 1", w)
	}
	// The run-time weight still scales the layer.
	in.SetLayerWeight(0, 0.5)
	in.Step(0)
	if w := in.LayerWeight(0); w != 0.5 {
		t.Fatalf("layer weight after SetLayerWeight(0.5) = %v", w)
	}
	// The frame carries the same clip names, times and weights.
	in.SetLayerWeight(0, 1)
	in.Step(0)
	data, err := EncodeFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, data)
	layer := f.Models[0].Layers[0]
	if layer.Weight != 1 || len(layer.Samples) != 2 ||
		layer.Samples[0] != (decodedSample{Clip: "Walk", Time: 0.25, Weight: 0.75, Loop: true}) ||
		layer.Samples[1] != (decodedSample{Clip: "Run", Time: 0.15, Weight: 0.25, Loop: true}) {
		t.Fatalf("encoded layer = %+v", layer)
	}
}

// Releasing a sampled layer resumes the state machine where it stood.
func TestReleaseLayerSamplesResumesTheStateMachine(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	in.Step(0.5) // Idle, half a second in
	before := append([]Sample(nil), in.Samples(0)...)
	if !in.SetLayerSamples(0, Sample{Clip: p.ClipID("Jump"), Time: 0.4, Weight: 1}) {
		t.Fatal("valid sample rejected")
	}
	in.Step(2) // would have advanced Idle by two seconds
	in.ReleaseLayerSamples(0)
	if in.LayerSampled(0) {
		t.Fatal("layer still sampled after release")
	}
	after := in.Samples(0)
	if len(after) != len(before) || after[0] != before[0] {
		t.Fatalf("samples after release = %+v, want the pre-sampling state %+v", after, before)
	}
	if name := in.State(0).Name; name != "Idle" {
		t.Fatalf("state after release = %q, want Idle", name)
	}
}

func TestSetLayerSamplesRejectsBadArguments(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	walk := p.ClipID("Walk")
	in.Step(0.1)
	before := append([]Sample(nil), in.Samples(0)...)
	tooMany := make([]Sample, MaxLayerSamples+1)
	for i := range tooMany {
		tooMany[i] = Sample{Clip: walk, Weight: 1}
	}
	cases := map[string]func() bool{
		"layer below range": func() bool { return in.SetLayerSamples(-1, Sample{Clip: walk, Weight: 1}) },
		"layer above range": func() bool { return in.SetLayerSamples(1, Sample{Clip: walk, Weight: 1}) },
		"unknown clip":      func() bool { return in.SetLayerSamples(0, Sample{Clip: 99, Weight: 1}) },
		"negative clip":     func() bool { return in.SetLayerSamples(0, Sample{Clip: -1, Weight: 1}) },
		"negative time":     func() bool { return in.SetLayerSamples(0, Sample{Clip: walk, Time: -0.1, Weight: 1}) },
		"NaN time":          func() bool { return in.SetLayerSamples(0, Sample{Clip: walk, Time: math.NaN(), Weight: 1}) },
		"negative weight":   func() bool { return in.SetLayerSamples(0, Sample{Clip: walk, Weight: -1}) },
		"infinite weight":   func() bool { return in.SetLayerSamples(0, Sample{Clip: walk, Weight: math.Inf(1)}) },
		"too many samples":  func() bool { return in.SetLayerSamples(0, tooMany...) },
	}
	for name, call := range cases {
		if call() {
			t.Errorf("%s: accepted", name)
		}
		if in.LayerSampled(0) {
			t.Fatalf("%s: a rejected call left the layer sampled", name)
		}
		if got := in.Samples(0); len(got) != len(before) || got[0] != before[0] {
			t.Fatalf("%s: a rejected call changed the samples to %+v", name, got)
		}
	}
	// No samples is valid: the layer contributes nothing.
	if !in.SetLayerSamples(0) {
		t.Fatal("an empty sample list was rejected")
	}
	if w := in.LayerWeight(0); w != 0 {
		t.Fatalf("an empty sampled layer has weight %v, want 0", w)
	}
}

// Curves follow the samples a sampled layer holds. Walk's leftFootIK curve is
// 1 at 0 s, 0 at 0.5 s and 1 at 1 s, so it reads 0.5 at 0.25 s.
func TestSampledLayerDrivesCurves(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	walk := p.ClipID("Walk")
	in.SetLayerSamples(0, Sample{Clip: walk, Time: 0.25, Weight: 1, Loop: true})
	near(t, "curve at 0.25 s", in.Curve("leftFootIK"), 0.5, 1e-9)
	in.SetLayerSamples(0, Sample{Clip: walk, Time: 0.5, Weight: 1, Loop: true})
	near(t, "curve at 0.5 s", in.Curve("leftFootIK"), 0, 1e-9)
	in.Step(0.1)
	near(t, "curve after Step", in.Curve("leftFootIK"), 0, 1e-9)
}

func TestSetLayerSamplesDoesNotAllocate(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("hero")
	enc := new(FrameEncoder)
	walk, run := p.ClipID("Walk"), p.ClipID("Run")
	step := 0
	frame := func() {
		step++
		phase := float64(step%60) / 60
		in.SetLayerSamples(0,
			Sample{Clip: walk, Time: phase, Weight: 1 - phase, Loop: true},
			Sample{Clip: run, Time: phase * 0.6, Weight: phase, Loop: true})
		in.SetRoot(Pose{X: phase, Z: -phase, Yaw: phase})
		in.Step(frameDT)
		if _, err := enc.Encode(in); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 120; i++ {
		frame()
	}
	if allocs := testing.AllocsPerRun(600, frame); allocs != 0 {
		t.Fatalf("a sampled frame with a root allocates %.2f times", allocs)
	}
}

// A root set on an instance goes out with every frame, after the flags byte.
func TestFrameCarriesRootPlacement(t *testing.T) {
	p := mustProgram(t, locomotion())
	placed, free := p.New("placed"), p.New("free")
	placed.SetRoot(Pose{X: 12.5, Y: 0.25, Z: -40, Yaw: 1.5})
	placed.Step(0.1)
	free.Step(0.1)
	data, err := EncodeFrame(placed, free)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, data)
	if got := f.Models[0]; got.Flags != frameFlagRoot || got.Root != [4]float32{12.5, 0.25, -40, 1.5} || len(got.Layers) != 1 {
		t.Fatalf("placed model = %+v", got)
	}
	if got := f.Models[1]; got.Flags != 0 || got.Root != [4]float32{} {
		t.Fatalf("a model with no root carries flags %d and root %v", got.Flags, got.Root)
	}
	if pose, ok := placed.Root(); !ok || pose != (Pose{X: 12.5, Y: 0.25, Z: -40, Yaw: 1.5}) {
		t.Fatalf("Root() = %+v, %v", pose, ok)
	}
	// A non-finite placement is ignored and the last one stays.
	placed.SetRoot(Pose{X: math.NaN()})
	if pose, _ := placed.Root(); pose.X != 12.5 {
		t.Fatalf("a NaN placement replaced the root: %+v", pose)
	}
	placed.ClearRoot()
	if _, ok := placed.Root(); ok {
		t.Fatal("root still set after ClearRoot")
	}
	data, err = EncodeFrame(placed)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeFrame(t, data).Models[0]; got.Flags != 0 {
		t.Fatalf("flags after ClearRoot = %d", got.Flags)
	}
}

// A character whose pose the level-of-detail schedule holds this frame still
// sends its placement, and nothing else. The time since its last pose is kept
// for the frame that carries the pose.
func TestHeldPoseSendsPlacementOnly(t *testing.T) {
	g := locomotion()
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	p := mustProgram(t, g)
	in := p.New("far")
	in.SetIKWeight(p.IKID("leftFoot"), 1)
	in.SetGround(p.IKID("leftFoot"), 0, [3]float64{0, 1, 0})
	// 10 Hz for every character, so a 60 Hz frame is due once in six. The
	// level keeps IK, so a due pose carries the foot chain.
	sched := &LODScheduler{Levels: []LODLevel{{MinPixels: 0, UpdateHz: 10, IK: true}}}
	cam := LODCamera{Position: [3]float64{0, 2, 50}, FOVY: 1, ViewportHeight: 720}
	enc := new(FrameEncoder)
	var due, held int
	var heldDT, dueDT []float32
	for frame := 0; frame < 12; frame++ {
		x := float64(frame)
		in.SetRoot(Pose{X: x})
		in.Step(1.0 / 60)
		isDue := len(sched.Update(1.0/60, cam, []LODTarget{{Inst: in, Height: 1.8}})) == 1
		data, err := enc.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		m := decodeFrame(t, data).Models[0]
		if m.Root[0] != float32(x) {
			t.Fatalf("frame %d: root x = %v, want %v", frame, m.Root[0], x)
		}
		if isDue {
			due++
			dueDT = append(dueDT, m.DT)
			if m.Flags&frameFlagPlacementOnly != 0 || len(m.Layers) != 1 || len(m.IK) != 1 {
				t.Fatalf("frame %d: a due pose was sent as placement only: %+v", frame, m)
			}
			continue
		}
		held++
		heldDT = append(heldDT, m.DT)
		if m.Flags != frameFlagRoot|frameFlagPlacementOnly || len(m.Layers) != 0 || len(m.IK) != 0 || len(m.Look) != 0 {
			t.Fatalf("frame %d: a held pose sent more than its placement: %+v", frame, m)
		}
		// "GSA2", no clip names, one model, ID, rig hash, dt, flags, root.
		if want := 4 + 2 + 2 + (2 + len("far")) + 4 + 4 + 1 + 16; len(data) != want {
			t.Fatalf("frame %d: placement-only frame is %d bytes, want %d", frame, len(data), want)
		}
	}
	if due != 2 || held != 10 {
		t.Fatalf("12 frames at 60 Hz on a 10 Hz schedule: %d due, %d held; want 2 and 10", due, held)
	}
	for _, dt := range heldDT {
		if dt != 0 {
			t.Fatalf("a placement-only entry carries dt %v, want 0", dt)
		}
	}
	// The second due pose carries the six frames since the first.
	near(t, "dt of the second due pose", float64(dueDT[1]), 6.0/60, 1e-6)
}

// Without a root, a held instance has nothing to send: Encode writes its
// full pose as before, so callers that pass held instances keep working.
func TestHeldPoseWithoutRootEncodesInFull(t *testing.T) {
	p := mustProgram(t, locomotion())
	in := p.New("far")
	in.Step(0.1)
	in.lod.hold = true
	data, err := EncodeFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeFrame(t, data).Models[0]; m.Flags != 0 || len(m.Layers) != 1 {
		t.Fatalf("held instance with no root = %+v", m)
	}
}

// rootGoldenFrame encodes a fixed frame with one placed, sampled model and
// one placement-only model. The TypeScript decoder test reads the same bytes
// (client/js/scene3d-animation-graph.test.mjs).
func rootGoldenFrame(t *testing.T) []byte {
	t.Helper()
	p := mustProgram(t, locomotion())
	runner := p.New("runner")
	runner.SetLayerSamples(0,
		Sample{Clip: p.ClipID("Walk"), Time: 0.25, Weight: 0.75, Loop: true},
		Sample{Clip: p.ClipID("Run"), Time: 0.125, Weight: 0.25, Loop: true})
	runner.SetRoot(Pose{X: 3.5, Y: 0.125, Z: -42, Yaw: 0.75})
	runner.Step(0.03125)
	distant := p.New("distant")
	distant.SetRoot(Pose{X: -8, Z: 16.5, Yaw: -1.5})
	distant.Step(0.03125)
	distant.lod.hold = true
	data, err := EncodeFrame(runner, distant)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRootGoldenFrame(t *testing.T) {
	data := rootGoldenFrame(t)
	path := filepath.Join("testdata", "frame-root.golden.hex")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(hex.EncodeToString(data)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestRootGoldenFrame -update)", err)
	}
	if got := hex.EncodeToString(data); got != strings.TrimSpace(string(want)) {
		t.Fatalf("encoder output drifted from %s:\n got %s\nwant %s", path, got, strings.TrimSpace(string(want)))
	}
}
