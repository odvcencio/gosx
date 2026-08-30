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
}

func assertPointNear(t *testing.T, got, want point3, tolerance float64) {
	t.Helper()
	if math.Abs(got.X-want.X) > tolerance || math.Abs(got.Y-want.Y) > tolerance || math.Abs(got.Z-want.Z) > tolerance {
		t.Fatalf("point = %#v, want %#v", got, want)
	}
}
