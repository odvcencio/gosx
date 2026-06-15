package boardgpu

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestBoardTextJSWGSLMatchesGo guards the single source of truth for the glyph
// shader: the JS WebGPU renderer (16a) embeds a verbatim copy of boardTextWGSL
// as `var BOARD_TEXT_WGSL = "..."`, since the glyph quads are built JS-side and
// can't read the Go bundle. board_text.go is Selena-drift-tested
// (TestBoardTextStaticMaterialMatchesSelenaCompile), but nothing otherwise pins
// the JS copy to it — so an edit to board_text.sel/.go would silently leave the
// JS stale (wrong bindings/offsets → broken glyphs or a silent pipeline-create
// fallback). This test fails when the two diverge.
func TestBoardTextJSWGSLMatchesGo(t *testing.T) {
	jsPath := filepath.Join("..", "..", "client", "js", "bootstrap-src", "16a-scene-webgpu.js")
	data, err := os.ReadFile(jsPath)
	if err != nil {
		t.Skipf("16a source not available (%v); JS↔Go BoardText drift guard skipped", err)
	}
	re := regexp.MustCompile(`var BOARD_TEXT_WGSL = ("(?:[^"\\]|\\.)*");`)
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatal("BOARD_TEXT_WGSL literal not found in 16a-scene-webgpu.js — was it renamed?")
	}
	jsWGSL, err := strconv.Unquote(string(m[1]))
	if err != nil {
		t.Fatalf("could not unquote BOARD_TEXT_WGSL: %v", err)
	}
	if jsWGSL != boardTextWGSL {
		t.Fatalf("16a BOARD_TEXT_WGSL drifted from Go boardTextWGSL — regenerate the JS copy from render/boardgpu/board_text.go.\n--- JS (16a) ---\n%s\n--- Go (boardTextWGSL) ---\n%s", jsWGSL, boardTextWGSL)
	}
}
