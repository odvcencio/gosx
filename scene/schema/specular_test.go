package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func specColor(v [3]float64) *[3]float64 { return &v }

// specularDocument carries one record of every specular-bearing family so
// every validateSpecular call site is exercised together.
func specularDocument(intensity *float64, color *[3]float64) Document {
	return Document{
		Schema:             scene.SceneIRSchema,
		Objects:            []scene.ObjectIR{{ID: "a", Kind: "box", SpecularIntensity: intensity, SpecularColor: color}},
		Models:             []scene.ModelIR{{ObjectIR: scene.ObjectIR{ID: "m", SpecularIntensity: intensity, SpecularColor: color}, Src: "/m.glb"}},
		InstancedMeshes:    []scene.InstancedMeshIR{{ID: "i", Kind: "box", Count: 1, Transforms: make([]float64, 16), SpecularIntensity: intensity, SpecularColor: color}},
		InstancedGLBMeshes: []scene.InstancedGLBMeshIR{{ID: "g", Src: "/g.glb", Instances: []scene.MeshInstanceIR{{X: 1}}, SpecularIntensity: intensity, SpecularColor: color}},
	}
}

func validateDoc(t *testing.T, doc Document) Report {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	return ValidateJSON(data, Options{})
}

func runDocDirect(doc Document) []Diagnostic {
	var report Report
	validateDocument(&report, doc, Options{})
	return report.Diagnostics
}

func requireSpecularPaths(t *testing.T, diags []Diagnostic, wantPaths ...string) {
	t.Helper()
	got := map[string]string{}
	for _, d := range diags {
		if d.Severity == Error && strings.Contains(d.Code, "scene.material.specular") {
			got[d.Path] = d.Code
		}
	}
	for _, want := range wantPaths {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing specular Error diagnostic at %s; got %v", want, got)
		}
	}
}

var (
	intensityPaths = []string{
		"objects[0].specularIntensity",
		"models[0].specularIntensity",
		"instancedMeshes[0].specularIntensity",
		"instancedGLBMeshes[0].specularIntensity",
	}
	colorPaths = []string{
		"objects[0].specularColor[0]",
		"models[0].specularColor[0]",
		"instancedMeshes[0].specularColor[0]",
		"instancedGLBMeshes[0].specularColor[0]",
	}
)

// TestSchemaSpecularRejected pins representable invalid values: intensity and
// color are diagnosed independently, and together both appear.
func TestSchemaSpecularRejected(t *testing.T) {
	cases := []struct {
		name          string
		intensity     *float64
		color         *[3]float64
		wantIntensity bool
		wantColor     bool
	}{
		{"negativeIntensity", scene.Float(-0.25), nil, true, false},
		{"overOneIntensity", scene.Float(1.5), nil, true, false},
		{"negativeColor", nil, specColor([3]float64{-1, 0, 0}), false, true},
		{"bothInvalid", scene.Float(2), specColor([3]float64{-1, -1, -1}), true, true},
	}
	for _, tc := range cases {
		report := validateDoc(t, specularDocument(tc.intensity, tc.color))
		if report.Valid {
			t.Fatalf("%s: unexpectedly valid: %v", tc.name, report.Diagnostics)
		}
		var want []string
		if tc.wantIntensity {
			want = append(want, intensityPaths...)
		}
		if tc.wantColor {
			want = append(want, colorPaths...)
		}
		requireSpecularPaths(t, report.Diagnostics, want...)
	}
}

// TestSchemaSpecularNonFinite covers non-finite values, which JSON cannot
// represent, routed directly through validateDocument.
func TestSchemaSpecularNonFinite(t *testing.T) {
	diags := runDocDirect(specularDocument(scene.Float(math.NaN()), specColor([3]float64{math.Inf(1), 0, 0})))
	requireSpecularPaths(t, diags, append(append([]string{}, intensityPaths...), colorPaths...)...)
}

// TestSchemaSpecularAccepted pins the authored contract: absent, zero, one,
// and HDR linear colors all validate.
func TestSchemaSpecularAccepted(t *testing.T) {
	cases := []struct {
		name      string
		intensity *float64
		color     *[3]float64
	}{
		{"absent", nil, nil},
		{"zeroBlack", scene.Float(0), specColor([3]float64{0, 0, 0})},
		{"oneWhite", scene.Float(1), specColor([3]float64{1, 1, 1})},
		{"hdr", scene.Float(0.5), specColor([3]float64{2.5, 1, 0.5})},
	}
	for _, tc := range cases {
		report := validateDoc(t, specularDocument(tc.intensity, tc.color))
		if !report.Valid {
			t.Fatalf("%s: rejected: %v", tc.name, report.Diagnostics)
		}
		for _, d := range report.Diagnostics {
			if (d.Severity == Error || d.Severity == Fatal) && strings.Contains(d.Code, "scene.material.specular") {
				t.Fatalf("%s: unexpected specular diagnostic %s at %s", tc.name, d.Code, d.Path)
			}
		}
	}
}

func specularFamilies() []struct {
	field  string
	record string
} {
	return []struct {
		field  string
		record string
	}{
		{"objects", `"id":"a","kind":"box"`},
		{"models", `"id":"m","src":"/m.glb"`},
		{"instancedMeshes", `"id":"i","kind":"box","count":1`},
		{"instancedGLBMeshes", `"id":"g","src":"/g.glb"`},
	}
}

func rawSpecularJSON(t *testing.T, family, record, extra string) []byte {
	t.Helper()
	return []byte(fmt.Sprintf(`{"schema":%q,%q:[{%s%s}]}`, scene.SceneIRSchema, family, record, extra))
}

// TestSchemaSpecularRawColorShape covers shapes encoding/json silently
// accepts: short arrays (zero-padded), long arrays, and null elements
// (zeroed). Each family must yield the shape diagnostic.
func TestSchemaSpecularRawColorShape(t *testing.T) {
	cases := []struct{ name, color string }{
		{"short", `"specularColor":[1,2]`},
		{"long", `"specularColor":[1,2,3,4]`},
		{"hugeFourth", `"specularColor":[1,2,3,1e9999]`},
		{"nullElement", `"specularColor":[1,null,3]`},
	}
	for _, family := range specularFamilies() {
		for _, tc := range cases {
			report := ValidateJSON(rawSpecularJSON(t, family.field, family.record, ","+tc.color), Options{})
			found := false
			for _, d := range report.Diagnostics {
				if d.Code == "scene.material.specular_color_invalid_shape" && strings.HasPrefix(d.Path, family.field+"[0].specularColor") {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s/%s: no shape diagnostic: %v", family.field, tc.name, report.Diagnostics)
			}
		}
	}
}

// TestSchemaSpecularWrongTypeDecoderRejection covers wrong-typed channels,
// which the existing decoder rejects fatally before any shape scan.
func TestSchemaSpecularWrongTypeDecoderRejection(t *testing.T) {
	wrongColors := []string{`"red"`, `{"r":1,"g":1,"b":1}`, `1`, `true`, `[1,"bad",3]`, `[1,true,3]`}
	for _, family := range specularFamilies() {
		for _, color := range wrongColors {
			report := ValidateJSON(rawSpecularJSON(t, family.field, family.record, `,"specularColor":`+color), Options{})
			requireFatalOnly(t, family.field, color, report)
		}
		report := ValidateJSON(rawSpecularJSON(t, family.field, family.record, `,"specularIntensity":"high"`), Options{})
		requireFatalOnly(t, family.field, "intensity", report)
	}
}

func requireFatalOnly(t *testing.T, family, what string, report Report) {
	t.Helper()
	hasFatal, hasShape := false, false
	for _, d := range report.Diagnostics {
		if d.Code == "scene.schema.invalid_json" && d.Severity == Fatal {
			hasFatal = true
		}
		if d.Code == "scene.material.specular_color_invalid_shape" {
			hasShape = true
		}
	}
	if !hasFatal {
		t.Fatalf("%s/%s: expected fatal decode diagnostic, got %v", family, what, report.Diagnostics)
	}
	if hasShape {
		t.Fatalf("%s/%s: unexpected post-decode shape diagnostic: %v", family, what, report.Diagnostics)
	}
}

// TestSchemaSpecularWholeNullOptional pins that whole-value nulls are absent,
// not shape or value errors.
func TestSchemaSpecularWholeNullOptional(t *testing.T) {
	report := ValidateJSON(rawSpecularJSON(t, "objects", `"id":"a","kind":"box"`, `,"specularIntensity":null,"specularColor":null`), Options{})
	if !report.Valid {
		t.Fatalf("whole-null optional specular rejected: %v", report.Diagnostics)
	}
	for _, d := range report.Diagnostics {
		if strings.Contains(d.Code, "scene.material.specular") {
			t.Fatalf("whole-null optional produced %s at %s", d.Code, d.Path)
		}
	}
}
