package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestBeaconTelemetryDoesNotDependOnCSSHas(t *testing.T) {
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(css), ":has(") {
		t.Fatal("telemetry must remain truthful in browsers without CSS :has()")
	}
	for _, marker := range []string{
		".beacon__canvas .gosx-scene3d-unsupported",
		"place-items: center",
		"text-align: center",
	} {
		if !strings.Contains(string(css), marker) {
			t.Errorf("intentional renderer fallback styling missing %q", marker)
		}
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

func TestBeaconPublishesTheCuratedOrbitInteractionContract(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	page := string(source)
	for _, marker := range []string{
		"Drag or swipe to orbit",
		"scroll or pinch to zoom",
		"arrows explore",
		"+/− zoom",
		"Home restores the opening view",
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("public interaction guidance missing %q", marker)
		}
	}

	props := BlackglassBeaconProgram()
	if props.Controls != scene.ControlOrbit || props.AutoRotate == nil || *props.AutoRotate {
		t.Fatalf("Beacon interaction must remain user-directed orbit: controls=%q autoRotate=%v", props.Controls, props.AutoRotate)
	}
	if props.ControlMinDistance != 9 || props.ControlMaxDistance != 42 {
		t.Fatalf("Beacon orbit bounds = %.1f..%.1f, want 9..42", props.ControlMinDistance, props.ControlMaxDistance)
	}
}
