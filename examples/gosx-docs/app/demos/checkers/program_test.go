package checkers

import (
	"testing"

	checkermaterials "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers/materials"
	"m31labs.dev/gosx/scene"
)

func TestBoardCompositionShape(t *testing.T) {
	holes := boardHoles()
	if len(holes) != boardHoleCount {
		t.Fatalf("board holes = %d, want %d", len(holes), boardHoleCount)
	}
	seen := make(map[int]bool, len(holes))
	for _, hole := range holes {
		if seen[hole.ID] {
			t.Fatalf("duplicate hole ID %d", hole.ID)
		}
		seen[hole.ID] = true
	}

	camps := initialPieceCamps(holes)
	positions := make(map[[2]float64]bool, pieceCount)
	count := 0
	for player, camp := range camps {
		if len(camp) != 10 {
			t.Fatalf("player %d pieces = %d, want 10", player+1, len(camp))
		}
		for _, position := range camp {
			key := [2]float64{position.X, position.Z}
			if positions[key] {
				t.Fatalf("piece camps overlap at %+v", key)
			}
			positions[key] = true
			count++
		}
	}
	if count != pieceCount {
		t.Fatalf("pieces = %d, want %d", count, pieceCount)
	}
}

func TestShowcaseSceneUsesBoundedInstancing(t *testing.T) {
	props := ShowcaseScene()
	ir := props.SceneIR()
	if props.EventSignalNamespace != "checkers.pick" || props.ControlRotateDirection != "grab" {
		t.Fatalf("scene input contract is not enabled: namespace=%q rotate=%q", props.EventSignalNamespace, props.ControlRotateDirection)
	}
	if got := len(ir.InstancedMeshes); got != 3 {
		t.Fatalf("instanced mesh batches = %d, want 3 (sockets + two active camps)", got)
	}
	if got := ir.InstancedMeshes[0].Count; got != boardHoleCount {
		t.Fatalf("socket instances = %d, want %d", got, boardHoleCount)
	}
	total := 0
	for _, batch := range ir.InstancedMeshes {
		if batch.Pickable == nil || !*batch.Pickable {
			t.Fatalf("instanced batch %q must be explicitly pickable", batch.ID)
		}
	}
	for _, batch := range ir.InstancedMeshes[1:] {
		total += batch.Count
	}
	if total != activePieceCount {
		t.Fatalf("piece instances = %d, want %d", total, activePieceCount)
	}
}

func TestShowcaseSceneMaterialSelectorCompilesRealSelenaFamilies(t *testing.T) {
	for _, family := range checkermaterials.Families() {
		ir := ShowcaseSceneWithMaterial(string(family)).SceneIR()
		if len(ir.Objects) == 0 || ir.Objects[0].ShaderBackend != "selena" || ir.Objects[0].CustomVertexWGSL == "" || ir.Objects[0].CustomFragment == "" {
			t.Fatalf("%s board is not an active Selena material: %+v", family, ir.Objects)
		}
	}
}

func TestShowcaseSceneHasLayeredBoardDetail(t *testing.T) {
	ir := ShowcaseScene().SceneIR()
	ids := make(map[string]bool, len(ir.Objects))
	for _, object := range ir.Objects {
		ids[object.ID] = true
	}
	for _, id := range []string{"checkers-board-base", "checkers-pedestal", "checkers-outer-rim", "checkers-inner-fillet", "checkers-underglow"} {
		if !ids[id] {
			t.Errorf("layered board detail missing %q", id)
		}
	}
	if len(ir.Lights) < 4 || len(ir.PostEffects) < 4 {
		t.Fatalf("showcase lighting/post stack is too thin: lights=%d post=%d", len(ir.Lights), len(ir.PostEffects))
	}
}

func TestShowcaseNativePickingHitsSocketAndRejectsRoundCorner(t *testing.T) {
	props := ShowcaseScene()
	trace := scene.TraceGraph(props.Graph, scene.Ray{Origin: scene.Vec3(0, 5, 0), Direction: scene.Vec3(0, -1, 0)}, scene.PickableOnly())
	if trace.Closest == nil || trace.Closest.ID != "checkers-sockets" || trace.Closest.InstanceIndex == nil || *trace.Closest.InstanceIndex != 60 || trace.Closest.Method != "analytic-sphere" {
		t.Fatalf("center socket trace = %#v", trace)
	}
	corner := scene.TraceGraph(props.Graph, scene.Ray{Origin: scene.Vec3(4.2, 5, 4.2), Direction: scene.Vec3(0, -1, 0)}, scene.PickableOnly())
	if corner.Closest != nil {
		t.Fatalf("round-board corner produced false hit: %#v", corner.Closest)
	}
}
