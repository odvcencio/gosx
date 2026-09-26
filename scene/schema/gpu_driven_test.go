package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

const gpuDrivenTestMesh = `"instancedMeshes":[{"id":"crates","kind":"box","count":1,"transforms":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]}]`

func gpuDrivenTestDocument(extra string) []byte {
	return []byte(`{"schema":"` + scene.SceneIRSchema + `",` + extra + `}`)
}

func TestValidateJSONAcceptsGPUDriven(t *testing.T) {
	for _, mode := range []string{
		`{}`,
		`{"shadowCulling":true}`,
		`{"occlusion":true,"shadowCulling":false}`,
		`null`,
	} {
		report := ValidateJSON(gpuDrivenTestDocument(gpuDrivenTestMesh+`,"gpuDriven":`+mode), Options{Strict: true})
		if !report.Valid || len(report.Diagnostics) != 0 {
			t.Fatalf("gpuDriven %s: expected a clean valid report: %+v", mode, report.Diagnostics)
		}
	}
}

func TestValidateJSONRejectsMalformedGPUDriven(t *testing.T) {
	cases := []struct {
		mode string
		code string
		path string
	}{
		{`true`, "scene.gpu_driven.invalid", "gpuDriven"},
		{`[]`, "scene.gpu_driven.invalid", "gpuDriven"},
		{`{"occlusion":"yes"}`, "scene.gpu_driven.invalid_flag", "gpuDriven.occlusion"},
		{`{"shadowCulling":1}`, "scene.gpu_driven.invalid_flag", "gpuDriven.shadowCulling"},
	}
	for _, c := range cases {
		report := ValidateJSON(gpuDrivenTestDocument(gpuDrivenTestMesh+`,"gpuDriven":`+c.mode), Options{})
		if report.Valid {
			t.Fatalf("gpuDriven %s: expected an invalid report", c.mode)
		}
		if !hasDiagnostic(report, Error, c.code, c.path) {
			t.Fatalf("gpuDriven %s: expected error %s at %s: %+v", c.mode, c.code, c.path, report.Diagnostics)
		}
	}
}

func TestValidateJSONWarnsOnInertGPUDriven(t *testing.T) {
	unknown := ValidateJSON(gpuDrivenTestDocument(gpuDrivenTestMesh+`,"gpuDriven":{"lod":true}`), Options{})
	if !unknown.Valid || !hasDiagnostic(unknown, Warn, "scene.gpu_driven.unknown_field", "gpuDriven.lod") {
		t.Fatalf("expected a valid report with an unknown-field warning: %+v", unknown.Diagnostics)
	}
	empty := ValidateJSON(gpuDrivenTestDocument(`"gpuDriven":{}`), Options{})
	if !empty.Valid || !hasDiagnostic(empty, Warn, "scene.gpu_driven.no_instanced_meshes", "gpuDriven") {
		t.Fatalf("expected a valid report with a no-instanced-meshes warning: %+v", empty.Diagnostics)
	}
}

func TestValidateJSONRejectsNullGPUDrivenFlags(t *testing.T) {
	for _, key := range []string{"occlusion", "shadowCulling"} {
		report := ValidateJSON(gpuDrivenTestDocument(gpuDrivenTestMesh+`,"gpuDriven":{"`+key+`":null}`), Options{})
		if report.Valid || !hasDiagnostic(report, Error, "scene.gpu_driven.invalid_flag", "gpuDriven."+key) {
			t.Fatalf("gpuDriven.%s: expected an invalid-flag error for null: %+v", key, report)
		}
	}
}

// The typed authoring path must always produce a document the validator
// accepts without a diagnostic about the mode.
func TestValidateJSONAcceptsLoweredGPUDriven(t *testing.T) {
	props := scene.Props{
		GPUDriven: &scene.GPUDriven{Occlusion: true},
		Graph: scene.NewGraph(scene.InstancedMesh{
			ID:        "crates",
			Count:     2,
			Geometry:  scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Positions: []scene.Vector3{{X: -1}, {X: 1}},
		}),
	}
	data, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateJSON(data, Options{})
	for _, diag := range report.Diagnostics {
		if strings.HasPrefix(diag.Code, "scene.gpu_driven.") {
			t.Fatalf("lowered gpuDriven produced %s: %+v", diag.Code, diag)
		}
	}
	if !report.Valid {
		t.Fatalf("lowered scene is invalid: %+v", report.Diagnostics)
	}
}

func TestJSONSchemaIncludesGPUDriven(t *testing.T) {
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(JSONSchema(), &doc); err != nil {
		t.Fatal(err)
	}
	var mode struct {
		Type       []string `json:"type"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(doc.Properties["gpuDriven"], &mode); err != nil {
		t.Fatal(err)
	}
	if strings.Join(mode.Type, ",") != "object,null" {
		t.Fatalf("schema gpuDriven type = %v, want object and null", mode.Type)
	}
	for _, key := range []string{"occlusion", "shadowCulling"} {
		if mode.Properties[key].Type != "boolean" {
			t.Fatalf("schema gpuDriven.%s type = %q, want boolean", key, mode.Properties[key].Type)
		}
	}
}

func hasDiagnostic(report Report, severity Severity, code, path string) bool {
	for _, diag := range report.Diagnostics {
		if diag.Severity == severity && diag.Code == code && diag.Path == path {
			return true
		}
	}
	return false
}
