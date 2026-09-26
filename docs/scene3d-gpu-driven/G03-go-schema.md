# G03 — JSON schema and Go validator for `gpuDriven` (repo: gosx)

Depends on: G02 (the test lowers a typed scene).

## Goal

`scene/schema/schema.json` documents the field, and `schema.ValidateJSON`
(used by `gosx scene validate` and `scene/inspect`) checks it:

| Input | Diagnostic | Severity |
|---|---|---|
| absent or `null` | none | — |
| not an object (`true`, `[]`, `"x"`) | `scene.gpu_driven.invalid` at `gpuDriven` | Error |
| `occlusion` / `shadowCulling` not a boolean | `scene.gpu_driven.invalid_flag` at `gpuDriven.<key>` | Error |
| any other key | `scene.gpu_driven.unknown_field` at `gpuDriven.<key>` | Warn |
| valid mode but `instancedMeshes` empty | `scene.gpu_driven.no_instanced_meshes` at `gpuDriven` | Warn |

`Document.GPUDriven` is a `json.RawMessage`, not `*scene.GPUDrivenIR`. A typed
field would turn `"occlusion": "yes"` into a whole-document decode failure
(`scene.schema.invalid_json`, Fatal) instead of a pointed diagnostic.

## Step 1 — `scene/schema/schema.json`

Anchor (exactly once, in the top-level `properties`):

```json
    "shadowMaxPixels": {
      "type": "integer",
      "minimum": 0
    },
```

Insert directly after it:

```json
    "gpuDriven": {
      "type": "object",
      "additionalProperties": true,
      "properties": {
        "occlusion": { "type": "boolean" },
        "shadowCulling": { "type": "boolean" }
      }
    },
```

`additionalProperties: true` matches every other object in this schema; the Go
validator warns on unknown keys instead.

## Step 2 — `scene/schema/validate.go`: four edits

2a. Imports. Anchor (exactly once):

```go
	"math"
	"strings"
```

Replace with:

```go
	"math"
	"sort"
	"strings"
```

2b. The document field. Anchor (exactly once, in `type Document struct`):

```go
	ShadowMaxPixels    int                        `json:"shadowMaxPixels,omitempty"`
```

Insert directly after it:

```go
	GPUDriven          json.RawMessage            `json:"gpuDriven,omitempty"`
```

2c. The call. Anchor (exactly once, in `validateDocument`):

```go
	if doc.ShadowMaxPixels < 0 {
		report.add(Error, "scene.shadow.invalid_max_pixels", "shadowMaxPixels must not be negative", "shadowMaxPixels", "", nil)
	}
```

Insert directly after it:

```go
	validateGPUDriven(report, doc.GPUDriven, len(doc.InstancedMeshes))
```

2d. The function. Anchor (exactly once):

```go
func validateParentMatrixRaw(
```

Insert directly BEFORE that line:

```go
// validateGPUDriven checks the optional gpuDriven renderer mode. The runtime
// reads two booleans. A value of the wrong type is an error, because the
// author asked for a mode that will not engage as written. An unknown key is
// a warning, and so is a mode on a scene with no instanced mesh, because the
// mode has nothing to act on there.
func validateGPUDriven(report *Report, raw json.RawMessage, instancedMeshes int) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		report.add(Error, "scene.gpu_driven.invalid", "gpuDriven must be an object", "gpuDriven", "", nil)
		return
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "occlusion", "shadowCulling":
			var value bool
			if json.Unmarshal(fields[key], &value) != nil {
				report.add(Error, "scene.gpu_driven.invalid_flag", "gpuDriven."+key+" must be a boolean", "gpuDriven."+key, "", nil)
			}
		default:
			report.add(Warn, "scene.gpu_driven.unknown_field", "gpuDriven field is not recognized", "gpuDriven."+key, "", map[string]any{"field": key})
		}
	}
	if instancedMeshes == 0 {
		report.add(Warn, "scene.gpu_driven.no_instanced_meshes", "gpuDriven has no effect: the scene declares no instancedMeshes", "gpuDriven", "", nil)
	}
}

```

(Keep one blank line between the new function's closing brace and
`func validateParentMatrixRaw(`.)

## Step 3 — create `scene/schema/gpu_driven_test.go`

Exact content:

```go
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
		Properties map[string]struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(JSONSchema(), &doc); err != nil {
		t.Fatal(err)
	}
	mode, ok := doc.Properties["gpuDriven"]
	if !ok || mode.Type != "object" {
		t.Fatalf("schema gpuDriven = %+v, want an object property", mode)
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
```

`hasDiagnostic` is new; the package already has `hasCode`, which ignores
severity and path, so do not reuse it here.

## Verify

```sh
gofmt -l scene/schema           # prints nothing
go vet ./scene/schema
go test ./scene/schema ./scene/inspect -count=1
go test ./cmd/gosx -run Scene -count=1
git diff --check
```

Do not run the whole `./cmd/gosx` package as a check: several of its build
tests need TinyGo on PATH and fail without it at the baseline too.

## Commit

`add(scene): validate the gpuDriven scene mode`

- document gpuDriven in the SceneIR JSON schema
- report malformed flags as errors and inert or unknown keys as warnings
