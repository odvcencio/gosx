package inspect

import (
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/geom"
)

// TestPrimitiveVertexCountMatchesTheGenerator pins the memory table to the
// generator every renderer reads.
//
// A drift here is invisible in production: the report still prints a number, and
// a scene budget still passes or fails on it. Only a comparison against the
// generator can catch a table that has fallen behind.
func TestPrimitiveVertexCountMatchesTheGenerator(t *testing.T) {
	cases := []struct {
		kind            string
		segments        int
		radialSegments  int
		tubularSegments int
		params          geom.Params
	}{
		{kind: "cube", params: geom.Params{Kind: "cube"}},
		{kind: "box", params: geom.Params{Kind: "box"}},
		{kind: "plane", params: geom.Params{Kind: "plane"}},
		{kind: "pyramid", params: geom.Params{Kind: "pyramid"}},
		{kind: "sphere", params: geom.Params{Kind: "sphere"}},
		{kind: "sphere", segments: 12, params: geom.Params{Kind: "sphere", Segments: 12}},
		{kind: "cylinder", params: geom.Params{Kind: "cylinder"}},
		{kind: "cylinder", segments: 7, params: geom.Params{Kind: "cylinder", Segments: 7}},
		{kind: "cone", params: geom.Params{Kind: "cone"}},
		{kind: "torus", params: geom.Params{Kind: "torus"}},
		{
			kind: "torus", radialSegments: 20, tubularSegments: 10,
			params: geom.Params{Kind: "torus", RadialSegments: 20, TubularSegments: 10},
		},
		{kind: "torusknot", params: geom.Params{Kind: "torusknot"}},
		{
			kind: "torusknot", radialSegments: 8, tubularSegments: 64,
			params: geom.Params{Kind: "torusknot", RadialSegments: 8, TubularSegments: 64},
		},
	}
	for _, testCase := range cases {
		got := primitiveVertexCount(testCase.kind, testCase.segments, testCase.radialSegments, testCase.tubularSegments)
		want := geom.DrawVertexCount(testCase.params)
		if got != want {
			t.Fatalf("%s(seg %d, rad %d, tube %d): the report says %d vertices, the generator builds %d",
				testCase.kind, testCase.segments, testCase.radialSegments, testCase.tubularSegments, got, want)
		}
	}
}

// TestTorusKnotVertexCountIsCounted proves the knot has a real entry. The
// renderer's own table used to stop at the torus, so a knot reported nothing at
// all.
func TestTorusKnotVertexCountIsCounted(t *testing.T) {
	for _, spelling := range []string{"torusknot", "torusKnot", "TorusKnotGeometry", "torus-knot"} {
		if got := primitiveVertexCount(spelling, 0, 0, 0); got != 128*16*6 {
			t.Fatalf("%q reported %d vertices, want the default knot grid", spelling, got)
		}
	}
	// The authored resolution must change the number.
	coarse := primitiveVertexCount("torusknot", 0, 4, 16)
	fine := primitiveVertexCount("torusknot", 0, 8, 64)
	if coarse >= fine {
		t.Fatalf("a coarse knot reported %d vertices and a fine one %d", coarse, fine)
	}
}

// TestGeneratedMeshBytesReadTheirOwnVertices is the regression test for a memory
// report that understated a whole family of geometry.
//
// Every generated mesh lowers to a "gltf-mesh" object with inline vertices. The
// old estimate looked at the kind name only, found no case for it, and fell
// through to the 36-vertex default. A mesh with a hundred thousand vertices was
// reported as one cube.
func TestGeneratedMeshBytesReadTheirOwnVertices(t *testing.T) {
	cases := map[string]scene.BufferGeometry{
		"tetrahedron":  scene.TetrahedronGeometry(1, 0),
		"icosahedron":  scene.IcosahedronGeometry(1, 3),
		"dodecahedron": scene.DodecahedronGeometry(1, 0),
		"circle":       scene.CircleGeometry(1, 64, 0, 0),
		"ring":         scene.RingGeometry(0.5, 1, 64, 2, 0, 0),
		"shape":        scene.ShapeGeometry(scene.Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}, 0),
		"extrude": scene.ExtrudeGeometry(
			scene.Shape{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}},
			scene.ExtrudeOptions{Depth: 1},
		),
	}
	for name, geometry := range cases {
		ir := scene.Props{Graph: scene.NewGraph(scene.Mesh{ID: "subject", Geometry: geometry})}.SceneIR()
		if len(ir.Objects) != 1 {
			t.Fatalf("%s: %d objects, want 1", name, len(ir.Objects))
		}
		object := ir.Objects[0]
		if object.Vertices == nil {
			t.Fatalf("%s: the object carries no vertices", name)
		}
		want := int64(object.Vertices.Count) * bytesPerVertex
		if got := objectGeometryBytes(object); got != want {
			t.Fatalf("%s: reported %d bytes, want %d", name, got, want)
		}
		// The default fallback is 36 vertices. A generated mesh larger than that
		// must not report the fallback.
		if object.Vertices.Count > 36 && objectGeometryBytes(object) == 36*bytesPerVertex {
			t.Fatalf("%s: a %d-vertex mesh reported the 36-vertex default",
				name, object.Vertices.Count)
		}
	}
}

// TestNamedPrimitiveBytesStillUseTheKindTable proves the new vertex path did not
// take over the parametric kinds. Those ship ten numbers, not a vertex list, so
// their size must still come from the formula.
func TestNamedPrimitiveBytesStillUseTheKindTable(t *testing.T) {
	ir := scene.Props{Graph: scene.NewGraph(
		scene.Mesh{ID: "ball", Geometry: scene.SphereGeometry{Radius: 1, Segments: 24}},
	)}.SceneIR()
	object := ir.Objects[0]
	if object.Vertices != nil {
		t.Fatal("a parametric sphere must not ship inline vertices")
	}
	want := int64(geom.DrawVertexCount(geom.Params{Kind: "sphere", Segments: 24})) * bytesPerVertex
	if got := objectGeometryBytes(object); got != want {
		t.Fatalf("reported %d bytes, want %d", got, want)
	}
}

// TestPolyhedronAndDiscKindsAreNamed covers the kind names a future lowering
// could emit for the generated families. A missing name here would send the kind
// to the 36-vertex default.
func TestPolyhedronAndDiscKindsAreNamed(t *testing.T) {
	cases := []struct {
		kind     string
		detail   int
		vertices int
	}{
		{"tetrahedron", 0, 4 * 3},
		{"tetrahedronGeometry", 1, 4 * 4 * 3},
		{"octahedron", 0, 8 * 3},
		{"icosahedron", 0, 20 * 3},
		{"icosahedron", 2, 20 * 9 * 3},
		{"dodecahedron", 0, 36 * 3},
	}
	for _, testCase := range cases {
		if got := primitiveVertexCount(testCase.kind, testCase.detail, 0, 0); got != testCase.vertices {
			t.Fatalf("%s detail %d reported %d vertices, want %d",
				testCase.kind, testCase.detail, got, testCase.vertices)
		}
	}
	if got := primitiveVertexCount("circle", 32, 0, 0); got != 96 {
		t.Fatalf("a 32-segment circle reported %d vertices, want 96", got)
	}
	if got := primitiveVertexCount("ring", 32, 2, 0); got != 384 {
		t.Fatalf("a 32-by-2 ring reported %d vertices, want 384", got)
	}
	// An unknown kind must still report the safe default rather than zero, so a
	// budget never reads a scene as free.
	if got := primitiveVertexCount("nosuchkind", 0, 0, 0); got != 36 {
		t.Fatalf("an unknown kind reported %d vertices, want the default 36", got)
	}
}
