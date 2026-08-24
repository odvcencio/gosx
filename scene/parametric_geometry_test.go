package scene

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene/geom"
)

// parametricWrapperPlane is the tilted plane z = 1 + 2x - 3y over the unit
// square. Its du × dv normal is normalize((-2, 3, 1)).
func parametricWrapperPlane(u, v float64) Vector3 {
	return Vector3{X: u, Y: v, Z: 1 + 2*u - 3*v}
}

func TestParametricGeometryNilSurfaceReturnsZeroValue(t *testing.T) {
	if got := ParametricGeometry(nil, 4, 4); !isEmptyBufferGeometry(got) {
		t.Fatalf("ParametricGeometry(nil) = %+v, want zero-value BufferGeometry", got)
	}
}

// isEmptyBufferGeometry reports whether g carries nothing at all.
func isEmptyBufferGeometry(g BufferGeometry) bool {
	return len(g.Positions) == 0 && len(g.Normals) == 0 && len(g.UVs) == 0 &&
		len(g.Tangents) == 0 && len(g.Indices) == 0
}

func TestParametricGeometryProducesIndexedBufferGeometry(t *testing.T) {
	const slices, stacks = 6, 5
	geometry := ParametricGeometry(parametricWrapperPlane, slices, stacks)

	wantVertices := (slices + 1) * (stacks + 1)
	if len(geometry.Positions) != wantVertices*3 {
		t.Fatalf("got %d position floats (%d vertices), want %d vertices",
			len(geometry.Positions), len(geometry.Positions)/3, wantVertices)
	}
	if len(geometry.Normals) != wantVertices*3 {
		t.Fatalf("expected normals for every vertex, got %d floats", len(geometry.Normals))
	}
	if len(geometry.UVs) != wantVertices*2 {
		t.Fatalf("expected UVs for every vertex, got %d floats", len(geometry.UVs))
	}
	wantTriangles := 2 * slices * stacks
	if len(geometry.Indices) != wantTriangles*3 {
		t.Fatalf("index count = %d, want %d", len(geometry.Indices), wantTriangles*3)
	}
	for _, index := range geometry.Indices {
		if index < 0 || index >= wantVertices {
			t.Fatalf("index %d escapes %d vertices", index, wantVertices)
		}
	}
	for i := 0; i < len(geometry.Positions); i++ {
		if math.IsNaN(geometry.Positions[i]) || math.IsInf(geometry.Positions[i], 0) {
			t.Fatalf("position float %d is non-finite", i)
		}
	}
}

func TestParametricGeometryRejectsNonFiniteSurfacesWithAnEmptyGeometry(t *testing.T) {
	surface := func(u, v float64) Vector3 {
		if u == 1 && v == 1 {
			return Vector3{X: math.NaN()}
		}
		return parametricWrapperPlane(u, v)
	}
	if got := ParametricGeometry(surface, 4, 4); !isEmptyBufferGeometry(got) {
		t.Fatalf("non-finite surface = %+v, want empty geometry with no partial mesh", got)
	}
}

func TestParametricGeometryMatchesCanonicalGenerator(t *testing.T) {
	geometry := ParametricGeometry(parametricWrapperPlane, 9, 7)
	want := geom.Parametric(func(u, v float64) (float64, float64, float64) {
		p := parametricWrapperPlane(u, v)
		return p.X, p.Y, p.Z
	}, 9, 7, geom.AttrNormals|geom.AttrUVs)
	if want == nil {
		t.Fatal("canonical geom.Parametric returned nil")
	}
	equal := func(a, b []float64) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if !equal(geometry.Positions, want.Positions) ||
		!equal(geometry.Normals, want.Normals) ||
		!equal(geometry.UVs, want.UVs) ||
		len(geometry.Indices) != len(want.Indices) {
		t.Fatal("public wrapper diverged from the canonical scene/geom generator")
	}
}

func TestParametricGeometryLoweringExpansionReportsTheDrawnVertexCount(t *testing.T) {
	const slices, stacks = 8, 8
	geometry := ParametricGeometry(parametricWrapperPlane, slices, stacks)

	vertices := bufferGeometryVertices(geometry)
	if vertices == nil {
		t.Fatal("BufferGeometry lowering returned nil")
	}
	drawn := 2 * slices * stacks * 3
	if vertices.Count != drawn {
		t.Fatalf("drawn vertex count = %d, want %d", vertices.Count, drawn)
	}
	if len(vertices.Positions) != drawn*3 || len(vertices.Normals) != drawn*3 || len(vertices.UVs) != drawn*2 {
		t.Fatal("the lowered payload must carry positions, normals, and UVs for every drawn vertex")
	}

	props := Props{Graph: NewGraph(Mesh{
		ID:       "parametric",
		Geometry: geometry,
		Material: StandardMaterial{Color: "#ffffff"},
	})}
	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.Kind != "gltf-mesh" {
		t.Fatalf("expected kind gltf-mesh, got %q", obj.Kind)
	}
	if obj.Vertices == nil || obj.Vertices.Count != drawn {
		t.Fatalf("lowered object carries %+v, want count %d", obj.Vertices, drawn)
	}
}

func TestParametricGeometryExactRaycast(t *testing.T) {
	const slices, stacks = 4, 4
	geometry := ParametricGeometry(parametricWrapperPlane, slices, stacks)

	hit, ok := genRaycast(t, geometry, Ray{
		Origin:    Vec3(0.5, 0.5, 10),
		Direction: Vec3(0, 0, -1),
	})
	if !ok {
		t.Fatal("the downward ray missed the parametric plane")
	}
	// The ray meets the plane at z = 1 + 2*0.5 - 3*0.5 = 0.5.
	if math.Abs(hit.Distance-9.5) > 1e-9 {
		t.Errorf("hit distance = %g, want 9.5", hit.Distance)
	}
	point := hit.Point
	if math.Abs(point.X-0.5) > 1e-9 || math.Abs(point.Y-0.5) > 1e-9 || math.Abs(point.Z-0.5) > 1e-9 {
		t.Errorf("hit point = (%g, %g, %g), want (0.5, 0.5, 0.5)", point.X, point.Y, point.Z)
	}

	if _, ok := genRaycast(t, geometry, Ray{
		Origin:    Vec3(0.5, 0.5, 5),
		Direction: Vec3(1, 0, 0),
	}); ok {
		t.Error("a ray parallel to the plane and offset from it must miss")
	}

	if _, ok := genRaycast(t, geometry, Ray{
		Origin:    Vec3(5, 5, 10),
		Direction: Vec3(0, 0, -1),
	}); ok {
		t.Error("a ray outside the unit square must miss the finite plane patch")
	}
}
