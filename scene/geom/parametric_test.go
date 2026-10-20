package geom

import (
	"math"
	"reflect"
	"testing"
)

// parametricPlane is a tilted plane over the unit square: z = 1 + 2x - 3y.
func parametricPlane(u, v float64) (float64, float64, float64) {
	return u, v, 1 + 2*u - 3*v
}

// parametricCylinder wraps a cylinder around the Y axis with a U seam at u=0
// and u=1. sin(2πu), cos(2πu) keeps the du × dv normal pointing radially out.
func parametricCylinder(u, v float64) (float64, float64, float64) {
	theta := 2 * math.Pi * u
	return math.Sin(theta), 2*v - 1, math.Cos(theta)
}

// parametricSquareCylinder sweeps a square prism around the Y axis. Its U seam
// runs through the middle of one flat face, so both seam columns sample the
// same straight run: their one-sided du differences agree, and the duplicated
// seam carries duplicated positions and normals while U stays honest at 0
// versus 1.
func parametricSquareCylinder(u, v float64) (float64, float64, float64) {
	corner := func(m int) (float64, float64) {
		angle := math.Pi/4 + float64(m)*math.Pi/2
		return math.Cos(angle), -math.Sin(angle)
	}
	q := u * 4
	side := int(math.Round(q)) % 4
	t := q - math.Round(q) + 0.5 // [0, 1] along the current face
	x0, z0 := corner(side)
	x1, z1 := corner((side + 1) % 4)
	return x0 + t*(x1-x0), 2*v - 1, z0 + t*(z1-z0)
}

// parametricPinch is the plane z = 0 with the two horizontal neighbors of the
// grid point (0.5, 0.5) folded onto each other. The centered du difference at
// that one vertex becomes exactly zero, so its cross product vanishes and the
// deterministic fallback must take over — while every other vertex keeps a
// plain +Z normal.
func parametricPinch(u, v float64) (float64, float64, float64) {
	if v == 0.5 && u == 0.25 {
		return 0.75, 0.5, 0
	}
	return u, v, 0
}

func TestParametricVertexCount(t *testing.T) {
	tests := []struct {
		name           string
		slices, stacks int
		want           int
	}{
		{"defaults", 0, 0, 9 * 9},
		{"negative selects default", -3, -1, 9 * 9},
		{"minimum", 1, 1, 2 * 2},
		{"explicit", 5, 3, 6 * 4},
		{"clamped high", 1 << 20, 2, 513 * 3},
		{"both clamped high", 1 << 20, 1 << 20, 513 * 513},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParametricVertexCount(tc.slices, tc.stacks); got != tc.want {
				t.Fatalf("ParametricVertexCount(%d, %d) = %d, want %d", tc.slices, tc.stacks, got, tc.want)
			}
		})
	}
}

func TestParametricCountsMatchThePublishedVertexCount(t *testing.T) {
	cases := []struct {
		slices, stacks int
		resolvedSlices int
		resolvedStacks int
	}{
		{0, 0, 8, 8},
		{1, 1, 1, 1},
		{5, 3, 5, 3},
		{600, 2, 512, 2},
	}
	for _, c := range cases {
		mesh := Parametric(parametricPlane, c.slices, c.stacks, AttrNormals|AttrUVs)
		if mesh == nil {
			t.Fatalf("Parametric(%d, %d) returned nil", c.slices, c.stacks)
		}
		if got := mesh.VertexCount(); got != (c.resolvedSlices+1)*(c.resolvedStacks+1) {
			t.Fatalf("slices=%d stacks=%d: vertex count = %d, want %d", c.slices, c.stacks,
				got, (c.resolvedSlices+1)*(c.resolvedStacks+1))
		}
		if got := ParametricVertexCount(c.slices, c.stacks); got != mesh.VertexCount() {
			t.Fatalf("ParametricVertexCount(%d, %d) = %d, generator emitted %d",
				c.slices, c.stacks, got, mesh.VertexCount())
		}
		wantTriangles := 2 * c.resolvedSlices * c.resolvedStacks
		if got := len(mesh.Indices) / 3; got != wantTriangles {
			t.Fatalf("slices=%d stacks=%d: triangle count = %d, want %d", c.slices, c.stacks, got, wantTriangles)
		}
		for _, idx := range mesh.Indices {
			if idx < 0 || idx >= mesh.VertexCount() {
				t.Fatalf("index %d escapes %d vertices", idx, mesh.VertexCount())
			}
		}
	}
}

func TestParametricSamplesEveryGridPointOnceInRowMajorOrder(t *testing.T) {
	const slices, stacks = 3, 2
	var calls [][2]float64
	surface := func(u, v float64) (float64, float64, float64) {
		calls = append(calls, [2]float64{u, v})
		return u, v, 0
	}
	mesh := Parametric(surface, slices, stacks, PositionsOnly)
	if mesh == nil {
		t.Fatal("Parametric returned nil for a valid surface")
	}

	wantCalls := (slices + 1) * (stacks + 1)
	if len(calls) != wantCalls {
		t.Fatalf("callback ran %d times, want exactly %d (one per grid point)", len(calls), wantCalls)
	}
	first, last := calls[0], calls[len(calls)-1]
	if first[0] != 0 || first[1] != 0 {
		t.Fatalf("first sample = (%g, %g), want (0, 0)", first[0], first[1])
	}
	if last[0] != 1 || last[1] != 1 {
		t.Fatalf("last sample = (%g, %g), want exact endpoints (1, 1)", last[0], last[1])
	}
	k := 0
	for j := 0; j <= stacks; j++ {
		v := float64(j) / float64(stacks)
		for i := 0; i <= slices; i++ {
			u := float64(i) / float64(slices)
			if calls[k][0] != u || calls[k][1] != v {
				t.Fatalf("sample %d = (%g, %g), want row-major (%g, %g)",
					k, calls[k][0], calls[k][1], u, v)
			}
			k++
		}
	}
}

func TestParametricTiltedPlanePositionsNormalsAndWinding(t *testing.T) {
	const slices, stacks = 4, 3
	mesh := Parametric(parametricPlane, slices, stacks, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("Parametric returned nil for the tilted plane")
	}
	cols := slices + 1

	// Exact positions and UVs per grid vertex.
	for j := 0; j <= stacks; j++ {
		v := float64(j) / float64(stacks)
		for i := 0; i <= slices; i++ {
			u := float64(i) / float64(slices)
			k := j*cols + i
			x, y, z := mesh.Positions[k*3], mesh.Positions[k*3+1], mesh.Positions[k*3+2]
			wx, wy, wz := parametricPlane(u, v)
			if x != wx || y != wy || z != wz {
				t.Errorf("vertex (%d,%d) position = (%g, %g, %g), want (%g, %g, %g)", i, j, x, y, z, wx, wy, wz)
			}
			if mesh.UVs[k*2] != u || mesh.UVs[k*2+1] != v {
				t.Errorf("vertex (%d,%d) uv = (%g, %g), want (%g, %g)", i, j, mesh.UVs[k*2], mesh.UVs[k*2+1], u, v)
			}
		}
	}

	// Every normal is finite, unit length, and points along normalized du×dv.
	du := vec3{X: 1, Y: 0, Z: 2}
	dv := vec3{X: 0, Y: 1, Z: -3}
	want := normalize(crossVec(du, dv))
	for k := 0; k < mesh.VertexCount(); k++ {
		nx, ny, nz := mesh.Normals[k*3], mesh.Normals[k*3+1], mesh.Normals[k*3+2]
		if math.IsNaN(nx) || math.IsNaN(ny) || math.IsNaN(nz) ||
			math.IsInf(nx, 0) || math.IsInf(ny, 0) || math.IsInf(nz, 0) {
			t.Fatalf("normal %d carries a non-finite component", k)
		}
		length := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if math.Abs(length-1) > 1e-12 {
			t.Errorf("normal %d has length %g, want 1", k, length)
		}
		if math.Abs(nx-want.X) > 1e-12 || math.Abs(ny-want.Y) > 1e-12 || math.Abs(nz-want.Z) > 1e-12 {
			t.Errorf("normal %d = (%g, %g, %g), want du×dv direction (%g, %g, %g)",
				k, nx, ny, nz, want.X, want.Y, want.Z)
		}
	}

	// Every indexed triangle is non-degenerate and wound so its right-hand-rule
	// normal agrees with the shared vertex normals.
	vertexNormal := func(k int) vec3 {
		return vec3{X: mesh.Normals[k*3], Y: mesh.Normals[k*3+1], Z: mesh.Normals[k*3+2]}
	}
	positionAt := func(k int) vec3 {
		return vec3{X: mesh.Positions[k*3], Y: mesh.Positions[k*3+1], Z: mesh.Positions[k*3+2]}
	}
	for tIdx := 0; tIdx < len(mesh.Indices); tIdx += 3 {
		a, b, c := mesh.Indices[tIdx], mesh.Indices[tIdx+1], mesh.Indices[tIdx+2]
		pa, pb, pc := positionAt(a), positionAt(b), positionAt(c)
		geometric := triangleNormal(pa, pb, pc)
		area := dotVec(crossVec(subVec(pb, pa), subVec(pc, pa)), geometric)
		if area <= 0 {
			t.Fatalf("triangle %d is degenerate or inconsistently wound (area term %g)", tIdx/3, area)
		}
		for _, k := range []int{a, b, c} {
			if dotVec(geometric, vertexNormal(k)) <= 0 {
				t.Fatalf("triangle %d winds against the interpolated normals at vertex %d", tIdx/3, k)
			}
		}
	}
}

func TestParametricKeepsPeriodicUSeamHonest(t *testing.T) {
	// 16 slices keeps every sample strictly inside a face run around the seam,
	// so the one-sided du differences at U=0 and U=1 walk the same straight
	// segment and must produce duplicated normals, not mirrored ones.
	const slices, stacks = 16, 2
	mesh := Parametric(parametricSquareCylinder, slices, stacks, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("Parametric returned nil for the cylinder")
	}
	cols := slices + 1

	for j := 0; j <= stacks; j++ {
		first := j*cols + 0
		seam := j*cols + slices
		for c := 0; c < 3; c++ {
			if mesh.Positions[first*3+c] != mesh.Positions[seam*3+c] {
				t.Errorf("row %d: seam positions differ on component %d", j, c)
			}
			if math.Abs(mesh.Normals[first*3+c]-mesh.Normals[seam*3+c]) > 1e-9 {
				t.Errorf("row %d: seam normals differ on component %d", j, c)
			}
		}
		if mesh.UVs[first*2] != 0 {
			t.Errorf("row %d: first seam U = %g, want 0", j, mesh.UVs[first*2])
		}
		if mesh.UVs[seam*2] != 1 {
			t.Errorf("row %d: last seam U = %g, want 1", j, mesh.UVs[seam*2])
		}
		if mesh.UVs[first*2+1] != mesh.UVs[seam*2+1] {
			t.Errorf("row %d: seam V disagrees across the seam", j)
		}

		// The seam sits mid-face of the bottom face run (z = -1), so the
		// outward normal there points straight down -Z.
		nx, ny, nz := mesh.Normals[first*3], mesh.Normals[first*3+1], mesh.Normals[first*3+2]
		if math.Abs(nx) > 1e-12 || math.Abs(ny) > 1e-12 || math.Abs(nz+1) > 1e-12 {
			t.Errorf("row %d: seam normal = (%g, %g, %g), want outward (0, 0, -1)", j, nx, ny, nz)
		}
	}

	// A circular parameterization still duplicates seam positions exactly and
	// keeps every vertex finite even where the one-sided normals mirror.
	round := Parametric(parametricCylinder, 6, 2, AttrNormals|AttrUVs)
	if round == nil {
		t.Fatal("Parametric returned nil for the circular cylinder")
	}
	roundCols := 6 + 1
	for j := 0; j <= 2; j++ {
		a, b := j*roundCols+0, j*roundCols+6
		for c := 0; c < 3; c++ {
			if math.Abs(round.Positions[a*3+c]-round.Positions[b*3+c]) > 1e-9 {
				t.Fatalf("circular seam positions differ on component %d", c)
			}
		}
	}
}

func TestParametricRejectsNilAndNonFiniteCallbacks(t *testing.T) {
	if got := Parametric(nil, 4, 4, AttrNormals|AttrUVs); got != nil {
		t.Fatalf("Parametric(nil) = %+v, want nil", got)
	}
	nanAt := func(targetU, targetV float64) SurfaceFunc {
		return func(u, v float64) (float64, float64, float64) {
			if u == targetU && v == targetV {
				return math.NaN(), 0, 0
			}
			return u, v, 0
		}
	}
	infAt := func(targetU, targetV float64) SurfaceFunc {
		return func(u, v float64) (float64, float64, float64) {
			if u == targetU && v == targetV {
				return 0, math.Inf(1), math.Inf(-1)
			}
			return u, v, 0
		}
	}
	tests := []struct {
		name    string
		surface SurfaceFunc
	}{
		{"NaN at origin", nanAt(0, 0)},
		{"NaN interior", nanAt(0.5, 0.5)},
		{"Inf at far corner", infAt(1, 1)},
		{"Inf interior", infAt(0.25, 0.75)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			surface := func(u, v float64) (float64, float64, float64) {
				calls++
				return tc.surface(u, v)
			}
			if got := Parametric(surface, 4, 4, AttrNormals|AttrUVs); got != nil {
				t.Fatalf("non-finite surface produced %+v, want nil with no partial mesh", got)
			}
			if calls != 25 {
				t.Fatalf("non-finite surface callback ran %d times, want all 25 grid samples", calls)
			}
		})
	}
}

func TestParametricSingularSampleStaysFiniteAndDeterministic(t *testing.T) {
	const slices, stacks = 4, 4
	mesh := Parametric(parametricPinch, slices, stacks, AttrNormals|AttrUVs)
	if mesh == nil {
		t.Fatal("Parametric returned nil for the pinched plane")
	}
	cols := slices + 1

	fallbackSeen := 0
	for k := 0; k < mesh.VertexCount(); k++ {
		nx, ny, nz := mesh.Normals[k*3], mesh.Normals[k*3+1], mesh.Normals[k*3+2]
		if math.IsNaN(nx) || math.IsNaN(ny) || math.IsNaN(nz) ||
			math.IsInf(nx, 0) || math.IsInf(ny, 0) || math.IsInf(nz, 0) {
			t.Fatalf("singular surface emitted a non-finite normal at vertex %d", k)
		}
		length := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if math.Abs(length-1) > 1e-12 {
			t.Errorf("vertex %d normal length = %g, want 1", k, length)
		}
		if nx == 0 && ny == 1 && nz == 0 {
			i, j := k%cols, k/cols
			if i != 2 || j != 2 {
				t.Errorf("fallback normal appeared at unexpected grid point (%d, %d)", i, j)
			}
			fallbackSeen++
		} else if !(nx == 0 && ny == 0 && nz == 1) {
			t.Errorf("vertex %d normal = (%g, %g, %g), want +Z away from the pinch", k, nx, ny, nz)
		}
	}
	if fallbackSeen != 1 {
		t.Fatalf("exactly one singular sample expected, saw %d", fallbackSeen)
	}

	repeat := Parametric(parametricPinch, slices, stacks, AttrNormals|AttrUVs)
	if repeat == nil {
		t.Fatal("repeat build returned nil")
	}
	if !reflect.DeepEqual(mesh.Positions, repeat.Positions) ||
		!reflect.DeepEqual(mesh.Normals, repeat.Normals) ||
		!reflect.DeepEqual(mesh.UVs, repeat.UVs) ||
		!reflect.DeepEqual(mesh.Indices, repeat.Indices) {
		t.Fatal("repeated builds of the singular surface are not deeply identical")
	}
}

func TestParametricPositionsOnlyLeavesOtherStreamsNil(t *testing.T) {
	mesh := Parametric(parametricPlane, 4, 4, PositionsOnly)
	if mesh == nil {
		t.Fatal("Parametric returned nil for PositionsOnly")
	}
	if mesh.Normals != nil {
		t.Error("PositionsOnly must leave Normals nil")
	}
	if mesh.UVs != nil {
		t.Error("PositionsOnly must leave UVs nil")
	}
	if mesh.Colors != nil {
		t.Error("PositionsOnly must leave Colors nil")
	}
	if mesh.VertexCount() != 25 || len(mesh.Indices) != 4*4*6 {
		t.Fatalf("positions/index fill wrong: %d vertices, %d index floats", mesh.VertexCount(), len(mesh.Indices))
	}
}

func TestParametricIsDeeplyDeterministic(t *testing.T) {
	build := func() *Mesh {
		return Parametric(parametricCylinder, 7, 5, AttrNormals|AttrUVs)
	}
	a, b := build(), build()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two identical Parametric calls produced different meshes")
	}
}
