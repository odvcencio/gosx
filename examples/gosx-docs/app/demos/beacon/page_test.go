package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestBeaconKeepsPublicRendererDiagnosticsOutOfThePage(t *testing.T) {
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		".beacon__canvas .gosx-scene3d-unsupported",
		"place-items: center",
		"text-align: center",
		".blackglass-artifact--ledger dl > div:nth-last-child(-n + 2)",
		"display: none",
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
	if !strings.Contains(page, `<Scene3D {...data.scene} stats={false} />`) {
		t.Error("Scene3D stats must be disabled on the public route")
	}
	for _, marker := range []string{`data-gosx-scene3d-status`, `__telemetry`, `starting…`, `measuring…`} {
		if strings.Contains(page, marker) {
			t.Errorf("public page must not render debug output %q", marker)
		}
	}
	if strings.Contains(page, `<script`) {
		t.Fatal("beacon page must not add bespoke JavaScript")
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
		"Use the arrow keys to explore",
		"Use + or − to zoom",
		"Press Home to restore this view",
		`aria-label="Camera view"`,
		`aria-label="Light period"`,
	} {
		if !strings.Contains(page, marker) {
			t.Errorf("public interaction guidance missing %q", marker)
		}
	}

	props := BlackglassBeaconProgram()
	if props.Controls != scene.ControlOrbit || props.AutoRotate == nil || *props.AutoRotate {
		t.Fatalf("Beacon interaction must remain user-directed orbit: controls=%q autoRotate=%v", props.Controls, props.AutoRotate)
	}
	if props.ControlMinDistance != 6 || props.ControlMaxDistance != 48 {
		t.Fatalf("Beacon orbit bounds = %.1f..%.1f, want 6..48", props.ControlMinDistance, props.ControlMaxDistance)
	}
}
