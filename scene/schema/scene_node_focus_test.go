package schema

import (
	"strings"
	"testing"
)

func TestValidateInteractiveNodesRequireLabel(t *testing.T) {
	for _, raw := range []string{
		`{"objects":[{"id":"mesh","kind":"box","interactive":true}]}`,
		`{"models":[{"id":"model","src":"/model.glb","interactive":true,"label":"  "}]}`,
	} {
		report := ValidateJSON([]byte(raw), Options{})
		if report.Valid {
			t.Fatalf("interactive scene node without a non-empty label was accepted: %s", raw)
		}
		found := false
		for _, diagnostic := range report.Diagnostics {
			if strings.Contains(strings.ToLower(diagnostic.Message), "interactive") && strings.Contains(strings.ToLower(diagnostic.Message), "label") {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected clear Interactive/Label diagnostic for %s: %+v", raw, report.Diagnostics)
		}
	}
}

func TestValidateInteractiveNodesAcceptLabel(t *testing.T) {
	report := ValidateJSON([]byte(`{"objects":[{"id":"mesh","kind":"box","interactive":true,"label":"Accessible mesh"}],"models":[{"id":"model","src":"/model.glb","interactive":true,"label":"Accessible model"}]}`), Options{})
	if !report.Valid {
		t.Fatalf("valid interactive labels rejected: %+v", report.Diagnostics)
	}
}
