package checkers

import "testing"

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
	ir := ShowcaseScene().SceneIR()
	if got := len(ir.InstancedMeshes); got != 3 {
		t.Fatalf("instanced mesh batches = %d, want 3 (sockets + two active camps)", got)
	}
	if got := ir.InstancedMeshes[0].Count; got != boardHoleCount {
		t.Fatalf("socket instances = %d, want %d", got, boardHoleCount)
	}
	total := 0
	for _, batch := range ir.InstancedMeshes[1:] {
		total += batch.Count
	}
	if total != activePieceCount {
		t.Fatalf("piece instances = %d, want %d", total, activePieceCount)
	}
}

func TestShowcaseSceneMaterialSelectorCompilesRealSelenaFamilies(t *testing.T) {
	for _, family := range []string{"imperial-jade", "carved-wood", "brushed-steel"} {
		ir := ShowcaseSceneWithMaterial(family).SceneIR()
		if len(ir.Objects) == 0 || ir.Objects[0].ShaderBackend != "selena" || ir.Objects[0].CustomVertexWGSL == "" || ir.Objects[0].CustomFragment == "" {
			t.Fatalf("%s board is not an active Selena material: %+v", family, ir.Objects)
		}
	}
}
