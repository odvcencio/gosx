package docs

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

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

func momentMeshes(t *testing.T, nodes []scene.Node, count, budget int) map[string]scene.Mesh {
	t.Helper()
	if len(nodes) != count {
		t.Fatalf("moment has %d nodes, want %d", len(nodes), count)
	}
	wire := momentWire(t, nodes)
	if len(wire) > budget {
		t.Fatalf("moment wire = %d bytes, budget = %d", len(wire), budget)
	}
	t.Logf("moment: %d nodes, %d JSON bytes", count, len(wire))
	meshes := make(map[string]scene.Mesh, count)
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

func TestBeachBeaconConstructionAndTerrain(t *testing.T) {
	ground := beachgen.TerrainHeight(beaconX, beaconZ, beachgen.Seed)
	heights := map[string]float64{"beacon-tower": beaconTower / 2, "beacon-band": beaconTower * .55,
		"beacon-gallery": beaconTower + .1, "beacon-lantern": beaconTower + .8, "beacon-roof": beaconTower + 1.8,
		"beacon-beam": beaconTower + .8, "beacon-beam-halo": beaconTower + .8}
	for _, period := range beachgen.Periods {
		t.Run(period, func(t *testing.T) {
			nodes := blackglassBeachBeacon(period)
			meshes := momentMeshes(t, nodes, 7, 5_200)
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

func TestBeachMomentsIntegrated(t *testing.T) {
	for _, period := range beachgen.Periods {
		p := BlackglassBeachProgram("shore", period)
		wire := momentWire(t, p.Graph.Nodes)
		if !bytes.Contains(wire, []byte(`"beacon-beam"`)) {
			t.Fatal("beacon missing from beach program")
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
		t.Logf("%s beach runtime props: %d bytes", period, len(full))
	}
}
