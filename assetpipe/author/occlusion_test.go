package author

import "testing"

func TestVertexOcclusionSeesOpenSkyAndACeiling(t *testing.T) {
	// Vertex 0 faces up under a large ceiling 0.1 above it; vertex 1 faces
	// down, away from it; vertex 2 is not chosen.
	p := []Vec3{{0, 0, 0}, {0, 0, 0}, {5, 5, 5}, {-50, -50, .1}, {50, -50, .1}, {0, 50, .1}}
	normals := []Vec3{{0, 0, 1}, {0, 0, -1}, {0, 0, 1}, {0, 0, -1}, {0, 0, -1}, {0, 0, -1}}
	tris := [][3]int{{3, 4, 5}}
	ao := VertexOcclusion(p, normals, tris, []bool{true, true, false, false, false, false}, Occlusion{Reach: 100, Margin: 100})
	if ao[0] > .05 {
		t.Fatalf("under the ceiling: open share %v, want about 0", ao[0])
	}
	if ao[1] != 1 {
		t.Fatalf("facing away: open share %v, want 1", ao[1])
	}
	if ao[2] != 0 {
		t.Fatalf("unchosen vertex got %v", ao[2])
	}
	if near := VertexOcclusion(p, normals, tris, []bool{true, false, false, false, false, false}, Occlusion{Reach: .05})[0]; near != 1 {
		t.Fatalf("a ceiling beyond reach still covered: %v", near)
	}
}
