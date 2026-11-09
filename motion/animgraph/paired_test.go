package animgraph

import (
	"math"
	"testing"
)

// pairGraph is a minimal humanoid program with idle and the two paired
// clips, plus hand chains for contact IK.
func pairGraph(t *testing.T) *Program {
	t.Helper()
	ik, _ := Humanoid(false)
	return mustProgram(t, Graph{
		Clips: []Clip{
			{Name: "Idle", Duration: 2, Loop: true},
			{Name: "PairA", Duration: 1.2, Events: []Event{{Name: "contact", Time: 0.5}}},
			{Name: "PairB", Duration: 1.2},
			{Name: "Walk", Duration: 1, Loop: true},
			{Name: "Waddle", Duration: 0.5, Loop: true},
		},
		Layers: []Layer{
			{Name: "base", Machine: StateMachine{States: []State{{Name: "Idle", Clip: "Idle"}, {Name: "Walk", Clip: "Walk"}}}},
			{Name: "style", Additive: true, SyncLayer: "base", Machine: StateMachine{States: []State{{Name: "Waddle", Clip: "Waddle"}}}},
		},
		IK: ik,
	})
}

func pairClip() PairedClip {
	return PairedClip{
		Name: "front", Outcome: "stop", Contact: 0.5, BlendIn: 0.1, BlendOut: 0.2,
		Roles: [2]PairRole{
			{Name: "a", Clip: "PairA", Facing: 0, Path: []RootKey{{0, 0, -1.5}, {0.5, 0, -0.4}, {1.2, 0, 0.3}}},
			{Name: "b", Clip: "PairB", Facing: math.Pi, Path: []RootKey{{0, 0, 0.4}, {1.2, 0, 1.1}}},
		},
		IK: []ContactIK{{Role: 0, Chain: "rightHand", Bone: "leftShoulder", Offset: [3]float64{0, 0, 0.1}, In: 0.3, Full: 0.45, Out: 0.6, End: 0.8}},
	}
}

func poseNear(t *testing.T, label string, got, want Pose, tol float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > tol || math.Abs(got.Y-want.Y) > tol || math.Abs(got.Z-want.Z) > tol || math.Abs(angleDiff(got.Yaw, want.Yaw)) > tol {
		t.Fatalf("%s = %+v, want %+v", label, got, want)
	}
}

func TestPairedMoveMeetsAtTheDecidedPointAndTime(t *testing.T) {
	p := pairGraph(t)
	a, b := p.New("a"), p.New("b")
	clip := pairClip()
	start := PairStart{
		Poses: [2]Pose{{X: 10, Z: 5, Yaw: 0.3}, {X: 11, Z: 6.5, Yaw: 3.0}},
		Align: Pose{X: 10.5, Z: 6, Yaw: 0.2},
		In:    0.8,
	}
	move, err := StartPaired(&clip, a, b, start)
	if err != nil {
		t.Fatal(err)
	}
	// Before any step each root is where the simulation has it.
	poseNear(t, "role 0 at start", move.Root(0), start.Poses[0], 1e-9)
	poseNear(t, "role 1 at start", move.Root(1), start.Poses[1], 1e-9)
	var contactAt float64 = -1
	elapsed := 0.0
	contactEvents := 0
	for frame := 0; frame < 120 && !move.Done(); frame++ {
		move.Step(frameDT)
		a.Step(frameDT)
		b.Step(frameDT)
		elapsed += frameDT
		for _, e := range a.Events() {
			if e.Kind == Notify && e.Name == "contact" {
				contactEvents++
			}
		}
		if contactAt < 0 && a.Action(0).Time >= clip.Contact-1e-9 {
			contactAt = elapsed
			// At contact both roots sit at their authored contact offsets
			// from the decided alignment root: role 0 at (0, -0.4) facing
			// the alignment yaw, role 1 at its path point for 0.5 s,
			// (0, 0.4 + 0.7 * 0.5 / 1.2), turned half a circle.
			x, z := rotateYaw(0, -0.4, 0.2)
			poseNear(t, "role 0 at contact", move.Root(0), Pose{X: 10.5 + x, Z: 6 + z, Yaw: 0.2}, 1e-6)
			x, z = rotateYaw(0, 0.4+0.7*0.5/1.2, 0.2)
			poseNear(t, "role 1 at contact", move.Root(1), Pose{X: 10.5 + x, Z: 6 + z, Yaw: 0.2 + math.Pi}, 1e-6)
			ik := a.IK(p.IKID("rightHand"))
			if ik.BoneTarget != b || HumanoidBones[ik.TargetBone] != "leftShoulder" || a.IKWeight(p.IKID("rightHand")) != 1 {
				t.Fatalf("contact IK at contact = %+v weight %v", ik, a.IKWeight(p.IKID("rightHand")))
			}
		}
	}
	// The time warp plays 0.5 s of clip in the 0.8 s the simulation gave.
	if math.Abs(contactAt-start.In) > frameDT+1e-9 {
		t.Fatalf("contact landed at %.4f s, want %.4f s", contactAt, start.In)
	}
	if contactEvents != 1 {
		t.Fatalf("contact event fired %d times", contactEvents)
	}
	if !move.Done() || !move.Contacted() || a.Action(0).Playing || a.IK(p.IKID("rightHand")).BoneTarget != nil {
		t.Fatalf("move did not finish cleanly: done %v contacted %v action %+v", move.Done(), move.Contacted(), a.Action(0))
	}
	// After contact playback runs at rate 1: 0.7 s of clip after 0.8 s.
	if math.Abs(elapsed-(0.8+0.7)) > 3*frameDT {
		t.Fatalf("move lasted %.3f s, want about 1.5 s", elapsed)
	}
}

func TestPairedLibraryPicksByOutcomeAndApproach(t *testing.T) {
	lib := PairedLibrary{Clips: []PairedClip{
		{Name: "stopFront", Outcome: "stop", Approach: 0},
		{Name: "stopSide", Outcome: "stop", Approach: math.Pi / 2},
		{Name: "stopBehind", Outcome: "stop", Approach: math.Pi},
		{Name: "stopBehind2", Outcome: "stop", Approach: math.Pi - 0.05},
		{Name: "breakFront", Outcome: "break", Approach: 0},
	}}
	if c, ok := lib.Pick("stop", 1.4, 0); !ok || c.Name != "stopSide" {
		t.Fatalf("side approach picked %+v", c)
	}
	if c, ok := lib.Pick("break", 3, 0); !ok || c.Name != "breakFront" {
		t.Fatalf("only break clip not picked: %+v", c)
	}
	picks := map[string]bool{}
	for seed := uint32(0); seed < 4; seed++ {
		c, _ := lib.Pick("stop", -math.Pi+0.01, seed)
		picks[c.Name] = true
	}
	if !picks["stopBehind"] || !picks["stopBehind2"] || len(picks) != 2 {
		t.Fatalf("seeded ties picked %v", picks)
	}
	if _, ok := lib.Pick("fumble", 0, 0); ok {
		t.Fatal("picked a clip for an unknown outcome")
	}
}

func TestStartPairedValidates(t *testing.T) {
	p := pairGraph(t)
	a, b := p.New("a"), p.New("b")
	clip := pairClip()
	clip.Roles[1].Clip = "Nope"
	if _, err := StartPaired(&clip, a, b, PairStart{In: 1}); err == nil {
		t.Fatal("unknown role clip accepted")
	}
	clip = pairClip()
	clip.IK[0].Bone = "tail"
	if _, err := StartPaired(&clip, a, b, PairStart{In: 1}); err == nil {
		t.Fatal("non-canonical contact bone accepted")
	}
	clip = pairClip()
	if _, err := StartPaired(&clip, a, a, PairStart{In: 1}); err == nil {
		t.Fatal("one instance in both roles accepted")
	}
}

func TestActionOverlaysTheMachineAndBlendsBack(t *testing.T) {
	p := pairGraph(t)
	in := p.New("m")
	in.Step(frameDT)
	if !in.PlayAction("PairA", ActionOptions{BlendIn: 0.2, BlendOut: 0.2}) {
		t.Fatal("PlayAction failed")
	}
	for i := 0; i < 6; i++ { // 0.1 s: halfway through the blend-in
		in.Step(frameDT)
	}
	weights := map[int]float64{}
	for _, s := range in.Samples(0) {
		weights[s.Clip] += s.Weight
	}
	near(t, "action weight", weights[p.ClipID("PairA")], 0.5, 1e-9)
	near(t, "machine weight", weights[p.ClipID("Idle")], 0.5, 1e-9)
	for i := 0; i < 80; i++ {
		in.Step(frameDT)
	}
	if in.Action(0).Playing {
		t.Fatalf("action still playing after its 1.2 s clip: %+v", in.Action(0))
	}
	if s := in.Samples(0); len(s) != 1 || s[0].Clip != p.ClipID("Idle") || s[0].Weight != 1 {
		t.Fatalf("machine did not resume alone: %+v", s)
	}
	in.PlayAction("Walk", ActionOptions{Loop: true})
	in.Step(frameDT)
	in.StopAction(0, 0.1)
	for i := 0; i < 7; i++ {
		in.Step(frameDT)
	}
	if in.Action(0).Playing {
		t.Fatal("StopAction did not end a looping action")
	}
}

func TestSyncLayerFollowsTheLeaderPhase(t *testing.T) {
	p := pairGraph(t)
	in := p.New("m")
	for i := 0; i < 37; i++ {
		in.Step(frameDT)
	}
	// Idle is 2 s; the 0.5 s Waddle follows Idle's normalized time, so both
	// sit at the same fraction of their cycle.
	base, style := in.Samples(0)[0], in.Samples(1)[0]
	near(t, "synced fraction", style.Time/0.5, base.Time/2, 1e-9)
	if _, err := (Graph{
		Clips:  []Clip{{Name: "A", Duration: 1}},
		Layers: []Layer{{Name: "x", SyncLayer: "y", Machine: StateMachine{States: []State{{Name: "A", Clip: "A"}}}}},
	}).Compile(); err == nil {
		t.Fatal("sync to a missing layer compiled")
	}
}

func TestLODSchedulerLevelsRatesAndFades(t *testing.T) {
	p := pairGraph(t)
	near0, mid, far := p.New("near"), p.New("mid"), p.New("far")
	cam := LODCamera{FOVY: math.Pi / 3, ViewportHeight: 1080}
	// Projected height = 1.8 / d * 1080 / (2 tan 30deg) = 1683.6 / d pixels.
	chars := []LODTarget{
		{Inst: near0, Position: [3]float64{0, 0, 4}, Height: 1.8}, // 421 px: level 0
		{Inst: mid, Position: [3]float64{0, 0, 12}, Height: 1.8},  // 140 px: level 1
		{Inst: far, Position: [3]float64{0, 0, 100}, Height: 1.8}, // 17 px: level 3
	}
	s := &LODScheduler{}
	counts := map[*Instance]int{}
	for frame := 0; frame < 120; frame++ {
		for _, c := range chars {
			c.Inst.Step(frameDT)
		}
		for _, in := range s.Update(frameDT, cam, chars) {
			counts[in]++
			if _, err := EncodeFrame(in); err != nil {
				t.Fatal(err)
			}
		}
	}
	for in, want := range map[*Instance]int{near0: 0, mid: 1, far: 3} {
		if level, _ := in.LOD(); level != want {
			t.Fatalf("%s at level %d, want %d", in.ID(), level, want)
		}
	}
	// Two seconds: every frame, 30 Hz and 10 Hz.
	if counts[near0] != 120 || counts[mid] < 59 || counts[mid] > 61 || counts[far] < 19 || counts[far] > 21 {
		t.Fatalf("update counts near %d mid %d far %d, want 120, 60, 20", counts[near0], counts[mid], counts[far])
	}
	if far.lod.ik != 0 || far.lod.layerScale[1] != 0 || far.lod.detail != 2 || mid.lod.detail != 1 {
		t.Fatalf("far/mid reductions not applied: %+v / %+v", far.lod, mid.lod)
	}
	// Hysteresis: 210 px is below level 0's 220 px threshold but within 15%
	// of it, so the near character stays at level 0.
	chars[0].Position[2] = 1683.6 / 210
	s.Update(frameDT, cam, chars)
	if level, _ := near0.LOD(); level != 0 {
		t.Fatalf("210 px dropped to level %d despite hysteresis", level)
	}
	chars[0].Position[2] = 1683.6 / 150
	s.Update(frameDT, cam, chars)
	if level, _ := near0.LOD(); level != 1 {
		t.Fatalf("150 px stayed at level %d", level)
	}
	// IK fades over 0.25 s instead of switching off.
	in := p.New("fade")
	s2 := &LODScheduler{}
	target := []LODTarget{{Inst: in, Position: [3]float64{0, 0, 4}, Height: 1.8}}
	s2.Update(frameDT, cam, target)
	target[0].Position[2] = 60 // 28 px: level 3, no IK
	s2.Update(frameDT, cam, target)
	in.Step(0.1)
	near(t, "IK fade after 0.1 s", in.lod.ik, 0.6, 1e-9)
	// A full-detail budget of one keeps only the largest character at 0.
	both := []LODTarget{{Inst: p.New("x"), Position: [3]float64{0, 0, 3}, Height: 1.8}, {Inst: p.New("y"), Position: [3]float64{0, 0, 2}, Height: 1.8}}
	budget := &LODScheduler{FullBudget: 1}
	budget.Update(frameDT, cam, both)
	if lx, _ := both[0].Inst.LOD(); lx != 1 {
		t.Fatalf("budget left the smaller character at level %d", lx)
	}
	if ly, _ := both[1].Inst.LOD(); ly != 0 {
		t.Fatalf("budget demoted the largest character to level %d", ly)
	}
}

func TestFrameCarriesDetailBoneTargetsAndSkippedTime(t *testing.T) {
	p := pairGraph(t)
	a, b := p.New("a"), p.New("b")
	a.lod.detail = 2
	hand := p.IKID("rightHand")
	a.SetIKWeight(hand, 1)
	if !a.SetIKTargetBone(hand, b, "leftShoulder", [3]float64{0, 0.05, 0.1}) {
		t.Fatal("SetIKTargetBone failed")
	}
	a.Step(0.05)
	a.Step(0.05) // two steps since the last encode: dt 0.1
	b.Step(0.05)
	f := decodeFrame(t, mustEncode(t, b, a))
	m := f.Models[1]
	if m.ID != "a" || m.Flags != 2 || math.Abs(float64(m.DT)-0.1) > 1e-6 {
		t.Fatalf("model header = %+v", m)
	}
	if len(m.IK) != 1 || m.IK[0].Flags != 8 || len(m.IK[0].Values) != 0 || m.IK[0].Other != 0 || HumanoidBones[m.IK[0].Bone] != "leftShoulder" || m.IK[0].Offset != [3]float32{0, 0.05, 0.1} {
		t.Fatalf("bone target = %+v", m.IK)
	}
	// Without the other model in the frame the bone target is not sent.
	if g := decodeFrame(t, mustEncode(t, a)); len(g.Models[0].IK) != 0 {
		t.Fatalf("bone target sent without its model: %+v", g.Models[0].IK)
	}
}

func mustEncode(t *testing.T, models ...*Instance) []byte {
	t.Helper()
	data, err := EncodeFrame(models...)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), data...)
}
