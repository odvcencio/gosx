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

// Two pieces lofted ring on ring are one surface for shading when they name
// one Surface: the ring they share gets one normal on both, and each keeps
// its own UVs. Without it each piece's normal there leans its own way.
func TestSurfaceJoinsShells(t *testing.T) {
	build := func(surface string) (*Mesh, Shell, Shell) {
		m := &Mesh{}
		// A drum whose top ring is the bottom ring of a cone above it.
		low := m.SeamStack("low", 0, 0, []Ring{{Vec3{0, 0, 0}, .2, .2}, {Vec3{0, .2, 0}, .2, .2}}, 12)
		high := m.SeamStack("high", 0, 1, []Ring{{Vec3{0, .2, 0}, .2, .2}, {Vec3{0, .4, 0}, .1, .1}}, 12)
		m.Shells[0].Surface, m.Shells[1].Surface = surface, surface
		return m, low, high
	}
	for _, joined := range []bool{false, true} {
		surface := ""
		if joined {
			surface = "garment"
		}
		m, low, high := build(surface)
		n := weldNormals(Normals(m.P, m.Tri), m.seamGroups())
		for j := 0; j < 13; j++ {
			a, b := low.First+13+j, high.First+j
			if m.P[a] != m.P[b] {
				t.Fatalf("column %d: the shared ring is not at one place", j)
			}
			same := n[a].Sub(n[b]).Len() < 1e-12
			if same != joined {
				t.Fatalf("surface %q, column %d: normals %v and %v (one normal: %v)", surface, j, n[a], n[b], same)
			}
			if m.UV[a] == m.UV[b] && j > 0 {
				t.Fatalf("column %d: the pieces' UVs were merged", j)
			}
		}
	}
}
