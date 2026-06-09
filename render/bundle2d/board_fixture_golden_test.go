package bundle2d_test

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/render/bundle2d"
)

// The board-bundle wire fixtures under testdata/ are the Go↔JS golden
// contract for the 16a WebGPU board path: the SAME bytes are asserted here
// against bundle2d's marshal output and consumed by the node tests in
// client/js/runtime.test.js (createBoardWebGPUHarness drives the real chunked
// 16a renderer with them). If the wire shape changes intentionally, regenerate
// with
//
//	GOSX_UPDATE_BOARD_FIXTURES=1 go test ./render/bundle2d/ -run TestBoardGPUBundleGolden
//
// and re-run the js suite — the node tests assert renderer behavior against
// the fresh bytes, so a regenerated fixture that breaks them is a REAL
// regression, not a stale golden.

// boardFixtureRectsNodes is the two-rect board behind
// testdata/board_fixture_rects.json (zoom 0.5 keeps the rects un-culled; see
// the runtime.test.js fixture banner).
func boardFixtureRectsNodes() []gosx.CanvasBoardNode {
	return []gosx.CanvasBoardNode{
		{ID: "card-a", Kind: "rect", X: 16, Y: 24, Width: 200, Height: 120, Color: "#3a86ff"},
		{ID: "card-b", Kind: "rect", X: 280, Y: 60, Width: 160, Height: 90, Color: "#ffbe0b"},
	}
}

// boardFixtureMixedNodes is the mixed board (1 rect + 2 lines + 1 label + 1
// sprite) behind testdata/board_fixture_mixed.json — it pins the M1 slice-2
// deferral: line/label/sprite payloads ride the wire but only the rect draws.
func boardFixtureMixedNodes() []gosx.CanvasBoardNode {
	return []gosx.CanvasBoardNode{
		{ID: "card-a", Kind: "rect", X: 16, Y: 24, Width: 200, Height: 120, Color: "#3a86ff"},
		{ID: "edge-1", Kind: "line", X1: 216, Y1: 84, X2: 280, Y2: 105, Color: "#8d99ae"},
		{ID: "edge-2", Kind: "line", X1: 0, Y1: 0, X2: 50, Y2: 50, Color: "#ef233c"},
		{ID: "title", Kind: "label", X: 20, Y: 20, Color: "#edf2f4", Text: "Board"},
		{ID: "logo", Kind: "image", X: 30, Y: 40, Width: 32, Height: 32, Src: "/logo.png"},
	}
}

// TestBoardGPUBundleGolden locks the GPU board bundle wire bytes:
// MarshalCanvasBundle(ComputeCanvasGPUBundleWithBackground(nodes, "#102030",
// 640, 480, 0.5, 0, 0)) must equal the shared testdata fixture byte-for-byte.
// This is what guarantees the node-side renderer tests exercise EXACTLY what
// Go emits — including the Selena BoardFill material fields attached by
// AttachBoardGPUGeometry.
func TestBoardGPUBundleGolden(t *testing.T) {
	cases := []struct {
		golden string
		nodes  []gosx.CanvasBoardNode
	}{
		{golden: "board_fixture_rects.json", nodes: boardFixtureRectsNodes()},
		{golden: "board_fixture_mixed.json", nodes: boardFixtureMixedNodes()},
	}
	for _, tc := range cases {
		t.Run(tc.golden, func(t *testing.T) {
			bundle := bundle2d.ComputeCanvasGPUBundleWithBackground(tc.nodes, "#102030", 640, 480, 0.5, 0, 0)
			got, err := bundle2d.MarshalCanvasBundle(bundle)
			if err != nil {
				t.Fatalf("MarshalCanvasBundle: %v", err)
			}
			path := filepath.Join("testdata", tc.golden)
			if os.Getenv("GOSX_UPDATE_BOARD_FIXTURES") == "1" {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("update golden: %v", err)
				}
				t.Logf("regenerated %s (%d bytes)", path, len(got))
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (regenerate with GOSX_UPDATE_BOARD_FIXTURES=1): %v", err)
			}
			if got != string(want) {
				t.Errorf("marshal output diverged from %s.\nIf intentional, regenerate with GOSX_UPDATE_BOARD_FIXTURES=1 and re-run the js suite (client/js/runtime.test.js reads the same file).\ngot  (%d bytes): %.300s…\nwant (%d bytes): %.300s…", path, len(got), got, len(want), want)
			}
		})
	}
}
