package geom

import (
	"math"
	"testing"
)

var cylinderProfile = []float64{1, 0, 1, 2}

func TestLatheVertexCount(t *testing.T) {
	tests := []struct {
		name          string
		profilePoints int
		segments      int
		want          int
	}{
		{"default segments", 2, 0, 13 * 2},
		{"explicit segments", 2, 7, 8 * 2},
		{"non-positive selects default", 2, -5, 13 * 2},
		{"clamped high", 3, 100000, 513 * 3},
		{"too few points", 1, 12, 0},
		{"zero points", 0, 12, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LatheVertexCount(tc.profilePoints, tc.segments); got != tc.want {
				t.Fatalf("LatheVertexCount(%d, %d) = %d, want %d", tc.profilePoints, tc.segments, got, tc.want)
			}
		})
	}
}

func TestLatheCylinderCountsAndBounds(t *testing.T) {
	m := Lathe(cylinderProfile, 12, 0, 2*math.Pi, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Lathe returned nil for valid profile")
	}
	if got, want := len(m.Positions), 26*3; got != want {
		t.Fatalf("positions length = %d, want %d", got, want)
	}
	if got, want := len(m.Normals), 26*3; got != want {
		t.Fatalf("normals length = %d, want %d", got, want)
	}
	if got, want := len(m.UVs), 26*2; got != want {
		t.Fatalf("uvs length = %d, want %d", got, want)
	}
	if got, want := len(m.Indices), 12*2*3; got != want {
		t.Fatalf("indices length = %d, want %d", got, want)
	}
	for _, idx := range m.Indices {
		if idx < 0 || idx >= 26 {
			t.Fatalf("index out of range: %d", idx)
		}
	}
	// Bounds of a unit-radius, height-2 cylinder centered on Y in [0, 2].
	for i := 0; i < len(m.Positions); i += 3 {
		x, y, z := m.Positions[i], m.Positions[i+1], m.Positions[i+2]
		r := math.Hypot(x, z)
		if math.Abs(r-1) > 1e-9 {
			t.Errorf("vertex radius = %g, want 1", r)
		}
		if y < -1e-9 || y > 2+1e-9 {
			t.Errorf("vertex y = %g outside [0, 2]", y)
		}
	}
	// Seam vertices share position but carry distinct U values.
	first, last := 0, 12*2
	for k := 0; k < 3; k++ {
		if math.Abs(m.Positions[first*3+k]-m.Positions[last*3+k]) > 1e-9 {
			t.Errorf("seam position mismatch component %d", k)
		}
	}
	if m.UVs[0] != 0 {
		t.Errorf("first seam U = %g, want 0", m.UVs[0])
	}
	if math.Abs(m.UVs[last*2]-1) > 1e-9 {
		t.Errorf("last seam U = %g, want 1", m.UVs[last*2])
	}
}

func TestLatheCylinderNormals(t *testing.T) {
	m := Lathe(cylinderProfile, 8, 0, 2*math.Pi, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	for i := 0; i < len(m.Normals); i += 3 {
		nx, ny, nz := m.Normals[i], m.Normals[i+1], m.Normals[i+2]
		if math.IsNaN(nx) || math.IsNaN(ny) || math.IsNaN(nz) ||
			math.IsInf(nx, 0) || math.IsInf(ny, 0) || math.IsInf(nz, 0) {
			t.Fatalf("non-finite normal at vertex %d", i/3)
		}
		l := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if math.Abs(l-1) > 1e-9 {
			t.Errorf("normal length = %g, want 1", l)
		}
		if ny != 0 {
			t.Errorf("cylinder normal Y = %g, want 0", ny)
		}
		// Outward: dot with radial direction must be positive.
		px, pz := m.Positions[i], m.Positions[i+2]
		radial := math.Hypot(px, pz)
		if dot := (nx*px + nz*pz) / radial; dot <= 0 {
			t.Errorf("normal not outward: dot = %g", dot)
		}
	}
}

func TestLatheUVEndpoints(t *testing.T) {
	m := Lathe(cylinderProfile, 6, 0, 2*math.Pi, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	points := 2
	checks := []struct {
		vertex int
		u, v   float64
	}{}
	for j := 0; j <= 6; j++ {
		for i := 0; i < points; i++ {
			checks = append(checks, struct {
				vertex int
				u, v   float64
			}{j*points + i, float64(j) / 6, float64(i)})
		}
	}
	for _, c := range checks {
		gotU, gotV := m.UVs[c.vertex*2], m.UVs[c.vertex*2+1]
		if math.Abs(gotU-c.u) > 1e-12 || math.Abs(gotV-c.v) > 1e-12 {
			t.Errorf("uv at vertex %d = (%g, %g), want (%g, %g)", c.vertex, gotU, gotV, c.u, c.v)
		}
	}
}

func TestLathePartialSweep(t *testing.T) {
	m := Lathe(cylinderProfile, 4, math.Pi/2, math.Pi, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	// First vertex at phi = pi/2: (0, 0, 1).
	if math.Abs(m.Positions[0]) > 1e-12 || math.Abs(m.Positions[1]) > 1e-12 || math.Abs(m.Positions[2]-1) > 1e-12 {
		t.Errorf("partial sweep start = (%g, %g, %g), want (0, 0, 1)", m.Positions[0], m.Positions[1], m.Positions[2])
	}
	// Last row at phi = 3pi/2: (0, *, -1).
	last := 4 * 2 * 3
	if math.Abs(m.Positions[last]) > 1e-12 || math.Abs(m.Positions[last+2]+1) > 1e-12 {
		t.Errorf("partial sweep end = (%g, %g, %g), want (0, *, -1)", m.Positions[last], m.Positions[last+1], m.Positions[last+2])
	}
}

func TestLatheNonFiniteStartFallsBackToZero(t *testing.T) {
	for _, start := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		m := Lathe(cylinderProfile, 4, start, math.Pi, AttrNormals|AttrUVs)
		if m == nil {
			t.Fatal("Lathe returned nil")
		}
		if m.Positions[0] != 1 || m.Positions[1] != 0 || m.Positions[2] != 0 {
			t.Fatalf("start position = (%g, %g, %g), want (1, 0, 0)", m.Positions[0], m.Positions[1], m.Positions[2])
		}
	}
}

func TestLatheRejectsInvalidProfiles(t *testing.T) {
	tests := map[string][]float64{
		"nil":             nil,
		"one point":       {1, 0},
		"odd length":      {1, 0, 1},
		"nan":             {1, 0, math.NaN(), 1},
		"inf":             {math.Inf(1), 0, 1, 1},
		"negative radius": {-1, 0, 1, 1},
	}
	for name, profile := range tests {
		if m := Lathe(profile, 12, 0, 2*math.Pi, AttrNormals|AttrUVs); m != nil {
			t.Errorf("%s: expected nil mesh, got %+v", name, m)
		}
	}
}

func TestLatheDegenerateTangentFiniteNormals(t *testing.T) {
	m := Lathe([]float64{1, 0, 1, 0, 2, 1}, 6, 0, 2*math.Pi, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	for i, n := range m.Normals {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			t.Fatalf("non-finite normal component %d", i)
		}
	}
	for i := 0; i < len(m.Normals); i += 3 {
		l := math.Sqrt(m.Normals[i]*m.Normals[i] + m.Normals[i+1]*m.Normals[i+1] + m.Normals[i+2]*m.Normals[i+2])
		if math.Abs(l-1) > 1e-9 {
			t.Errorf("degenerate-tangent normal length = %g, want 1", l)
		}
	}
}

func TestLathePositionsOnly(t *testing.T) {
	m := Lathe(cylinderProfile, 6, 0, 2*math.Pi, 0)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	if m.Normals != nil {
		t.Error("normals should be nil for positions-only request")
	}
	if m.UVs != nil {
		t.Error("uvs should be nil for positions-only request")
	}
	if len(m.Positions) != 7*2*3 {
		t.Errorf("positions length = %d, want %d", len(m.Positions), 7*2*3)
	}
}

func TestLatheCylinderTriangleWinding(t *testing.T) {
	m := Lathe(cylinderProfile, 8, 0, 2*math.Pi, PositionsOnly)
	if m == nil {
		t.Fatal("Lathe returned nil")
	}
	for triangle := 0; triangle < len(m.Indices); triangle += 3 {
		a := m.Indices[triangle] * 3
		b := m.Indices[triangle+1] * 3
		c := m.Indices[triangle+2] * 3
		abx := m.Positions[b] - m.Positions[a]
		aby := m.Positions[b+1] - m.Positions[a+1]
		abz := m.Positions[b+2] - m.Positions[a+2]
		acx := m.Positions[c] - m.Positions[a]
		acy := m.Positions[c+1] - m.Positions[a+1]
		acz := m.Positions[c+2] - m.Positions[a+2]
		nx := aby*acz - abz*acy
		nz := abx*acy - aby*acx
		centerX := (m.Positions[a] + m.Positions[b] + m.Positions[c]) / 3
		centerZ := (m.Positions[a+2] + m.Positions[b+2] + m.Positions[c+2]) / 3
		if dot := nx*centerX + nz*centerZ; dot <= 0 {
			t.Fatalf("triangle %d winding is not outward: radial dot = %g", triangle/3, dot)
		}
	}
}
