package animgraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// palmGraph is the humanoid graph with both hand chains declaring a palm.
func palmGraph() Graph {
	g := locomotion()
	g.Layers[0].Machine.Entry = "Move"
	ik, look := Humanoid(false)
	for i := range ik {
		if ik[i].Name == "leftHand" || ik[i].Name == "rightHand" {
			ik[i].PalmAxis = [3]float64{0, -1, 0}
			ik[i].MaxSwing, ik[i].MaxTwist, ik[i].ForearmTwist = 1.0, 0.6, 0.8
		}
	}
	g.IK, g.LookAt = ik, look
	return g
}

// The values below were taken from origin/main before this change: a graph
// that declares no palm must keep its rig JSON and its hash.
const (
	plainRigSHA256 = "df06cec53ef952929aea94850139cf6fea0409cd93f9d94984843e64f801a6d9"
	plainRigHash   = 4025116161
	plainRigLen    = 906
)

func plainRigGraph() Graph {
	g := locomotion()
	ik, look := Humanoid(false)
	g.IK, g.LookAt = ik, look
	g.Masks = []Mask{{Name: "upper", Include: []string{"spine"}, Exclude: []string{"leftUpperLeg"}, Weights: map[string]float64{"spine": 0.3}}}
	g.Rig = Rig{Humanoid: "mixamo", Bones: map[string]string{"hips": "mixamorig:Hips"}}
	g.Pelvis = Pelvis{MaxDrop: 0.4}
	return g
}

func TestRigJSONWithoutPalmIsUnchanged(t *testing.T) {
	p := mustProgram(t, plainRigGraph())
	sum := sha256.Sum256(p.RigJSON())
	if got := hex.EncodeToString(sum[:]); got != plainRigSHA256 || len(p.RigJSON()) != plainRigLen || uint64(p.RigHash()) != plainRigHash {
		t.Fatalf("rig JSON changed for a graph without a palm: sha256 %s len %d hash %d", got, len(p.RigJSON()), p.RigHash())
	}
	for _, key := range []string{`"palm"`, `"swing"`, `"twist"`, `"forearm"`} {
		if strings.Contains(string(p.RigJSON()), key) {
			t.Fatalf("rig JSON of a graph without a palm carries %s", key)
		}
	}
}

func TestRigJSONCarriesPalmWhenSet(t *testing.T) {
	p := mustProgram(t, palmGraph())
	var rig struct {
		IK []map[string]json.RawMessage `json:"ik"`
	}
	if err := json.Unmarshal(p.RigJSON(), &rig); err != nil {
		t.Fatal(err)
	}
	if len(rig.IK) != 4 {
		t.Fatalf("%d chains", len(rig.IK))
	}
	for _, chain := range rig.IK {
		var name string
		_ = json.Unmarshal(chain["name"], &name)
		_, hasPalm := chain["palm"]
		want := name == "leftHand" || name == "rightHand"
		for _, key := range []string{"palm", "swing", "twist", "forearm"} {
			if _, ok := chain[key]; ok != want {
				t.Errorf("chain %s: key %q present = %v, want %v", name, key, ok, want)
			}
		}
		if want && hasPalm {
			if string(chain["palm"]) != "[0,-1,0]" || string(chain["swing"]) != "1" || string(chain["twist"]) != "0.6" || string(chain["forearm"]) != "0.8" {
				t.Errorf("chain %s: palm %s swing %s twist %s forearm %s", name, chain["palm"], chain["swing"], chain["twist"], chain["forearm"])
			}
		}
	}
	// A palm axis that is not unit length is written as a unit vector.
	g := palmGraph()
	g.IK[2].PalmAxis = [3]float64{0, -3, 0}
	if !strings.Contains(string(mustProgram(t, g).RigJSON()), `"palm":[0,-1,0]`) {
		t.Fatal("palm axis not normalized in the rig JSON")
	}
}

func TestCompileRejectsInvalidPalm(t *testing.T) {
	cases := map[string]func(*TwoBoneIK){
		"nan axis":              func(c *TwoBoneIK) { c.PalmAxis = [3]float64{math.NaN(), 1, 0} },
		"negative swing":        func(c *TwoBoneIK) { c.MaxSwing = -1 },
		"infinite twist":        func(c *TwoBoneIK) { c.MaxTwist = math.Inf(1) },
		"negative forearm":      func(c *TwoBoneIK) { c.ForearmTwist = -0.1 },
		"limits without a palm": func(c *TwoBoneIK) { c.PalmAxis = [3]float64{}; c.MaxSwing = 0.5 },
	}
	for name, mutate := range cases {
		g := palmGraph()
		mutate(&g.IK[2])
		if _, err := g.Compile(); err == nil {
			t.Errorf("%s: Compile accepted an invalid palm declaration", name)
		}
	}
}

func TestFrameEncodingEndNormal(t *testing.T) {
	p := mustProgram(t, palmGraph())
	hand, plain := p.IKID("rightHand"), p.IKID("leftFoot")
	build := func(aim bool) *Instance {
		in := p.New("hero")
		in.Step(0.1)
		in.SetIKWeight(hand, 0.5)
		in.SetIKTarget(hand, [3]float64{1, 1.5, 0.3})
		in.SetIKPole(hand, [3]float64{1, 1, -1})
		if aim {
			in.SetIKEndNormal(hand, [3]float64{0, 3, 0}, 0.5)
		}
		return in
	}
	base, err := new(FrameEncoder).Encode(build(false))
	if err != nil {
		t.Fatal(err)
	}
	baseBytes := append([]byte(nil), base...)

	// Set and cleared, the frame is the frame without the aim.
	cleared := build(true)
	cleared.ClearIKEnd(hand)
	if got, _ := new(FrameEncoder).Encode(cleared); !bytes.Equal(got, baseBytes) {
		t.Fatal("a cleared aim changed the frame")
	}
	// A zero normal is no target; a chain with no palm ignores the setter.
	zero := build(false)
	zero.SetIKEndNormal(hand, [3]float64{}, 1)
	zero.SetIKEndNormal(plain, [3]float64{0, 1, 0}, 1)
	if got, _ := new(FrameEncoder).Encode(zero); !bytes.Equal(got, baseBytes) {
		t.Fatal("a zero normal, or a chain without a palm, changed the frame")
	}

	aimed, err := new(FrameEncoder).Encode(build(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(aimed) != len(baseBytes)+16 {
		t.Fatalf("aim added %d bytes, want 16", len(aimed)-len(baseBytes))
	}
	f := decodeFrame(t, aimed)
	var ik decodedIK
	for _, e := range f.Models[0].IK {
		if e.Chain == hand {
			ik = e
		}
	}
	if ik.Flags != 1|2|16 || ik.Weight != 0.5 || ik.EndNormal != [3]float32{0, 1, 0} || ik.EndWeight != 0.25 {
		t.Fatalf("hand entry = %+v (the wire weight is EndWeight x IKWeight, the normal a unit vector)", ik)
	}
	// The encoder still does not allocate with an aim set.
	in := build(true)
	enc := new(FrameEncoder)
	for i := 0; i < 8; i++ {
		in.Step(frameDT)
		if _, err := enc.Encode(in); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(200, func() {
		in.SetIKEndNormal(hand, [3]float64{0, 1, 0}, 0.7)
		in.Step(frameDT)
		if _, err := enc.Encode(in); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Step+Encode with an aim allocates %.2f times per frame", allocs)
	}
}

func TestSetIKEndNormalFadesWithTheReach(t *testing.T) {
	p := mustProgram(t, palmGraph())
	hand := p.IKID("rightHand")
	in := p.New("hero")
	in.SetIKWeight(hand, 1)
	in.SetIKEndNormal(hand, [3]float64{0, 0, 2}, 1)
	if got := in.IK(hand); !got.HasEnd || got.EndNormal != [3]float64{0, 0, 1} || got.EndWeight != 1 {
		t.Fatalf("IK = %+v", got)
	}
	in.SetIKEndNormal(hand, [3]float64{math.NaN(), 0, 1}, 1)
	if got := in.IK(hand); got.EndNormal != [3]float64{0, 0, 1} {
		t.Fatal("a non-finite normal replaced the stored one")
	}
	in.SetIKEndNormal(hand, [3]float64{0, 1, 0}, 3)
	if got := in.IK(hand); got.EndWeight != 1 {
		t.Fatalf("weight %v not clamped to 1", got.EndWeight)
	}
	in.ClearIKEnd(hand)
	if in.IK(hand).HasEnd {
		t.Fatal("ClearIKEnd left the aim on")
	}
}

// ---------------------------------------------------------------------------
// The reference step
// ---------------------------------------------------------------------------

type aimCase struct {
	Name         string     `json:"name"`
	EndWorld     [4]float64 `json:"endWorld"`
	ParentWorld  [4]float64 `json:"parentWorld"`
	RestLocal    [4]float64 `json:"restLocal"`
	PalmLocal    [3]float64 `json:"palmLocal"`
	TwistAxis    [3]float64 `json:"twistAxis"`
	Normal       [3]float64 `json:"normal"`
	Weight       float64    `json:"weight"`
	MaxSwing     float64    `json:"maxSwing"`
	MaxTwist     float64    `json:"maxTwist"`
	ForearmTwist float64    `json:"forearmTwist"`
	Local        [4]float64 `json:"local"`
	MidTwist     float64    `json:"midTwist"`
}

func (c *aimCase) run() {
	c.Local, c.MidTwist = AimEnd(c.EndWorld, c.ParentWorld, c.RestLocal, c.PalmLocal, c.TwistAxis, c.Normal, c.Weight,
		EndAimLimits{MaxSwing: c.MaxSwing, MaxTwist: c.MaxTwist, ForearmTwist: c.ForearmTwist})
}

// lcg is a small deterministic generator so the cases are the same on every
// machine.
type lcg uint64

func (l *lcg) next() float64 {
	*l = *l*6364136223846793005 + 1442695040888963407
	return float64(uint64(*l)>>11) / float64(1<<53)
}

func (l *lcg) unit() [3]float64 {
	for {
		v := [3]float64{l.next()*2 - 1, l.next()*2 - 1, l.next()*2 - 1}
		if n := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]); n > 0.2 && n < 1 {
			return [3]float64{v[0] / n, v[1] / n, v[2] / n}
		}
	}
}

func (l *lcg) quat(maxAngle float64) [4]float64 {
	return quatAxisAngle(l.unit(), (l.next()*2-1)*maxAngle)
}

// armCase builds a case whose animated end sits within about 0.5 rad of rest.
func armCase(r *lcg, name string, limits EndAimLimits, weight float64) aimCase {
	axis := r.unit()
	rest := r.quat(1.5)
	parent := r.quat(3)
	// The clip's wrist: rest plus a modest offset.
	local := quatMul(r.quat(0.4), rest)
	return aimCase{
		Name: name, ParentWorld: parent, RestLocal: rest, EndWorld: quatMul(parent, local),
		PalmLocal: r.unit(), TwistAxis: axis, Normal: r.unit(), Weight: weight,
		MaxSwing: limits.MaxSwing, MaxTwist: limits.MaxTwist, ForearmTwist: limits.ForearmTwist,
	}
}

// aimed returns the palm's world axis and the end bone's world rotation after
// the step, with the mid bone's twist applied to the parent.
func (c aimCase) aimed() (palm [3]float64, world [4]float64) {
	parent := quatMul(c.ParentWorld, quatAxisAngle(c.TwistAxis, c.MidTwist))
	world = quatMul(parent, c.Local)
	return quatRotate(world, c.PalmLocal), world
}

func angleBetween(a, b [3]float64) float64 {
	dot := a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
	cross := [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	return math.Atan2(math.Sqrt(cross[0]*cross[0]+cross[1]*cross[1]+cross[2]*cross[2]), dot)
}

// restDelta splits the end bone's final local rotation, relative to rest,
// into swing and twist about the chain axis.
func (c aimCase) restDelta() (swing, twist float64) {
	delta := quatMul(c.Local, quatConj(c.RestLocal))
	return swingTwistAngles(delta, c.TwistAxis)
}

func TestEndAimReference(t *testing.T) {
	r := lcg(7)
	wide := EndAimLimits{MaxSwing: math.Pi, MaxTwist: math.Pi}
	t.Run("meets the normal inside the limits", func(t *testing.T) {
		for i := 0; i < 200; i++ {
			c := armCase(&r, "wide", wide, 1)
			c.run()
			palm, _ := c.aimed()
			if a := angleBetween(palm, c.Normal); a > 1e-4 {
				t.Fatalf("case %d: palm is %g rad from the normal", i, a)
			}
		}
	})
	t.Run("end bone stays inside swing and twist", func(t *testing.T) {
		limits := EndAimLimits{MaxSwing: 0.5, MaxTwist: 0.3, ForearmTwist: 0.4}
		clamped := 0
		for i := 0; i < 400; i++ {
			c := armCase(&r, "narrow", limits, 1)
			c.run()
			swing, twist := c.restDelta()
			if swing > limits.MaxSwing+1e-9 || math.Abs(twist) > limits.MaxTwist+1e-9 {
				t.Fatalf("case %d: swing %g (max %g), twist %g (max %g)", i, swing, limits.MaxSwing, twist, limits.MaxTwist)
			}
			if swing > limits.MaxSwing-1e-6 {
				clamped++
			}
		}
		if clamped < 20 {
			t.Fatalf("only %d of 400 cases reached the swing limit: the cases do not exercise it", clamped)
		}
	})
	t.Run("excess twist goes to the mid bone, up to ForearmTwist", func(t *testing.T) {
		// Wide swing so only twist limits act; the palm is on the chain axis
		// side so a turn about the axis is the whole request.
		limits := EndAimLimits{MaxSwing: math.Pi, MaxTwist: 0.2, ForearmTwist: 0.5}
		axis := [3]float64{0, 0, 1}
		for _, want := range []float64{0.1, 0.4, 0.7, 1.0, -0.4, -0.9} {
			c := aimCase{
				ParentWorld: quatIdentity, RestLocal: quatIdentity, EndWorld: quatIdentity,
				PalmLocal: [3]float64{1, 0, 0}, TwistAxis: axis, Weight: 1,
				MaxSwing: limits.MaxSwing, MaxTwist: limits.MaxTwist, ForearmTwist: limits.ForearmTwist,
				Normal: quatRotate(quatAxisAngle(axis, want), [3]float64{1, 0, 0}),
			}
			c.run()
			_, twist := c.restDelta()
			wantEnd := math.Max(-limits.MaxTwist, math.Min(limits.MaxTwist, want))
			wantMid := math.Max(-limits.ForearmTwist, math.Min(limits.ForearmTwist, want-wantEnd))
			near(t, "end twist", twist, wantEnd, 1e-9)
			near(t, "mid twist", c.MidTwist, wantMid, 1e-9)
			palm, _ := c.aimed()
			reachable := math.Abs(want) <= limits.MaxTwist+limits.ForearmTwist
			if a := angleBetween(palm, c.Normal); reachable && a > 1e-9 {
				t.Fatalf("twist %g: palm %g rad from the normal", want, a)
			}
		}
	})
	t.Run("weight zero returns the input", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			c := armCase(&r, "zero", EndAimLimits{MaxSwing: 0.1, MaxTwist: 0.1, ForearmTwist: 0.1}, 0)
			c.run()
			in := quatMul(quatConj(c.ParentWorld), c.EndWorld)
			for k := range in {
				near(t, "local", c.Local[k], in[k], 1e-12)
			}
			if c.MidTwist != 0 {
				t.Fatalf("mid twist %g at weight 0", c.MidTwist)
			}
		}
	})
	t.Run("continuous in the weight", func(t *testing.T) {
		limits := EndAimLimits{MaxSwing: 1.2, MaxTwist: 0.5, ForearmTwist: 0.6}
		for i := 0; i < 40; i++ {
			base := armCase(&r, "weight", limits, 0)
			prev := base
			prev.run()
			// The animated pose is inside the limits (armCase offsets are
			// at most 0.4 rad from rest), so no step in the weight may
			// jump: 1/200 of the weight moves the rotation by about 1/200
			// of the turn, never by a fixed step.
			for k := 1; k <= 200; k++ {
				c := base
				c.Weight = float64(k) / 200
				c.run()
				if d := quatAngle(quatMul(c.Local, quatConj(prev.Local))); d > 0.05 {
					t.Fatalf("case %d weight %g: rotation jumped %g rad", i, c.Weight, d)
				}
				if d := math.Abs(c.MidTwist - prev.MidTwist); d > 0.05 {
					t.Fatalf("case %d weight %g: mid twist jumped %g", i, c.Weight, d)
				}
				prev = c
			}
		}
	})
	t.Run("a zero normal is no target", func(t *testing.T) {
		c := armCase(&r, "none", wide, 1)
		c.Normal = [3]float64{}
		c.run()
		in := quatMul(quatConj(c.ParentWorld), c.EndWorld)
		for k := range in {
			near(t, "local", c.Local[k], in[k], 1e-12)
		}
	})
}

func TestEndAimGolden(t *testing.T) {
	r := lcg(11)
	var cases []aimCase
	limitsets := []EndAimLimits{
		{MaxSwing: math.Pi, MaxTwist: math.Pi},
		{MaxSwing: 0.6, MaxTwist: 0.3, ForearmTwist: 0.5},
		{MaxSwing: 1.2, MaxTwist: 0.1, ForearmTwist: 1.0},
		{MaxSwing: 0.2, MaxTwist: 0.8},
	}
	for i := 0; i < 24; i++ {
		limits := limitsets[i%len(limitsets)]
		weight := []float64{1, 1, 0.5, 0.25, 0, 0.9}[i%6]
		c := armCase(&r, "case", limits, weight)
		c.Name = "case-" + string(rune('a'+i))
		c.run()
		cases = append(cases, c)
	}
	// An animated pose already past the limits, which the step pulls in.
	out := armCase(&r, "outside", EndAimLimits{MaxSwing: 0.1, MaxTwist: 0.05, ForearmTwist: 0.05}, 1)
	out.EndWorld = quatMul(out.ParentWorld, quatMul(quatAxisAngle(out.TwistAxis, 0.9), out.RestLocal))
	out.run()
	cases = append(cases, out)

	data, err := json.MarshalIndent(cases, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "ik_end_aim.golden.json")
	if *updateGolden {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestEndAimGolden -update)", err)
	}
	var want []aimCase
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(cases) {
		t.Fatalf("golden has %d cases, test builds %d", len(want), len(cases))
	}
	// The golden is read back with a tolerance, not byte for byte: the step is
	// float math and a compiler may fuse a multiply and an add on some chips.
	for i, c := range cases {
		w := want[i]
		for k := range c.Local {
			near(t, c.Name+" local", c.Local[k], w.Local[k], 1e-9)
			near(t, c.Name+" input", c.EndWorld[k], w.EndWorld[k], 1e-12)
		}
		near(t, c.Name+" mid twist", c.MidTwist, w.MidTwist, 1e-9)
	}
}

// TestGoldenFrameWithAim writes the bytes of a frame that carries a palm aim.
// The TypeScript decoder test reads the same bytes.
func TestGoldenFrameWithAim(t *testing.T) {
	p := mustProgram(t, palmGraph())
	in := p.New("hero")
	in.SetFloat("speed", 2.7)
	for i := 0; i < 15; i++ {
		in.Step(1.0 / 60)
	}
	hand := p.IKID("rightHand")
	in.SetIKWeight(hand, 0.5)
	in.SetIKTarget(hand, [3]float64{0.5, 1.25, 0.75})
	in.SetIKEndNormal(hand, [3]float64{0, 2, 0}, 0.5)
	data, err := EncodeFrame(in)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "frame-aim.golden.hex")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(hex.EncodeToString(data)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestGoldenFrameWithAim -update)", err)
	}
	if strings.TrimSpace(string(want)) != hex.EncodeToString(data) {
		t.Fatal("frame bytes changed; rerun with -update if the layout change is intended")
	}
}
