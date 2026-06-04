package bundle2d_test

import (
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/render/bundle2d"
)

// TestComputeCanvasBundleEmitsHTML proves the shared switch emits a RenderHTML
// record for an "html" node with all fields carried through.
func TestComputeCanvasBundleEmitsHTML(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{
			ID:            "page",
			Kind:          "html",
			Markup:        `<b data-gosx-html-key="k">x</b>`,
			X:             0,
			Y:             0,
			Width:         1280,
			Height:        720,
			PointerEvents: "auto",
		},
	}

	b := bundle2d.ComputeCanvasBundle(nodes, 1280, 720, 1, 0, 0)

	if got := len(b.HTML); got != 1 {
		t.Fatalf("HTML len = %d, want 1", got)
	}
	h := b.HTML[0]
	if h.Markup != nodes[0].Markup {
		t.Errorf("HTML[0].Markup = %q, want %q", h.Markup, nodes[0].Markup)
	}
	if h.ID != "page" {
		t.Errorf("HTML[0].ID = %q, want %q", h.ID, "page")
	}
	if h.X != 0 || h.Y != 0 {
		t.Errorf("HTML[0].X/Y = %v/%v, want 0/0", h.X, h.Y)
	}
	if h.Width != 1280 || h.Height != 720 {
		t.Errorf("HTML[0].Width/Height = %v/%v, want 1280/720", h.Width, h.Height)
	}
	if h.PointerEvents != "auto" {
		t.Errorf("HTML[0].PointerEvents = %q, want %q", h.PointerEvents, "auto")
	}
}
