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
	const nearMax = 9e307
	validSmall := []float64{
		1e-6, 0, 0, 0,
		1e-6, 2e-6, 0, 0,
		0, 0, -3e-6, 0,
		4, 5, 6, 1,
	}
	for name, testCase := range map[string]struct {
		matrix []float64
		valid  bool
	}{
		"valid small shear reflection": {matrix: validSmall, valid: true},
		"valid large shear reflection": {matrix: []float64{-2e150, 0, 0, 0, 1e150, 3e150, 0, 0, 0, 0, 4e150, 0, 4, 5, 6, 1}, valid: true},
		"valid near max finite":        {matrix: []float64{nearMax, -nearMax, 0, 0, nearMax, nearMax, 0, 0, 0, 0, nearMax, 0, 0, 0, 0, 1}, valid: true},
		"valid anisotropic tiny":       {matrix: []float64{1e-297, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1}, valid: true},
		"valid anisotropic tiny shear": {matrix: []float64{1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1}, valid: true},
		"valid transposed tiny shear":  {matrix: []float64{1e-297, 5e-298, -4e-298, 0, 0, 2e-303, 1e-303, 0, 0, 0, 2e-303, 0, 0, 0, 0, 1}, valid: true},
		"valid reflected tiny shear":   {matrix: []float64{-1e-297, 0, 0, 0, 5e-298, 2e-303, 0, 0, -4e-298, 1e-303, 2e-303, 0, 0, 0, 0, 1}, valid: true},
		"valid just above threshold":   {matrix: []float64{1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 1.000001e-303, 0, 0, 0, 0, 1}, valid: true},
		"below determinant threshold":  {matrix: []float64{1e-297, 0, 0, 0, 0, 1e-303, 0, 0, 0, 0, 0.999999e-303, 0, 0, 0, 0, 1}},
		"inverse coefficient overflow": {matrix: []float64{1e-308, 0, 0, 0, 0, 1e-308, 0, 0, 0, 0, 2e-320, 0, 0, 0, 0, 1}},
		"non affine bottom row":        {matrix: append([]float64(nil), validSmall...)},
		"singular":                     {matrix: append([]float64(nil), validSmall...)},
	} {
		matrix := testCase.matrix
		switch name {
		case "non affine bottom row":
			matrix[3] = 1
		case "singular":
			matrix[4], matrix[5], matrix[6] = matrix[0], matrix[1], matrix[2]
		}
		report := Report{}
		validateDocument(&report, Document{Objects: []scene.ObjectIR{{ID: name, Kind: "box", ParentMatrix: matrix}}}, Options{})
		if gotValid := !hasError(report.Diagnostics); gotValid != testCase.valid {
			t.Fatalf("%s validity = %v, diagnostics: %+v", name, gotValid, report.Diagnostics)
		}
	}
}
