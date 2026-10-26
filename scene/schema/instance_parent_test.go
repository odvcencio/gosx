package schema

import "testing"

func TestInstanceParentAndPaletteValidation(t *testing.T) {
	for _, src := range []string{
		`{"objects":[{"id":"ship","kind":"box"}],"instancedMeshes":[{"id":"windows","kind":"box","count":0,"parentNode":"ship"}]}`,
		`{"objects":[{"id":"ship/hull","kind":"box"}],"instancedMeshes":[{"id":"windows","kind":"box","count":0,"parentNode":"ship"}]}`,
		`{"instancedGLBMeshes":[{"id":"crowd","src":"/a.glb","palette":["#ff0000"],"instances":[{"tint":1},{}]}]}`,
	} {
		if report := ValidateJSON([]byte(src), Options{Strict: true}); !report.Valid {
			t.Fatalf("rejected %s: %+v", src, report.Diagnostics)
		}
	}
	for _, src := range []string{
		`{"instancedMeshes":[{"id":"windows","kind":"box","count":0,"parentNode":"missing"}]}`,
		`{"instancedGLBMeshes":[{"id":"crowd","src":"/a.glb","instances":[{"tint":1}]}]}`,
		`{"instancedGLBMeshes":[{"id":"crowd","src":"/a.glb","palette":[" "],"instances":[{}]}]}`,
		`{"instancedGLBMeshes":[{"id":"crowd","src":"/a.glb","palette":["red"],"instances":[{"tint":-1}]}]}`,
	} {
		if report := ValidateJSON([]byte(src), Options{Strict: true}); report.Valid {
			t.Fatalf("accepted %s", src)
		}
	}
	src := `{"instancedMeshes":[{"id":"windows","kind":"box","count":0,"parentNode":"missing"}]}`
	if report := ValidateJSON([]byte(src), Options{}); !report.Valid {
		t.Fatalf("live missing parent rejected: %+v", report.Diagnostics)
	}
}
