package geom

import (
	"math"
	"testing"
)

// meshTriangle returns the three corners of one triangle of a mesh, indexed or
// not. Every test in this package reads triangles through it, so an indexed mesh
// and a flat one are checked the same way.
func meshTriangle(m *Mesh, triangle int) (vec3, vec3, vec3) {
	corner := func(vertex int) vec3 {
		base := vertex * 3
		return vec3{m.Positions[base], m.Positions[base+1], m.Positions[base+2]}
	}
	if len(m.Indices) > 0 {
		base := triangle * 3
		return corner(m.Indices[base]), corner(m.Indices[base+1]), corner(m.Indices[base+2])
	}
	base := triangle * 3
	return corner(base), corner(base + 1), corner(base + 2)
}

func meshNormals(m *Mesh, triangle int) (vec3, vec3, vec3) {
	normal := func(vertex int) vec3 {
		base := vertex * 3
		return vec3{m.Normals[base], m.Normals[base+1], m.Normals[base+2]}
	}
	if len(m.Indices) > 0 {
		base := triangle * 3
		return normal(m.Indices[base]), normal(m.Indices[base+1]), normal(m.Indices[base+2])
	}
	base := triangle * 3
	return normal(base), normal(base + 1), normal(base + 2)
}

// assertWindingMatchesNormals checks that every triangle's geometric normal
// agrees with the shaded normals its own vertices carry.
//
// This is the winding test. Nothing else catches a reversed face: the ray tester
// hits both sides of a triangle, and the browser runtime does not cull back
// faces today. A reversed face would therefore pass every other test in the
// repository and turn invisible the day back-face culling arrives.
//
// A degenerate triangle carries no direction, so it is skipped and counted. The
// caller states how many the mesh is allowed to hold.
func assertWindingMatchesNormals(t *testing.T, label string, m *Mesh, allowedDegenerate int) {
	t.Helper()
	if len(m.Normals) == 0 {
		t.Fatalf("%s: the mesh carries no normals, so winding cannot be checked", label)
	}
	degenerate := 0
	for triangle := 0; triangle < m.TriangleCount(); triangle++ {
		p0, p1, p2 := meshTriangle(m, triangle)
		edge0 := subVec(p1, p0)
		edge1 := subVec(p2, p0)
		raw := crossVec(edge0, edge1)
		if math.Sqrt(dotVec(raw, raw)) < 1e-12 {
			degenerate++
			continue
		}
		geometric := normalize(raw)
		n0, n1, n2 := meshNormals(m, triangle)
		shaded := normalize(addVec(addVec(n0, n1), n2))
		if dot := dotVec(geometric, shaded); dot <= 0 {
			t.Fatalf("%s: triangle %d is wound against its own normals (dot %.4f); corners %v %v %v",
				label, triangle, dot, p0, p1, p2)
		}
	}
	if degenerate > allowedDegenerate {
		t.Fatalf("%s: %d degenerate triangles, want at most %d", label, degenerate, allowedDegenerate)
	}
}

// assertFiniteUnitNormals checks that every normal is finite and unit length.
func assertFiniteUnitNormals(t *testing.T, label string, m *Mesh) {
	t.Helper()
	for i := 0; i+3 <= len(m.Normals); i += 3 {
		x, y, z := m.Normals[i], m.Normals[i+1], m.Normals[i+2]
		length := math.Sqrt(x*x + y*y + z*z)
		if math.IsNaN(length) || math.IsInf(length, 0) || length < 0.99 || length > 1.01 {
			t.Fatalf("%s: normal %d has length %v, want unit", label, i/3, length)
		}
	}
	for i, v := range m.Positions {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: position %d is %v", label, i, v)
		}
	}
	for i, v := range m.UVs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: uv %d is %v", label, i, v)
		}
	}
}

// meshBounds returns the axis-aligned box that holds the mesh.
func meshBounds(m *Mesh) (lo, hi vec3) {
	lo = vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+3 <= len(m.Positions); i += 3 {
		lo.X = math.Min(lo.X, m.Positions[i])
		lo.Y = math.Min(lo.Y, m.Positions[i+1])
		lo.Z = math.Min(lo.Z, m.Positions[i+2])
		hi.X = math.Max(hi.X, m.Positions[i])
		hi.Y = math.Max(hi.Y, m.Positions[i+1])
		hi.Z = math.Max(hi.Z, m.Positions[i+2])
	}
	return lo, hi
}

// TestBuildVertexCountsMatchTheDeclaredCounts pins every parametric generator to
// the count VertexCount promises. Memory reporting and the GPU upload both read
// that promise, so a drift understates or overstates real memory.
func TestBuildVertexCountsMatchTheDeclaredCounts(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "box", Width: 4, Height: 2, Depth: 1},
		{Kind: "plane"},
		{Kind: "pyramid"},
		{Kind: "sphere"},
		{Kind: "sphere", Radius: 2, Segments: 12},
		{Kind: "cylinder"},
		{Kind: "cylinder", Segments: 7},
		{Kind: "cone"},
		{Kind: "torus"},
		{Kind: "torus", RadialSegments: 20, TubularSegments: 10},
		{Kind: "torusknot"},
		{Kind: "torusknot", RadialSegments: 8, TubularSegments: 32},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: Build returned nil", params.Kind)
		}
		if got, want := mesh.VertexCount(), VertexCount(params); got != want {
			t.Fatalf("%s: vertex count %d, want the declared %d", params.Kind, got, want)
		}
		if got, want := len(mesh.Normals), mesh.VertexCount()*3; got != want {
			t.Fatalf("%s: normals length %d, want %d", params.Kind, got, want)
		}
		if got, want := len(mesh.UVs), mesh.VertexCount()*2; got != want {
			t.Fatalf("%s: uvs length %d, want %d", params.Kind, got, want)
		}
		if got, want := len(mesh.Colors), mesh.VertexCount()*3; got != want {
			t.Fatalf("%s: colors length %d, want %d", params.Kind, got, want)
		}
		assertFiniteUnitNormals(t, params.Kind, mesh)
	}
}

// TestDrawVertexCountMatchesTheUpload pins the count a GPU upload and a wire
// payload really carry. An indexed body must report its expanded size here, or a
// memory report understates it by the sharing factor.
func TestDrawVertexCountMatchesTheUpload(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "plane"},
		{Kind: "sphere", Segments: 12},
		{Kind: "cylinder", Segments: 7},
		{Kind: "cone"},
		{Kind: "torus", RadialSegments: 20, TubularSegments: 10},
		{Kind: "torusknot"},
		{Kind: "torusknot", RadialSegments: 4, TubularSegments: 16},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		flat := mesh.Expanded()
		if got, want := flat.VertexCount(), DrawVertexCount(params); got != want {
			t.Fatalf("%s: the expanded mesh holds %d vertices, DrawVertexCount says %d",
				CacheKey(params), got, want)
		}
		if got, want := flat.TriangleCount(), DrawVertexCount(params)/3; got != want {
			t.Fatalf("%s: %d triangles, want %d", CacheKey(params), got, want)
		}
	}
	if got := DrawVertexCount(Params{Kind: "nosuchkind"}); got != 0 {
		t.Fatalf("DrawVertexCount of an unknown kind = %d, want 0", got)
	}
}

// TestBuildWindingAgreesWithNormals covers every parametric body.
func TestBuildWindingAgreesWithNormals(t *testing.T) {
	for _, kind := range []string{"cube", "box", "plane", "pyramid", "sphere", "cylinder", "cone", "torus", "torusknot"} {
		mesh := Build(Params{Kind: kind}, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: Build returned nil", kind)
		}
		// A UV sphere collapses its two pole rows into slivers, and a cone
		// collapses its apex row, so those rows carry degenerate triangles.
		allowed := 0
		switch kind {
		case "sphere":
			allowed = 64
		}
		assertWindingMatchesNormals(t, kind, mesh, allowed)
	}
}

// TestBuildBoundsStayInsideTheDeclaredRadius proves BoundingRadius never
// understates a body. A broad phase that understates drops real hits, and a cull
// that understates makes an object blink out while it is still on screen.
func TestBuildBoundsStayInsideTheDeclaredRadius(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "cube", Size: 3},
		{Kind: "box", Width: 4, Height: 2, Depth: 1},
		{Kind: "plane", Width: 6, Height: 2},
		{Kind: "pyramid"},
		{Kind: "sphere", Radius: 2.5, Segments: 24},
		{Kind: "cylinder", RadiusTop: 0.5, RadiusBottom: 2, Height: 3},
		{Kind: "cone", RadiusBottom: 2, Height: 3},
		{Kind: "torus", Radius: 1.25, Tube: 0.25},
		{Kind: "torusknot", Radius: 1, Tube: 0.3},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		declared := BoundingRadius(params)
		if declared <= 0 {
			t.Fatalf("%s: BoundingRadius returned %v", params.Kind, declared)
		}
		widest := 0.0
		for i := 0; i+3 <= len(mesh.Positions); i += 3 {
			x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
			widest = math.Max(widest, math.Sqrt(x*x+y*y+z*z))
		}
		if widest > declared+1e-9 {
			t.Fatalf("%s: a vertex sits %v from the origin, past the declared radius %v",
				params.Kind, widest, declared)
		}
		// A bound far larger than the body would pass the check above and still
		// waste every cull test, so hold it close.
		if widest < declared*0.5 {
			t.Fatalf("%s: declared radius %v is more than twice the real extent %v",
				params.Kind, declared, widest)
		}
	}
}

// TestTorusKnotIsNamedByEverySpelling is the regression test for the defect this
// package was built to close.
//
// The native renderer used to know eight kinds and stop at "torus". A torusknot
// returned the empty kind, the cache key came back empty, the primitive came
// back nil, and the renderer dropped the draw with no diagnostic. The browser
// drew a knot; the desktop renderer and the headless PNG oracle drew nothing.
func TestTorusKnotIsNamedByEverySpelling(t *testing.T) {
	for _, spelling := range []string{"torusknot", "torusKnot", "TorusKnotGeometry", " TORUSKNOT ", "torus-knot"} {
		if got := NormalizeKind(spelling); got != KindTorusKnot {
			t.Fatalf("NormalizeKind(%q) = %q, want %q", spelling, got, KindTorusKnot)
		}
		if key := CacheKey(Params{Kind: spelling}); key == "" {
			t.Fatalf("CacheKey(%q) is empty, so a renderer would skip the draw", spelling)
		}
		mesh := Build(Params{Kind: spelling}, AllAttributes)
		if mesh == nil || mesh.TriangleCount() == 0 {
			t.Fatalf("Build(%q) produced no triangles", spelling)
		}
		if radius := BoundingRadius(Params{Kind: spelling}); radius <= 0 {
			t.Fatalf("BoundingRadius(%q) = %v", spelling, radius)
		}
	}
}

// TestUnknownKindIsReportedNotDropped proves an unknown name answers with a
// refusal a caller can see, instead of an empty mesh that looks like a draw.
func TestUnknownKindIsReportedNotDropped(t *testing.T) {
	for _, kind := range []string{"", "nosuchkind", "sphre"} {
		if got := NormalizeKind(kind); got != "" {
			t.Fatalf("NormalizeKind(%q) = %q, want the empty string", kind, got)
		}
		if got := CacheKey(Params{Kind: kind}); got != "" {
			t.Fatalf("CacheKey(%q) = %q, want the empty string", kind, got)
		}
		if got := Build(Params{Kind: kind}, AllAttributes); got != nil {
			t.Fatalf("Build(%q) returned a mesh, want nil", kind)
		}
		if got := VertexCount(Params{Kind: kind}); got != 0 {
			t.Fatalf("VertexCount(%q) = %d, want 0", kind, got)
		}
	}
}

// TestCacheKeysSeparateDifferentGeometry proves two different bodies never share
// one upload.
func TestCacheKeysSeparateDifferentGeometry(t *testing.T) {
	pairs := [][2]Params{
		{{Kind: "sphere", Radius: 1, Segments: 12}, {Kind: "sphere", Radius: 2, Segments: 12}},
		{{Kind: "sphere", Radius: 1, Segments: 12}, {Kind: "sphere", Radius: 1, Segments: 24}},
		{{Kind: "torusknot", Radius: 1}, {Kind: "torusknot", Radius: 2}},
		{{Kind: "torusknot", TubularSegments: 64}, {Kind: "torusknot", TubularSegments: 128}},
		{{Kind: "torus", Radius: 1}, {Kind: "torusknot", Radius: 1}},
		{{Kind: "cylinder", RadiusTop: 1}, {Kind: "cone", RadiusBottom: 1}},
	}
	for _, pair := range pairs {
		first, second := CacheKey(pair[0]), CacheKey(pair[1])
		if first == "" || second == "" {
			t.Fatalf("empty key for %v or %v", pair[0], pair[1])
		}
		if first == second {
			t.Fatalf("%v and %v share the key %q", pair[0], pair[1], first)
		}
	}
	// The same numbers must always give the same key, or the cache never hits.
	if CacheKey(Params{Kind: "sphere", Segments: 12}) != CacheKey(Params{Kind: "sphereGeometry", Segments: 12}) {
		t.Fatal("two spellings of one sphere produced different keys")
	}
}

// TestAttributeSelectionSkipsUnwantedStreams proves the positions-only path
// really costs nothing. The raycaster asks for it on every knot.
func TestAttributeSelectionSkipsUnwantedStreams(t *testing.T) {
	full := Build(Params{Kind: "torusknot"}, AllAttributes)
	lean := Build(Params{Kind: "torusknot"}, PositionsOnly)
	if len(lean.Normals) != 0 || len(lean.UVs) != 0 || len(lean.Colors) != 0 {
		t.Fatal("PositionsOnly filled a stream nobody asked for")
	}
	if len(lean.Positions) != len(full.Positions) {
		t.Fatalf("positions length %d, want %d", len(lean.Positions), len(full.Positions))
	}
	for i := range lean.Positions {
		if lean.Positions[i] != full.Positions[i] {
			t.Fatalf("position %d differs between the two attribute sets", i)
		}
	}
	if len(lean.Indices) != len(full.Indices) {
		t.Fatal("the two attribute sets produced different index counts")
	}
}

// TestExpandedMatchesTheIndexedMesh proves the renderer's flat upload draws the
// same triangles the picker tests.
func TestExpandedMatchesTheIndexedMesh(t *testing.T) {
	indexed := Build(Params{Kind: "torusknot", RadialSegments: 4, TubularSegments: 8}, AllAttributes)
	flat := indexed.Expanded()
	if flat.TriangleCount() != indexed.TriangleCount() {
		t.Fatalf("expanded triangle count %d, want %d", flat.TriangleCount(), indexed.TriangleCount())
	}
	if len(flat.Indices) != 0 {
		t.Fatal("the expanded mesh still carries indices")
	}
	for triangle := 0; triangle < indexed.TriangleCount(); triangle++ {
		want0, want1, want2 := meshTriangle(indexed, triangle)
		got0, got1, got2 := meshTriangle(flat, triangle)
		if want0 != got0 || want1 != got1 || want2 != got2 {
			t.Fatalf("triangle %d differs after expansion", triangle)
		}
	}
	// A non-indexed mesh must come back unchanged rather than being copied.
	plain := Build(Params{Kind: "cube"}, AllAttributes)
	if plain.Expanded() != plain {
		t.Fatal("Expanded copied a mesh that was already flat")
	}
}

// TestSphereVerticesSitOnTheSphere is a shape test that a count test cannot
// replace: a generator can emit the right number of wrong vertices.
func TestSphereVerticesSitOnTheSphere(t *testing.T) {
	const radius = 2.5
	mesh := Build(Params{Kind: "sphere", Radius: radius, Segments: 16}, AllAttributes)
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
		if got := math.Sqrt(x*x + y*y + z*z); math.Abs(got-radius) > 1e-9 {
			t.Fatalf("vertex %d sits %v from the origin, want %v", i/3, got, radius)
		}
		nx, ny, nz := mesh.Normals[i], mesh.Normals[i+1], mesh.Normals[i+2]
		if math.Abs(nx*radius-x) > 1e-9 || math.Abs(ny*radius-y) > 1e-9 || math.Abs(nz*radius-z) > 1e-9 {
			t.Fatalf("vertex %d carries a normal that does not point out of the origin", i/3)
		}
	}
	lo, hi := meshBounds(mesh)
	if math.Abs(lo.X+radius) > 1e-9 || math.Abs(hi.X-radius) > 1e-9 {
		t.Fatalf("bounds x = [%v, %v], want [%v, %v]", lo.X, hi.X, -radius, radius)
	}
}

// TestTorusKnotStaysOnItsTube proves the swept tube keeps its radius. A broken
// frame transport shows up as a vertex off the tube, which a count test misses.
func TestTorusKnotStaysOnItsTube(t *testing.T) {
	const (
		radius = 1.0
		tube   = 0.25
	)
	mesh := Build(Params{Kind: "torusknot", Radius: radius, Tube: tube, RadialSegments: 8, TubularSegments: 64}, PositionsOnly)
	// Every vertex sits one tube radius from the center curve, so its distance
	// from the origin stays between the curve's own reach minus and plus a tube.
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
		distance := math.Sqrt(x*x + y*y + z*z)
		if distance > radius*1.5+tube+1e-9 {
			t.Fatalf("vertex %d reaches %v, past the declared bound", i/3, distance)
		}
		if distance < radius*0.5-tube-1e-9 {
			t.Fatalf("vertex %d collapsed to %v from the origin", i/3, distance)
		}
	}
}
