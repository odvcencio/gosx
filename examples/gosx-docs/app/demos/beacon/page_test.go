package docs

import (
	"os"
	"strings"
	"testing"
)

func TestBeaconTelemetryDoesNotDependOnCSSHas(t *testing.T) {
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(css), ":has(") {
		t.Fatal("telemetry must remain truthful in browsers without CSS :has()")
	}

	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, marker := range []string{
		`data-gosx-scene3d-status-scope`,
		`data-gosx-scene3d-status="renderer"`,
		`data-gosx-scene3d-status="fallback"`,
		`data-gosx-scene3d-status="quality"`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("telemetry binding missing %q", marker)
		}
	}
	if strings.Contains(page, `<script`) {
		t.Fatal("beacon must use shared Scene3D status bindings instead of bespoke JavaScript")
	}
}
