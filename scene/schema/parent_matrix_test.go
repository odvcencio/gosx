package schema

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestValidateParentMatrixShapeAndFiniteness(t *testing.T) {
	identity := `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]`
	for _, family := range []struct {
		name   string
		prefix string
		suffix string
	}{
		{name: "object", prefix: `{"objects":[{"id":"object","kind":"box","parentMatrix":`, suffix: `}]}`},
		{name: "model", prefix: `{"models":[{"id":"model","src":"model.glb","parentMatrix":`, suffix: `}]}`},
		{name: "points", prefix: `{"points":[{"id":"points","count":0,"parentMatrix":`, suffix: `}]}`},
		{name: "glb instance", prefix: `{"instancedGLBMeshes":[{"id":"batch","src":"model.glb","instances":[{"parentMatrix":`, suffix: `}]}]}`},
	} {
		t.Run(family.name+"/valid", func(t *testing.T) {
			if report := ValidateJSON([]byte(family.prefix+identity+family.suffix), Options{}); !report.Valid {
				t.Fatalf("valid matrix rejected: %+v", report.Diagnostics)
			}
		})
		for name, matrix := range map[string]string{
			"15 values": `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0]`,
			"17 values": `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1,0]`,
			"string":    `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,"x"]`,
			"null":      `[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,null]`,
		} {
			t.Run(family.name+"/"+name, func(t *testing.T) {
				report := ValidateJSON([]byte(family.prefix+matrix+family.suffix), Options{})
				if report.Valid {
					t.Fatalf("invalid %s matrix accepted", name)
				}
			})
		}
	}
}

func TestValidateParentMatrixRejectsTypedNonFiniteValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		matrix := make([]float64, 16)
		matrix[0], matrix[5], matrix[10], matrix[15] = 1, 1, 1, 1
		matrix[6] = value
		report := Report{}
		validateDocument(&report, Document{Objects: []scene.ObjectIR{{ID: "object", Kind: "box", ParentMatrix: matrix}}}, Options{})
		found := false
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Code == "scene.transform.invalid_parent_matrix" && strings.Contains(diagnostic.Path, "parentMatrix") {
				found = true
			}
		}
		if !found {
			t.Fatalf("non-finite value %v did not produce parent matrix diagnostic: %+v", value, report.Diagnostics)
		}
	}
}

func TestValidateParentMatrixRequiresAffineAndScaleInvariantInvertible(t *testing.T) {
	validSmall := []float64{
		1e-6, 0, 0, 0,
		1e-6, 2e-6, 0, 0,
		0, 0, -3e-6, 0,
		4, 5, 6, 1,
	}
	for name, matrix := range map[string][]float64{
		"valid small shear reflection": validSmall,
		"non affine bottom row":        append([]float64(nil), validSmall...),
		"singular":                     append([]float64(nil), validSmall...),
	} {
		switch name {
		case "non affine bottom row":
			matrix[3] = 1
		case "singular":
			matrix[4], matrix[5], matrix[6] = matrix[0], matrix[1], matrix[2]
		}
		report := Report{}
		validateDocument(&report, Document{Objects: []scene.ObjectIR{{ID: name, Kind: "box", ParentMatrix: matrix}}}, Options{})
		if gotValid := !hasError(report.Diagnostics); gotValid != (name == "valid small shear reflection") {
			t.Fatalf("%s validity = %v, diagnostics: %+v", name, gotValid, report.Diagnostics)
		}
	}
}
