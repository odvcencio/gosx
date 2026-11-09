package scene

import (
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"math"
	"reflect"
	"strings"
	"testing"
)

// scatterGolden is the FNV-64a digest of testScatter(42).Placements(). It pins
// the layout across platforms; update it only on an intended change.
const scatterGolden = 0x1b8f4c2efd73430b

func testScatterTerrain() *Terrain {
	return &Terrain{Heightmap: GenerateHeightmap(TerrainNoise{Seed: 9, Columns: 129, Rows: 129, Island: 0.6, Ridged: 0.3}),
		Width: 256, HeightScale: 40, Position: Vector3{Y: -8}}
}

func testScatter(seed uint64) Scatter {
	water := 0.0
	return Scatter{
		ID: "veg", Terrain: testScatterTerrain(), Seed: seed, WaterLevel: &water, WaterMargin: 0.5,
		Paths: []ScatterPath{{Points: []Vector3{{X: -100, Z: 0}, {X: 100, Z: 10}}, Width: 6}},
		Species: []ScatterSpecies{
			{Name: "grass", Density: 0.4, MaxSlope: 30, MinScale: 0.7, MaxScale: 1.3, Align: 1, Wind: 0.8, WindHeight: 0.6,
				LODs: []ScatterLOD{
					{Distance: 40, Geometry: ConeGeometry(0.15, 0.6), Material: &StandardMaterial{Color: "#4f7a2a"}, OffsetY: 0.3},
					{Distance: 80, Geometry: BoxGeometry{Width: 0.2, Height: 0.5, Depth: 0.2}, Material: &StandardMaterial{Color: "#4f7a2a"}, OffsetY: 0.25, Keep: 0.5},
				}},
			{Name: "tree", Density: 0.01, MinHeight: Float(2), MaxSlope: 35, MinScale: 0.8, MaxScale: 1.4, Sink: 0.3, CastShadow: true,
				LODs:      []ScatterLOD{{Distance: 120, Geometry: ConeGeometry(1.5, 6), Material: &StandardMaterial{Color: "#2f5a2a"}, OffsetY: 3}},
				Billboard: &ScatterBillboard{Distance: 400, Width: 3, Height: 6, Material: &StandardMaterial{Color: "#2f5a2a"}}},
			{Name: "rock", Density: 0.004, MinSlope: 20, LODs: []ScatterLOD{{Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, OffsetY: 0.3}}},
		},
	}
}

// ConeGeometry is a test helper for a cone of radius r and height h.
func ConeGeometry(r, h float64) Geometry {
	return CylinderGeometry{RadiusTop: 0.0001, RadiusBottom: r, Height: h, Segments: 6}
}

func TestScatterSeedReproducesPlacementExactly(t *testing.T) {
	a := testScatter(42).Placements()
	b := testScatter(42).Placements()
	if len(a) < 1000 {
		t.Fatalf("only %d placements; the test terrain should hold thousands", len(a))
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same seed produced different placements")
	}
	other := testScatter(43).Placements()
	if reflect.DeepEqual(a, other) {
		t.Fatal("a different seed produced the same placements")
	}
	// The lowered wire is identical too.
	wire := func(seed uint64) string {
		data, err := json.Marshal(Graph{Nodes: []Node{testScatter(seed)}}.SceneIR())
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if wire(42) != wire(42) {
		t.Fatal("the same seed lowered to different wire bytes")
	}
	sum := fnv.New64a()
	for _, p := range a {
		for _, v := range []float64{float64(p.Species), p.Position.X, p.Position.Y, p.Position.Z, p.Yaw, p.Scale, p.Up.X, p.Up.Y, p.Up.Z} {
			_ = binary.Write(sum, binary.LittleEndian, math.Float64bits(v))
		}
	}
	if got := sum.Sum64(); got != scatterGolden {
		t.Fatalf("placement digest = %#x, want %#x", got, uint64(scatterGolden))
	}
}

func TestScatterMasks(t *testing.T) {
	s := testScatter(7)
	placements := s.Placements()
	counts := map[int]int{}
	for _, p := range placements {
		counts[p.Species]++
		sample := s.Terrain.Sample(p.Position.X, p.Position.Z)
		species := s.Species[p.Species]
		if sample.Height < 0.5 {
			t.Fatalf("%s placed in the water at %+v (ground %.2f)", species.Name, p.Position, sample.Height)
		}
		if scatterOnPath(s.Paths, p.Position.X, p.Position.Z) {
			t.Fatalf("%s placed on the path at %+v", species.Name, p.Position)
		}
		if species.MinHeight != nil && sample.Height < *species.MinHeight {
			t.Fatalf("%s below its minimum height at %+v", species.Name, p.Position)
		}
		maxSlope := species.MaxSlope
		if maxSlope == 0 {
			maxSlope = 90
		}
		if sample.Slope > maxSlope+1e-6 || sample.Slope < species.MinSlope-1e-6 {
			t.Fatalf("%s on a %.2f degree slope, outside %v..%v", species.Name, sample.Slope, species.MinSlope, maxSlope)
		}
		if math.Abs(p.Position.Y-(sample.Height-species.Sink)) > 1e-9 {
			t.Fatalf("%s not on the ground: y %v, ground %v", species.Name, p.Position.Y, sample.Height)
		}
		if species.MinScale > 0 && (p.Scale < species.MinScale || p.Scale > species.MaxScale) {
			t.Fatalf("%s scale %v outside %v..%v", species.Name, p.Scale, species.MinScale, species.MaxScale)
		}
	}
	if counts[0] == 0 || counts[1] == 0 || counts[2] == 0 {
		t.Fatalf("a species placed nothing: %v", counts)
	}
	// A mask that is zero in the west half empties it.
	s.Mask = func(x, z float64) float64 {
		if x < 0 {
			return 0
		}
		return 1
	}
	for _, p := range s.Placements() {
		if p.Position.X < 0 {
			t.Fatalf("masked half holds an instance at %+v", p.Position)
		}
	}
}

func TestScatterDensityTracksArea(t *testing.T) {
	flat := &Terrain{Heightmap: &Heightmap{Columns: 2, Rows: 2, Data: []uint16{0, 0, 0, 0}}, Width: 100}
	s := Scatter{Terrain: flat, Seed: 1, Species: []ScatterSpecies{{Density: 2, LODs: []ScatterLOD{{Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}}}}}}
	got := len(s.Placements())
	if math.Abs(float64(got)-20000) > 300 {
		t.Fatalf("density 2 over 100x100 m placed %d, want about 20000", got)
	}
}

func TestScatterLowersBandedCellBatches(t *testing.T) {
	ir := Graph{Nodes: []Node{testScatter(42)}}.SceneIR()
	if len(ir.InstancedMeshes) == 0 {
		t.Fatal("scatter lowered no batches")
	}
	placements := testScatter(42).Placements()
	perSpecies := map[string]int{}
	bands := map[string][][2]float64{}
	rocks := 0
	for _, m := range ir.InstancedMeshes {
		parts := strings.Split(m.ID, "/")
		if len(parts) != 4 || parts[0] != "veg" {
			t.Fatalf("batch ID %q does not follow scatter/species/cell/level", m.ID)
		}
		switch parts[1] {
		case "rock":
			rocks++
			if m.LODGroup != "" {
				t.Fatalf("an unbanded species carries LOD fields: %+v", m.LODGroup)
			}
		case "grass", "tree":
			if m.LODGroup == "" || m.LODMaxDistance <= m.LODMinDistance {
				t.Fatalf("banded batch %q has band %v..%v", m.ID, m.LODMinDistance, m.LODMaxDistance)
			}
			bands[m.LODGroup] = append(bands[m.LODGroup], [2]float64{m.LODMinDistance, m.LODMaxDistance})
		}
		if parts[3] == "0" {
			perSpecies[parts[1]] += m.Count
		}
		if m.Count >= 2 && (len(m.CompressedTransforms) == 0 || m.TransformStride != 16 || m.Transforms != nil) {
			t.Fatalf("batch %q is not compressed", m.ID)
		}
		if parts[1] == "grass" && (m.Wind == nil || m.Wind.Sway != 0.8 || m.Wind.Height != 0.6) {
			t.Fatalf("grass batch %q lost its wind: %+v", m.ID, m.Wind)
		}
	}
	if rocks != 1 {
		t.Fatalf("rocks lowered to %d batches, want one batch for an unbanded species", rocks)
	}
	// Level 0 holds every placement of its species.
	want := map[string]int{}
	species := testScatter(42).Species
	for _, p := range placements {
		want[species[p.Species].Name]++
	}
	for name, n := range perSpecies {
		if n != want[name] {
			t.Fatalf("%s level 0 holds %d instances, placements hold %d", name, n, want[name])
		}
	}
	// Bands in one cell tile [0, far) without overlap.
	for group, list := range bands {
		for i := 1; i < len(list); i++ {
			if list[i][0] != list[i-1][1] {
				t.Fatalf("%s bands %v are not contiguous", group, list)
			}
		}
	}
	// The billboard level draws two crossed cards per tree.
	for _, m := range ir.InstancedMeshes {
		if strings.HasPrefix(m.ID, "veg/tree/") && strings.HasSuffix(m.ID, "/1") {
			level0 := strings.TrimSuffix(m.ID, "/1") + "/0"
			for _, n := range ir.InstancedMeshes {
				if n.ID == level0 && m.Count != 2*n.Count {
					t.Fatalf("billboard %q holds %d cards for %d trees", m.ID, m.Count, n.Count)
				}
			}
		}
	}
}

func TestScatterTransformsDecodeNearPlacements(t *testing.T) {
	s := testScatter(5)
	ir := Graph{Nodes: []Node{s}}.SceneIR()
	var batch *InstancedMeshIR
	for i := range ir.InstancedMeshes {
		if strings.HasPrefix(ir.InstancedMeshes[i].ID, "veg/rock/") {
			batch = &ir.InstancedMeshes[i]
		}
	}
	if batch == nil || batch.Count < 2 {
		t.Fatal("no rock batch")
	}
	values := reinterleaveFloat64(DecompressFloat64Array(batch.CompressedTransforms), 16)
	var rocks []ScatterInstance
	for _, p := range s.Placements() {
		if p.Species == 2 {
			rocks = append(rocks, p)
		}
	}
	if len(values) != 16*len(rocks) {
		t.Fatalf("decoded %d floats for %d rocks", len(values), len(rocks))
	}
	for i, rock := range rocks {
		want := scatterMatrix(rock, 0.3, false, 0)
		for k := 0; k < 16; k++ {
			// 8-bit lanes over a 256 m terrain: translation steps are about 1 m,
			// basis steps about 1/128.
			tolerance := 0.02
			if k >= 12 {
				tolerance = 1.1
			}
			if math.Abs(values[i*16+k]-want[k]) > tolerance {
				t.Fatalf("rock %d lane %d decodes to %v, want %v", i, k, values[i*16+k], want[k])
			}
		}
	}
}

func TestScatterWithoutTerrainPlacesNothing(t *testing.T) {
	if got := (Scatter{Species: []ScatterSpecies{{Density: 1}}}).Placements(); got != nil {
		t.Fatalf("placed %d instances without a terrain", len(got))
	}
	if ir := (Graph{Nodes: []Node{&Scatter{}}}).SceneIR(); len(ir.InstancedMeshes) != 0 {
		t.Fatal("an empty scatter lowered batches")
	}
}
