package vm

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
)

func TestSceneParentMatrixAppliesOutsideLeafLiveMotion(t *testing.T) {
	object := sceneObject{
		X: 1, Y: 2, Z: 3,
		ScaleX: 2, ScaleY: 1, ScaleZ: 1,
		SpinZ:  math.Pi / 2,
		ShiftX: 2,
		ParentMatrix: [16]float64{
			2, 0, 0, 0,
			0, 3, 0, 0,
			0, 0, 4, 0,
			10, 20, 30, 1,
		},
		HasParentMatrix: true,
	}
	got := translatePoint(point3{X: 1}, object, spinQuatForObject(object, 1), clipTRS{}, 1)
	want := point3{X: 16, Y: 32, Z: 42}
	assertPointNear(t, got, want, 1e-12)

	// Changing the live spin changes the leaf-local result, while the same
	// parent remains the outermost affine transform.
	got = translatePoint(point3{X: 1}, object, motion.Quat{W: 1}, clipTRS{}, 0)
	want = point3{X: 20, Y: 26, Z: 42}
	assertPointNear(t, got, want, 1e-12)
}

func TestSceneParentMatrixUsesInverseTransposeForNormals(t *testing.T) {
	object := sceneObject{
		Kind: "box", Width: 2, Height: 2, Depth: 2,
		ParentMatrix: [16]float64{
			2, 0, 0, 0,
			1, 3, 0, 0,
			0, 0, -4, 0,
			0, 0, 0, 1,
		},
		HasParentMatrix: true,
	}
	got := sceneObjectWorldNormal(object, point3{X: 1}, motion.Quat{W: 1}, clipTRS{})
	want := point3{X: 3 / math.Sqrt(10), Y: -1 / math.Sqrt(10)}
	assertPointNear(t, got, want, 1e-12)
	if math.Abs(math.Sqrt(dotPoint3(got, got))-1) > 1e-12 {
		t.Fatalf("normal is not unit length: %#v", got)
	}
}

func TestSceneParentMatrixResolverCopiesAndRejectsInvalidValues(t *testing.T) {
	values := make([]any, 16)
	for index := range values {
		values[index] = float64(0)
	}
	values[0], values[5], values[10], values[15] = 1.0, 1.0, 1.0, 1.0
	matrix, ok := sceneParentMatrixFromAny(values)
	if !ok || matrix[0] != 1 || matrix[15] != 1 {
		t.Fatalf("valid matrix rejected: %#v, %v", matrix, ok)
	}
	values[0] = 9.0
	if matrix[0] != 1 {
		t.Fatal("resolved parent matrix aliases caller-owned values")
	}
	values[0] = math.NaN()
	if _, ok := sceneParentMatrixFromAny(values); ok {
		t.Fatal("NaN parent matrix accepted")
	}
	if _, ok := sceneParentMatrixFromAny(values[:15]); ok {
		t.Fatal("15-value parent matrix accepted")
	}
	values[0] = "1"
	if _, ok := sceneParentMatrixFromAny(values); ok {
		t.Fatal("numeric string in parent matrix accepted")
	}
	values[0] = 1.0
	values[3] = 1.0
	if _, ok := sceneParentMatrixFromAny(values); ok {
		t.Fatal("non-affine bottom row accepted")
	}
	values[3] = 0.0
	values[4], values[5], values[6] = values[0], values[1], values[2]
	if _, ok := sceneParentMatrixFromAny(values); ok {
		t.Fatal("singular parent matrix accepted")
	}

	const nearMax = 9e307
	for name, valid := range map[string][]float64{
		"uniform large":           {1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1e150, 0, 0, 0, 0, 1},
		"uniform small":           {1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1e-150, 0, 0, 0, 0, 1},
		"sheared reflected large": {-2e150, 0, 0, 0, 1e150, 3e150, 0, 0, 0, 0, 4e150, 0, 5, 6, 7, 1},
		"sheared reflected small": {-2e-150, 0, 0, 0, 1e-150, 3e-150, 0, 0, 0, 0, 4e-150, 0, 5, 6, 7, 1},
		"near max finite":         {nearMax, -nearMax, 0, 0, nearMax, nearMax, 0, 0, 0, 0, nearMax, 0, 0, 0, 0, 1},
		"anisotropic tiny":        {1e-297, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1},
		"anisotropic tiny shear":  {1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1},
		"transposed tiny shear":   {1e-297, 5e-298, -4e-298, 0, 0, 2e-303, 1e-303, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1},
		"reflected tiny shear":    {-1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1},
		"just above threshold":    {1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 1.000001e-303, 0, 0, 0, 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := sceneParentMatrixFromAny(valid); !ok {
				t.Fatal("valid affine parent matrix rejected")
			}
		})
	}
	for name, invalid := range map[string][]float64{
		"below determinant threshold":  {1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 0.999999e-303, 0, 0, 0, 0, 1},
		"inverse coefficient overflow": {1e-308, 0, 0, 0, 0, 1e-308, 0, 0, 0, 0, 2e-320, 0, 0, 0, 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := sceneParentMatrixFromAny(invalid); ok {
				t.Fatal("invalid affine parent matrix accepted")
			}
		})
	}
}

func TestReflectedParentBakeKeepsAuthoredFrontFaceAndNormal(t *testing.T) {
	matrix := []any{
		-2.0, 0.0, 0.0, 0.0,
		1.0, 3.0, 0.0, 0.0,
		0.0, 0.0, 1.0, 0.0,
		0.0, 0.0, 0.0, 1.0,
	}
	node := resolvedNode{Kind: "mesh", Props: map[string]any{
		"id": "reflected", "geometry": "box", "width": 2.0, "height": 2.0, "depth": 2.0,
		"parentMatrix": matrix, "wireframe": false,
	}}
	bundle := buildRenderBundle(map[string]any{}, []resolvedNode{node}, 320, 240, 0, newSpinScratch())
	if len(bundle.MeshObjects) != 1 || len(bundle.WorldMeshPositions) < 9 || len(bundle.WorldMeshNormals) < 3 {
		t.Fatalf("reflected mesh was not baked: %#v", bundle.MeshObjects)
	}
	p := func(vertex int) point3 {
		base := vertex * 3
		return point3{X: bundle.WorldMeshPositions[base], Y: bundle.WorldMeshPositions[base+1], Z: bundle.WorldMeshPositions[base+2]}
	}
	p0, p1, p2 := p(0), p(1), p(2)
	e1 := point3{X: p1.X - p0.X, Y: p1.Y - p0.Y, Z: p1.Z - p0.Z}
	e2 := point3{X: p2.X - p0.X, Y: p2.Y - p0.Y, Z: p2.Z - p0.Z}
	face := normalizePoint3(point3{
		X: e1.Y*e2.Z - e1.Z*e2.Y,
		Y: e1.Z*e2.X - e1.X*e2.Z,
		Z: e1.X*e2.Y - e1.Y*e2.X,
	})
	normal := point3{X: bundle.WorldMeshNormals[0], Y: bundle.WorldMeshNormals[1], Z: bundle.WorldMeshNormals[2]}
	if dotPoint3(face, normal) < 0.999 {
		t.Fatalf("reflected triangle winding and inverse-transpose normal disagree: face=%#v normal=%#v", face, normal)
	}
}

func assertPointNear(t *testing.T, got, want point3, tolerance float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > tolerance || math.Abs(got.Y-want.Y) > tolerance || math.Abs(got.Z-want.Z) > tolerance {
		t.Fatalf("point = %#v, want %#v", got, want)
	}
}
