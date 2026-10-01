package docs

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

//go:embed moments-budget.json
var beachMomentsBudgetJSON []byte

type beachMomentLimit struct {
	Nodes, JSONBytes int
}

func beachMomentsLimits(t *testing.T) (limits struct {
	Moments map[string]beachMomentLimit
	Program struct{ Nodes, JSONBytes, GzipBytes int }
}) {
	t.Helper()
	if err := json.Unmarshal(beachMomentsBudgetJSON, &limits); err != nil {
		t.Fatal(err)
	}
	return limits
}

func checkMomentBudget(t *testing.T, name string, nodes []scene.Node) []byte {
	t.Helper()
	limit := beachMomentsLimits(t).Moments[name]
	wire := momentWire(t, nodes)
	if len(nodes) != limit.Nodes || len(wire) > limit.JSONBytes {
		t.Fatalf("%s exceeded governance: %d nodes, %d JSON bytes", name, len(nodes), len(wire))
	}
	t.Logf("%s: %d nodes, %d JSON bytes", name, len(nodes), len(wire))
	return wire
}

func momentWire(t *testing.T, nodes []scene.Node) []byte {
	t.Helper()
	p := scene.Props{Graph: scene.NewGraph(nodes...)}
	ir := p.CanonicalIR()
	if err := ir.Validate(); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func momentMeshes(t *testing.T, name string, nodes []scene.Node) map[string]scene.Mesh {
	t.Helper()
	checkMomentBudget(t, name, nodes)
	meshes := make(map[string]scene.Mesh, len(nodes))
	for _, node := range nodes {
		mesh, ok := node.(scene.Mesh)
		if !ok || mesh.ID == "" {
			t.Fatalf("moment node must be a named mesh: %T", node)
		}
		if _, exists := meshes[mesh.ID]; exists {
			t.Fatalf("duplicate mesh %q", mesh.ID)
		}
		material, ok := mesh.Material.(scene.StandardMaterial)
		if !ok || material.Texture != "" || material.NormalMap != "" || material.EmissiveMap != "" {
			t.Fatalf("%s must use an asset-free PBR material", mesh.ID)
		}
		if geometry, ok := mesh.Geometry.(scene.BufferGeometry); ok {
			checkMomentGeometry(t, geometry)
		}
		meshes[mesh.ID] = mesh
	}
	return meshes
}

func checkMomentGeometry(t *testing.T, g scene.BufferGeometry) {
	t.Helper()
	if !g.Immutable || g.Revision != 1 || len(g.Positions)%3 != 0 || len(g.Positions) == 0 || len(g.Normals) != len(g.Positions) || len(g.Indices)%3 != 0 || len(g.Indices) == 0 {
		t.Fatal("invalid retained triangle geometry")
	}
	for _, values := range [][]float64{g.Positions, g.Normals} {
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatal("non-finite geometry")
			}
		}
	}
	for _, i := range g.Indices {
		if i < 0 || i >= len(g.Positions)/3 {
			t.Fatalf("invalid vertex index %d", i)
		}
	}
}

// Sample the approach at walking step scale, including an uphill look-ahead.
func checkBeachApproach(t *testing.T, path []scene.Vector3) {
	t.Helper()
	w := blackglassBeachWalk()
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		steps := int(math.Ceil(math.Hypot(b.X-a.X, b.Z-a.Z) / .15))
		for j := 0; j <= steps; j++ {
			f := float64(j) / float64(steps)
			x, z := a.X+(b.X-a.X)*f, a.Z+(b.Z-a.Z)*f
			h := beachgen.TerrainHeight(x, z, beachgen.Seed)
			if h < w.Water.Level-w.Water.MaxDepth {
				t.Fatalf("approach enters deep water at (%g,%g)", x, z)
			}
			dx, dz := (b.X-a.X)/float64(steps), (b.Z-a.Z)/float64(steps)
			span := .5 / math.Hypot(dx, dz)
			rise := beachgen.TerrainHeight(x+dx*span, z+dz*span, beachgen.Seed) - h
			if rise > .3 && rise > .5*math.Tan(w.MaxSlope*math.Pi/180) {
				t.Fatalf("approach climbs an unwalkable slope at (%g,%g)", x, z)
			}
			for _, c := range w.Colliders {
				if c.Kind == "box" {
					if math.Abs(h-c.Y) <= c.SizeY/2 {
						ox := math.Max(0, math.Abs(x-c.X)-c.SizeX/2)
						oz := math.Max(0, math.Abs(z-c.Z)-c.SizeZ/2)
						if math.Hypot(ox, oz) < .35 {
							t.Fatalf("approach crosses box collider at (%g,%g)", x, z)
						}
					}
					continue
				}
				radius := c.Radius
				if c.Kind == "sphere" {
					dy := h - c.Y
					if math.Abs(dy) > radius {
						continue
					}
					radius = math.Sqrt(radius*radius - dy*dy)
				}
				if c.Kind == "cylinder" && (h < c.Y || c.Height > 0 && h > c.Y+c.Height) {
					continue
				}
				if c.Kind != "box" && math.Hypot(x-c.X, z-c.Z) < radius+.35 {
					t.Fatalf("approach crosses %s collider at (%g,%g)", c.Kind, x, z)
				}
			}
		}
	}
}

func TestBeachBeaconConstructionAndTerrain(t *testing.T) {
	ground := beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed)
	heights := map[string]float64{"beacon-tower": beaconTower / 2, "beacon-band": beaconTower * .55,
		"beacon-gallery": beaconTower + .1, "beacon-lantern": beaconTower + .8, "beacon-roof": beaconTower + 1.8,
		"beacon-beam": beaconTower + .8, "beacon-beam-halo": beaconTower + .8}
	for _, period := range beachgen.Periods {
		t.Run(period, func(t *testing.T) {
			nodes := blackglassBeachBeacon(period)
			meshes := momentMeshes(t, "beacon", nodes)
			if !bytes.Equal(momentWire(t, nodes), momentWire(t, blackglassBeachBeacon(period))) {
				t.Fatal("beacon construction is not deterministic")
			}
			for id, h := range heights {
				m, ok := meshes[id]
				if !ok || m.Position.X != beaconX || m.Position.Z != beaconZ || math.Abs(m.Position.Y-ground-h) > 1e-9 {
					t.Fatalf("%s is not anchored to the terrain: %+v", id, m.Position)
				}
			}
			for _, id := range []string{"beacon-beam", "beacon-beam-halo"} {
				m := meshes[id]
				mat := m.Material.(scene.StandardMaterial)
				if m.Spin.Y != .45 || m.DepthWrite == nil || *m.DepthWrite || m.CastShadow || m.ReceiveShadow || mat.BlendMode != scene.BlendAdditive {
					t.Fatalf("%s must be a spinning additive beam without depth/shadow writes", id)
				}
				g := m.Geometry.(scene.BufferGeometry)
				if len(g.Positions)/3 != 28 || len(g.Indices)/3 != 48 {
					t.Fatal("beam exceeds its 12-sided opposed-cone budget")
				}
			}
			collider := blackglassBeachBeaconCollider()
			if collider.Y != ground || collider.X != beaconX || collider.Z != beaconZ || collider.Radius < 1.6 {
				t.Fatal("tower collider is not grounded")
			}
		})
	}
}

func TestBeachBeaconBrighterAtBlueHour(t *testing.T) {
	day := blackglassBeachBeacon(beachgen.PeriodGolden)
	blue := blackglassBeachBeacon(beachgen.PeriodBlue)
	for _, i := range []int{3, 5, 6} {
		d := day[i].(scene.Mesh).Material.(scene.StandardMaterial)
		b := blue[i].(scene.Mesh).Material.(scene.StandardMaterial)
		if i == 3 && b.Emissive <= d.Emissive || i != 3 && *b.Opacity <= *d.Opacity {
			t.Fatalf("blue hour is not brighter for %s", day[i].(scene.Mesh).ID)
		}
	}
}

func TestBeachBeaconCanBeSeenFromEasternShore(t *testing.T) {
	checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(29, 0, 7), scene.Vec3(32, 0, 2)})
}

func TestBeachMomentsIntegrated(t *testing.T) {
	for _, period := range beachgen.Periods {
		p := BlackglassBeachProgram("shore", period)
		wire := momentWire(t, p.Graph.Nodes)
		for _, id := range []string{"beacon-beam", "tide-pool-0", "tide-pool-rims", "wreck-ribs", "wreck-prow", "glass-trail-soles", "glass-trail-heels", "sun-grotto-arch", "sun-grotto-patch"} {
			if !bytes.Contains(wire, []byte(`"`+id+`"`)) {
				t.Fatalf("%s missing from beach program", id)
			}
		}
		if p.Controls != scene.ControlFirstPerson || p.Walk == nil || p.Environment.Ocean == nil {
			t.Fatal("moments must preserve the walking beach")
		}
		found := false
		for _, c := range p.Walk.Colliders {
			found = found || c == blackglassBeachBeaconCollider()
		}
		if !found {
			t.Fatal("beacon collider missing from walk")
		}
		full, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		gz := gzip.NewWriter(&compressed)
		if _, err := gz.Write(full); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s beach runtime props: %d bytes, %d gzip bytes", period, len(full), compressed.Len())
		// Nineteen moment nodes, eight existing scene nodes; unchanged
		// asset and runtime bundles. These limits ratchet the complete props.
		limit := beachMomentsLimits(t).Program
		if len(p.Graph.Nodes) != limit.Nodes || len(full) > limit.JSONBytes || compressed.Len() > limit.GzipBytes {
			t.Fatal("beach moments exceeded the complete scene budget")
		}
	}
}
