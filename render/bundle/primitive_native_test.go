package bundle

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestPrimitiveForKnownKinds(t *testing.T) {
	cases := []struct {
		kind        string
		wantVertexN int
	}{
		{"cube", 36},
		{"cubeGeometry", 36},
		{"box", 36},
		{"boxGeometry", 36},
		{"plane", 6},
		{"planeGeometry", 6},
		{"pyramid", 18},
		{"pyramidGeometry", 18},
		{"sphere", 32 * 16 * 6},
		{"sphereGeometry", 32 * 16 * 6},
		{"cylinder", 32 * 12},
		{"cylinderGeometry", 32 * 12},
		{"cone", 32 * 6},
		{"coneGeometry", 32 * 6},
		{"torus", 32 * 16 * 6},
		{"torusGeometry", 32 * 16 * 6},
	}

	for _, tc := range cases {
		geo := primitiveForKind(tc.kind)
		if geo == nil {
			t.Fatalf("%s: primitive should be non-nil", tc.kind)
		}
		if geo.vertexCount != tc.wantVertexN {
			t.Fatalf("%s: vertexCount %d, want %d", tc.kind, geo.vertexCount, tc.wantVertexN)
		}
		assertPrimitiveBuffers(t, tc.kind, geo)
	}

	if got := primitiveForKind("nosuchkind"); got != nil {
		t.Error("unknown kind should return nil")
	}
}

func assertPrimitiveBuffers(t *testing.T, kind string, geo *primitiveGeometry) {
	t.Helper()
	if geo.vertexCount == 0 {
		t.Fatalf("%s: vertexCount is 0", kind)
	}
	if len(geo.positions) != geo.vertexCount*3 {
		t.Fatalf("%s: positions len %d, want %d", kind, len(geo.positions), geo.vertexCount*3)
	}
	if len(geo.colors) != geo.vertexCount*3 {
		t.Fatalf("%s: colors len %d, want %d", kind, len(geo.colors), geo.vertexCount*3)
	}
	if len(geo.normals) != geo.vertexCount*3 {
		t.Fatalf("%s: normals len %d, want %d", kind, len(geo.normals), geo.vertexCount*3)
	}
	if len(geo.uvs) != geo.vertexCount*2 {
		t.Fatalf("%s: uvs len %d, want %d", kind, len(geo.uvs), geo.vertexCount*2)
	}

	for i, v := range geo.positions {
		if !isFinite32(v) {
			t.Fatalf("%s: non-finite position[%d]=%v", kind, i, v)
		}
	}
	for i, v := range geo.colors {
		if !isFinite32(v) || v < 0 || v > 1 {
			t.Fatalf("%s: invalid color[%d]=%v", kind, i, v)
		}
	}
	for i := 0; i < len(geo.normals); i += 3 {
		x, y, z := geo.normals[i], geo.normals[i+1], geo.normals[i+2]
		length := math.Sqrt(float64(x*x + y*y + z*z))
		if length < 0.99 || length > 1.01 {
			t.Fatalf("%s: normal %d length %f, want unit", kind, i/3, length)
		}
	}
	for i, v := range geo.uvs {
		if !isFinite32(v) {
			t.Fatalf("%s: non-finite uv[%d]=%v", kind, i, v)
		}
	}
}

func TestPrimitiveGenerationClampsSegments(t *testing.T) {
	for name, geo := range map[string]*primitiveGeometry{
		"sphere":   sphereGeometry(1, 0, 0),
		"cylinder": cylinderGeometry(1, 1, 2, 0),
		"cone":     cylinderGeometry(0, 1, 2, 0),
		"torus":    torusGeometry(1, 0.25, 0, 0),
	} {
		if geo == nil || geo.vertexCount == 0 {
			t.Fatalf("%s: expected generated geometry", name)
		}
		assertPrimitiveBuffers(t, name, geo)
	}
}

func TestPrimitiveParameterizedGeometry(t *testing.T) {
	sphere := primitiveForParams(primitiveParams{Kind: "sphere", Radius: 2, Segments: 12})
	if sphere == nil {
		t.Fatal("sphere: expected geometry")
	}
	if sphere.vertexCount != 12*6*6 {
		t.Fatalf("sphere vertexCount %d, want %d", sphere.vertexCount, 12*6*6)
	}
	assertPositionExtents(t, "sphere", sphere, [3]float32{-2, -2, -2}, [3]float32{2, 2, 2}, 0.02)

	box := primitiveForParams(primitiveParams{Kind: "box", Width: 4, Height: 2, Depth: 1})
	if box == nil {
		t.Fatal("box: expected geometry")
	}
	assertPositionExtents(t, "box", box, [3]float32{-2, -1, -0.5}, [3]float32{2, 1, 0.5}, 0)

	torus := primitiveForParams(primitiveParams{Kind: "torus", Radius: 1.25, Tube: 0.25, RadialSegments: 20, TubularSegments: 10})
	if torus == nil {
		t.Fatal("torus: expected geometry")
	}
	if torus.vertexCount != 20*10*6 {
		t.Fatalf("torus vertexCount %d, want %d", torus.vertexCount, 20*10*6)
	}

	a := primitiveCacheKey(primitiveParams{Kind: "sphere", Radius: 1, Segments: 12})
	b := primitiveCacheKey(primitiveParams{Kind: "sphere", Radius: 2, Segments: 12})
	if a == "" || b == "" || a == b {
		t.Fatalf("parameterized cache keys should be non-empty and distinct: %q %q", a, b)
	}
}

func TestPrimitiveCacheUsesGeometryParameters(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	before := len(d.buffers)
	first := engine.RenderInstancedMesh{Kind: "sphere", Radius: 1, Segments: 12}
	second := engine.RenderInstancedMesh{Kind: "sphere", Radius: 2, Segments: 12}
	if _, err := r.ensurePrimitiveForMesh(first); err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	if got := len(d.buffers) - before; got != 4 {
		t.Fatalf("first primitive upload created %d buffers, want 4", got)
	}
	if _, err := r.ensurePrimitiveForMesh(first); err != nil {
		t.Fatalf("ensure first again: %v", err)
	}
	if got := len(d.buffers) - before; got != 4 {
		t.Fatalf("same primitive parameters should reuse cache, created %d buffers", got)
	}
	if _, err := r.ensurePrimitiveForMesh(second); err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if got := len(d.buffers) - before; got != 8 {
		t.Fatalf("distinct primitive parameters should upload another 4 buffers, created %d", got)
	}
}

func assertPositionExtents(t *testing.T, kind string, geo *primitiveGeometry, wantMin, wantMax [3]float32, tolerance float64) {
	t.Helper()
	mins := [3]float32{geo.positions[0], geo.positions[1], geo.positions[2]}
	maxs := mins
	for i := 0; i < len(geo.positions); i += 3 {
		for axis := 0; axis < 3; axis++ {
			value := geo.positions[i+axis]
			if value < mins[axis] {
				mins[axis] = value
			}
			if value > maxs[axis] {
				maxs[axis] = value
			}
		}
	}
	for axis := 0; axis < 3; axis++ {
		if math.Abs(float64(mins[axis]-wantMin[axis])) > tolerance {
			t.Fatalf("%s min[%d]=%v, want %v", kind, axis, mins[axis], wantMin[axis])
		}
		if math.Abs(float64(maxs[axis]-wantMax[axis])) > tolerance {
			t.Fatalf("%s max[%d]=%v, want %v", kind, axis, maxs[axis], wantMax[axis])
		}
	}
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}
