package scene

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateCrowdFixture = flag.Bool("update-crowd-fixture", false, "rewrite client/js/testdata/scene3d-crowd-fixture.json")

// testCrowdBowl is a two-tier stadium bowl: a 24-section lower tier, a
// 24-section upper tier with a gap for a scoreboard end, plus a standing
// student section. Section colours alternate between two palettes.
func testCrowdBowl(seed uint32) Crowd {
	home := []CrowdColor{{Color: "#c8102e", Weight: 3}, {Color: "#ffffff", Weight: 1}}
	away := []CrowdColor{{Color: "#003087", Weight: 2}, {Color: "#ffc72c", Weight: 1}}
	lower := make([]CrowdSection, 24)
	for i := range lower {
		lower[i].Colors = home
		if i >= 18 {
			lower[i].Colors = away
		}
	}
	lower[3].Standing = true
	lower[3].Props = &CrowdPropMix{Towels: 0.4, Scarves: 0.2}
	upper := make([]CrowdSection, 20)
	for i := range upper {
		upper[i].Colors = home
	}
	return Crowd{
		ID:    "bowl",
		Seed:  seed,
		Props: CrowdPropMix{Lights: 0.05, Banners: 0.02},
		Stands: []CrowdStand{
			{ID: "lower", Shape: CrowdShapeBowl, HalfWidth: 60, HalfDepth: 42, CornerRadius: 26, Rows: 21, Sections: lower},
			{ID: "upper", Shape: CrowdShapeBowl, HalfWidth: 84, HalfDepth: 66, CornerRadius: 40, Rows: 14, FrontHeight: 14, LoopStart: 0.05, LoopEnd: 0.95, Sections: upper},
		},
	}
}

func mustCrowdLayout(t *testing.T, c Crowd) CrowdLayout {
	t.Helper()
	layout, err := c.Layout()
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func TestCrowdBowlLayoutShape(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(7))
	if n := len(layout.Seats); n < 28000 || n > 34000 {
		t.Fatalf("seats = %d, want about 30,000", n)
	}
	if len(layout.Sections) != 44 {
		t.Fatalf("sections = %d, want 44", len(layout.Sections))
	}
	for i, seat := range layout.Seats {
		if seat.Index != i {
			t.Fatalf("seat %d has index %d", i, seat.Index)
		}
		if i > 0 && seat.Section < layout.Seats[i-1].Section {
			t.Fatalf("seat %d: sections are not contiguous", i)
		}
		if seat.Wave < 0 || seat.Wave >= 1 {
			t.Fatalf("seat %d: wave %v out of [0,1)", i, seat.Wave)
		}
		// Every bowl fan faces the field: inward along the bowl's normal,
		// give or take the 4-degree yaw jitter. On the straight +X side the
		// normal is exactly -X.
		lookX, lookZ := math.Sin(seat.Yaw), math.Cos(seat.Yaw)
		r := math.Hypot(seat.Position.X, seat.Position.Z)
		if dot := (lookX*-seat.Position.X + lookZ*-seat.Position.Z) / r; dot < 0.5 {
			t.Fatalf("seat %d faces away from the field: dot %.3f", i, dot)
		}
		if seat.Stand == 0 && seat.Position.X > 0 && math.Abs(seat.Position.Z) < 10 && lookX > -math.Cos(5*math.Pi/180) {
			t.Fatalf("seat %d on the +X side looks (%.3f, %.3f), want -X", i, lookX, lookZ)
		}
	}
	for _, section := range layout.Sections {
		if section.Count == 0 {
			t.Fatalf("section %s is empty", section.ID)
		}
	}
}

func TestCrowdRowsRiseWithRake(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(1))
	for _, seat := range layout.Seats {
		front := 1.0
		rise := 0.42
		if seat.Stand == 1 {
			front = 14
		}
		if want := front + float64(seat.Row)*rise; math.Abs(seat.Position.Y-want) > 1e-9 {
			t.Fatalf("seat %d row %d: y = %v, want %v", seat.Index, seat.Row, seat.Position.Y, want)
		}
	}
}

func TestCrowdAislesSeparateSections(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(3))
	// Within one row, fans of neighbouring sections are at least an aisle
	// apart, and fans of the same section one seat pitch apart.
	type key struct{ stand, row int }
	last := map[key]CrowdSeat{}
	for _, seat := range layout.Seats {
		k := key{seat.Stand, seat.Row}
		prev, ok := last[k]
		last[k] = seat
		if !ok || prev.Section == seat.Section {
			continue
		}
		gap := math.Hypot(seat.Position.X-prev.Position.X, seat.Position.Z-prev.Position.Z)
		if gap < 1.2+0.5-1e-6 {
			t.Fatalf("row %d: sections %d and %d only %.3f m apart", seat.Row, prev.Section, seat.Section, gap)
		}
	}
}

func TestCrowdLayoutIsReproducible(t *testing.T) {
	a := mustCrowdLayout(t, testCrowdBowl(42))
	b := mustCrowdLayout(t, testCrowdBowl(42))
	if len(a.Seats) != len(b.Seats) {
		t.Fatalf("seat counts differ: %d and %d", len(a.Seats), len(b.Seats))
	}
	for i := range a.Seats {
		if a.Seats[i] != b.Seats[i] {
			t.Fatalf("seat %d differs between equal descriptions", i)
		}
	}
	c := mustCrowdLayout(t, testCrowdBowl(43))
	same := 0
	for i := range a.Seats {
		if i < len(c.Seats) && a.Seats[i].Seed == c.Seats[i].Seed {
			same++
		}
	}
	if same > 0 {
		t.Fatalf("%d seeds repeat under a different crowd seed", same)
	}
}

func TestCrowdFillKeepsOtherFansInPlace(t *testing.T) {
	full := testCrowdBowl(5)
	half := testCrowdBowl(5)
	half.Fill = 0.5
	a := mustCrowdLayout(t, full)
	b := mustCrowdLayout(t, half)
	ratio := float64(len(b.Seats)) / float64(len(a.Seats))
	if ratio < 0.47 || ratio > 0.53 {
		t.Fatalf("fill 0.5 kept %.3f of the fans", ratio)
	}
	bySeat := map[int]CrowdSeat{}
	for _, seat := range a.Seats {
		bySeat[seat.Seat] = seat
	}
	for _, seat := range b.Seats {
		was, ok := bySeat[seat.Seat]
		if !ok {
			t.Fatalf("seat %d appears only in the half-full crowd", seat.Seat)
		}
		if was.Position != seat.Position || was.Color != seat.Color || was.Prop != seat.Prop || was.Seed != seat.Seed {
			t.Fatalf("seat %d changed when other seats emptied", seat.Seat)
		}
	}
}

func TestCrowdPaletteFollowsWeights(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(11))
	counts := map[int][2]int{}
	for _, seat := range layout.Seats {
		if seat.Stand != 0 || seat.Section >= 18 {
			continue
		}
		c := counts[0]
		c[seat.Color]++
		counts[0] = c
	}
	c := counts[0]
	share := float64(c[0]) / float64(c[0]+c[1])
	if math.Abs(share-0.75) > 0.02 {
		t.Fatalf("home colour share = %.3f, want 0.75 (weights 3:1)", share)
	}
}

func TestCrowdPropMix(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(13))
	var student, other [5]int
	for _, seat := range layout.Seats {
		if seat.Section == 3 {
			student[seat.Prop]++
		} else {
			other[seat.Prop]++
		}
	}
	total := 0
	for _, n := range student {
		total += n
	}
	towels := float64(student[CrowdPropTowel]) / float64(total)
	scarves := float64(student[CrowdPropScarf]) / float64(total)
	if math.Abs(towels-0.4) > 0.06 || math.Abs(scarves-0.2) > 0.06 || student[CrowdPropLight] != 0 {
		t.Fatalf("student props: towels %.3f scarves %.3f lights %d", towels, scarves, student[CrowdPropLight])
	}
	total = 0
	for _, n := range other {
		total += n
	}
	if lights := float64(other[CrowdPropLight]) / float64(total); math.Abs(lights-0.05) > 0.01 {
		t.Fatalf("default lights share = %.3f, want 0.05", lights)
	}
}

func TestCrowdOpenBowlLeavesGap(t *testing.T) {
	layout := mustCrowdLayout(t, testCrowdBowl(17))
	for _, seat := range layout.Seats {
		if seat.Stand != 1 {
			continue
		}
		// The loop starts mid +X side; 5% each side of it is open, which on
		// this bowl clears the +X end around z = 0.
		if seat.Position.X > 80 && math.Abs(seat.Position.Z) < 10 {
			t.Fatalf("upper seat %d sits in the open end at (%.1f, %.1f)", seat.Index, seat.Position.X, seat.Position.Z)
		}
	}
}

func TestCrowdLineArcAndFloorShapes(t *testing.T) {
	crowd := Crowd{Seed: 9, Stands: []CrowdStand{
		{ID: "side", Shape: CrowdShapeLine, Start: Vector3{X: -30, Z: 20}, End: Vector3{X: 30, Z: 20}, Rows: 10},
		{ID: "curve", Shape: CrowdShapeArc, Center: Vector3{}, Radius: 25, StartAngle: math.Pi, EndAngle: 1.5 * math.Pi, Rows: 8},
		{ID: "pit", Shape: CrowdShapeFloor, Center: Vector3{Z: -5}, HalfWidth: 10, HalfDepth: 4, Density: 2, Facing: Vector3{Z: -40}},
	}}
	layout := mustCrowdLayout(t, crowd)
	var line, arc, floor int
	for _, seat := range layout.Seats {
		lookX, lookZ := math.Sin(seat.Yaw), math.Cos(seat.Yaw)
		switch seat.Stand {
		case 0:
			line++
			// Rows run away from the origin (+Z) and fans look back toward it.
			if seat.Position.Z < 20 || lookZ > -0.95 {
				t.Fatalf("line seat at z %.2f looks (%.2f, %.2f)", seat.Position.Z, lookX, lookZ)
			}
		case 1:
			arc++
			r := math.Hypot(seat.Position.X, seat.Position.Z)
			if r < 25 || r > 25+8*0.85 || seat.Position.X > 0 || seat.Position.Z > 0 {
				t.Fatalf("arc seat off the arc: r %.2f at (%.2f, %.2f)", r, seat.Position.X, seat.Position.Z)
			}
		case 2:
			floor++
			if seat.Position.Y != 0 || math.Abs(seat.Position.X) > 10 || seat.Position.Z < -9 || seat.Position.Z > -1 || lookZ > -0.8 {
				t.Fatalf("floor fan at (%.2f, %.2f, %.2f) looks %.2f", seat.Position.X, seat.Position.Y, seat.Position.Z, lookZ)
			}
		}
	}
	// 60 m at 0.5 m per seat, 10 rows; 20 x 8 m at 2 per square metre.
	if line != 1200 || floor < 300 || floor > 330 || arc < 300 {
		t.Fatalf("line %d arc %d floor %d", line, arc, floor)
	}
}

func TestCrowdValidation(t *testing.T) {
	many := []CrowdColor{{Color: "#111"}, {Color: "#222"}, {Color: "#333"}, {Color: "#444"}, {Color: "#555"}}
	cases := map[string]Crowd{
		"duplicate section": {Stands: []CrowdStand{{Start: Vector3{X: -5}, End: Vector3{X: 5}, Sections: []CrowdSection{{ID: "a"}, {ID: "a"}}}}},
		"five colours":      {Stands: []CrowdStand{{Start: Vector3{X: -5}, End: Vector3{X: 5}, Sections: []CrowdSection{{Colors: many}}}}},
		"unknown action":    {Stands: []CrowdStand{{Start: Vector3{X: -5}, End: Vector3{X: 5}, Sections: []CrowdSection{{Action: "moonwalk"}}}}},
		"short line":        {Stands: []CrowdStand{{Start: Vector3{X: 0}, End: Vector3{X: 0.1}}}},
		"unknown shape":     {Stands: []CrowdStand{{Shape: "zigzag", Start: Vector3{X: -5}, End: Vector3{X: 5}}}},
	}
	for name, crowd := range cases {
		if _, err := crowd.IR(); err == nil {
			t.Errorf("%s: IR accepted an invalid crowd", name)
		}
	}
	huge := Crowd{Stands: []CrowdStand{{Shape: CrowdShapeFloor, HalfWidth: 200, HalfDepth: 200, Density: 4}}}
	if _, err := huge.IR(); !errors.Is(err, ErrCrowdTooLarge) {
		t.Fatalf("a 640,000-fan floor gave %v, want ErrCrowdTooLarge", err)
	}
}

func TestCrowdEnvironmentWire(t *testing.T) {
	props := Props{Environment: Environment{Crowds: []Crowd{testCrowdBowl(1)}}}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Environment struct {
			Crowds []CrowdIR `json:"crowds"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Environment.Crowds) != 1 || decoded.Environment.Crowds[0].Seats < 28000 {
		t.Fatalf("crowd did not reach the wire: %+v", decoded.Environment.Crowds)
	}
	// About a kilobyte per section-dense crowd, not one record per fan.
	if len(data) > 20000 {
		t.Fatalf("a 30,000-fan crowd costs %d wire bytes", len(data))
	}
	legacyScene, _ := props.LegacyProps()["scene"].(map[string]any)
	legacyEnvironment, _ := legacyScene["environment"].(map[string]any)
	if crowds, _ := legacyEnvironment["crowds"].([]CrowdIR); len(crowds) != 1 {
		t.Fatalf("legacy props dropped the crowd: %#v", legacyEnvironment)
	}
	plain, err := json.Marshal(Props{}.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("crowds")) {
		t.Fatal("a scene without crowds carries a crowds field")
	}
}

// crowdFixture is the browser parity fixture: crowd-layout.ts must expand the
// same seats from the same IR (scene3d-crowd.test.mjs).
type crowdFixture struct {
	Actions  []CrowdActionInfo    `json:"actions"`
	Crowds   []crowdFixtureCrowd  `json:"crowds"`
	Hashes   map[string]uint32    `json:"hashes"`
	Sections []CrowdSectionLayout `json:"sections"`
	// Frame is a GSC1 frame Go encoded, and FrameFields what it holds.
	Frame       []byte     `json:"frame"`
	FrameFields CrowdFrame `json:"frameFields"`
}

type crowdFixtureCrowd struct {
	IR     CrowdIR     `json:"ir"`
	Seats  []CrowdSeat `json:"seats"`
	Count  int         `json:"count"`
	Stride int         `json:"stride"`
	Digest uint32      `json:"digest"`
}

// crowdSeatDigest is FNV-1a over every seat's integer fields, in order.
// scene3d-crowd.test.mjs computes the same digest over the browser layout.
func crowdSeatDigest(seats []CrowdSeat) uint32 {
	h := uint32(2166136261)
	mix := func(v uint32) {
		for k := 0; k < 4; k++ {
			h ^= v & 0xff
			h *= 16777619
			v >>= 8
		}
	}
	for _, seat := range seats {
		for _, v := range []uint32{uint32(seat.Seat), uint32(seat.Section), uint32(seat.Row), seat.Seed, uint32(seat.Color), uint32(seat.Accent), uint32(seat.Prop), uint32(seat.Variant), uint32(seat.Phase), uint32(seat.Eagerness)} {
			mix(v)
		}
	}
	return h
}

func crowdFixtureData(t *testing.T) []byte {
	t.Helper()
	small := Crowd{ID: "mixed", Seed: 2026, Fill: 0.8, Props: CrowdPropMix{Lights: 0.2, Scarves: 0.1, Towels: 0.1, Banners: 0.1}, Stands: []CrowdStand{
		{ID: "side", Shape: CrowdShapeLine, Start: Vector3{X: -12, Z: 15}, End: Vector3{X: 12, Z: 15}, Rows: 4, Sections: []CrowdSection{{Span: 2}, {Colors: []CrowdColor{{Color: "#123456"}}}}},
		{ID: "curve", Shape: CrowdShapeArc, Radius: 18, StartAngle: 3.5, EndAngle: 4.6, Rows: 3},
		{ID: "ring", Shape: CrowdShapeBowl, HalfWidth: 30, HalfDepth: 22, CornerRadius: 9, Rows: 3, LoopStart: 0.6, LoopEnd: 0.1, Sections: []CrowdSection{{}, {}, {}}},
		{ID: "pit", Shape: CrowdShapeFloor, Center: Vector3{X: 2, Z: -3}, HalfWidth: 4, HalfDepth: 2.5, Density: 1.5, Facing: Vector3{Z: -30}, Sections: []CrowdSection{{}, {Span: 0.5}}},
	}}
	fixture := crowdFixture{Actions: CrowdActions(), Hashes: map[string]uint32{}}
	for _, crowd := range []Crowd{small, testCrowdBowl(99)} {
		ir, err := crowd.IR()
		if err != nil {
			t.Fatal(err)
		}
		layout, err := ir.Layout()
		if err != nil {
			t.Fatal(err)
		}
		// Keep the fixture small: sampled seats for positions, and a digest
		// of every seat's discrete fields.
		stride := 7
		if len(layout.Seats) > 5000 {
			stride = 499
		}
		var seats []CrowdSeat
		for i := 0; i < len(layout.Seats); i += stride {
			seats = append(seats, layout.Seats[i])
		}
		fixture.Crowds = append(fixture.Crowds, crowdFixtureCrowd{IR: ir, Seats: seats, Count: len(layout.Seats), Stride: stride, Digest: crowdSeatDigest(layout.Seats)})
		if crowd.ID == "bowl" {
			fixture.Sections = layout.Sections
		}
	}
	director, err := NewCrowdDirector(testCrowdBowl(99))
	if err != nil {
		t.Fatal(err)
	}
	if err := director.SetExcitement(40, 0.75, 2, "lower-5"); err != nil {
		t.Fatal(err)
	}
	if err := director.Sync(41, CrowdClap, 96, "lower-5"); err != nil {
		t.Fatal(err)
	}
	if err := director.RaiseProps(41, false, "upper-2"); err != nil {
		t.Fatal(err)
	}
	director.StartWave(42, CrowdWave{Origin: 0.6, Speed: -0.09, Laps: 2})
	if err := director.SetPalette(42, []string{"#0a1b2c", "#d4e5f6"}, nil, "upper-7"); err != nil {
		t.Fatal(err)
	}
	fixture.FrameFields = director.Frame(42.5)
	if fixture.Frame, err = EncodeCrowdFrame(fixture.FrameFields); err != nil {
		t.Fatal(err)
	}
	for _, x := range []uint32{0, 1, 2, 0x9e3779b9, 0xffffffff, 123456789} {
		fixture.Hashes[strings.TrimSpace(jsonUint(x))] = crowdHash(x)
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func jsonUint(x uint32) string {
	data, _ := json.Marshal(x)
	return string(data)
}

// TestCrowdBrowserFixture keeps the browser parity fixture in step with the
// Go layout. Regenerate it with:
//
//	go test ./scene -run TestCrowdBrowserFixture -update-crowd-fixture
func TestCrowdBrowserFixture(t *testing.T) {
	path := filepath.Join("..", "client", "js", "testdata", "scene3d-crowd-fixture.json")
	want := crowdFixtureData(t)
	if *updateCrowdFixture {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with -update-crowd-fixture)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("client/js/testdata/scene3d-crowd-fixture.json is stale; regenerate with -update-crowd-fixture")
	}
}

func TestCrowdHashMatchesReference(t *testing.T) {
	// lowbias32 values computed independently (Python, 32-bit wrap).
	for in, want := range map[uint32]uint32{1: 0x688990c0, 0x9e3779b9: 0x01fce552} {
		if got := crowdHash(in); got != want {
			t.Fatalf("crowdHash(%#x) = %#x, want %#x", in, got, want)
		}
	}
	if crowdHash(0) != 0 {
		t.Fatalf("crowdHash(0) = %#x, want 0", crowdHash(0))
	}
}

func TestCrowdMaterialsLowerLikeMeshMaterials(t *testing.T) {
	plain, err := testCrowdBowl(1).IR()
	if err != nil {
		t.Fatal(err)
	}
	if plain.Material.Fan["color"] != "#ffffff" || plain.Material.Fan["roughness"] != 0.85 || plain.Material.Props["roughness"] != 0.7 || plain.Material.LightGlow != 1.5 {
		t.Fatalf("default materials: %+v", plain.Material)
	}
	crowd := testCrowdBowl(1)
	selena := instancedGLBSelenaMaterial()
	crowd.Material = CrowdMaterial{Fan: selena, Props: StandardMaterial{Color: "#eeeeee", Roughness: 0.4, Sheen: 0.6}, LightGlow: 3}
	ir, err := crowd.IR()
	if err != nil {
		t.Fatal(err)
	}
	if ir.Material.Fan["shaderBackend"] != "selena" || ir.Material.Fan["customFragment"] != selena.FragmentGLSL {
		t.Fatalf("the Selena fan material lost its shader record: %v", ir.Material.Fan["shaderBackend"])
	}
	if ir.Material.Props["sheen"] != 0.6 || ir.Material.LightGlow != 3 {
		t.Fatalf("prop material: %+v", ir.Material.Props)
	}
}
