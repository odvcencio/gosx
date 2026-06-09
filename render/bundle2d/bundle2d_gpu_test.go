package bundle2d

import (
	"testing"

	"m31labs.dev/gosx"
)

func TestAttachBoardGPUGeometry(t *testing.T) {
	nodes := []gosx.CanvasBoardNode{
		{ID: "a", Kind: "rect", X: -100, Y: 0, Width: 70, Height: 70, Color: "#ff0000"},
		{ID: "b", Kind: "rect", X: 50, Y: 10, Width: 40, Height: 20, Color: "#00ff00"},
	}
	b := ComputeCanvasGPUBundle(nodes, 360, 180, 1, 0, 0)

	if len(b.Objects) != 2 {
		t.Fatalf("want 2 objects, got %d", len(b.Objects))
	}
	// 6 verts/object: WorldPositions 18 floats, WorldNormals 18, WorldUVs 12.
	if got, want := len(b.WorldPositions), 2*18; got != want {
		t.Errorf("WorldPositions len=%d want %d", got, want)
	}
	if got, want := len(b.WorldNormals), 2*18; got != want {
		t.Errorf("WorldNormals len=%d want %d", got, want)
	}
	if got, want := len(b.WorldUVs), 2*12; got != want {
		t.Errorf("WorldUVs len=%d want %d", got, want)
	}
	// Objects point at their own 6-vertex slices, in order.
	if b.Objects[0].VertexOffset != 0 || b.Objects[0].VertexCount != 6 {
		t.Errorf("obj0 offset/count = %d/%d, want 0/6", b.Objects[0].VertexOffset, b.Objects[0].VertexCount)
	}
	if b.Objects[1].VertexOffset != 6 || b.Objects[1].VertexCount != 6 {
		t.Errorf("obj1 offset/count = %d/%d, want 6/6", b.Objects[1].VertexOffset, b.Objects[1].VertexCount)
	}
	// VertexCount must be triangle-list valid for the renderer's object path.
	for i, o := range b.Objects {
		if o.VertexCount%3 != 0 {
			t.Errorf("obj%d VertexCount=%d not a multiple of 3", i, o.VertexCount)
		}
	}
	// The first quad's vertices must equal object 0's rect bounds (z=0). First
	// vertex = (MinX, MinY, 0).
	bb := b.Objects[0].Bounds
	if b.WorldPositions[0] != bb.MinX || b.WorldPositions[1] != bb.MinY || b.WorldPositions[2] != 0 {
		t.Errorf("obj0 first vertex = (%v,%v,%v), want bounds min (%v,%v,0)",
			b.WorldPositions[0], b.WorldPositions[1], b.WorldPositions[2], bb.MinX, bb.MinY)
	}
	// All Z coordinates are 0 (the board sits on the z=0 plane).
	for i := 2; i < len(b.WorldPositions); i += 3 {
		if b.WorldPositions[i] != 0 {
			t.Fatalf("non-zero Z at vertex %d: %v", i/3, b.WorldPositions[i])
		}
	}
}

// TestAttachBoardGPUGeometry_NoObjectsIsNoop guards the empty-board path.
func TestAttachBoardGPUGeometry_NoObjectsIsNoop(t *testing.T) {
	b := ComputeCanvasGPUBundle(nil, 360, 180, 1, 0, 0)
	if len(b.WorldPositions) != 0 {
		t.Errorf("empty board should emit no WorldPositions, got %d", len(b.WorldPositions))
	}
}
