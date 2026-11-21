package author

import (
	"math"
	"testing"
)

// A seamed stack runs u from 0 to 1 once round, its v by profile length, and
// the seam's two columns get one normal, so the shading has no crease.
func TestSeamStack(t *testing.T) {
	m := &Mesh{}
	rings := []Ring{{Vec3{0, 0, 0}, .2, .1}, {Vec3{0, .1, 0}, .2, .1}, {Vec3{0, .4, 0}, .2, .1}}
	m.SeamStack("drum", 0, 0, rings, 12)
	if len(m.P) != 3*13 {
		t.Fatalf("%d vertices, want 39", len(m.P))
	}
	if m.UV[0][0] != 0 || m.UV[12][0] != 1 || m.P[0] != m.P[12] {
		t.Fatalf("seam column: u %v and %v, positions %v and %v", m.UV[0][0], m.UV[12][0], m.P[0], m.P[12])
	}
	if v := m.UV[13][1]; math.Abs(v-.25) > 1e-9 {
		t.Fatalf("second ring v = %v, want 0.25 (0.1 of 0.4 along the profile)", v)
	}
	n := weldNormals(Normals(m.P, m.Tri), m.seamGroups())
	for k := 0; k < 3; k++ {
		a, b := n[k*13], n[k*13+12]
		if a.Sub(b).Len() > 1e-12 {
			t.Fatalf("ring %d: seam normals differ: %v and %v", k, a, b)
		}
	}
	// Mid ring, the seam is at +Z: its normal points there.
	if n[13][2] < .99 {
		t.Fatalf("seam normal %v does not face +Z", n[13])
	}
}
