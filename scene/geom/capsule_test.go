package geom

import (
	"math"
	"slices"
	"testing"
)

// capsuleRingStart returns the first vertex of the k-th full ring (k counts
// from 0 across all 2*capSegments rings). Both poles sit at vertices 0 and
// vertexCount-1, and every ring holds radialSegments+1 vertices.
func capsuleRingStart(k, capSegments, radialSegments int) int {
	return 1 + k*(radialSegments+1)
}

func TestCapsuleVertexCount(t *testing.T) {
	tests := []struct {
		name           string
		capSegments    int
		radialSegments int
		want           int
	}{
		{"defaults", 0, 0, 2 + 2*4*(8+1)},
		{"minimum segments", 1, 3, 2 + 2*1*(3+1)},
		{"explicit", 2, 5, 2 + 2*2*(5+1)},
		{"non-positive selects defaults", -7, -1, 74},
		{"radial below minimum clamps up", 4, 1, 2 + 2*4*(3+1)},
		{"cap above maximum clamps down", 1000, 16, 2 + 2*64*(16+1)},
		{"both clamp to limits", 1000, 10000, 2 + 2*64*(128+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CapsuleVertexCount(tc.capSegments, tc.radialSegments); got != tc.want {
				t.Fatalf("CapsuleVertexCount(%d, %d) = %d, want %d", tc.capSegments, tc.radialSegments, got, tc.want)
			}
		})
	}
}

func TestCapsuleCountsMatchVertexCount(t *testing.T) {
	for _, tc := range []struct {
		capSegments, radialSegments int
	}{
		{0, 0},
		{1, 3},
		{2, 5},
		{3, 12},
		{64, 128},
	} {
		m := Capsule(1, 1, tc.capSegments, tc.radialSegments, AttrNormals|AttrUVs)
		if m == nil {
			t.Fatalf("Capsule(%d, %d) returned nil", tc.capSegments, tc.radialSegments)
		}
		wantVertices := CapsuleVertexCount(tc.capSegments, tc.radialSegments)
		if got := len(m.Positions) / 3; got != wantVertices {
			t.Errorf("Capsule(%d, %d) vertices = %d, want %d", tc.capSegments, tc.radialSegments, got, wantVertices)
		}
		s, r := resolveCapsuleSegments(tc.capSegments, tc.radialSegments)
		wantTriangles := 4 * r * s
		if got := len(m.Indices) / 3; got != wantTriangles {
			t.Errorf("Capsule(%d, %d) triangles = %d, want %d", tc.capSegments, tc.radialSegments, got, wantTriangles)
		}
		for _, idx := range m.Indices {
			if idx < 0 || idx >= wantVertices {
				t.Fatalf("index out of range: %d", idx)
			}
		}
	}
}

func TestCapsuleExactBoundsForCustomRadiusAndLength(t *testing.T) {
	const (
		radius = 0.5
		length = 2.0
	)
	m := Capsule(radius, length, 3, 9, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Capsule returned nil")
	}
	half := length / 2
	minY, maxY := math.Inf(1), math.Inf(-1)
	maxRadial := 0.0
	for i := 0; i < len(m.Positions); i += 3 {
		x, y, z := m.Positions[i], m.Positions[i+1], m.Positions[i+2]
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
		if r := math.Hypot(x, z); r > maxRadial {
			maxRadial = r
		}
		if math.Abs(y) > half+radius+1e-12 {
			t.Errorf("vertex y = %g outside [-%g, %g]", y, half+radius, half+radius)
		}
		if math.Hypot(x, z) > radius+1e-12 {
			t.Errorf("vertex radius %g exceeds %g", math.Hypot(x, z), radius)
		}
	}
	// The poles sit exactly at -(Length/2 + Radius) and +(Length/2 + Radius).
	if minY != -(half+radius) || maxY != half+radius {
		t.Errorf("Y bounds = [%g, %g], want [%.17g, %.17g]", minY, maxY, -(half + radius), half+radius)
	}
	// The equator rings reach exactly the authored radius.
	if math.Abs(maxRadial-radius) > 1e-12 {
		t.Errorf("max radial extent = %g, want %g", maxRadial, radius)
	}
}

func TestCapsuleFiniteStreamsUnitOutwardNormals(t *testing.T) {
	m := Capsule(1.25, 0.75, 5, 11, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Capsule returned nil")
	}
	if len(m.Normals) != len(m.Positions) {
		t.Fatalf("normals length = %d, want %d", len(m.Normals), len(m.Positions))
	}
	if len(m.UVs) != len(m.Positions)/3*2 {
		t.Fatalf("uvs length = %d, want %d", len(m.UVs), len(m.Positions)/3*2)
	}
	for v := 0; v < len(m.Positions)/3; v++ {
		px, py, pz := m.Positions[v*3], m.Positions[v*3+1], m.Positions[v*3+2]
		nx, ny, nz := m.Normals[v*3], m.Normals[v*3+1], m.Normals[v*3+2]
		u, vv := m.UVs[v*2], m.UVs[v*2+1]
		for _, f := range []float64{px, py, pz, nx, ny, nz, u, vv} {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				t.Fatalf("vertex %d carries non-finite value %g", v, f)
			}
		}
		if l := math.Sqrt(nx*nx + ny*ny + nz*nz); math.Abs(l-1) > 1e-9 {
			t.Errorf("vertex %d normal length = %g, want 1", v, l)
		}
		// The capsule is convex and contains the origin, so an outward normal
		// always has a strictly positive dot product with the vertex itself.
		if dot := nx*px + ny*py + nz*pz; dot <= 0 {
			t.Errorf("vertex %d normal not outward: dot = %g at (%g, %g, %g)", v, dot, px, py, pz)
		}
	}
}

func TestCapsuleUVBoundsEndpointsAndMonotonicV(t *testing.T) {
	m := Capsule(1, 1, 4, 8, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Capsule returned nil")
	}
	vertices := len(m.Positions) / 3
	prevV := -1.0
	for v := 0; v < vertices; v++ {
		u, vv := m.UVs[v*2], m.UVs[v*2+1]
		if u < 0 || u > 1 {
			t.Errorf("vertex %d U = %g outside [0, 1]", v, u)
		}
		if vv < 0 || vv > 1 {
			t.Errorf("vertex %d V = %g outside [0, 1]", v, vv)
		}
		if vv < prevV {
			t.Errorf("V decreased from %g to %g at vertex %d", prevV, vv, v)
		}
		prevV = vv
	}
	// Bottom pole is vertex 0 with V=0; top pole is the last vertex with V=1.
	if m.UVs[0] != 0.5 || m.UVs[1] != 0 {
		t.Errorf("bottom pole UV = (%g, %g), want (0.5, 0)", m.UVs[0], m.UVs[1])
	}
	last := (vertices - 1) * 2
	if m.UVs[last] != 0.5 || m.UVs[last+1] != 1 {
		t.Errorf("top pole UV = (%g, %g), want (0.5, 1)", m.UVs[last], m.UVs[last+1])
	}
}

func TestCapsuleLongitudeSeamIsExact(t *testing.T) {
	const capSegments, radialSegments = 3, 7
	m := Capsule(1, 1, capSegments, radialSegments, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Capsule returned nil")
	}
	rings := 2 * capSegments
	for k := 0; k < rings; k++ {
		start := capsuleRingStart(k, capSegments, radialSegments)
		first, last := start, start+radialSegments
		for c := 0; c < 3; c++ {
			if m.Positions[first*3+c] != m.Positions[last*3+c] {
				t.Fatalf("ring %d seam position component %d: %g != %g", k, c,
					m.Positions[first*3+c], m.Positions[last*3+c])
			}
			if m.Normals[first*3+c] != m.Normals[last*3+c] {
				t.Fatalf("ring %d seam normal component %d: %g != %g", k, c,
					m.Normals[first*3+c], m.Normals[last*3+c])
			}
		}
		if m.UVs[first*2] != 0 {
			t.Errorf("ring %d first U = %g, want 0", k, m.UVs[first*2])
		}
		if m.UVs[last*2] != 1 {
			t.Errorf("ring %d last U = %g, want 1", k, m.UVs[last*2])
		}
		if m.UVs[first*2+1] != m.UVs[last*2+1] {
			t.Errorf("ring %d seam V mismatch: %g != %g", k, m.UVs[first*2+1], m.UVs[last*2+1])
		}
	}
}

func TestCapsuleTrianglesOutwardAndNonDegenerate(t *testing.T) {
	for _, tc := range []struct {
		capSegments, radialSegments int
		radius, length              float64
	}{{4, 8, 1, 1}, {1, 3, 1, 1}, {6, 12, 0.5, 2}} {
		m := Capsule(tc.radius, tc.length, tc.capSegments, tc.radialSegments, AttrNormals|AttrUVs)
		if m == nil {
			t.Fatalf("Capsule(%v, %v, %d, %d) returned nil", tc.radius, tc.length, tc.capSegments, tc.radialSegments)
		}
		for tIdx := 0; tIdx+2 < len(m.Indices); tIdx += 3 {
			a, b, c := m.Indices[tIdx], m.Indices[tIdx+1], m.Indices[tIdx+2]
			p0 := vec3{m.Positions[a*3], m.Positions[a*3+1], m.Positions[a*3+2]}
			p1 := vec3{m.Positions[b*3], m.Positions[b*3+1], m.Positions[b*3+2]}
			p2 := vec3{m.Positions[c*3], m.Positions[c*3+1], m.Positions[c*3+2]}
			n := crossVec(subVec(p1, p0), subVec(p2, p0))
			area := 0.5 * math.Sqrt(dotVec(n, n))
			if !(area > 0) || area < 1e-10 {
				t.Fatalf("triangle %d area = %g, want strictly positive", tIdx/3, area)
			}
			// Outward winding: for a convex solid containing the origin the
			// face normal must agree with the triangle centroid direction.
			centroid := scaleVec(addVec(addVec(p0, p1), p2), 1.0/3.0)
			if dotVec(n, centroid) <= 0 {
				t.Fatalf("triangle %d winds inward: cross dot centroid = %g", tIdx/3, dotVec(n, centroid))
			}
		}
	}
}

func TestCapsuleCapBodyJoinContinuousWithoutCoincidentRings(t *testing.T) {
	const (
		radius         = 0.8
		length         = 2.0
		capSegments    = 3
		radialSegments = 6
	)
	m := Capsule(radius, length, capSegments, radialSegments, AttrNormals|AttrUVs)
	if m == nil {
		t.Fatal("Capsule returned nil")
	}
	step := (math.Pi / 2) / float64(capSegments)
	// Ring rows in emission order: S-1 bottom-cap rings, both equators, S-1 top.
	bottomJoinCap := capsuleRingStart(capSegments-2, capSegments, radialSegments)
	bottomJoinBody := capsuleRingStart(capSegments-1, capSegments, radialSegments)
	topJoinBody := capsuleRingStart(capSegments, capSegments, radialSegments)
	topJoinCap := capsuleRingStart(capSegments+1, capSegments, radialSegments)

	// checkPair asserts the two rows are adjacent on the surface but never
	// coincident: sign gives the direction the body ring sits relative to the
	// cap ring along Y (+1 below the equator, -1 above it).
	checkPair := func(name string, capRow, bodyRow int, sign float64) {
		t.Helper()
		expectedGap := radius * math.Sin(step) // cos(pi/2*(S/(S+1))) == sin(step)
		for j := 0; j <= radialSegments; j++ {
			cp := vec3{
				m.Positions[(capRow+j)*3],
				m.Positions[(capRow+j)*3+1],
				m.Positions[(capRow+j)*3+2],
			}
			bp := vec3{
				m.Positions[(bodyRow+j)*3],
				m.Positions[(bodyRow+j)*3+1],
				m.Positions[(bodyRow+j)*3+2],
			}
			gap := sign * (bp.Y - cp.Y)
			if gap <= 0 {
				t.Fatalf("%s column %d join rows coincide or invert: cap y=%g body y=%g", name, j, cp.Y, bp.Y)
			}
			if math.Abs(gap-expectedGap) > 1e-12 {
				t.Errorf("%s column %d join gap = %g, want %g", name, j, gap, expectedGap)
			}
			cr := math.Hypot(cp.X, cp.Z)
			br := math.Hypot(bp.X, bp.Z)
			if br <= cr || br > radius+1e-12 {
				t.Errorf("%s column %d radii not strictly between cap ring and full radius: %g -> %g", name, j, cr, br)
			}
		}
	}
	checkPair("bottom", bottomJoinCap, bottomJoinBody, 1)
	checkPair("top", topJoinCap, topJoinBody, -1)
}

func TestCapsuleInvalidRadiusAndLengthDefaultDeterministically(t *testing.T) {
	reference := Capsule(1, 1, 0, 0, AllAttributes)
	if reference == nil {
		t.Fatal("reference Capsule returned nil")
	}
	for _, tc := range []struct {
		name           string
		radius, length float64
	}{
		{"zero", 0, 0},
		{"negative", -2, -3},
		{"nan", math.NaN(), math.NaN()},
		{"positive infinity", math.Inf(1), math.Inf(1)},
		{"negative infinity", math.Inf(-1), math.Inf(-1)},
	} {
		m := Capsule(tc.radius, tc.length, 0, 0, AllAttributes)
		if m == nil {
			t.Fatalf("%s: Capsule returned nil", tc.name)
		}
		if !slices.Equal(m.Positions, reference.Positions) ||
			!slices.Equal(m.Normals, reference.Normals) ||
			!slices.Equal(m.UVs, reference.UVs) ||
			!slices.Equal(m.Indices, reference.Indices) {
			t.Errorf("%s: output differs from the radius=1 length=1 reference", tc.name)
		}
	}
}

func TestCapsulePositionsOnlyOmitsNormalsAndUVs(t *testing.T) {
	full := Capsule(1, 1, 2, 6, AttrNormals|AttrUVs)
	posOnly := Capsule(1, 1, 2, 6, PositionsOnly)
	if posOnly == nil {
		t.Fatal("PositionsOnly Capsule returned nil")
	}
	if posOnly.Normals != nil {
		t.Error("PositionsOnly mesh carries normals")
	}
	if posOnly.UVs != nil {
		t.Error("PositionsOnly mesh carries UVs")
	}
	if !slices.Equal(posOnly.Positions, full.Positions) {
		t.Error("PositionsOnly positions differ from the full build")
	}
	if !slices.Equal(posOnly.Indices, full.Indices) {
		t.Error("PositionsOnly indices differ from the full build")
	}
}

func TestCapsuleRepeatedCallsAreDeterministic(t *testing.T) {
	a := Capsule(0.75, 1.5, 5, 13, AllAttributes)
	b := Capsule(0.75, 1.5, 5, 13, AllAttributes)
	if a == nil || b == nil {
		t.Fatal("Capsule returned nil")
	}
	if !slices.Equal(a.Positions, b.Positions) ||
		!slices.Equal(a.Normals, b.Normals) ||
		!slices.Equal(a.UVs, b.UVs) ||
		!slices.Equal(a.Indices, b.Indices) {
		t.Error("repeated Capsule calls produced different buffers")
	}
}
