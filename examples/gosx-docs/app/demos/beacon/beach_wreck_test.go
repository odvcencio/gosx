package docs

import (
	"bytes"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachWreckConstructionAndTerrain(t *testing.T) {
	nodes := blackglassBeachWreck()
	wire := momentWire(t, nodes)
	if len(nodes) != 3 || len(wire) > 4_700 || !bytes.Equal(wire, momentWire(t, blackglassBeachWreck())) {
		t.Fatalf("wreck must be deterministic and bounded: %d nodes, %d bytes", len(nodes), len(wire))
	}
	t.Logf("wreck: %d nodes, %d JSON bytes", len(nodes), len(wire))
	ribs := nodes[0].(scene.InstancedMesh)
	if ribs.ID != "wreck-ribs" || ribs.Count != 28 || len(ribs.Positions) != 28 || len(ribs.Scales) != 28 || len(ribs.Rotations) != 28 || ribs.Geometry.(scene.CylinderGeometry).Segments != 6 {
		t.Fatal("wreck ribs must use one bounded primitive batch")
	}
	for k, p := range ribs.Positions {
		length, angle := ribs.Scales[k].Y, ribs.Rotations[k].Z
		dx, dy := -math.Sin(angle)*length/2, math.Cos(angle)*length/2
		for j, endpoint := range []scene.Vector3{scene.Vec3(p.X-dx, p.Y-dy, p.Z), scene.Vec3(p.X+dx, p.Y+dy, p.Z)} {
			want := -.13
			if k%2 == 0 && j == 1 || k%2 == 1 && j == 0 {
				want = .65
			} else if k%2 == 1 && j == 1 {
				want = 1.8
			}
			h := beachgen.TerrainHeight(endpoint.X, endpoint.Z, beachgen.Seed)
			if math.Abs(endpoint.Y-h-want) > 1e-9 {
				t.Fatalf("rib %d end %d is not anchored to the dune", k, j)
			}
		}
	}
	keel, prow := nodes[1].(scene.Mesh), nodes[2].(scene.Mesh)
	if math.Abs(keel.Position.Y-beachgen.TerrainHeight(keel.Position.X, keel.Position.Z, beachgen.Seed)+.08) > 1e-9 ||
		math.Abs(prow.Position.Y-beachgen.TerrainHeight(prow.Position.X, prow.Position.Z, beachgen.Seed)-1.25) > 1e-9 {
		t.Fatal("keel and prow must be half-buried in the terrain")
	}
	if len(blackglassBeachWreckColliders()) != 15 {
		t.Fatal("wreck's solid ribs and prow need walk colliders")
	}
}

func TestBeachWreckCanBeEnteredFromStern(t *testing.T) {
	checkBeachApproach(t, []scene.Vector3{scene.Vec3(.5, 0, 22), scene.Vec3(-10, 0, 29), scene.Vec3(wreckX, 0, 29), scene.Vec3(wreckX, 0, 23.3)})
}
