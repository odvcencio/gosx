package bundle2d_test

import (
	"html"
	"regexp"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle2d"
)

// enginePropsAttr extracts the data-gosx-engine-props JSON from the HTML a
// gosx.CanvasBoard renders — the exact wire string the WASM bootstrap hands to
// vm.NewCanvasBoardAdapter. Pulling it from rendered HTML (rather than
// re-deriving it) means the parity test exercises the real authoring→wire path.
var enginePropsAttr = regexp.MustCompile(`data-gosx-engine-props="([^"]*)"`)

func extractEngineProps(t *testing.T, node gosx.Node) string {
	t.Helper()
	htmlOut := gosx.RenderHTML(node)
	m := enginePropsAttr.FindStringSubmatch(htmlOut)
	if m == nil {
		t.Fatalf("data-gosx-engine-props not found in rendered canvas HTML:\n%s", htmlOut)
	}
	return html.UnescapeString(m[1])
}

// wasmBundleJSON reproduces what the WASM CanvasBoardAdapter.RenderBundle()
// produces for a static board carrying the given nodes: it renders the
// gosx.CanvasBoard primitive to its DOM placeholder, pulls the engine-props JSON
// off it, hydrates a CanvasBoardAdapter from an EMPTY program (so EngineNodes is
// nil and the static props.nodes fallback path runs — exactly the Muddy
// site-map board's situation), and marshals the resulting bundle with the same
// bridge marshaler the WASM bridge uses.
func wasmBundleJSON(t *testing.T, nodes []gosx.CanvasBoardNode, background string, width, height int) string {
	t.Helper()
	board := gosx.CanvasBoard(gosx.CanvasBoardProps{
		ID:         "parity-board",
		Width:      width,
		Height:     height,
		Background: background,
		Zoom:       1.0, // matches the server camera below (zoom=1, pan=0,0)
		Nodes:      nodes,
	})
	propsJSON := extractEngineProps(t, board)

	adapter := vm.NewCanvasBoardAdapter(&rootengine.Program{}, propsJSON)
	bundle := adapter.RenderBundle(width, height, 0)
	js, err := bridge.MarshalEngineRenderBundle(bundle)
	if err != nil {
		t.Fatalf("MarshalEngineRenderBundle (wasm path): %v", err)
	}
	return js
}

// representativeNodes is a board that exercises every node kind ComputeCanvasBundle
// projects — rects (with distinct + shared colors to prove material dedup), lines,
// labels, and a sprite — plus an id-less node (index-derived id) and a degenerate
// zero-size rect (unit-extent fallback). The parity contract must hold across all
// of them.
func representativeNodes() []gosx.CanvasBoardNode {
	return []gosx.CanvasBoardNode{
		{Kind: "line", ID: "edge-1", X1: 10, Y1: 20, X2: 210, Y2: 220, Color: "#475569"},
		{Kind: "rect", ID: "home", X: 0, Y: 0, Width: 200, Height: 72, Color: "#2563eb"},
		{Kind: "rect", ID: "about", X: 280, Y: 0, Width: 200, Height: 72, Color: "#2563eb"}, // shares color → dedup
		{Kind: "rect", ID: "shop", X: 0, Y: 120, Width: 200, Height: 72, Color: "#7c3aed"},
		{Kind: "rect", X: 600, Y: 600}, // id-less + zero-size → index id + unit extent
		{Kind: "label", ID: "home:label", X: 14, Y: 28, Color: "#e2e8f0", Text: "Home"},
		{Kind: "image", ID: "logo", X: 5, Y: 5, Width: 48, Height: 48, Src: "/logo.png"},
	}
}

// TestServerBundleJSONMatchesWASMBundleJSON is the keystone parity assertion:
// the server-computed bundle (bundle2d.ComputeCanvasBundle + MarshalCanvasBundle,
// plain Go, no syscall/js) is byte-identical to the bundle the existing WASM
// CanvasBoardAdapter produces for the same nodes and camera. If this passes, a
// WASM-free client painting the inline server JSON sees exactly what the WASM
// path would have painted.
func TestServerBundleJSONMatchesWASMBundleJSON(t *testing.T) {
	const (
		width  = 1280
		height = 720
	)
	nodes := representativeNodes()

	serverBundle := bundle2d.ComputeCanvasBundle(nodes, width, height, 1, 0, 0)
	serverJSON, err := bundle2d.MarshalCanvasBundle(serverBundle)
	if err != nil {
		t.Fatalf("MarshalCanvasBundle (server path): %v", err)
	}

	wasmJSON := wasmBundleJSON(t, nodes, "", width, height)

	if serverJSON != wasmJSON {
		t.Fatalf("server bundle JSON != WASM bundle JSON\n server: %s\n wasm:   %s", serverJSON, wasmJSON)
	}
}

// TestServerBundleJSONMatchesWASMBundleJSONWithBackground proves parity holds for
// ComputeCanvasBundleWithBackground too: the background propagates into
// RenderBundle.Background identically to gosx.CanvasBoardProps.Background on the
// WASM path. This is the entry gosx-studio's site-map surface uses.
func TestServerBundleJSONMatchesWASMBundleJSONWithBackground(t *testing.T) {
	const (
		width  = 960
		height = 540
	)
	nodes := representativeNodes()
	const background = "#0f1720"

	serverBundle := bundle2d.ComputeCanvasBundleWithBackground(nodes, background, width, height, 1, 0, 0)
	serverJSON, err := bundle2d.MarshalCanvasBundle(serverBundle)
	if err != nil {
		t.Fatalf("MarshalCanvasBundle (server path): %v", err)
	}

	wasmJSON := wasmBundleJSON(t, nodes, background, width, height)

	if serverJSON != wasmJSON {
		t.Fatalf("server bundle JSON != WASM bundle JSON (with background)\n server: %s\n wasm:   %s", serverJSON, wasmJSON)
	}
}

// TestComputeCanvasBundleShape sanity-checks the projection independent of the
// WASM path: the representative board yields the expected object/line/label/
// sprite counts, a deduped material set, an OrthoCamera2D, and no lights/post-FX
// (the 2D-mode gate). Guards against a future refactor that quietly drops a kind.
func TestComputeCanvasBundleShape(t *testing.T) {
	bundle := bundle2d.ComputeCanvasBundle(representativeNodes(), 1280, 720, 1, 0, 0)

	if got := len(bundle.Objects); got != 4 {
		t.Errorf("Objects = %d, want 4 (rects)", got)
	}
	if got := len(bundle.Lines); got != 1 {
		t.Errorf("Lines = %d, want 1", got)
	}
	if got := len(bundle.Labels); got != 1 {
		t.Errorf("Labels = %d, want 1", got)
	}
	if got := len(bundle.Sprites); got != 1 {
		t.Errorf("Sprites = %d, want 1", got)
	}
	// 3 distinct rect colors (#2563eb shared by two rects, #7c3aed, and the
	// id-less rect's empty color) → 3 materials.
	if got := len(bundle.Materials); got != 3 {
		t.Errorf("Materials = %d, want 3 (deduped)", got)
	}
	if bundle.Camera.Mode != "ortho2d" {
		t.Errorf("Camera.Mode = %q, want ortho2d", bundle.Camera.Mode)
	}
	if len(bundle.Lights) != 0 || len(bundle.PostEffects) != 0 {
		t.Errorf("2D bundle carries lights/post-FX: lights=%d postfx=%d", len(bundle.Lights), len(bundle.PostEffects))
	}
}
