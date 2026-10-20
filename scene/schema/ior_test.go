package schema

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

// iorDocument carries one record of every kind that validates an IOR, so a
// missing validator call site is caught by a per-record path assertion. It is
// built as a schema.Document directly (the shape validateDocument consumes);
// the JSON route marshals this same document.
func iorDocument(value *float64) Document {
	return Document{
		Schema:             scene.SceneIRSchema,
		Objects:            []scene.ObjectIR{{ID: "a", Kind: "box", IOR: value}},
		Models:             []scene.ModelIR{{ObjectIR: scene.ObjectIR{ID: "m", IOR: value}, Src: "/m.glb"}},
		InstancedMeshes:    []scene.InstancedMeshIR{{ID: "i", Kind: "box", Count: 1, Transforms: make([]float64, 16), IOR: value}},
		InstancedGLBMeshes: []scene.InstancedGLBMeshIR{{ID: "g", Src: "/g.glb", Instances: []scene.MeshInstanceIR{{X: 1}}, IOR: value}},
	}
}

func runIORDocument(doc Document) []Diagnostic {
	var report Report
	validateDocument(&report, doc, Options{})
	return report.Diagnostics
}

func runIORJSON(t *testing.T, doc Document) Report {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	return ValidateJSON(data, Options{})
}

func iorDiagnosticPaths(diags []Diagnostic) map[string]bool {
	paths := map[string]bool{}
	for _, d := range diags {
		if strings.Contains(d.Code, "scene.material.ior") && d.Severity == Error {
			paths[d.Path] = true
		}
	}
	return paths
}

func requireAllIORPaths(t *testing.T, diags []Diagnostic) {
	t.Helper()
	paths := iorDiagnosticPaths(diags)
	for _, want := range []string{"objects[0].ior", "models[0].ior", "instancedMeshes[0].ior", "instancedGLBMeshes[0].ior"} {
		if !paths[want] {
			t.Fatalf("no Error diagnostic at %s; got %v — a validateIOR call site is missing", want, paths)
		}
	}
}

// TestSchemaIORNonFiniteViaDocument covers non-finite IOR values, which JSON
// cannot represent, by routing directly through validateDocument for every
// record kind.
func TestSchemaIORNonFiniteViaDocument(t *testing.T) {
	for _, value := range []*float64{scene.Float(math.NaN()), scene.Float(math.Inf(1)), scene.Float(math.Inf(-1))} {
		diags := runIORDocument(iorDocument(value))
		requireAllIORPaths(t, diags)
		for _, diag := range diags {
			if !strings.Contains(diag.Code, "scene.material.ior") {
				continue
			}
			if diag.Severity != Error {
				t.Fatalf("non-finite ior %v severity = %q, want %q", *value, diag.Severity, Error)
			}
			if diag.Code != "scene.material.ior_non_finite" {
				t.Fatalf("non-finite ior %v produced code %q, want scene.material.ior_non_finite", *value, diag.Code)
			}
		}
	}
}

// TestSchemaIORRepresentableViaJSON covers negative and sub-1 values through
// the public ValidateJSON route, asserts the real severity and path, and proves
// that every record kind (object, model, instancedMesh, instancedGLB) reaches a
// validateIOR call site.
func TestSchemaIORRepresentableViaJSON(t *testing.T) {
	for _, tc := range []struct {
		value float64
		code  string
	}{
		{-0.5, "scene.material.ior_negative"},
		{0.5, "scene.material.ior_out_of_range"},
	} {
		report := runIORJSON(t, iorDocument(scene.Float(tc.value)))
		if report.Valid {
			t.Fatalf("ior %v unexpectedly validated: %v", tc.value, report.Diagnostics)
		}
		requireAllIORPaths(t, report.Diagnostics)
		found := false
		for _, diag := range report.Diagnostics {
			if diag.Code != tc.code {
				continue
			}
			found = true
			if diag.Severity != Error {
				t.Fatalf("ior %v severity = %q, want %q", tc.value, diag.Severity, Error)
			}
			if !strings.Contains(diag.Path, ".ior") {
				t.Fatalf("ior %v path = %q, want a .ior path", tc.value, diag.Path)
			}
		}
		if !found {
			t.Fatalf("ior %v produced no %s diagnostic in %v", tc.value, tc.code, report.Diagnostics)
		}
	}
}

// TestSchemaIORAcceptedValues pins the authored contract: absent, zero, and
// finite values of at least one pass validation with no upper clamp — the
// public route reports Valid true with no error diagnostics.
func TestSchemaIORAcceptedValues(t *testing.T) {
	for _, value := range []*float64{nil, scene.Float(0), scene.Float(1), scene.Float(1.5), scene.Float(42)} {
		report := runIORJSON(t, iorDocument(value))
		if !report.Valid {
			t.Fatalf("ior %v rejected: %v", value, report.Diagnostics)
		}
		for _, diag := range report.Diagnostics {
			if diag.Severity == Error && strings.Contains(diag.Code, "scene.material.ior") {
				t.Fatalf("ior %v rejected with %s at %s", value, diag.Code, diag.Path)
			}
		}
	}
}
