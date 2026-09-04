package schema

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

// cutoffDocument carries one record of every kind that validates an alpha
// cutoff, so a missing validator call site is caught by a per-record path
// assertion.
func cutoffDocument(value scene.AlphaCutoff) Document {
	return Document{
		Schema:             scene.SceneIRSchema,
		Objects:            []scene.ObjectIR{{ID: "a", Kind: "box", AlphaCutoff: value}},
		Models:             []scene.ModelIR{{ObjectIR: scene.ObjectIR{ID: "m", AlphaCutoff: value}, Src: "/m.glb"}},
		InstancedMeshes:    []scene.InstancedMeshIR{{ID: "i", Kind: "box", Count: 1, Transforms: make([]float64, 16), AlphaCutoff: value}},
		InstancedGLBMeshes: []scene.InstancedGLBMeshIR{{ID: "g", Src: "/g.glb", Instances: []scene.MeshInstanceIR{{X: 1}}, AlphaCutoff: value}},
	}
}

func cutoffDiagnosticPaths(diags []Diagnostic) map[string]bool {
	paths := map[string]bool{}
	for _, d := range diags {
		if strings.Contains(d.Code, "scene.material.alpha_cutoff") && d.Severity == Error {
			paths[d.Path] = true
		}
	}
	return paths
}

func requireAllCutoffPaths(t *testing.T, diags []Diagnostic) {
	t.Helper()
	paths := cutoffDiagnosticPaths(diags)
	for _, want := range []string{"objects[0].alphaCutoff", "models[0].alphaCutoff", "instancedMeshes[0].alphaCutoff", "instancedGLBMeshes[0].alphaCutoff"} {
		if !paths[want] {
			t.Fatalf("no Error diagnostic at %s; got %v — a validateAlphaCutoff call site is missing", want, paths)
		}
	}
}

// TestSchemaAlphaCutoffNonFiniteViaDocument covers non-finite constructed
// values, which JSON cannot represent, for every record kind.
func TestSchemaAlphaCutoffNonFiniteViaDocument(t *testing.T) {
	for _, value := range []scene.AlphaCutoff{scene.Cutoff(math.NaN()), scene.Cutoff(math.Inf(1)), scene.Cutoff(math.Inf(-1))} {
		var report Report
		validateDocument(&report, cutoffDocument(value), Options{})
		requireAllCutoffPaths(t, report.Diagnostics)
		for _, diag := range report.Diagnostics {
			if !strings.Contains(diag.Code, "scene.material.alpha_cutoff") {
				continue
			}
			if diag.Severity != Error || diag.Code != "scene.material.alpha_cutoff_non_finite" {
				t.Fatalf("non-finite cutoff produced %q/%q", diag.Severity, diag.Code)
			}
		}
	}
}

// TestSchemaAlphaCutoffNegativeViaDocument covers negative constructed values
// and asserts the precise per-record diagnostic path for all four records.
func TestSchemaAlphaCutoffNegativeViaDocument(t *testing.T) {
	var report Report
	validateDocument(&report, cutoffDocument(scene.Cutoff(-0.5)), Options{})
	if report.Valid {
		t.Fatalf("negative alphaCutoff unexpectedly validated: %v", report.Diagnostics)
	}
	requireAllCutoffPaths(t, report.Diagnostics)
	for _, diag := range report.Diagnostics {
		if diag.Code == "scene.material.alpha_cutoff_negative" && diag.Severity != Error {
			t.Fatalf("negative cutoff severity = %q, want error", diag.Severity)
		}
	}
}

// TestSchemaAlphaCutoffAcceptedValues pins the authored contract: omitted,
// explicit disable, zero, and finite non-negative values (including above 1)
// pass validation.
func TestSchemaAlphaCutoffAcceptedValues(t *testing.T) {
	for _, value := range []scene.AlphaCutoff{{}, scene.CutoffDisabled(), scene.Cutoff(0), scene.Cutoff(0.5), scene.Cutoff(1), scene.Cutoff(1.5), scene.Cutoff(42)} {
		var report Report
		validateDocument(&report, cutoffDocument(value), Options{})
		for _, diag := range report.Diagnostics {
			if diag.Severity == Error && strings.Contains(diag.Code, "scene.material.alpha_cutoff") {
				t.Fatalf("alphaCutoff %v rejected with %s at %s", value, diag.Code, diag.Path)
			}
		}
	}
}

// TestSchemaAlphaCutoffInvalidJSON proves an invalid numeric cutoff in wire
// JSON is a decode failure, not a silent downgrade to disabled or omitted.
func TestSchemaAlphaCutoffInvalidJSON(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"box","alphaCutoff":-1}]}`,
		`{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"box","alphaCutoff":"high"}]}`,
	} {
		report := ValidateJSON([]byte(raw), Options{})
		if report.Valid {
			t.Fatalf("invalid alphaCutoff JSON validated: %s", raw)
		}
		found := false
		for _, diag := range report.Diagnostics {
			if diag.Code == "scene.schema.invalid_json" {
				found = true
			}
		}
		if !found {
			t.Fatalf("invalid alphaCutoff JSON produced no decode diagnostic: %v", report.Diagnostics)
		}
	}
	// Explicit null and numeric values decode cleanly.
	ok := `{"schema":"gosx.scene3d.ir.v1","objects":[{"id":"a","kind":"box","alphaCutoff":null},{"id":"b","kind":"box","alphaCutoff":0.5}]}`
	report := ValidateJSON([]byte(ok), Options{})
	if !report.Valid {
		t.Fatalf("null/numeric alphaCutoff rejected: %v", report.Diagnostics)
	}
}
