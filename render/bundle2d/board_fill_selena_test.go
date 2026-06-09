package bundle2d_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/render/bundle2d"
	"m31labs.dev/gosx/scene"
)

// TestBoardFillSelenaCompiles proves the board fill .sel compiles to valid WGSL
// (both stages) — the Selena authoring half of the board fill material. The
// emitted WGSL is what the 16a port attaches to the rect material's
// CustomVertexWGSL/CustomFragmentWGSL slots. Render verification happens in the
// 16a browser port (the native renderer is fixed-pipeline and ignores custom
// WGSL).
func TestBoardFillSelenaCompiles(t *testing.T) {
	if strings.TrimSpace(bundle2d.BoardFillSelenaSource) == "" {
		t.Fatal("BoardFillSelenaSource is empty (embed failed)")
	}
	mat, layout, err := scene.CompileSelenaMaterial(
		[]byte(bundle2d.BoardFillSelenaSource),
		scene.SelenaMaterialOptions{Material: "BoardFill"},
	)
	if err != nil {
		t.Fatalf("CompileSelenaMaterial(BoardFill): %v", err)
	}
	if layout.Material != "BoardFill" {
		t.Errorf("layout material = %q, want BoardFill", layout.Material)
	}
	if strings.TrimSpace(mat.VertexWGSL) == "" {
		t.Error("BoardFill emitted no vertex WGSL")
	}
	if strings.TrimSpace(mat.FragmentWGSL) == "" {
		t.Error("BoardFill emitted no fragment WGSL")
	}
	// The fill must expose baseColor so the per-rect color can ride a uniform.
	if !strings.Contains(strings.ToLower(mat.VertexWGSL+mat.FragmentWGSL), "basecolor") {
		t.Errorf("BoardFill WGSL does not reference baseColor:\n--- vertex ---\n%s\n--- fragment ---\n%s", mat.VertexWGSL, mat.FragmentWGSL)
	}
	t.Logf("BoardFill Selena → WGSL OK (vertex %d bytes, fragment %d bytes)", len(mat.VertexWGSL), len(mat.FragmentWGSL))
}
