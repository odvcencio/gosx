package boardgpu

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

// TestBoardTextSelenaSourceEmbedded guards the embed: an empty source means the
// //go:embed failed.
func TestBoardTextSelenaSourceEmbedded(t *testing.T) {
	if strings.TrimSpace(BoardTextSelenaSource) == "" {
		t.Fatal("BoardTextSelenaSource is empty (embed failed)")
	}
	if !strings.Contains(BoardTextSelenaSource, "material BoardText") {
		t.Errorf("BoardTextSelenaSource missing the BoardText material declaration")
	}
}

// TestBoardTextStaticMaterialMatchesSelenaCompile pins the static browser-runtime
// glyph shader payload to the Selena compiler output, so editing board_text.sel
// without regenerating the static WGSL/layout fails CI (mirrors BoardFill).
func TestBoardTextStaticMaterialMatchesSelenaCompile(t *testing.T) {
	material, _, err := scene.CompileSelenaMaterial(
		[]byte(BoardTextSelenaSource),
		scene.SelenaMaterialOptions{Material: "BoardText"},
	)
	if err != nil {
		t.Fatalf("CompileSelenaMaterial(BoardText): %v", err)
	}
	if material.VertexWGSL != boardTextStaticMaterial.VertexWGSL {
		t.Fatalf("static vertex WGSL drifted from Selena output\n--- got (Selena) ---\n%s\n--- want (static) ---\n%s", material.VertexWGSL, boardTextStaticMaterial.VertexWGSL)
	}
	if material.FragmentWGSL != boardTextStaticMaterial.FragmentWGSL {
		t.Fatalf("static fragment WGSL drifted from Selena output")
	}
	got, err := json.Marshal(boardTextStaticMaterial.ShaderLayout)
	if err != nil {
		t.Fatalf("marshal static shader layout: %v", err)
	}
	want, err := json.Marshal(material.ShaderLayout)
	if err != nil {
		t.Fatalf("marshal compiled shader layout: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("static shader layout drifted from Selena output\ngot:  %s\nwant: %s", got, want)
	}
}

// TestBoardTextCompiledReturnsStatic verifies the accessor returns the pinned
// material (the glyph pass / bundle attach reads it through this seam).
func TestBoardTextCompiledReturnsStatic(t *testing.T) {
	m, err := boardTextCompiled()
	if err != nil {
		t.Fatalf("boardTextCompiled: %v", err)
	}
	if m.ShaderBackend != "selena" || strings.TrimSpace(m.FragmentWGSL) == "" {
		t.Fatalf("boardTextCompiled returned an empty/non-selena material: %+v", m)
	}
}
