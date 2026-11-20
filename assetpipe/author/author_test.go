package author

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// legRig is a small test figure: a pelvis block and two legs of two joints,
// each a ring stack, with a clip that bends the left knee.
func legRig(t *testing.T) (*Skeleton, *Mesh) {
	t.Helper()
	sk := &Skeleton{}
	sk.Add("pelvis", "", Vec3{0, 1, 0})
	for _, s := range []struct {
		n string
		x float64
	}{{"l", .15}, {"r", -.15}} {
		sk.Add("leg"+s.n, "pelvis", Vec3{s.x, 1, 0})
		sk.Add("knee"+s.n, "leg"+s.n, Vec3{s.x, .55, 0})
		sk.Add("foot"+s.n, "knee"+s.n, Vec3{s.x, .05, 0})
	}
	m := &Mesh{}
	m.RingStack("hips", 0, 0, []Ring{{Vec3{0, .9, 0}, .25, .15}, {Vec3{0, 1.2, 0}, .25, .15}}, 16)
	for _, s := range []struct {
		n string
		x float64
	}{{"l", .15}, {"r", -.15}} {
		m.RingStack("thigh", sk.Must("leg"+s.n), 0, []Ring{{Vec3{s.x, .56, 0}, .07, .07}, {Vec3{s.x, .75, 0}, .09, .09}, {Vec3{s.x, .98, 0}, .1, .1}}, 12)
		m.RingStack("shin", sk.Must("knee"+s.n), 0, []Ring{{Vec3{s.x, .08, 0}, .05, .05}, {Vec3{s.x, .3, 0}, .06, .06}, {Vec3{s.x, .56, 0}, .07, .07}}, 12)
	}
	return sk, m
}

func bendClip(deg float64) Clip {
	return Clip{Name: "bend", Duration: 1, Keys: 5, Sample: func(t float64) Pose {
		return Pose{Rot: map[string]Quat{"kneel": Euler(-deg*t, 0, 0), "legl": Euler(deg*t/2, 0, 0)}, Root: Vec3{0, 1, 0}}
	}}
}

func writeRead(t *testing.T, m *Model) (*Rig, []byte) {
	t.Helper()
	d, err := m.Document()
	if err != nil {
		t.Fatal(err)
	}
	a := NewAtlas(1, 1, 4)
	a.Paint(0, func(int, int) (float64, float64, float64) { return 200, 100, 50 })
	if err := EmbedImage(d, a.PNG(), "image/png"); err != nil {
		t.Fatal(err)
	}
	glb, err := d.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	back, err := gltfedit.Parse(glb, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ReadRig(back)
	if err != nil {
		t.Fatal(err)
	}
	return r, glb
}

func TestEulerRoundTrip(t *testing.T) {
	for _, e := range []Vec3{{10, 20, 30}, {-60, 5, 80}, {0, -45, 0}, {120, 10, -20}} {
		got := EulerOf(Euler(e[0], e[1], e[2]))
		if got.Sub(e).Len() > 1e-9 {
			t.Errorf("EulerOf(Euler(%v)) = %v", e, got)
		}
	}
	if a := Euler(90, 0, 0).Angle(); math.Abs(a-90) > 1e-9 {
		t.Errorf("angle %v", a)
	}
}

func TestShellsAreClosedAndOutward(t *testing.T) {
	m := &Mesh{}
	m.Ellipsoid("ball", 0, 0, Vec3{0, 1, 0}, Vec3{.1, .2, .1}, 12, 7)
	m.RingStack("can", 0, 0, []Ring{{Vec3{1, 0, 0}, .1, .1}, {Vec3{1, .5, 0}, .1, .1}}, 10)
	m.Tube("rail", 0, 0, []Vec3{{0, 0, 0}, {0, .5, .1}, {0, 1, 0}}, .01, 6)
	if len(m.Shells) != 3 || m.Shells[0].End-m.Shells[0].First != 12*8 || m.Shells[2].End-m.Shells[2].First != 18 {
		t.Fatalf("shell ranges %+v", m.Shells)
	}
	comps, closed, inside := shellVolumes(m.P, m.Tri)
	if comps != 3 || closed != 2 || inside != 0 {
		t.Fatalf("components %d closed %d inside-out %d, want 3 2 0", comps, closed, inside)
	}
	// Flip the can's winding: the audit's volume test must see it.
	for i, tr := range m.Tri {
		if tr[0] >= m.Shells[1].First && tr[0] < m.Shells[1].End {
			m.Tri[i] = [3]int{tr[0], tr[2], tr[1]}
		}
	}
	if _, _, inside := shellVolumes(m.P, m.Tri); inside != 1 {
		t.Fatalf("flipped can: %d inside-out shells, want 1", inside)
	}
}

func TestAtlasGridInsetsCells(t *testing.T) {
	g := AtlasGrid{Cols: 4, Rows: 4, Inset: .07, Span: .86}
	uv := g.Map(5, 0, 1)
	if math.Abs(uv[0]-(1.07/4)) > 1e-12 || math.Abs(uv[1]-(1.93/4)) > 1e-12 {
		t.Fatalf("cell 5 corner = %v", uv)
	}
}

func TestModelRoundTripPosesMatch(t *testing.T) {
	sk, mesh := legRig(t)
	m := &Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(60)},
		Wrappers:  []string{"lateral", "heading"},
		NodeClips: []NodeClip{{Name: "x", Node: "lateral", Path: "translation", Times: []float64{0, 1}, Values: [][]float64{{-1, 0, 0}, {1, 0, 0}}}},
		Material:  Material{Name: "m", Roughness: .5}}
	r, _ := writeRead(t, m)
	if len(r.Names) != len(sk.Joints) || r.Names[2] != "kneel" || r.Parent[2] != 1 {
		t.Fatalf("joints %v parents %v", r.Names, r.Parent)
	}
	if len(r.Clips) != 1 || r.Clips[0].Name != "bend" {
		t.Fatalf("clips %+v (the wrapper clip drives no joint and is skipped)", r.Clips)
	}
	for k, tm := range r.Clips[0].Times() {
		want := sk.Posed(mesh, m.Clips[0].Sample(float64(k)/4))
		got := r.Posed(r.Local(r.Clips[0], tm))
		for i := range want {
			if d := want[i].Sub(got[i]).Len(); d > 1e-5 {
				t.Fatalf("key %d vertex %d off by %g", k, i, d)
			}
		}
	}
}

func TestAuditKneeNotchesAndBands(t *testing.T) {
	sk, mesh := legRig(t)
	opts := AuditOptions{Waist: [2]string{"", ""}, Legs: [2]string{"legl", "legr"},
		Bands: map[string]JointBand{"kneel": {Min: Vec3{-150, -10, -10}, Max: Vec3{5, 10, 10}}}}
	m := &Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(110)}}
	r, _ := writeRead(t, m)
	rigid, err := Audit(r, opts)
	if err != nil {
		t.Fatal(err)
	}
	knee := func(rep AuditReport) JointAudit {
		for _, j := range rep.Clips[0].Joints {
			if j.Name == "kneel" {
				return j
			}
		}
		t.Fatal("no kneel probe")
		return JointAudit{}
	}
	// The thigh's top ring sits below the hip, so nothing crosses; a rigid
	// 110 degree knee opens a notch between the two shells.
	if rigid.HipCrossing != 0 || rigid.Clips[0].Torn != 0 {
		t.Fatalf("rigid: hip %d torn %v", rigid.HipCrossing, rigid.Clips[0].Torn)
	}
	if open := knee(rigid).Open; open == 0 {
		t.Fatalf("rigid knee at 110 degrees should notch the surface: %v", open)
	}

	sk2, mesh2 := legRig(t)
	for _, s := range []string{"l", "r"} {
		Band{Parent: sk2.Must("leg" + s), Child: sk2.Must("knee" + s), Centre: sk2.Joints[sk2.Must("knee"+s)].Bind, Axis: Vec3{0, -1, 0}, Width: .1}.
			Apply(mesh2, mesh2.ShellsOn(sk2.Must("leg"+s), sk2.Must("knee"+s)))
	}
	for i, in := range mesh2.Skin {
		if s := in.Weight[0] + in.Weight[1] + in.Weight[2] + in.Weight[3]; math.Abs(s-1) > 1e-12 {
			t.Fatalf("vertex %d weights sum %v", i, s)
		}
	}
	// A gentle bend keeps the blended surface whole; a deep one folds the
	// inside of the knee, which the notch share reports.
	r2, _ := writeRead(t, &Model{Generator: "test", MeshName: "body", Skeleton: sk2, Mesh: mesh2, Clips: []Clip{bendClip(30)}})
	gentle, _ := Audit(r2, opts)
	if open := knee(gentle).Open; open != 0 {
		t.Fatalf("blended knee at 30 degrees notched in %v of directions", open)
	}
	if gentle.Clips[0].Torn != 0 || gentle.Clips[0].Longest > 1.5 {
		t.Fatalf("blended knee at 30 degrees: torn %v longest %v", gentle.Clips[0].Torn, gentle.Clips[0].Longest)
	}
	// A 110 degree knee is inside the band; 160 is not.
	if f := rigid.Faults(Limits{OutOfBand: true, Open: 1, Torn: 1}); len(f) != 0 {
		t.Fatalf("faults at 110 degrees: %v", f)
	}
	r3, _ := writeRead(t, &Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(160)}})
	over, _ := Audit(r3, opts)
	if f := over.Faults(Limits{OutOfBand: true, Open: 1, Torn: 1}); len(f) != 1 || !strings.Contains(f[0], "kneel x -160") {
		t.Fatalf("faults at 160 degrees: %v", f)
	}
}

func TestAuditFindsLegWeightAboveHipAndTornEdges(t *testing.T) {
	sk, mesh := legRig(t)
	// Bind a vertex of the hips' top ring to the left knee: it sits above
	// the hip and tears its edges when the knee bends.
	hips := mesh.Shells[0]
	v := hips.First + 16 + 4
	mesh.Skin[v] = Rigid(sk.Must("kneel"))
	r, _ := writeRead(t, &Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(90)}})
	rep, _ := Audit(r, AuditOptions{Legs: [2]string{"legl", "legr"}})
	if rep.HipCrossing != 1 {
		t.Fatalf("hip crossing %d, want 1", rep.HipCrossing)
	}
	c := rep.Clips[0]
	if c.Torn == 0 || c.Longest < 3 || (c.LongestEdge[0] != v && c.LongestEdge[1] != v) {
		t.Fatalf("torn %v longest %v edge %v, want the planted vertex %d", c.Torn, c.Longest, c.LongestEdge, v)
	}
	if f := rep.Faults(Limits{Torn: 0}); len(f) < 2 {
		t.Fatalf("faults %v", f)
	}
}

func TestBandFalloffAndChildReassignment(t *testing.T) {
	sk, mesh := legRig(t)
	l := sk.Must("legl")
	b := Band{Parent: 0, Child: l, Centre: Vec3{.15, .95, 0}, Axis: Vec3{0, -1, 0}, Width: .08,
		Falloff: func(p Vec3) float64 { return Smoothstep(p[0], 0, .12) }}
	b.Apply(mesh, mesh.ShellsOn(0, l))
	thigh := mesh.Shells[1]
	for i := thigh.First; i < thigh.End; i++ {
		if mesh.P[i][1] > 1.03 && mesh.Skin[i] != Rigid(0) {
			t.Fatalf("thigh vertex above the band keeps leg weight: %+v", mesh.Skin[i])
		}
	}
	hips := mesh.Shells[0]
	for i := hips.First; i < hips.End; i++ {
		if mesh.P[i][0] <= 0 && mesh.Skin[i] != Rigid(0) {
			t.Fatalf("hips vertex at x=%v across the midline took leg weight", mesh.P[i][0])
		}
	}
}

func TestLengthenMovesChildrenAndStretchesBone(t *testing.T) {
	sk, mesh := legRig(t)
	shin := mesh.Shells[2]
	before := mesh.P[shin.First]
	sk.Lengthen(mesh, "legl", 1.2, 0, nil)
	if got := sk.Joints[sk.Must("kneel")].Bind; math.Abs(got[1]-(1-.45*1.2)) > 1e-12 {
		t.Fatalf("knee at %v", got)
	}
	if d := mesh.P[shin.First].Sub(before); math.Abs(d[1]+.09) > 1e-12 {
		t.Fatalf("shin moved by %v, want -0.09", d)
	}
	thigh := mesh.Shells[1]
	// The thigh's bottom ring at .56 sits 44/45 of the way down the bone and
	// moves almost as far as the knee; its top at .98 barely moves.
	if y := mesh.P[thigh.First][1]; math.Abs(y-(.56-.09*.44/.45)) > 1e-12 {
		t.Fatalf("thigh bottom at %v", y)
	}
	if y := mesh.P[thigh.End-1][1]; y < .97 {
		t.Fatalf("thigh top at %v", y)
	}
}

func TestMorphTargetsAreWritten(t *testing.T) {
	sk, mesh := legRig(t)
	mesh.AddTarget("broad", .25, func(_ int, p Vec3) Vec3 { return Vec3{p[0] * .2, 0, p[2] * .2} })
	d, err := (&Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(30)}}).Document()
	if err != nil {
		t.Fatal(err)
	}
	g := d.Meshes[0]
	if len(g.Primitives[0].Targets) != 1 || len(g.Weights) != 1 || g.Weights[0] != .25 || !strings.Contains(string(g.Extras), `"broad"`) {
		t.Fatalf("mesh %+v", g)
	}
	vals, _, err := d.ReadAccessor(g.Primitives[0].Targets[0]["POSITION"])
	if err != nil || math.Abs(vals[0]-mesh.Targets[0].Delta[0][0]) > 1e-6 {
		t.Fatalf("target values %v %v", vals[:3], err)
	}
	if got := mesh.Morphed([]float64{1})[0]; got.Sub(mesh.P[0].Add(mesh.Targets[0].Delta[0])).Len() > 1e-12 {
		t.Fatalf("Morphed %v", got)
	}
}

func TestHex(t *testing.T) {
	c, err := Hex("#1d595a")
	if err != nil || c != (RGB{0x1d, 0x59, 0x5a}) || HexOf(c) != "#1d595a" {
		t.Fatalf("%v %v", c, err)
	}
	if _, err := Hex("1d595a"); err == nil {
		t.Fatal("accepted a colour without #")
	}
}

func TestASmallTargetIsWrittenSparse(t *testing.T) {
	sk, mesh := legRig(t)
	mesh.AddTarget("knob", 0, func(i int, p Vec3) Vec3 {
		if i == 3 {
			return Vec3{.01, 0, -.02}
		}
		return Vec3{}
	})
	d, err := (&Model{Generator: "test", MeshName: "body", Skeleton: sk, Mesh: mesh, Clips: []Clip{bendClip(30)}}).Document()
	if err != nil {
		t.Fatal(err)
	}
	index := d.Meshes[0].Primitives[0].Targets[0]["POSITION"]
	if a := d.Accessors[index]; len(a.Sparse) == 0 || a.BufferView != nil {
		t.Fatalf("target accessor %+v is not sparse", a)
	}
	vals, _, err := d.ReadAccessor(index)
	if err != nil || len(vals) != 3*len(mesh.P) || math.Abs(vals[9]-.01) > 1e-7 || math.Abs(vals[11]+.02) > 1e-7 || vals[0] != 0 {
		t.Fatalf("target values %v %v", vals[:12], err)
	}
}
