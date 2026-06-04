package gosx

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCanvasBoardEmitsCanvasPlaceholder is the D1.1 acceptance: a
// CanvasBoard() call materializes a <canvas> element annotated with the
// data-gosx-surface-kind="canvas2d" attribute the WASM bootstrap dispatches
// on. Equivalent to Scene3D's <canvas data-gosx-engine-component=...> shape.
func TestCanvasBoardEmitsCanvasPlaceholder(t *testing.T) {
	node := CanvasBoard(CanvasBoardProps{
		ID:     "board",
		Width:  800,
		Height: 600,
	})
	html := RenderHTML(node)

	if !strings.HasPrefix(html, "<canvas") {
		t.Fatalf("expected <canvas> element, got %q", html)
	}
	if !strings.Contains(html, `id="board"`) {
		t.Errorf("missing id attribute: %s", html)
	}
	if !strings.Contains(html, `data-gosx-surface-kind="canvas2d"`) {
		t.Errorf("missing surface-kind attribute: %s", html)
	}
	if !strings.Contains(html, `data-gosx-engine-component="CanvasBoard"`) {
		t.Errorf("missing engine-component attribute: %s", html)
	}
	if !strings.Contains(html, `data-gosx-canvas2d="1"`) {
		t.Errorf("missing canvas2d flag attribute: %s", html)
	}
	if !strings.Contains(html, `width="800"`) || !strings.Contains(html, `height="600"`) {
		t.Errorf("missing canvas dimensions: %s", html)
	}
}

// TestCanvasBoardEncodesInitialPanZoomIntoProps confirms the constructor
// serializes Pan/Zoom into the data-gosx-engine-props JSON payload the WASM
// adapter reads at mount time. This is what makes the .gsx primitive
// declarative — no JS bridge needed for initial state.
func TestCanvasBoardEncodesInitialPanZoomIntoProps(t *testing.T) {
	node := CanvasBoard(CanvasBoardProps{
		ID:   "board",
		Pan:  CanvasBoardPan{X: 100, Y: -50},
		Zoom: 2.5,
	})
	html := RenderHTML(node)
	propsJSON := extractAttr(html, "data-gosx-engine-props")
	if propsJSON == "" {
		t.Fatalf("data-gosx-engine-props missing or empty: %s", html)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &payload); err != nil {
		t.Fatalf("props JSON malformed: %v\n%s", err, propsJSON)
	}
	board, ok := payload["board"].(map[string]any)
	if !ok {
		t.Fatalf("board not in payload: %#v", payload)
	}
	if zoom, _ := board["zoom"].(float64); zoom != 2.5 {
		t.Errorf("zoom = %v, want 2.5", zoom)
	}
	pan, ok := board["pan"].(map[string]any)
	if !ok {
		t.Fatalf("pan not in payload: %#v", board)
	}
	if x, _ := pan["x"].(float64); x != 100 {
		t.Errorf("pan.x = %v, want 100", x)
	}
	if y, _ := pan["y"].(float64); y != -50 {
		t.Errorf("pan.y = %v, want -50", y)
	}
}

// TestCanvasBoardSerializesNodes proves the initial Nodes slice flows into
// the wire-format payload so the CanvasBoardAdapter can build the first
// resolved snapshot from it.
func TestCanvasBoardSerializesNodes(t *testing.T) {
	node := CanvasBoard(CanvasBoardProps{
		ID: "board",
		Nodes: []CanvasBoardNode{
			{ID: "r1", Kind: "rect", X: 10, Y: 20, Width: 100, Height: 60, Color: "#fa0"},
			{ID: "r2", Kind: "rect", X: 200, Y: 40, Width: 80, Height: 40, Color: "#0af"},
		},
	})
	html := RenderHTML(node)
	propsJSON := extractAttr(html, "data-gosx-engine-props")

	var payload map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &payload); err != nil {
		t.Fatalf("props JSON malformed: %v", err)
	}
	nodes, _ := payload["nodes"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes in payload, got %d", len(nodes))
	}
}

// TestCanvasBoardOnPickStoredOnElement guarantees the pick handler name is
// captured on the DOM so the bootstrap can wire it to a $surface.event.*
// subscription. Empty OnPick must NOT emit a stray attribute.
func TestCanvasBoardOnPickStoredOnElement(t *testing.T) {
	with := RenderHTML(CanvasBoard(CanvasBoardProps{OnPick: "handlePick"}))
	if !strings.Contains(with, `data-gosx-onpick="handlePick"`) {
		t.Errorf("OnPick handler not present: %s", with)
	}
	without := RenderHTML(CanvasBoard(CanvasBoardProps{}))
	if strings.Contains(without, "data-gosx-onpick") {
		t.Errorf("OnPick attribute leaked when handler empty: %s", without)
	}
}

// TestCanvasBoardDefaultsZoom guards the public contract: an unspecified
// zoom resolves to 1.0 in the wire payload (matches CanvasBoardAdapter's
// internal default and avoids a divide-by-zero at the renderer).
func TestCanvasBoardDefaultsZoom(t *testing.T) {
	html := RenderHTML(CanvasBoard(CanvasBoardProps{ID: "b"}))
	propsJSON := extractAttr(html, "data-gosx-engine-props")
	var payload map[string]any
	_ = json.Unmarshal([]byte(propsJSON), &payload)
	board, _ := payload["board"].(map[string]any)
	if zoom, _ := board["zoom"].(float64); zoom != 1 {
		t.Errorf("default zoom = %v, want 1", zoom)
	}
}

// TestCanvasBoardSurfaceKindConstantIsStable freezes the wire string the
// WASM bridge dispatches on. A change here breaks every running deployment.
func TestCanvasBoardSurfaceKindConstantIsStable(t *testing.T) {
	if CanvasBoardSurfaceKind != "canvas2d" {
		t.Fatalf("CanvasBoardSurfaceKind = %q, want %q (do not change without coordinating with bridge.SurfaceKindCanvas2D)",
			CanvasBoardSurfaceKind, "canvas2d")
	}
}

// extractAttr is a tiny HTML-attribute scraper for tests — pulls the value
// of name="..." from html. Returns "" if the attribute is absent.
func extractAttr(html, name string) string {
	prefix := name + `="`
	i := strings.Index(html, prefix)
	if i < 0 {
		return ""
	}
	start := i + len(prefix)
	end := strings.Index(html[start:], `"`)
	if end < 0 {
		return ""
	}
	value := html[start : start+end]
	return unescapeAttr(value)
}

// TestCanvasBoardNodeHTMLFields verifies that CanvasBoardNode carries the
// Markup and PointerEvents fields required for the "html" kind, and that they
// round-trip correctly through JSON marshalling.
func TestCanvasBoardNodeHTMLFields(t *testing.T) {
	node := CanvasBoardNode{
		Kind:          "html",
		Markup:        `<b data-gosx-html-key="k">x</b>`,
		PointerEvents: "auto",
		X:             0,
		Y:             0,
		Width:         1280,
		Height:        720,
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, `"markup"`) {
		t.Errorf("JSON missing \"markup\" key: %s", s)
	}
	if !strings.Contains(s, `"pointerEvents"`) {
		t.Errorf("JSON missing \"pointerEvents\" key: %s", s)
	}

	var got CanvasBoardNode
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Markup != node.Markup {
		t.Errorf("Markup = %q, want %q", got.Markup, node.Markup)
	}
	if got.PointerEvents != node.PointerEvents {
		t.Errorf("PointerEvents = %q, want %q", got.PointerEvents, node.PointerEvents)
	}
}

// unescapeAttr inverts the HTML attribute escaping that node.go applies in
// renderAttrHTML. Handles both named entities and the decimal numeric
// entities (&#34;) the gosx renderer emits for quote characters.
func unescapeAttr(v string) string {
	v = strings.ReplaceAll(v, "&#34;", `"`)
	v = strings.ReplaceAll(v, "&#39;", `'`)
	v = strings.ReplaceAll(v, "&quot;", `"`)
	v = strings.ReplaceAll(v, "&lt;", "<")
	v = strings.ReplaceAll(v, "&gt;", ">")
	v = strings.ReplaceAll(v, "&amp;", "&")
	return v
}
