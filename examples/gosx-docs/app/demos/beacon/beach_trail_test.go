package docs

import (
	"bytes"
	"math"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBeachTrailConstructionAndTerrain(t *testing.T) {
	nodes := blackglassBeachTrail()
	wire := momentWire(t, nodes)
	if len(nodes) != 2 || len(wire) > 6_600 || !bytes.Equal(wire, momentWire(t, blackglassBeachTrail())) {
		t.Fatalf("trail must be deterministic and bounded: %d nodes, %d bytes", len(nodes), len(wire))
	}
	t.Logf("trail: %d nodes, %d JSON bytes", len(nodes), len(wire))
	for _, node := range nodes {
		b := node.(scene.InstancedMesh)
		if b.Count != 24 || len(b.Positions) != 24 || len(b.Rotations) != 24 || len(b.Scales) != 24 || b.CastShadow || !b.ReceiveShadow {
			t.Fatal("trail must use two small shadow-free primitive batches")
		}
		for i, p := range b.Positions {
			h := beachgen.TerrainHeight(p.X, p.Z, beachgen.Seed)
			if math.Abs(p.Y-h-.018) > .002 {
				t.Fatalf("print %d floats above or sinks beneath the terrain", i)
			}
			if i > 0 && (p.Z >= b.Positions[i-1].Z || math.Hypot(p.X-b.Positions[i-1].X, p.Z-b.Positions[i-1].Z) > .85) {
				t.Fatal("footprints must form a continuous trail toward the glass")
			}
		}
	}
	sole := nodes[0].(scene.InstancedMesh)
	first, last := sole.Positions[0], sole.Positions[sole.Count-1]
	if math.Hypot(first.X-.5, first.Z-22) > 2.5 || math.Hypot(last.X-beachgen.MonolithX, last.Z-beachgen.MonolithZ) > 2.5 {
		t.Fatal("trail must begin at the start view and end beside the monolith")
	}
	path := []scene.Vector3{scene.Vec3(.5, 0, 22)}
	path = append(path, sole.Positions...)
	checkBeachApproach(t, path)
}
